/* Two queued decoded buffers: latest already-due picture and nearest future
 * picture. When all are future, protect the nearest and retain the newest.
 * This is an asynchronous raw-only boundary; encoded references are untouched. */
typedef struct {GstBuffer *buffer;guint64 target,order;} AdmissionSlot;
typedef struct {
  GstElement parent;
  GstPad *sink,*src;
  GMutex lock;GCond changed;GThread *worker;
  AdmissionSlot slots[2];guint count,channel;
  guint64 order,received,dropped,released,generation;
  gboolean stopping,flushing,busy,eos;
  GstFlowReturn flow;
} GstEufyAdmission;
typedef struct {GstElementClass parent;} GstEufyAdmissionClass;
G_DEFINE_TYPE(GstEufyAdmission,gst_eufy_admission,GST_TYPE_ELEMENT)
static guint64 admission_now(GstEufyAdmission *self) {
  GstClock *clock=gst_element_get_clock(GST_ELEMENT(self));
  if(!clock)return 0;
  guint64 now=gst_clock_get_time(clock)-gst_element_get_base_time(GST_ELEMENT(self));
  gst_object_unref(clock);return now;
}
/* Caller holds lock. Release refs after unlocking: decoder-pool callbacks must
 * never run inside the queue's mutex. */
static guint admission_clear(GstEufyAdmission *self,GstBuffer **dropped) {
  guint n=self->count;for(guint i=0;i<n;i++)dropped[i]=self->slots[i].buffer;
  self->count=0;self->dropped+=n;return n;
}
static void admission_unref(GstBuffer **buffers,guint n) {
  for(guint i=0;i<n;i++)gst_buffer_unref(buffers[i]);
}
static gpointer admission_worker(gpointer data) {
  GstEufyAdmission *self=data;
  g_mutex_lock(&self->lock);
  while(!self->stopping) {
    if(self->flushing || (!self->count && !self->eos)) {g_cond_wait(&self->changed,&self->lock);continue;}
    if(!self->count && self->eos) {
      self->eos=FALSE;self->flow=GST_FLOW_EOS;self->busy=TRUE;
      g_mutex_unlock(&self->lock);gst_pad_push_event(self->src,gst_event_new_eos());g_mutex_lock(&self->lock);
      self->busy=FALSE;g_cond_broadcast(&self->changed);continue;
    }
    guint64 now=admission_now(self);gint due=-1;guint nearest=0;
    for(guint i=0;i<self->count;i++) {
      if(self->slots[i].target<self->slots[nearest].target)nearest=i;
      if(self->slots[i].target<=now && (due<0 || self->slots[i].order>self->slots[due].order))due=i;
    }
    if(due<0) {
      guint64 wait=self->slots[nearest].target>now?self->slots[nearest].target-now:0;
      /* Recheck on every arrival/event and at most every allowance. No need for
       * another picture or EOS to release a finite lone frame. */
      g_cond_wait_until(&self->changed,&self->lock,g_get_monotonic_time()+(gint64)(MIN(wait,(guint64)PACING_BOUND_NS)/1000)+1);
      continue;
    }
    AdmissionSlot selected=self->slots[due];GstBuffer *buffer=selected.buffer,*superseded[2];guint obsolete=0;
    self->count--;self->slots[due]=self->slots[self->count];
    for(guint i=0;i<self->count;) {
      if(self->slots[i].order<selected.order) {
        superseded[obsolete++]=self->slots[i].buffer;self->count--;self->slots[i]=self->slots[self->count];
      } else i++;
    }
    self->dropped+=obsolete;self->busy=TRUE;self->released++;guint64 generation=self->generation;
    g_mutex_unlock(&self->lock);admission_unref(superseded,obsolete);
    GstFlowReturn flow=gst_pad_push(self->src,buffer);g_mutex_lock(&self->lock);
    self->busy=FALSE;
    if(flow!=GST_FLOW_OK && !self->flushing && generation==self->generation)self->flow=flow;
    g_cond_broadcast(&self->changed);
  }
  g_mutex_unlock(&self->lock);return NULL;
}
static GstFlowReturn admission_chain(GstPad *pad,GstObject *parent,GstBuffer *buffer) {
  (void)pad;GstEufyAdmission *self=(GstEufyAdmission *)parent;
  guint64 now=admission_now(self),target=0;
  (void)arrival_due(self->channel,GST_BUFFER_PTS(buffer),&target);
  GstBuffer *dropped[3];guint drop_count=0;
  g_mutex_lock(&self->lock);
  if(self->flushing || self->stopping || self->flow!=GST_FLOW_OK) {
    GstFlowReturn flow=self->flushing || self->stopping?GST_FLOW_FLUSHING:self->flow;
    g_mutex_unlock(&self->lock);gst_buffer_unref(buffer);return flow;
  }
  AdmissionSlot candidates[3];guint count=self->count;
  memcpy(candidates,self->slots,count*sizeof(*candidates));
  candidates[count++]=(AdmissionSlot){buffer,target,++self->order};self->received++;
  gint due=-1,nearest=-1,newest=-1;
  for(guint i=0;i<count;i++) {
    if(candidates[i].target<=now) {
      if(due<0 || candidates[i].order>candidates[due].order)due=i;
    } else {
      if(nearest<0 || candidates[i].target<candidates[nearest].target)nearest=i;
      if(newest<0 || candidates[i].order>candidates[newest].order)newest=i;
    }
  }
  gboolean keep[3]={FALSE,FALSE,FALSE};
  if(due>=0)keep[due]=TRUE;
  if(nearest>=0)keep[nearest]=TRUE;
  if(due<0 && newest>=0)keep[newest]=TRUE;
  self->count=0;
  for(guint i=0;i<count;i++) {
    if(keep[i])self->slots[self->count++]=candidates[i];
    else dropped[drop_count++]=candidates[i].buffer;
  }
  g_assert(self->count<=2);self->dropped+=drop_count;g_cond_broadcast(&self->changed);
  g_mutex_unlock(&self->lock);admission_unref(dropped,drop_count);return GST_FLOW_OK;
}
static gboolean admission_sink_event(GstPad *pad,GstObject *parent,GstEvent *event) {
  (void)pad;GstEufyAdmission *self=(GstEufyAdmission *)parent;
  GstBuffer *dropped[2];guint count=0;GstEventType type=GST_EVENT_TYPE(event);
  if(type==GST_EVENT_EOS) {
    g_mutex_lock(&self->lock);self->eos=TRUE;g_cond_broadcast(&self->changed);g_mutex_unlock(&self->lock);gst_event_unref(event);return TRUE;
  }
  if(type==GST_EVENT_FLUSH_START || type==GST_EVENT_FLUSH_STOP) {
    g_mutex_lock(&self->lock);
    /* FLUSH_START releases blocked downstream work. Finish that old serialized
     * push before FLUSH_STOP reopens the pad, including an in-flight EOS. */
    if(type==GST_EVENT_FLUSH_STOP)
      while(self->busy && !self->stopping)g_cond_wait(&self->changed,&self->lock);
    self->generation++;self->flushing=type==GST_EVENT_FLUSH_START;
    count=admission_clear(self,dropped);self->eos=FALSE;self->flow=GST_FLOW_OK;
    g_cond_broadcast(&self->changed);g_mutex_unlock(&self->lock);admission_unref(dropped,count);
    return gst_pad_push_event(self->src,event);
  }
  if(GST_EVENT_IS_SERIALIZED(event)) {
    g_mutex_lock(&self->lock);
    if(type==GST_EVENT_SEGMENT || type==GST_EVENT_CAPS || type==GST_EVENT_STREAM_START)count=admission_clear(self,dropped);
    g_cond_broadcast(&self->changed);
    /* Tags and other serialized events cannot overtake retained pictures. */
    while((self->busy || self->count) && !self->stopping && !self->flushing)g_cond_wait(&self->changed,&self->lock);
    gboolean interrupted=self->stopping || self->flushing;
    g_mutex_unlock(&self->lock);admission_unref(dropped,count);
    if(interrupted){gst_event_unref(event);return FALSE;}
  }
  return gst_pad_push_event(self->src,event);
}
static gboolean admission_src_event(GstPad *pad,GstObject *parent,GstEvent *event) {
  (void)pad;return gst_pad_push_event(((GstEufyAdmission *)parent)->sink,event);
}
static gboolean admission_sink_query(GstPad *pad,GstObject *parent,GstQuery *query) {
  (void)pad;return gst_pad_peer_query(((GstEufyAdmission *)parent)->src,query);
}
static gboolean admission_src_query(GstPad *pad,GstObject *parent,GstQuery *query) {
  (void)pad;return gst_pad_peer_query(((GstEufyAdmission *)parent)->sink,query);
}
static GstStateChangeReturn admission_state(GstElement *element,GstStateChange transition) {
  GstEufyAdmission *self=(GstEufyAdmission *)element;
  if(transition==GST_STATE_CHANGE_READY_TO_PAUSED) {
    g_mutex_lock(&self->lock);self->stopping=self->flushing=self->eos=FALSE;self->flow=GST_FLOW_OK;
    self->order=self->received=self->dropped=self->released=0;g_mutex_unlock(&self->lock);
    self->worker=g_thread_new("eufy-admission",admission_worker,self);
  }
  GstStateChangeReturn result=GST_ELEMENT_CLASS(gst_eufy_admission_parent_class)->change_state(element,transition);
  if(transition==GST_STATE_CHANGE_PAUSED_TO_READY || (transition==GST_STATE_CHANGE_READY_TO_PAUSED && result==GST_STATE_CHANGE_FAILURE)) {
    GstBuffer *dropped[2];g_mutex_lock(&self->lock);self->stopping=TRUE;
    guint n=admission_clear(self,dropped);g_cond_broadcast(&self->changed);g_mutex_unlock(&self->lock);
    admission_unref(dropped,n);
    if(self->worker){g_thread_join(self->worker);self->worker=NULL;}
  }
  return result;
}
static void admission_prop(GObject *object,guint prop,const GValue *value,GParamSpec *spec) {
  if(prop==1)((GstEufyAdmission *)object)->channel=g_value_get_uint(value);
  else G_OBJECT_WARN_INVALID_PROPERTY_ID(object,prop,spec);
}
static void admission_finalize(GObject *object) {
  GstEufyAdmission *self=(GstEufyAdmission *)object;g_mutex_clear(&self->lock);g_cond_clear(&self->changed);
  G_OBJECT_CLASS(gst_eufy_admission_parent_class)->finalize(object);
}
static void gst_eufy_admission_class_init(GstEufyAdmissionClass *klass) {
  GObjectClass *object=G_OBJECT_CLASS(klass);object->set_property=admission_prop;object->finalize=admission_finalize;
  g_object_class_install_property(object,1,g_param_spec_uint("channel","channel","Per-tile clock channel",1,G_MAXUINT,1,G_PARAM_WRITABLE));
  GstElementClass *element=GST_ELEMENT_CLASS(klass);element->change_state=admission_state;
  GstCaps *caps=gst_caps_from_string("video/x-raw(ANY)");
  gst_element_class_add_pad_template(element,gst_pad_template_new("sink",GST_PAD_SINK,GST_PAD_ALWAYS,caps));
  gst_element_class_add_pad_template(element,gst_pad_template_new("src",GST_PAD_SRC,GST_PAD_ALWAYS,caps));gst_caps_unref(caps);
  gst_element_class_set_static_metadata(element,"Bounded due-picture admission","Generic/Video","Two decoded slots protect due pictures from future replacement","eufy-wall");
}
static void gst_eufy_admission_init(GstEufyAdmission *self) {
  self->channel=1;self->flow=GST_FLOW_OK;self->stopping=TRUE;g_mutex_init(&self->lock);g_cond_init(&self->changed);
  /* Class templates preserve allocator/caps features; no raw memory is mapped. */
  self->sink=gst_pad_new_from_template(gst_element_class_get_pad_template(GST_ELEMENT_GET_CLASS(self),"sink"),"sink");
  self->src=gst_pad_new_from_template(gst_element_class_get_pad_template(GST_ELEMENT_GET_CLASS(self),"src"),"src");
  gst_pad_set_chain_function(self->sink,GST_DEBUG_FUNCPTR(admission_chain));
  gst_pad_set_event_function(self->sink,GST_DEBUG_FUNCPTR(admission_sink_event));
  gst_pad_set_event_function(self->src,GST_DEBUG_FUNCPTR(admission_src_event));
  gst_pad_set_query_function(self->sink,GST_DEBUG_FUNCPTR(admission_sink_query));
  gst_pad_set_query_function(self->src,GST_DEBUG_FUNCPTR(admission_src_query));
  GST_PAD_SET_PROXY_CAPS(self->sink);GST_PAD_SET_PROXY_CAPS(self->src);GST_PAD_SET_PROXY_ALLOCATION(self->sink);
  gst_element_add_pad(GST_ELEMENT(self),self->sink);gst_element_add_pad(GST_ELEMENT(self),self->src);
}
