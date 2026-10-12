/* Real queue regression: 15 fps live source, unchanged one-buffer leaky queue,
 * sync=true sink with bounded 70 ms work and one 220 ms disturbance. */
#include <gst/gst.h>
#include <stdio.h>
static guint frames,discont;
static gint64 first,last,post_disturbance,queue_at,wait_sum,work_sum;
static guint work_us=70000,jitter_us,ingress_count,min_frames,slots=1;
static guint64 ages[180];
static gint64 start;
static void ingress(GstElement *identity,GstBuffer *buffer,gpointer data) {
 (void)identity;(void)data;ingress_count++;
 GST_BUFFER_OFFSET_END(buffer)=GST_BUFFER_PTS(buffer);
 if(jitter_us && ingress_count%15==0)g_usleep(jitter_us);
}
static GstPadProbeReturn queue_output(GstPad *pad,GstPadProbeInfo *info,gpointer data) {
  (void)pad;(void)data;
  queue_at=g_get_monotonic_time();
  if(GST_BUFFER_FLAG_IS_SET(GST_PAD_PROBE_INFO_BUFFER(info),GST_BUFFER_FLAG_DISCONT))discont++;
  return GST_PAD_PROBE_OK;
}
static void output(GstElement *sink,GstBuffer *buffer,GstPad *pad,gpointer data) {
  (void)pad;(void)data;
  gint64 now=g_get_monotonic_time();
  if(!frames)first=now;
  last=now;wait_sum+=now-queue_at;
  GstClock *clock=gst_element_get_clock(sink);
  guint64 running=gst_clock_get_time(clock)-gst_element_get_base_time(sink);gst_object_unref(clock);
  if(frames<180)ages[frames]=running-GST_BUFFER_OFFSET_END(buffer);
  frames++;
  if(frames==15){g_usleep(220000);post_disturbance=g_get_monotonic_time();}
  else g_usleep(work_us);
  work_sum+=g_get_monotonic_time()-now;
}
int main(int argc,char **argv) {
  for(int i=1;i<argc;i++) {
   if(sscanf(argv[i],"--work-us=%u",&work_us)==1)continue;
   if(sscanf(argv[i],"--jitter-us=%u",&jitter_us)==1)continue;
   if(sscanf(argv[i],"--min-frames=%u",&min_frames)==1)continue;
   if(sscanf(argv[i],"--slots=%u",&slots)==1)continue;
  }
  g_assert(work_us<=250000 && jitter_us<=250000 && slots>=1 && slots<=2);
  gst_init(&argc,&argv);GError *err=NULL;
  gchar *description=g_strdup_printf("videotestsrc is-live=true num-buffers=180 ! video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! identity name=j signal-handoffs=true ! queue name=q max-size-buffers=%u max-size-bytes=0 max-size-time=0 leaky=downstream ! eufypacing ! fakesink name=s sync=true async=false signal-handoffs=true",slots);
  GstElement *pipeline=gst_parse_launch(description,&err);g_free(description);
  g_assert_no_error(err);GstElement *q=gst_bin_get_by_name(GST_BIN(pipeline),"q"),*sink=gst_bin_get_by_name(GST_BIN(pipeline),"s");
  GstPad *pad=gst_element_get_static_pad(q,"src");gst_pad_add_probe(pad,GST_PAD_PROBE_TYPE_BUFFER,queue_output,NULL,NULL);gst_object_unref(pad);
  GstElement *j=gst_bin_get_by_name(GST_BIN(pipeline),"j");g_signal_connect(j,"handoff",G_CALLBACK(ingress),NULL);gst_object_unref(j);
  g_signal_connect(sink,"handoff",G_CALLBACK(output),NULL);start=g_get_monotonic_time();
  gst_element_set_state(pipeline,GST_STATE_PLAYING);
  GstBus *bus=gst_element_get_bus(pipeline);GstMessage *message=gst_bus_timed_pop_filtered(bus,20*GST_SECOND,GST_MESSAGE_EOS|GST_MESSAGE_ERROR);
  g_assert(message);g_assert(GST_MESSAGE_TYPE(message)==GST_MESSAGE_EOS);
  printf("frames=%u localDiscont=%u durationMs=%.3f fps=%.3f disturbanceEndUs=%"G_GINT64_FORMAT"\n",frames,discont,(last-first)/1000.0,(frames-1)*1000000.0/(last-first),post_disturbance);
  printf("queueToHandoffWaitMs=%.3f actualSinkWorkMs=%.3f totalMs=%.3f\n",wait_sum/1000.0,work_sum/1000.0,(g_get_monotonic_time()-start)/1000.0);
  for(guint i=0;i<frames;i++)for(guint j=i+1;j<frames;j++)if(ages[j]<ages[i]){guint64 tmp=ages[i];ages[i]=ages[j];ages[j]=tmp;}
  printf("slots=%u ageAtHandoffMedianMs=%.3f ageAtHandoffMaxMs=%.3f\n",slots,ages[frames/2]/1000000.0,ages[frames-1]/1000000.0);
  if(min_frames)g_assert(frames>=min_frames);
  gst_message_unref(message);gst_object_unref(bus);gst_element_set_state(pipeline,GST_STATE_NULL);gst_object_unref(q);gst_object_unref(sink);gst_object_unref(pipeline);return 0;
}
