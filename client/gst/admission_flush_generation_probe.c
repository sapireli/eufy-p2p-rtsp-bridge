/* Actual admission element and real GstPad scheduling; no camera/GPU input.
 * Compile once against the recorded pre-fix header and once against the fix. */
#include <gst/gst.h>
#include <string.h>
#define PACING_BOUND_NS 67000000ULL
static gboolean arrival_due(guint channel, guint64 pts, guint64 *target) {
  (void)channel; (void)pts; (void)target; return FALSE;
}
#ifndef ADMISSION_HEADER
#define ADMISSION_HEADER "decoded-admission.h"
#endif
#include ADMISSION_HEADER

typedef struct {
  GMutex lock; GCond changed;
  gboolean blocked, release_buffer, eos_blocked, release_eos, stop_started;
  guint buffers, eos_events, stop_events;
} Probe;
static Probe probe;
static GstPad *source;
static GstFlowReturn receive(GstPad *pad, GstObject *parent, GstBuffer *buffer) {
  (void)pad; (void)parent;
  g_mutex_lock(&probe.lock);
  probe.buffers++;
  gboolean old = probe.buffers == 1;
  if (old) {
    probe.blocked = TRUE; g_cond_broadcast(&probe.changed);
    while (!probe.release_buffer) g_cond_wait(&probe.changed, &probe.lock);
  }
  g_cond_broadcast(&probe.changed); g_mutex_unlock(&probe.lock);
  gst_buffer_unref(buffer);
  return old ? GST_FLOW_FLUSHING : GST_FLOW_OK;
}
static gboolean event(GstPad *pad, GstObject *parent, GstEvent *value) {
  (void)pad; (void)parent;
  g_mutex_lock(&probe.lock);
  if (GST_EVENT_TYPE(value) == GST_EVENT_EOS) {
    probe.eos_events++; probe.eos_blocked = TRUE;
    g_cond_broadcast(&probe.changed);
    while (!probe.release_eos) g_cond_wait(&probe.changed, &probe.lock);
  }
  if (GST_EVENT_TYPE(value) == GST_EVENT_FLUSH_STOP) probe.stop_events++;
  g_cond_broadcast(&probe.changed); g_mutex_unlock(&probe.lock);
  gst_event_unref(value); return TRUE;
}
static gpointer stop_flush(gpointer unused) {
  (void)unused;
  g_mutex_lock(&probe.lock); probe.stop_started = TRUE;
  g_cond_broadcast(&probe.changed); g_mutex_unlock(&probe.lock);
  g_assert(gst_pad_push_event(source, gst_event_new_flush_stop(TRUE)));
  return NULL;
}
static void wait_flag(gboolean *flag) {
  gint64 until = g_get_monotonic_time() + G_TIME_SPAN_SECOND;
  g_mutex_lock(&probe.lock);
  while (!*flag && g_get_monotonic_time() < until)
    g_cond_wait_until(&probe.changed, &probe.lock, until);
  g_assert(*flag); g_mutex_unlock(&probe.lock);
}
static void wait_buffers(guint count) {
  gint64 until = g_get_monotonic_time() + G_TIME_SPAN_SECOND;
  g_mutex_lock(&probe.lock);
  while (probe.buffers < count && g_get_monotonic_time() < until)
    g_cond_wait_until(&probe.changed, &probe.lock, until);
  g_assert(probe.buffers >= count); g_mutex_unlock(&probe.lock);
}
static void segment(void) {
  GstSegment value; gst_segment_init(&value, GST_FORMAT_TIME);
  g_assert(gst_pad_push_event(source, gst_event_new_segment(&value)));
}
static GstFlowReturn push(guint64 pts) {
  GstBuffer *buffer = gst_buffer_new_allocate(NULL, 4, NULL);
  GST_BUFFER_PTS(buffer) = pts;
  return gst_pad_push(source, buffer);
}
/* Give FLUSH_STOP a chance to enter the element. The old implementation clears
 * flushing before its serialized downstream push; the fix waits for old work. */
static gboolean reopened_while_busy(GstEufyAdmission *queue) {
  gint64 until = g_get_monotonic_time() + 100000;
  for (;;) {
    g_mutex_lock(&queue->lock);
    gboolean reopened = queue->busy && !queue->flushing;
    g_mutex_unlock(&queue->lock);
    if (reopened || g_get_monotonic_time() >= until) return reopened;
    g_usleep(1000);
  }
}
int main(int argc, char **argv) {
  gst_init(&argc, &argv); g_mutex_init(&probe.lock); g_cond_init(&probe.changed);
  GstElement *queue_element = g_object_new(gst_eufy_admission_get_type(), NULL);
  GstEufyAdmission *queue = (GstEufyAdmission *)queue_element;
  GstElement *sink = gst_element_factory_make("fakesink", NULL);
  g_assert(sink); g_object_set(sink, "async", FALSE, "sync", FALSE, NULL);
  GstPad *input = gst_element_get_static_pad(queue_element, "sink");
  GstPad *output = gst_element_get_static_pad(sink, "sink");
  gst_pad_set_chain_function(output, receive); gst_pad_set_event_function(output, event);
  source = gst_pad_new("source", GST_PAD_SRC); gst_pad_set_active(source, TRUE);
  g_assert(gst_pad_link(source, input) == GST_PAD_LINK_OK);
  g_assert(gst_element_link(queue_element, sink));
  gst_element_set_state(sink, GST_STATE_PLAYING);
  gst_element_set_state(queue_element, GST_STATE_PLAYING);
  g_assert(gst_pad_push_event(source, gst_event_new_stream_start("synthetic")));
  GstCaps *caps = gst_caps_from_string("video/x-raw,format=RGB,width=1,height=1,framerate=15/1");
  g_assert(gst_pad_push_event(source, gst_event_new_caps(caps))); gst_caps_unref(caps); segment();
  g_assert(push(0) == GST_FLOW_OK); wait_flag(&probe.blocked);
  g_assert(gst_pad_push_event(source, gst_event_new_flush_start()));
  GThread *thread = g_thread_new("flush-stop", stop_flush, NULL); wait_flag(&probe.stop_started);
  gboolean early_reopen = reopened_while_busy(queue);
  g_mutex_lock(&probe.lock); probe.release_buffer = TRUE;
  g_cond_broadcast(&probe.changed); g_mutex_unlock(&probe.lock); g_thread_join(thread);
  g_mutex_lock(&queue->lock);
  while (queue->busy) g_cond_wait(&queue->changed, &queue->lock);
  GstFlowReturn flow = queue->flow; g_mutex_unlock(&queue->lock);
  g_print("old-flow gate: earlyReopen=%d flowAfterFlushStop=%d\n", early_reopen, flow);
  gboolean failed = early_reopen || flow != GST_FLOW_OK;
  if (!failed) {
    segment(); g_assert(push(GST_SECOND) == GST_FLOW_OK); wait_buffers(2);
    g_assert(gst_pad_push_event(source, gst_event_new_eos())); wait_flag(&probe.eos_blocked);
    g_assert(gst_pad_push_event(source, gst_event_new_flush_start()));
    probe.stop_started = FALSE;
    thread = g_thread_new("eos-flush-stop", stop_flush, NULL); wait_flag(&probe.stop_started);
    early_reopen = reopened_while_busy(queue);
    g_mutex_lock(&probe.lock); probe.release_eos = TRUE;
    g_cond_broadcast(&probe.changed); g_mutex_unlock(&probe.lock); g_thread_join(thread);
    failed = early_reopen;
    segment(); g_assert(push(2 * GST_SECOND) == GST_FLOW_OK); wait_buffers(3);
    g_print("EOS gate: earlyReopen=%d eosEvents=%u stopEvents=%u resumedBuffers=%u\n",
            early_reopen, probe.eos_events, probe.stop_events, probe.buffers - 1);
  }
  gst_element_set_state(queue_element, GST_STATE_NULL); gst_element_set_state(sink, GST_STATE_NULL);
  gst_pad_set_active(source, FALSE); gst_object_unref(source); gst_object_unref(input);
  gst_object_unref(output); gst_object_unref(queue_element); gst_object_unref(sink);
  g_mutex_clear(&probe.lock); g_cond_clear(&probe.changed);
  return failed ? 1 : 0;
}
