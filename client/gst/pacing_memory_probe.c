/* Read-only memory identity and parent-buffer lifetime across PTS rebasing.
 * The retained NO_SHARE input deliberately defeats default writable-buffer copy. */
#include <gst/gst.h>
#include <stdio.h>
#include <string.h>
static GstBuffer *input;
static GstMemory *memory;
static gint seen,input_freed;
static gboolean require_identity,bad_diagnostic;
static void finalized(gpointer unused,GstMiniObject *object) {(void)unused;(void)object;g_atomic_int_inc(&input_freed);}
static GstPadProbeReturn capture(GstPad *pad,GstPadProbeInfo *info,gpointer unused) {
 (void)pad;(void)unused;input=gst_buffer_ref(GST_PAD_PROBE_INFO_BUFFER(info));
 memory=gst_buffer_peek_memory(input,0);GST_MINI_OBJECT_FLAG_SET(memory,GST_MEMORY_FLAG_NO_SHARE);
 gst_mini_object_weak_ref(GST_MINI_OBJECT(input),finalized,NULL);return GST_PAD_PROBE_OK;
}
static GstBuffer *retained_output;
static void handoff(GstElement *sink,GstBuffer *output,GstPad *pad,gpointer unused) {
 (void)sink;(void)pad;(void)unused;
 gboolean same=gst_buffer_peek_memory(output,0)==memory;
 GstParentBufferMeta *parent=gst_buffer_get_parent_buffer_meta(output);
 printf("sameMemory=%d independentHeader=%d parentRetained=%d sourcePts=%"G_GUINT64_FORMAT" outputPts=%"G_GUINT64_FORMAT"\n",same,output!=input,parent && parent->buffer==input,GST_BUFFER_PTS(input),GST_BUFFER_PTS(output));
 if(require_identity) {g_assert(same);g_assert(output!=input);g_assert(parent && parent->buffer==input);}
 g_assert(GST_BUFFER_PTS(input)==0);g_assert(GST_BUFFER_PTS(output)>0);g_assert(GST_BUFFER_OFFSET(output)==123);
 retained_output=gst_buffer_ref(output);g_atomic_int_inc(&seen);
}
static gint writable_seen,writable_freed;
static void writable_finalized(gpointer unused,GstMiniObject *object) {
 (void)unused;(void)object;g_atomic_int_inc(&writable_freed);
}
static GstPadProbeReturn writable_capture(GstPad *pad,GstPadProbeInfo *info,gpointer unused) {
 (void)pad;(void)unused;GstBuffer *buffer=GST_PAD_PROBE_INFO_BUFFER(info);
 g_assert(gst_buffer_is_writable(buffer));
 gst_mini_object_weak_ref(GST_MINI_OBJECT(buffer),writable_finalized,NULL);
 g_atomic_int_inc(&writable_seen);return GST_PAD_PROBE_OK;
}
static void writable_ownership_probe(void) {
 GError *error=NULL;
 GstElement *pipeline=gst_parse_launch("fakesrc num-buffers=64 format=time is-live=true do-timestamp=true sizetype=fixed sizemax=4 ! video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! eufypacing name=p ! fakesink sync=false async=false enable-last-sample=false",&error);
 g_assert_no_error(error);GstElement *pacer=gst_bin_get_by_name(GST_BIN(pipeline),"p");
 GstPad *pad=gst_element_get_static_pad(pacer,"sink");
 gst_pad_add_probe(pad,GST_PAD_PROBE_TYPE_BUFFER,writable_capture,NULL,NULL);gst_object_unref(pad);
 GstBus *bus=gst_element_get_bus(pipeline);gst_element_set_state(pipeline,GST_STATE_PLAYING);
 GstMessage *done=gst_bus_timed_pop_filtered(bus,15*GST_SECOND,GST_MESSAGE_EOS|GST_MESSAGE_ERROR);
 g_assert(done && GST_MESSAGE_TYPE(done)==GST_MESSAGE_EOS);gst_message_unref(done);
 gst_element_set_state(pipeline,GST_STATE_NULL);gst_object_unref(pacer);gst_object_unref(bus);gst_object_unref(pipeline);
 printf("writableSeen=%d writableFreed=%d\n",writable_seen,writable_freed);
 g_assert(writable_seen==64);g_assert(writable_freed==64);
}
int main(int argc,char **argv) {
 for(int i=1;i<argc;i++){if(!strcmp(argv[i],"--require-identity"))require_identity=TRUE;if(!strcmp(argv[i],"--bad-diagnostic"))bad_diagnostic=TRUE;}
 gst_init(&argc,&argv);
 for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--writable-ownership")){writable_ownership_probe();return 0;}
 GError *error=NULL;
 GstElement *pipeline=gst_parse_launch("appsrc name=s is-live=true format=time caps=video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! eufypacing name=p ! fakesink name=o sync=false async=false signal-handoffs=true",&error);g_assert_no_error(error);
 GstElement *source=gst_bin_get_by_name(GST_BIN(pipeline),"s"),*pacer=gst_bin_get_by_name(GST_BIN(pipeline),"p"),*sink=gst_bin_get_by_name(GST_BIN(pipeline),"o");
 if(bad_diagnostic)g_object_set(pacer,"diagnostic-file","/tmp",NULL);
 GstPad *pad=gst_element_get_static_pad(pacer,"sink");gst_pad_add_probe(pad,GST_PAD_PROBE_TYPE_BUFFER,capture,NULL,NULL);gst_object_unref(pad);
 g_signal_connect(sink,"handoff",G_CALLBACK(handoff),NULL);gst_element_set_state(pipeline,GST_STATE_PLAYING);gst_element_get_state(pipeline,NULL,NULL,GST_SECOND);
 GstBuffer *buffer=gst_buffer_new_allocate(NULL,4,NULL);GST_BUFFER_PTS(buffer)=0;GST_BUFFER_OFFSET(buffer)=123;
 GstFlowReturn flow;g_signal_emit_by_name(source,"push-buffer",buffer,&flow);gst_buffer_unref(buffer);g_assert(flow==GST_FLOW_OK);
 for(guint n=0;n<1000 && !g_atomic_int_get(&seen);n++) { g_usleep(1000); }
 g_assert(g_atomic_int_get(&seen)==1);
 gst_element_set_state(pipeline,GST_STATE_NULL);gst_object_unref(source);gst_object_unref(pacer);gst_object_unref(sink);gst_object_unref(pipeline);
 gst_buffer_unref(input);
 if(require_identity)g_assert(g_atomic_int_get(&input_freed)==0);
 gst_buffer_unref(retained_output);g_assert(g_atomic_int_get(&input_freed)==1);
 puts("memory probe completed; retained child lifetime verified when required");return 0;
}
