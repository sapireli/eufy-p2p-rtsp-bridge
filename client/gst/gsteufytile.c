#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
/* Independent decoder pipelines feeding a persistent compositor.
 * Included in the eufypacing plugin so installation remains one native module. */
#include <gst/app/gstappsrc.h>
#include <gst/app/gstappsink.h>
#include <gst/video/video.h>
#include <sys/stat.h>
#include <string.h>
#include "pacing.h"

typedef struct {
  GstBin parent;
  GstElement *source;
  gchar *config_file;
  GThread *worker;
  GMutex lock;
  GCond changed;
  gboolean stopping;
  guint64 delivered, restarts;
  gboolean hardware_pinned, hardware_active, yield_requested;
  guint hardware_priority;
  gchar *hardware_decoder;
} GstEufyTile;
typedef struct { GstBinClass parent; } GstEufyTileClass;
G_DEFINE_TYPE(GstEufyTile, gst_eufy_tile, GST_TYPE_BIN)

/* Process-local resource ownership, with no guessed hardware slot count. */
G_LOCK_DEFINE_STATIC(tile_pool);
static GList *tile_pool;
static GCond tile_pool_changed;
static void tile_hardware_active(GstEufyTile *s,gboolean active) {
  G_LOCK(tile_pool);s->hardware_active=active;g_cond_broadcast(&tile_pool_changed);G_UNLOCK(tile_pool);
}
static gboolean tile_yield_requested(GstEufyTile *s) {
  G_LOCK(tile_pool);gboolean yield=s->yield_requested;s->yield_requested=FALSE;G_UNLOCK(tile_pool);return yield;
}
static gboolean tile_reclaim(GstEufyTile *s) {
  GstEufyTile *victim=NULL;G_LOCK(tile_pool);
  for(GList *node=tile_pool;node;node=node->next) {
    GstEufyTile *candidate=node->data;
    if(candidate==s || !candidate->hardware_active || candidate->hardware_pinned || candidate->yield_requested ||
        g_strcmp0(candidate->hardware_decoder,s->hardware_decoder)!=0 ||
        !candidate->hardware_decoder || !*candidate->hardware_decoder)continue;
    if(!s->hardware_pinned && s->hardware_priority<=candidate->hardware_priority)continue;
    if(!victim || candidate->hardware_priority<victim->hardware_priority)victim=candidate;
  }
  if(victim) {
    gst_object_ref(victim);victim->yield_requested=TRUE;
    g_printerr("eufytile %s: requesting hardware release from %s\n",GST_OBJECT_NAME(s),GST_OBJECT_NAME(victim));
    /* A resource is not reassigned until its old pipeline reached NULL. */
    gint64 deadline=g_get_monotonic_time()+2*G_TIME_SPAN_SECOND;
    while(victim->hardware_active && g_cond_wait_until(&tile_pool_changed,&G_LOCK_NAME(tile_pool),deadline)){}
    gboolean released=!victim->hardware_active;G_UNLOCK(tile_pool);gst_object_unref(victim);return released;
  }
  G_UNLOCK(tile_pool);return FALSE;
}
static gboolean tile_resource_error(GstMessage *msg) {
  GError *error=NULL;gchar *debug=NULL;gst_message_parse_error(msg,&error,&debug);
  gchar *description=g_ascii_strdown(error?error->message:"",-1);
  gboolean resource=(error && error->domain==GST_RESOURCE_ERROR && error->code==GST_RESOURCE_ERROR_BUSY) || strstr(description,"busy") || strstr(description,"insufficient resources") ||
      strstr(description,"no free") || strstr(description,"resource exhausted") ||
      strstr(description,"failed to allocate required memory");
  g_free(description);g_clear_error(&error);g_free(debug);return resource;
}

static GstClockTime tile_running_time(GstEufyTile *s) {
  GstClock *clock=gst_element_get_clock(GST_ELEMENT(s));
  if(!clock)return 0;
  GstClockTime now=gst_clock_get_time(clock),base=gst_element_get_base_time(GST_ELEMENT(s));
  gst_object_unref(clock);
  return now>=base?now-base:0;
}
/* Translate media cadence into the persistent compositor's running time.
 * The final wall pacer owns waiting; this mapper adds no second holdback. */
static GstClockTime tile_map_pts(GstClockTime pts,GstClockTime now,GstClockTime *first_pts,GstClockTime *first_running,gboolean discontinuity) {
  if(!GST_CLOCK_TIME_IS_VALID(pts))return now;
  gboolean reset=discontinuity || !GST_CLOCK_TIME_IS_VALID(*first_pts) || pts<*first_pts;
  GstClockTime projected=reset?now:*first_running+pts-*first_pts;
  if((projected>now && projected-now>(GstClockTime)PACING_BOUND_NS) || (now>projected && now-projected>(GstClockTime)PACING_BOUND_NS))reset=TRUE;
  if(reset){*first_pts=pts;*first_running=now;projected=now;}
  return projected;
}
static gboolean tile_stopping(GstEufyTile *s) {
  g_mutex_lock(&s->lock);gboolean stop=s->stopping;g_mutex_unlock(&s->lock);return stop;
}
static void tile_wait(GstEufyTile *s,gint64 us) {
  g_mutex_lock(&s->lock);
  if(!s->stopping)g_cond_wait_until(&s->changed,&s->lock,g_get_monotonic_time()+us);
  g_mutex_unlock(&s->lock);
}
static gboolean tile_hardware_factory(const gchar *name) {
  return (g_str_has_prefix(name,"v4l2") || g_str_has_prefix(name,"va")) &&
      (strstr(name,"h264dec") || strstr(name,"h265dec"));
}
static void tile_decoder_resource(GstEufyTile *s,GstElement *decoder) {
  GstIterator *iterator=gst_bin_iterate_recurse(GST_BIN(decoder));GValue value=G_VALUE_INIT;gchar *resource=NULL;
  gboolean done=FALSE;
  while(!done) {
    switch(gst_iterator_next(iterator,&value)) {
      case GST_ITERATOR_OK: {
        GstElementFactory *factory=gst_element_get_factory(g_value_get_object(&value));
        const gchar *name=factory?gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(factory)):"";
        if(tile_hardware_factory(name)){resource=g_strdup(name);done=TRUE;}
        g_value_reset(&value);break;
      }
      case GST_ITERATOR_RESYNC:gst_iterator_resync(iterator);break;
      default:done=TRUE;break;
    }
  }
  if(G_IS_VALUE(&value))g_value_unset(&value);
  gst_iterator_free(iterator);
  G_LOCK(tile_pool);g_free(s->hardware_decoder);s->hardware_decoder=resource;G_UNLOCK(tile_pool);
}
static gboolean tile_hardware_error(GstMessage *msg) {
  if(!GST_IS_ELEMENT(GST_MESSAGE_SRC(msg)))return FALSE;
  GstElementFactory *factory=gst_element_get_factory(GST_ELEMENT(GST_MESSAGE_SRC(msg)));
  const gchar *name=factory?gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(factory)):"";
  if(!tile_hardware_factory(name))return FALSE;
  GError *error=NULL;gchar *debug=NULL;gst_message_parse_error(msg,&error,&debug);
  const gchar *known[]={"Failed to allocate required memory.","Failed to start decoding thread.",
    "Failed to start decoder","Could not initialize supporting library.","Could not open device '",
    "Cannot identify device '",NULL};
  gboolean match=error && error->domain==GST_RESOURCE_ERROR && error->code==GST_RESOURCE_ERROR_BUSY;
  if(error) {
    for(guint i=0;known[i];i++)if(strstr(error->message,known[i]))match=TRUE;
    if(strstr(error->message,"Decoder on device ") && strstr(error->message," has no supported input format"))match=TRUE;
  }
  g_clear_error(&error);g_free(debug);return match;
}
/* One transparent black RGBA frame clears a failed tile without touching siblings.
 * A live appsrc never sends EOS; absent producers therefore cannot end the wall. */
static void tile_black(GstEufyTile *s,GstCaps *caps) {
  GstVideoInfo info;
  if(!caps || !gst_video_info_from_caps(&info,caps) || GST_VIDEO_INFO_FORMAT(&info)!=GST_VIDEO_FORMAT_RGBA)return;
  GstBuffer *buffer=gst_buffer_new_allocate(NULL,info.size,NULL);
  gst_buffer_memset(buffer,0,0,info.size);
  GST_BUFFER_PTS(buffer)=tile_running_time(s);GST_BUFFER_DURATION(buffer)=GST_SECOND/30;
  gst_app_src_set_caps(GST_APP_SRC(s->source),caps);
  gst_app_src_push_buffer(GST_APP_SRC(s->source),buffer);
}
static gchar *tile_key(GKeyFile *file,const gchar *key) {
  gchar *value=g_key_file_get_string(file,"tile",key,NULL);
  return value?value:g_strdup("");
}
static guint tile_millis(GKeyFile *file,const gchar *key,guint fallback) {
  guint64 value=g_key_file_get_uint64(file,"tile",key,NULL);
  return value>0?(guint)MIN(value,G_MAXUINT):fallback;
}
static guint tile_retry_delay(guint previous,guint minimum,guint maximum,guint stable,gint64 ran_us) {
  if(!previous || ran_us>=(gint64)stable*1000)return minimum;
  return (guint)MIN((guint64)previous*2,maximum);
}
static gboolean tile_same_stat(const struct stat *a,const struct stat *b) {
  if(a->st_ino!=b->st_ino || a->st_size!=b->st_size || a->st_mtime!=b->st_mtime || a->st_ctime!=b->st_ctime)return FALSE;
#ifdef __APPLE__
  return a->st_mtimespec.tv_nsec==b->st_mtimespec.tv_nsec && a->st_ctimespec.tv_nsec==b->st_ctimespec.tv_nsec;
#else
  return a->st_mtim.tv_nsec==b->st_mtim.tv_nsec && a->st_ctim.tv_nsec==b->st_ctim.tv_nsec;
#endif
}
static gpointer tile_worker(gpointer data) {
  GstEufyTile *s=data;gchar *previous=NULL,*launch[3]={NULL,NULL,NULL};
  GstElement *decoder=NULL,*output=NULL;GstBus *bus=NULL;GstCaps *last_caps=NULL;
  guint mode=0,retry_delay=0,retry_min=1000,retry_max=30000,retry_stable=60000;
  gint64 config_at=0,retry_at=0,started_at=0,stats_at=g_get_monotonic_time();
  GstClockTime first_pts=GST_CLOCK_TIME_NONE,first_running=0;
  G_LOCK(tile_pool);tile_pool=g_list_prepend(tile_pool,s);G_UNLOCK(tile_pool);
  gboolean can_yield=FALSE,have_stat=FALSE,reclaimed=FALSE;struct stat config_stat={0};
  while(!tile_stopping(s)) {
    gint64 now=g_get_monotonic_time();gboolean changed=FALSE;
    if(now>=config_at) {
      config_at=now+100000;gchar *contents=NULL;
      struct stat current;
      gboolean file_changed=stat(s->config_file,&current)==0 && (!have_stat || !tile_same_stat(&current,&config_stat));
      if(file_changed && g_file_get_contents(s->config_file,&contents,NULL,NULL) && g_strcmp0(contents,previous)!=0) {
        config_stat=current;have_stat=TRUE;
        GKeyFile *file=g_key_file_new();GError *error=NULL;
        if(g_key_file_load_from_data(file,contents,-1,G_KEY_FILE_NONE,&error)) {
          for(guint i=0;i<3;i++)g_free(launch[i]);
          launch[0]=tile_key(file,"pipeline");launch[1]=tile_key(file,"compatible-pipeline");launch[2]=tile_key(file,"software-pipeline");
          retry_min=tile_millis(file,"restart-min-ms",1000);retry_max=MAX(retry_min,tile_millis(file,"restart-max-ms",30000));
          retry_stable=tile_millis(file,"restart-stable-ms",60000);retry_delay=0;
          G_LOCK(tile_pool);
          g_free(s->hardware_decoder);s->hardware_decoder=tile_key(file,"hardware-decoder");
          s->hardware_pinned=g_key_file_get_boolean(file,"tile","hardware-pinned",NULL);
          gint priority=g_key_file_get_integer(file,"tile","hardware-priority",NULL);
          s->hardware_priority=priority>0?(guint)priority:0;
          can_yield=launch[2] && *launch[2];s->yield_requested=FALSE;
          G_UNLOCK(tile_pool);
          g_free(previous);previous=g_strdup(contents);mode=0;retry_at=0;reclaimed=FALSE;changed=TRUE;
          g_printerr("eufytile %s: configuration updated\n",GST_OBJECT_NAME(s));
        } else {g_printerr("eufytile %s: invalid configuration: %s\n",GST_OBJECT_NAME(s),error->message);g_clear_error(&error);}
        g_key_file_unref(file);
      }
      g_free(contents);
    }
    if(tile_yield_requested(s) && can_yield) {
      mode=2;changed=TRUE;retry_at=0;retry_delay=0;
      g_printerr("eufytile %s: yielding hardware to higher-priority tile; software latched until config changes\n",GST_OBJECT_NAME(s));
    }
    if(changed && decoder) {
      gst_element_set_state(decoder,GST_STATE_NULL);gst_object_unref(output);gst_object_unref(bus);gst_object_unref(decoder);
      decoder=output=NULL;bus=NULL;tile_hardware_active(s,FALSE);tile_black(s,last_caps);
    }
    if(!decoder && launch[mode] && *launch[mode] && now>=retry_at) {
      started_at=now;
      GError *error=NULL;decoder=gst_parse_launch(launch[mode],&error);
      if(error || !decoder || !GST_IS_BIN(decoder)) {
        g_printerr("eufytile %s: invalid decoder pipeline: %s\n",GST_OBJECT_NAME(s),error?error->message:"not a bin");
        g_clear_error(&error);if(decoder)gst_object_unref(decoder);decoder=NULL;retry_delay=tile_retry_delay(retry_delay,retry_min,retry_max,retry_stable,now-started_at);retry_at=now+(gint64)retry_delay*1000;
      } else {
        output=gst_bin_get_by_name(GST_BIN(decoder),"tile_output");
        if(!output || !GST_IS_APP_SINK(output)) {
          g_printerr("eufytile %s: pipeline requires appsink name=tile_output\n",GST_OBJECT_NAME(s));
          if(output)gst_object_unref(output);
          output=NULL;gst_object_unref(decoder);decoder=NULL;retry_delay=tile_retry_delay(retry_delay,retry_min,retry_max,retry_stable,now-started_at);retry_at=now+(gint64)retry_delay*1000;
        } else {
          g_object_set(output,"sync",FALSE,"max-buffers",1u,"drop",TRUE,"enable-last-sample",FALSE,NULL);
          bus=gst_element_get_bus(decoder);first_pts=GST_CLOCK_TIME_NONE;
          tile_decoder_resource(s,decoder);
          GstStateChangeReturn result=gst_element_set_state(decoder,GST_STATE_PLAYING);
          tile_hardware_active(s,mode<2 && can_yield && s->hardware_decoder && *s->hardware_decoder);
          s->restarts++;
          g_printerr("eufytile %s: decoder started mode=%u state-result=%d\n",GST_OBJECT_NAME(s),mode,result);
          /* A failed synchronous transition still has its detailed ERROR on the bus. */
          if(result==GST_STATE_CHANGE_FAILURE && !gst_bus_have_pending(bus)) {
            gst_element_set_state(decoder,GST_STATE_NULL);gst_object_unref(output);gst_object_unref(bus);gst_object_unref(decoder);
            decoder=output=NULL;bus=NULL;tile_hardware_active(s,FALSE);retry_delay=tile_retry_delay(retry_delay,retry_min,retry_max,retry_stable,now-started_at);retry_at=now+(gint64)retry_delay*1000;
          }
        }
      }
    }
    if(decoder) {
      GstMessage *msg=gst_bus_pop_filtered(bus,GST_MESSAGE_ERROR|GST_MESSAGE_EOS);
      if(msg) {
        gboolean hardware=GST_MESSAGE_TYPE(msg)==GST_MESSAGE_ERROR && tile_hardware_error(msg);
        gboolean resource=hardware && tile_resource_error(msg);
        GError *error=NULL;gchar *debug=NULL;
        if(GST_MESSAGE_TYPE(msg)==GST_MESSAGE_ERROR)gst_message_parse_error(msg,&error,&debug);
        /* Do not print launch strings, which may contain RTSP credentials. */
        g_printerr("eufytile %s: decoder stopped mode=%u hardware-init=%d reason=%s\n",GST_OBJECT_NAME(s),mode,hardware,error?error->message:"EOS");
        g_clear_error(&error);g_free(debug);gst_message_unref(msg);
        gst_element_set_state(decoder,GST_STATE_NULL);gst_object_unref(output);gst_object_unref(bus);gst_object_unref(decoder);
        decoder=output=NULL;bus=NULL;tile_hardware_active(s,FALSE);tile_black(s,last_caps);retry_delay=tile_retry_delay(retry_delay,retry_min,retry_max,retry_stable,now-started_at);retry_at=now+(gint64)retry_delay*1000;
        guint next=mode;
        if(resource && can_yield && !reclaimed && mode<2 && tile_reclaim(s)) {reclaimed=TRUE;retry_at=0;}
        else {
          if(mode==0 && launch[1] && *launch[1])next=1;
          else if(hardware && mode<2 && launch[2] && *launch[2])next=2;
          if(next!=mode){mode=next;retry_at=0;retry_delay=0;g_printerr("eufytile %s: selecting isolated fallback mode=%u\n",GST_OBJECT_NAME(s),mode);}
        }
      } else {
        GstSample *sample=gst_app_sink_try_pull_sample(GST_APP_SINK(output),20*GST_MSECOND);
        if(sample) {
          GstCaps *caps=gst_sample_get_caps(sample);GstBuffer *input=gst_sample_get_buffer(sample);
          GstVideoInfo info;
          if(caps && input && gst_video_info_from_caps(&info,caps) && GST_VIDEO_INFO_FORMAT(&info)==GST_VIDEO_FORMAT_RGBA) {
            GstBuffer *buffer=gst_buffer_copy(input);GstClockTime pts=GST_BUFFER_PTS(input);
            GST_BUFFER_PTS(buffer)=tile_map_pts(pts,tile_running_time(s),&first_pts,&first_running,GST_BUFFER_FLAG_IS_SET(input,GST_BUFFER_FLAG_DISCONT));
            GST_BUFFER_DTS(buffer)=GST_CLOCK_TIME_NONE;
            gst_app_src_set_caps(GST_APP_SRC(s->source),caps);
            gst_caps_replace(&last_caps,caps);
            GstFlowReturn flow=gst_app_src_push_buffer(GST_APP_SRC(s->source),buffer);
            if(flow==GST_FLOW_OK)s->delivered++;
          }
          gst_sample_unref(sample);
        }
      }
    } else tile_wait(s,20000);
    if(now-stats_at>=5000000) {
      g_printerr("eufytile %s: delivered=%"G_GUINT64_FORMAT" restarts=%"G_GUINT64_FORMAT" mode=%u active=%d\n",GST_OBJECT_NAME(s),s->delivered,s->restarts,mode,decoder!=NULL);stats_at=now;
    }
  }
  if(decoder){gst_element_set_state(decoder,GST_STATE_NULL);gst_object_unref(output);gst_object_unref(bus);gst_object_unref(decoder);}
  G_LOCK(tile_pool);s->hardware_active=FALSE;tile_pool=g_list_remove(tile_pool,s);g_cond_broadcast(&tile_pool_changed);G_UNLOCK(tile_pool);
  for(guint i=0;i<3;i++)g_free(launch[i]);
  g_free(previous);gst_caps_replace(&last_caps,NULL);return NULL;
}
static GstStateChangeReturn tile_change_state(GstElement *element,GstStateChange transition) {
  GstEufyTile *s=(GstEufyTile *)element;
  if(transition==GST_STATE_CHANGE_READY_TO_PAUSED) {
    if(!s->config_file || !*s->config_file){GST_ELEMENT_ERROR(s,RESOURCE,SETTINGS,("config-file is required"),(NULL));return GST_STATE_CHANGE_FAILURE;}
    g_mutex_lock(&s->lock);s->stopping=FALSE;g_mutex_unlock(&s->lock);
  }
  if(transition==GST_STATE_CHANGE_PAUSED_TO_READY) {
    g_mutex_lock(&s->lock);s->stopping=TRUE;g_cond_broadcast(&s->changed);g_mutex_unlock(&s->lock);
    if(s->worker){g_thread_join(s->worker);s->worker=NULL;}
  }
  GstStateChangeReturn result=GST_ELEMENT_CLASS(gst_eufy_tile_parent_class)->change_state(element,transition);
  if(result!=GST_STATE_CHANGE_FAILURE && transition==GST_STATE_CHANGE_READY_TO_PAUSED)s->worker=g_thread_new("eufy-tile",tile_worker,s);
  return result;
}
static void tile_set_property(GObject *object,guint id,const GValue *value,GParamSpec *spec) {
  GstEufyTile *s=(GstEufyTile *)object;
  if(id==1){g_free(s->config_file);s->config_file=g_value_dup_string(value);}else G_OBJECT_WARN_INVALID_PROPERTY_ID(object,id,spec);
}
static void tile_finalize(GObject *object) {
  GstEufyTile *s=(GstEufyTile *)object;g_free(s->config_file);g_free(s->hardware_decoder);g_mutex_clear(&s->lock);g_cond_clear(&s->changed);
  G_OBJECT_CLASS(gst_eufy_tile_parent_class)->finalize(object);
}
static void gst_eufy_tile_class_init(GstEufyTileClass *klass) {
  GObjectClass *object=G_OBJECT_CLASS(klass);object->set_property=tile_set_property;object->finalize=tile_finalize;
  g_object_class_install_property(object,1,g_param_spec_string("config-file","config-file","Atomically replaced per-tile INI configuration",NULL,G_PARAM_WRITABLE|GST_PARAM_MUTABLE_READY));
  GstElementClass *element=GST_ELEMENT_CLASS(klass);element->change_state=tile_change_state;
  GstCaps *caps=gst_caps_from_string("video/x-raw,format=RGBA");
  gst_element_class_add_pad_template(element,gst_pad_template_new("src",GST_PAD_SRC,GST_PAD_ALWAYS,caps));gst_caps_unref(caps);
  gst_element_class_set_static_metadata(element,"Independent camera tile","Source/Video","Isolates camera decode, retries and fallback from compositor","eufy-wall");
}
static void gst_eufy_tile_init(GstEufyTile *s) {
  g_mutex_init(&s->lock);g_cond_init(&s->changed);
  s->source=gst_element_factory_make("appsrc",NULL);
  g_object_set(s->source,"is-live",TRUE,"format",GST_FORMAT_TIME,"block",FALSE,"max-buffers",(guint64)1,"max-bytes",(guint64)0,"max-time",(guint64)0,"leaky-type",2,NULL);
  gst_bin_add(GST_BIN(s),s->source);
  GstPad *pad=gst_element_get_static_pad(s->source,"src");
  gst_element_add_pad(GST_ELEMENT(s),gst_ghost_pad_new("src",pad));gst_object_unref(pad);
}
