/* Actual async admission lifecycle: lone frame without EOF, blocked downstream
 * flush, segment/caps ordering, finite EOS drain and state teardown/restart. */
#include <gst/gst.h>
static GMutex lock;static GCond changed;
static guint seen,widths[8];static gboolean blocked,release_block;
static void handoff(GstElement *sink,GstBuffer *buffer,GstPad *pad,gpointer unused) {
 (void)sink;(void)unused;GstCaps *caps=gst_pad_get_current_caps(pad);gint width=0;
 g_assert(gst_structure_get_int(gst_caps_get_structure(caps,0),"width",&width));gst_caps_unref(caps);
 g_mutex_lock(&lock);guint index=(guint)GST_BUFFER_OFFSET(buffer);g_assert(index<8);widths[index]=width;seen++;
 if(index==2) {blocked=TRUE;g_cond_broadcast(&changed);while(!release_block)g_cond_wait(&changed,&lock);}
 g_cond_broadcast(&changed);g_mutex_unlock(&lock);
}
static gpointer unblock(gpointer unused) {(void)unused;g_usleep(50000);g_mutex_lock(&lock);release_block=TRUE;g_cond_broadcast(&changed);g_mutex_unlock(&lock);return NULL;}
static void push(GstElement *source,guint index,GstClockTime pts,gsize bytes) {
 GstBuffer *buffer=gst_buffer_new_allocate(NULL,bytes,NULL);GST_BUFFER_PTS(buffer)=pts;GST_BUFFER_OFFSET(buffer)=index;
 GstFlowReturn flow;g_signal_emit_by_name(source,"push-buffer",buffer,&flow);gst_buffer_unref(buffer);g_assert(flow==GST_FLOW_OK);
}
static void wait_seen(guint required) {
 gint64 deadline=g_get_monotonic_time()+1000000;g_mutex_lock(&lock);
 while(seen<required && g_get_monotonic_time()<deadline)g_cond_wait_until(&changed,&lock,deadline);
 g_assert(seen>=required);g_mutex_unlock(&lock);
}
int main(int argc,char **argv) {
 gst_init(&argc,&argv);g_mutex_init(&lock);g_cond_init(&changed);GError *error=NULL;
 GstElement *pipeline=gst_parse_launch("appsrc name=s is-live=true format=time caps=video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! eufyarrival ! eufyadmission name=q ! eufypacing arrival-clock=true ! fakesink name=o sync=false async=false signal-handoffs=true",&error);g_assert_no_error(error);
 GstElement *source=gst_bin_get_by_name(GST_BIN(pipeline),"s"),*sink=gst_bin_get_by_name(GST_BIN(pipeline),"o");
 g_signal_connect(sink,"handoff",G_CALLBACK(handoff),NULL);gst_element_set_state(pipeline,GST_STATE_PLAYING);gst_element_get_state(pipeline,NULL,NULL,GST_SECOND);
 push(source,1,0,4);wait_seen(1); /* Nothing else arrived; no EOS was sent. */
 push(source,2,GST_SECOND,4);wait_seen(2);g_mutex_lock(&lock);g_assert(blocked);g_mutex_unlock(&lock);
 GstPad *pad=gst_element_get_static_pad(source,"src");GThread *release=g_thread_new("release-test-sink",unblock,NULL);
 gint64 before=g_get_monotonic_time();g_assert(gst_pad_push_event(pad,gst_event_new_flush_start()));g_assert(g_get_monotonic_time()-before<1000000);
 g_assert(gst_pad_push_event(pad,gst_event_new_flush_stop(TRUE)));g_thread_join(release);
 GstSegment segment;gst_segment_init(&segment,GST_FORMAT_TIME);g_assert(gst_pad_push_event(pad,gst_event_new_segment(&segment)));
 push(source,3,0,4);wait_seen(3);
 GstCaps *caps=gst_caps_from_string("video/x-raw,format=RGB,width=2,height=1,framerate=15/1");g_object_set(source,"caps",caps,NULL);gst_caps_unref(caps);
 push(source,4,100000000,8);GstFlowReturn flow;g_signal_emit_by_name(source,"end-of-stream",&flow);g_assert(flow==GST_FLOW_OK);
 GstBus *bus=gst_element_get_bus(pipeline);GstMessage *message=gst_bus_timed_pop_filtered(bus,2*GST_SECOND,GST_MESSAGE_EOS|GST_MESSAGE_ERROR);g_assert(message && GST_MESSAGE_TYPE(message)==GST_MESSAGE_EOS);gst_message_unref(message);wait_seen(4);g_assert(widths[3]==1 && widths[4]==2);
 gst_element_set_state(pipeline,GST_STATE_NULL);gst_element_get_state(pipeline,NULL,NULL,GST_SECOND);
 gst_element_set_state(pipeline,GST_STATE_PLAYING);gst_element_get_state(pipeline,NULL,NULL,GST_SECOND);push(source,5,0,8);wait_seen(5);
 gst_element_set_state(pipeline,GST_STATE_NULL);
 /* Non-live async preroll must complete without a following frame or clock
  * advance supplied by another source. Reuse the same actual admission path. */
 g_object_set(source,"is-live",FALSE,NULL);g_object_set(sink,"async",TRUE,NULL);
 gst_element_set_state(pipeline,GST_STATE_PLAYING);push(source,6,0,8);
 gst_element_get_state(pipeline,NULL,NULL,GST_SECOND);wait_seen(6);
 g_signal_emit_by_name(source,"end-of-stream",&flow);g_assert(flow==GST_FLOW_OK);
 message=gst_bus_timed_pop_filtered(bus,2*GST_SECOND,GST_MESSAGE_EOS|GST_MESSAGE_ERROR);g_assert(message && GST_MESSAGE_TYPE(message)==GST_MESSAGE_EOS);gst_message_unref(message);
 gst_element_set_state(pipeline,GST_STATE_NULL);gst_object_unref(bus);gst_object_unref(pad);gst_object_unref(source);gst_object_unref(sink);gst_object_unref(pipeline);g_mutex_clear(&lock);g_cond_clear(&changed);
 g_print("admission lifecycle: lone AU, blocked flush, segment/caps ordering, EOS and restart PASS\n");return 0;
}
