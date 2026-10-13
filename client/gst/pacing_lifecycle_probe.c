/* Genuine segment reset permits a timestamp restart; a local queue DISCONT
 * alone must not defeat the source timestamp ordering guard. */
#include <gst/gst.h>
static gint received;
static void output(GstElement *s,GstBuffer *b,GstPad *p,gpointer unused) {
 (void)s;(void)b;(void)p;(void)unused;g_atomic_int_inc(&received);
}
static void push(GstElement *source,GstClockTime pts,gboolean discont) {
 GstBuffer *buffer=gst_buffer_new();GST_BUFFER_PTS(buffer)=pts;
 if(discont)GST_BUFFER_FLAG_SET(buffer,GST_BUFFER_FLAG_DISCONT);
 GstFlowReturn flow;g_signal_emit_by_name(source,"push-buffer",buffer,&flow);gst_buffer_unref(buffer);g_assert(flow==GST_FLOW_OK);g_usleep(100000);
}
int main(int argc,char **argv) {
 gst_init(&argc,&argv);GError *error=NULL;
 GstElement *pipeline=gst_parse_launch("appsrc name=s is-live=true format=time caps=video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! eufypacing name=p ! fakesink name=o sync=false async=false signal-handoffs=true",&error);g_assert_no_error(error);
 GstElement *source=gst_bin_get_by_name(GST_BIN(pipeline),"s"),*sink=gst_bin_get_by_name(GST_BIN(pipeline),"o"),*pacer=gst_bin_get_by_name(GST_BIN(pipeline),"p");
 g_signal_connect(sink,"handoff",G_CALLBACK(output),NULL);gst_element_set_state(pipeline,GST_STATE_PLAYING);gst_element_get_state(pipeline,NULL,NULL,GST_SECOND);
 push(source,GST_SECOND,FALSE);push(source,2*GST_SECOND,FALSE);g_assert(g_atomic_int_get(&received)==2);
 push(source,0,TRUE);g_assert(g_atomic_int_get(&received)==2);
 GstSegment segment;gst_segment_init(&segment,GST_FORMAT_TIME);GstPad *pad=gst_element_get_static_pad(pacer,"sink");g_assert(gst_pad_send_event(pad,gst_event_new_segment(&segment)));gst_object_unref(pad);
 push(source,0,FALSE);g_assert(g_atomic_int_get(&received)==3);
 gst_element_set_state(pipeline,GST_STATE_NULL);gst_object_unref(source);gst_object_unref(sink);gst_object_unref(pacer);gst_object_unref(pipeline);
 g_print("lifecycle: raw DISCONT regression rejected; genuine SEGMENT timestamp restart accepted\n");return 0;
}
