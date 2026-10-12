/* Native GStreamer integration test: encoded/raw PTS association and no-input
 * expiry. Two appsrc branches isolate metadata from raw GstBuffer creation: no
 * decoder GstMeta is reused. */
#include <gst/gst.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
static gint encoded_count, raw_count;
static void handoff(GstElement *s, GstBuffer *b, GstPad *p, gpointer count) {
  (void)s;
  (void)b;
  (void)p;
  g_atomic_int_inc((gint *)count);
}
static void be(uint8_t *p, uint64_t v, unsigned n) {
  while (n) {
    p[--n] = (uint8_t)v;
    v >>= 8;
  }
}
static void push_encoded(GstElement *s, guint64 pts, uint64_t ingress,
                         uint64_t frame) {
  uint8_t payload[48] = {0}, nal[128] = {0, 0, 0, 1, 6, 5, 48};
  memcpy(payload, "EWFRESHNESS00001", 16);
  payload[16] = 1;
  payload[17] = 1;
  be(payload + 18, 500, 2);
  memcpy(payload + 20, "epoch123", 8);
  be(payload + 32, ingress, 8);
  be(payload + 40, frame, 8);
  size_t len = 7;
  unsigned zeros = 0;
  for (unsigned i = 0; i < 48; i++) {
    if (zeros >= 2 && payload[i] <= 3) {
      nal[len++] = 3;
      zeros = 0;
    }
    nal[len++] = payload[i];
    zeros = payload[i] == 0 ? zeros + 1 : 0;
  }
  nal[len++] = 128;
  GstBuffer *b = gst_buffer_new_allocate(NULL, len, NULL);
  gst_buffer_fill(b, 0, nal, len);
  GST_BUFFER_PTS(b) = pts;
  GstFlowReturn r;
  g_signal_emit_by_name(s, "push-buffer", b, &r);
  gst_buffer_unref(b);
  g_assert(r == GST_FLOW_OK);
}
static void push_raw(GstElement *s, guint64 pts) {
  GstBuffer *b = gst_buffer_new_allocate(NULL, 4, NULL);
  GST_BUFFER_PTS(b) = pts;
  GstFlowReturn r;
  g_signal_emit_by_name(s, "push-buffer", b, &r);
  gst_buffer_unref(b);
  g_assert(r == GST_FLOW_OK);
}
int main(int argc, char **argv) {
  gst_init(&argc, &argv);
  uint64_t now = (uint64_t)g_get_monotonic_time() * 1000;
  uint8_t clock[48] = {0};
  memcpy(clock, "EWCLK001epoch123", 16);
  be(clock + 16, now / 1000, 8);
  be(clock + 24, now, 8);
  be(clock + 32, now, 8);
  be(clock + 40, now + 5000000000ULL, 8);
  const char *path = "/tmp/eufy-freshness-probe-clock";
  g_assert(g_file_set_contents(path, (char *)clock, 48, NULL));
  GError *err = NULL;
  GstElement *pipe = gst_parse_launch(
      "appsrc name=encoded is-live=true format=time "
      "caps=video/x-h264,stream-format=byte-stream,alignment=au ! eufystamp "
      "channel=7 ! fakesink name=encoded_sink sync=false async=false "
      "signal-handoffs=true appsrc name=raw is-live=true format=time "
      "caps=video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! "
      "eufypacing channel=7 clock-file=/tmp/eufy-freshness-probe-clock ! "
      "fakesink name=raw_sink sync=false async=false signal-handoffs=true",
      &err);
  g_assert_no_error(err);
  GstElement *encoded = gst_bin_get_by_name(GST_BIN(pipe), "encoded"),
             *raw = gst_bin_get_by_name(GST_BIN(pipe), "raw"),
             *es = gst_bin_get_by_name(GST_BIN(pipe), "encoded_sink"),
             *rs = gst_bin_get_by_name(GST_BIN(pipe), "raw_sink");
  g_signal_connect(es, "handoff", G_CALLBACK(handoff), &encoded_count);
  g_signal_connect(rs, "handoff", G_CALLBACK(handoff), &raw_count);
  gst_element_set_state(pipe, GST_STATE_PLAYING);
  gst_element_get_state(pipe, NULL, NULL, GST_SECOND);
  push_encoded(encoded, 0, now / 1000, 1);
  for (int i = 0; i < 100 && !g_atomic_int_get(&encoded_count); i++)
    g_usleep(1000);
  g_assert(g_atomic_int_get(&encoded_count) == 1);
  push_raw(raw, 0);
  g_usleep(100000);
  g_assert(g_atomic_int_get(&raw_count) == 1);
  push_encoded(encoded, 67000000, now / 1000 - 600000, 2);
  for (int i = 0; i < 100 && g_atomic_int_get(&encoded_count) < 2; i++)
    g_usleep(1000);
  push_raw(raw, 67000000);
  push_raw(raw, 134000000);
  g_usleep(30000);
  g_assert(g_atomic_int_get(&raw_count) == 1);
  /* No more buffers: clear/expire without terminating the decoder pipeline. */
  GstBus *bus = gst_element_get_bus(pipe);
  GstMessage *m = gst_bus_timed_pop_filtered(
      bus, GST_SECOND, GST_MESSAGE_ELEMENT | GST_MESSAGE_ERROR);
  g_assert(m != NULL);
  g_assert(GST_MESSAGE_TYPE(m) == GST_MESSAGE_ELEMENT);
  g_assert(gst_structure_has_name(gst_message_get_structure(m),
                                  "freshness-expired"));
  gst_message_unref(m);
  /* A new fresh picture resumes in the same pipeline/reference state. */
  uint64_t recovered = (uint64_t)g_get_monotonic_time() * 1000;
  push_encoded(encoded, 200000000, recovered / 1000, 3);
  for (int i = 0; i < 100 && g_atomic_int_get(&encoded_count) < 3; i++)
    g_usleep(1000);
  push_raw(raw, 200000000);
  g_usleep(100000);
  g_assert(g_atomic_int_get(&raw_count) == 2);
  m = gst_bus_timed_pop_filtered(bus, 0, GST_MESSAGE_ERROR);
  g_assert(m == NULL);
  g_print("freshness integration: fresh=1 stale/missing=2 suppressed; no-input "
          "expiry fired; recovered without restart\n");
  gst_object_unref(bus);
  gst_element_set_state(pipe, GST_STATE_NULL);
  gst_object_unref(encoded);
  gst_object_unref(raw);
  gst_object_unref(es);
  gst_object_unref(rs);
  gst_object_unref(pipe);
  remove(path);
  return 0;
}
