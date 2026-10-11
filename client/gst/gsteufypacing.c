#include <gst/base/gstbasetransform.h>
#include "pacing.h"
#define PACKAGE "eufy-wall"
GST_DEBUG_CATEGORY_STATIC(eufy_pacing_debug);
#define GST_CAT_DEFAULT eufy_pacing_debug
typedef struct { GstBaseTransform parent; PacingClock clock; guint64 rendered, dropped; } GstEufyPacing;
typedef struct { GstBaseTransformClass parent; } GstEufyPacingClass;
G_DEFINE_TYPE(GstEufyPacing, gst_eufy_pacing, GST_TYPE_BASE_TRANSFORM)
static GstFlowReturn pace(GstBaseTransform *base, GstBuffer *buffer) {
  GstEufyPacing *self = (GstEufyPacing *)base;
  GstClock *clock = gst_element_get_clock(GST_ELEMENT(base));
  if (!clock) return GST_BASE_TRANSFORM_FLOW_DROPPED;
  GstClockTime now = gst_clock_get_time(clock) - gst_element_get_base_time(GST_ELEMENT(base));
  gst_object_unref(clock);
  GstClockTime raw_pts = GST_BUFFER_PTS(buffer);
  GstClockTime pts = gst_segment_to_running_time(&base->segment, GST_FORMAT_TIME, GST_BUFFER_PTS(buffer));
  int64_t target;
  if (!pacing_target(&self->clock, pts, now, GST_BUFFER_FLAG_IS_SET(buffer, GST_BUFFER_FLAG_DISCONT), &target)) {
    self->dropped++;
    return GST_BASE_TRANSFORM_FLOW_DROPPED;
  }
  GST_BUFFER_PTS(buffer) = gst_segment_position_from_running_time(&base->segment, GST_FORMAT_TIME, MAX(target, 0));
  GST_BUFFER_DTS(buffer) = GST_CLOCK_TIME_NONE;
  GST_DEBUG_OBJECT(base, "decoded rawPts=%" G_GUINT64_FORMAT " pts=%" G_GUINT64_FORMAT " now=%" G_GUINT64_FORMAT " target=%" G_GINT64_FORMAT " future=%" G_GINT64_FORMAT, raw_pts, pts, now, target, target - (int64_t)now);
  self->rendered++;
  return GST_FLOW_OK;
}
static gboolean query(GstBaseTransform *base, GstPadDirection direction, GstQuery *query) {
  if (direction == GST_PAD_SRC && GST_QUERY_TYPE(query) == GST_QUERY_LATENCY) {
    /* PTS already includes our allowance. Do not add source/decoder latency twice. */
    GST_DEBUG_OBJECT(base, "latency query: rebased PTS already contains allowance; extra latency=0");
    gst_query_set_latency(query, TRUE, 0, 0);
    return TRUE;
  }
  return GST_BASE_TRANSFORM_CLASS(gst_eufy_pacing_parent_class)->query(base, direction, query);
}
/* Downstream timing is rebased. Forwarding these QoS timestamps would make a
 * decoder still using source PTS treat valid encoded frames as seconds late.
 * This element already maps fresh late pictures to immediate output itself. */
static gboolean src_event(GstBaseTransform *base, GstEvent *event) {
  if (GST_EVENT_TYPE(event) == GST_EVENT_QOS) {
    gst_event_unref(event);
    return TRUE;
  }
  return GST_BASE_TRANSFORM_CLASS(gst_eufy_pacing_parent_class)->src_event(base, event);
}
static gboolean start(GstBaseTransform *base) {
  GstEufyPacing *self = (GstEufyPacing *)base;
  memset(&self->clock, 0, sizeof(self->clock)); self->rendered = self->dropped = 0;
  return TRUE;
}
static gboolean stop(GstBaseTransform *base) {
  GstEufyPacing *self = (GstEufyPacing *)base;
  GST_INFO_OBJECT(base, "pacing accepted=%" G_GUINT64_FORMAT " dropped=%" G_GUINT64_FORMAT, self->rendered, self->dropped);
  return TRUE;
}
static void gst_eufy_pacing_class_init(GstEufyPacingClass *klass) {
  GstElementClass *element = GST_ELEMENT_CLASS(klass);
  GstBaseTransformClass *transform = GST_BASE_TRANSFORM_CLASS(klass);
  GstCaps *caps = gst_caps_from_string("video/x-raw(ANY)");
  gst_element_class_add_pad_template(element, gst_pad_template_new("sink", GST_PAD_SINK, GST_PAD_ALWAYS, caps));
  gst_element_class_add_pad_template(element, gst_pad_template_new("src", GST_PAD_SRC, GST_PAD_ALWAYS, caps));
  gst_caps_unref(caps);
  gst_element_class_set_static_metadata(element, "Bounded decoded video pacing", "Filter/Video", "Rebase decoded PTS to a rolling local clock with a 67 ms allowance", "eufy-wall");
  transform->transform_ip = pace; transform->query = query; transform->src_event = src_event; transform->start = start; transform->stop = stop;
}
static void gst_eufy_pacing_init(GstEufyPacing *self) {
  gst_base_transform_set_in_place(GST_BASE_TRANSFORM(self), TRUE);
  gst_base_transform_set_passthrough(GST_BASE_TRANSFORM(self), FALSE);
}
static gboolean plugin_init(GstPlugin *plugin) { GST_DEBUG_CATEGORY_INIT(eufy_pacing_debug, "eufypacing", 0, "Decoded video pacing"); return gst_element_register(plugin, "eufypacing", GST_RANK_NONE, gst_eufy_pacing_get_type()); }
GST_PLUGIN_DEFINE(GST_VERSION_MAJOR, GST_VERSION_MINOR, eufypacing, "Bounded decoded video pacing", plugin_init, "1.0", "Proprietary", "eufy-wall", "https://github.com/sapireli/eufy-p2p-rtsp-bridge")
