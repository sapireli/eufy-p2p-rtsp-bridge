/* Headless hardware-path diagnostic. No DRM/display state changes. */
#include <gst/gst.h>
#include <stdio.h>
#include <dlfcn.h>
/* Match KMS' video-meta and minimum-buffer allocation without opening DRM.
 * The runtime library is already present on the hardware client. */
static GstPadProbeReturn allocation(GstPad *pad, GstPadProbeInfo *info, gpointer type) {
  (void)pad;
  GstQuery *query = GST_PAD_PROBE_INFO_QUERY(info);
  if (GST_QUERY_TYPE(query) != GST_QUERY_ALLOCATION) return GST_PAD_PROBE_OK;
  gst_query_add_allocation_meta(query, (GType)(gsize)type, NULL);
  gst_query_add_allocation_pool(query, NULL, 0, 2, 0);
  fprintf(stderr, "KMS-equivalent allocation: video-meta, min-buffers=2\n");
  return GST_PAD_PROBE_HANDLED;
}
static GstPadProbeReturn stamp(GstPad *pad, GstPadProbeInfo *info, gpointer label) {
  (void)pad;
  GstBuffer *buffer = GST_PAD_PROBE_INFO_BUFFER(info);
  printf("%s,%" G_GUINT64_FORMAT ",%" G_GUINT64_FORMAT "\n", (char *)label, gst_util_get_timestamp(), GST_BUFFER_PTS(buffer));
  return GST_PAD_PROBE_OK;
}
static GstPadProbeReturn qos(GstPad *pad, GstPadProbeInfo *info, gpointer label) {
  (void)pad;
  if (GST_EVENT_TYPE(GST_PAD_PROBE_INFO_EVENT(info)) == GST_EVENT_QOS)
    printf("%s,%" G_GUINT64_FORMAT ",0\n", (char *)label, gst_util_get_timestamp());
  return GST_PAD_PROBE_OK;
}
static void render(GstElement *sink, GstBuffer *buffer, GstPad *pad, gpointer data) {
  (void)sink; (void)pad; (void)data;
  printf("render,%" G_GUINT64_FORMAT ",%" G_GUINT64_FORMAT "\n", gst_util_get_timestamp(), GST_BUFFER_PTS(buffer));
}
int main(int argc, char **argv) {
  gst_init(&argc, &argv);
  if (argc != 2) { fprintf(stderr, "usage: pacing-probe 'pipeline with elements named encoded, decoder, pacer, output'\n"); return 2; }
  GError *error = NULL;
  GstElement *pipeline = gst_parse_launch(argv[1], &error);
  if (error) { fprintf(stderr, "%s\n", error->message); return 1; }
  const char *names[] = {"encoded", "decoder", "pacer"};
  const char *labels[] = {"encoded", "decoded", "pacer-arrival"};
  for (unsigned i = 0; i < 3; i++) {
    GstElement *element = gst_bin_get_by_name(GST_BIN(pipeline), names[i]);
    if (!element) return 2;
    GstPad *pad = gst_element_get_static_pad(element, i == 2 ? "sink" : "src");
    gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_BUFFER, stamp, (gpointer)labels[i], NULL);
    if (i == 0) gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_EVENT_UPSTREAM, qos, "qos-upstream", NULL);
    gst_object_unref(pad); gst_object_unref(element);
  }
  GstElement *output = gst_bin_get_by_name(GST_BIN(pipeline), "output");
  if (!output) return 2;
  g_signal_connect(output, "handoff", G_CALLBACK(render), NULL);
  GstPad *output_pad = gst_element_get_static_pad(output, "sink");
  gst_pad_add_probe(output_pad, GST_PAD_PROBE_TYPE_EVENT_UPSTREAM, qos, "qos-downstream", NULL);
  gst_object_unref(output_pad);
  if (g_getenv("EUFY_PROBE_KMS_ALLOCATION")) {
    void *library = dlopen("libgstvideo-1.0.so.0", RTLD_NOW);
    GType (*get_type)(void) = library ? (GType (*)(void))dlsym(library, "gst_video_meta_api_get_type") : NULL;
    if (!get_type) { fprintf(stderr, "Cannot load video-meta allocation type\n"); return 2; }
    GstPad *pad = gst_element_get_static_pad(output, "sink");
    gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_QUERY_DOWNSTREAM, allocation, (gpointer)(gsize)get_type(), NULL);
    gst_object_unref(pad);
  }
  gst_object_unref(output);
  gst_element_set_state(pipeline, GST_STATE_PLAYING);
  GstBus *bus = gst_element_get_bus(pipeline);
  guint seconds = g_getenv("EUFY_PROBE_SECONDS") ? MAX(1, atoi(g_getenv("EUFY_PROBE_SECONDS"))) : 90;
  GstMessage *message = gst_bus_timed_pop_filtered(bus, seconds * GST_SECOND, GST_MESSAGE_ERROR | GST_MESSAGE_EOS);
  int status = 0;
  if (message && GST_MESSAGE_TYPE(message) == GST_MESSAGE_ERROR) {
    gchar *debug; gst_message_parse_error(message, &error, &debug);
    fprintf(stderr, "%s: %s\n", error->message, debug ? debug : ""); g_free(debug); g_error_free(error); status = 1;
  }
  if (message) gst_message_unref(message);
  gst_element_set_state(pipeline, GST_STATE_NULL);
  gst_object_unref(bus); gst_object_unref(pipeline);
  return status;
}
