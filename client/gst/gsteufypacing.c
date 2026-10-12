#define _GNU_SOURCE
#include "freshness.h"
#include "pacing.h"
#include <gst/base/gstbasetransform.h>
#include <stdio.h>
#include <sys/ioctl.h>
#include <unistd.h>
#include "timing-observer.h"
#define PACKAGE "eufy-wall"
GST_DEBUG_CATEGORY_STATIC(eufy_pacing_debug);
#define GST_CAT_DEFAULT eufy_pacing_debug
typedef struct {
  GstBaseTransform parent;
  PacingClock clock;
  guint64 rendered, dropped;
  gboolean observe_only;
  gchar *diagnostic_file;
  TimingObserver timing;
  guint64 observed, matched, missing, unknown_clock, late_observed, reordered;
  guint64 submit_matched;
  guint64 age_max, submit_age_max, uncertainty_max, sink_push_max, longest_gap;
  guint64 last_observed, stats_at, previous_frame;
  guint64 age_buckets[101], submit_buckets[101];
  guint8 observed_epoch[8];
  gboolean have_observed_epoch;
  guint8 observed_clock[48];
  guint64 clock_read_at;
  gboolean observed_clock_valid;
  guint channel, plane, max_age_ms;
  guint64 last_frame;
  guint8 last_epoch[8];
  gboolean have_frame;
  gint drm_fd;
  gchar *clock_file;
  GMutex lock;
  GCond changed;
  GThread *timer;
  gboolean stopping;
  guint64 expired_count;
  guint64 deadline;
} GstEufyPacing;
typedef struct {
  guint channel;
  guint64 pts;
  FreshStamp stamp;
} StampEntry;
static GMutex registry_lock;
static StampEntry registry[256];
static guint registry_next;
static void remember(guint channel, guint64 pts, const FreshStamp *stamp) {
  g_mutex_lock(&registry_lock);
  registry[registry_next++ % 256] = (StampEntry){channel, pts, *stamp};
  g_mutex_unlock(&registry_lock);
}
static gboolean lookup_entry(guint channel, guint64 pts, FreshStamp *stamp, gboolean consume) {
  gboolean found = FALSE;
  g_mutex_lock(&registry_lock);
  for (guint offset = 0; offset < 256; offset++) {
    guint i=(registry_next+255-offset)%256;
    if (registry[i].channel == channel && registry[i].pts == pts) {
      *stamp = registry[i].stamp;
      if (consume) registry[i].channel = 0;
      found = TRUE;
      break;
    }
  }
  g_mutex_unlock(&registry_lock);
  return found;
}
static gboolean lookup(guint channel, guint64 pts, FreshStamp *stamp) {
  return lookup_entry(channel,pts,stamp,TRUE);
}
static gboolean peek(guint channel, guint64 pts, FreshStamp *stamp) {
  return lookup_entry(channel,pts,stamp,FALSE);
}
static gboolean clear_plane(GstEufyPacing *self) {
  if (self->drm_fd >= 0 && self->plane) {
    guint32 state[12] = {0};
    state[0] = self->plane;
    if (ioctl(self->drm_fd, 0xc03064b7UL, state) < 0) {
      GST_ERROR_OBJECT(self, "failed disabling expired DRM plane");
      return FALSE;
    }
  }
  return TRUE;
}
/* Sleep on the actual deadline, not a high-frequency polling loop on the Pi. */
static gpointer expiry(gpointer data) {
  GstEufyPacing *self = data;
  g_mutex_lock(&self->lock);
  for (;;) {
    if (self->stopping) {
      g_mutex_unlock(&self->lock);
      return NULL;
    }
    if (!self->deadline) {
      g_cond_wait(&self->changed, &self->lock);
      continue;
    }
    if ((guint64)g_get_monotonic_time() * 1000 < self->deadline) {
      g_cond_wait_until(&self->changed, &self->lock,
                        (gint64)(self->deadline / 1000));
      continue;
    }
    self->deadline = 0;
    self->expired_count++;
    if (!clear_plane(self)) {
      g_mutex_unlock(&self->lock);
      GST_ELEMENT_ERROR(self, RESOURCE, FAILED,
                        ("Cannot clear expired video plane"),
                        ("DRM plane disable failed"));
      return NULL;
    }
    /* A missed picture deadline is not a decoder failure. Preserve references
     * and keep receiving video; a subsequent fresh picture can resume output.
     */
    gst_element_post_message(
        GST_ELEMENT(self),
        gst_message_new_element(
            GST_OBJECT(self),
            gst_structure_new("freshness-expired", "channel", G_TYPE_UINT,
                              self->channel, "plane", G_TYPE_UINT, self->plane,
                              NULL)));
  }
}
static gboolean stamp_deadline(GstEufyPacing *self, const FreshStamp *stamp,
                               guint64 now, guint64 *deadline) {
  uint8_t clock[48];
  FILE *file = fopen(self->clock_file, "rb");
  if (!file)
    return FALSE;
  size_t n = fread(clock, 1, 48, file);
  fclose(file);
  gboolean valid = n == 48 && fresh_deadline(stamp, clock, now, deadline);
  if (n == 48)
    GST_DEBUG_OBJECT(
        self,
        "freshness frame=%" G_GUINT64_FORMAT " rawIngressUs=%" G_GUINT64_FORMAT
        " nowNs=%" G_GUINT64_FORMAT " clockRttNs=%" G_GUINT64_FORMAT
        " clockSampleNs=%" G_GUINT64_FORMAT " deadlineNs=%" G_GUINT64_FORMAT
        " remainingNs=%" G_GINT64_FORMAT " eligible=%d",
        stamp->frame, stamp->ingress_us, now,
        fresh_be(clock + 32, 8) - fresh_be(clock + 24, 8),
        fresh_be(clock + 32, 8), *deadline, (gint64)*deadline - (gint64)now,
        valid);
  return valid;
}
static gboolean frame_deadline(GstEufyPacing *self, guint64 pts, guint64 now,
                               guint64 *deadline, FreshStamp *stamp) {
  if (!lookup(self->channel, pts, stamp)) {
    GST_DEBUG_OBJECT(self, "freshness missing stamp pts=%" G_GUINT64_FORMAT,
                     pts);
    return FALSE;
  }
  if (self->have_frame && !memcmp(self->last_epoch, stamp->epoch, 8) &&
      stamp->frame <= self->last_frame) {
    GST_DEBUG_OBJECT(self,
                     "freshness reordered frame=%" G_GUINT64_FORMAT
                     " last=%" G_GUINT64_FORMAT,
                     stamp->frame, self->last_frame);
    return FALSE;
  }
  if (stamp->max_age_ms > self->max_age_ms)
    stamp->max_age_ms = self->max_age_ms;
  if (!stamp_deadline(self, stamp, now, deadline))
    return FALSE;
  self->have_frame = TRUE;
  self->last_frame = stamp->frame;
  memcpy(self->last_epoch, stamp->epoch, 8);
  return TRUE;
}

/* Observation never clears planes, rejects a deadline, or changes pacing. */
static gboolean observation_age(GstEufyPacing *self, const FreshStamp *stamp,
                                guint64 now, guint64 *upper) {
  if (!self->observed_clock_valid || now-self->clock_read_at >= 100000000 ||
      memcmp(self->observed_clock+8, stamp->epoch, 8)) {
    FILE *file = fopen(self->clock_file, "rb");
    self->observed_clock_valid = file && fread(self->observed_clock, 1, 48, file)==48;
    if (file) fclose(file);
    self->clock_read_at = now;
  }
  guint64 lower, uncertainty;
  gboolean valid = self->observed_clock_valid &&
      fresh_age_bounds(stamp, self->observed_clock, now, &lower, upper, &uncertainty);
  if (valid) self->uncertainty_max=MAX(self->uncertainty_max,uncertainty);
  return valid;
}
static guint64 histogram_percentile(guint64 *values, guint64 count, guint numerator, guint64 maximum) {
  guint64 target=(count*numerator+99)/100, seen=0;
  if (!count) return 0;
  for (guint i=0;i<101;i++) {seen+=values[i];if(seen>=target)return i==100?maximum:(guint64)(i+1)*25000000;}
  return 0;
}
static void observation_report(GstEufyPacing *self, guint64 now, gboolean force) {
  if (!self->observe_only) return;
  if (!self->stats_at) self->stats_at=now;
  if (!force && now-self->stats_at<5000000000ULL) return;
  g_printerr("eufy-observe channel=%u scope=decoded-after-queue-and-kms-return"
      " observed=%"G_GUINT64_FORMAT" matched=%"G_GUINT64_FORMAT
      " submitMatched=%"G_GUINT64_FORMAT" missing=%"G_GUINT64_FORMAT" unknownClock=%"G_GUINT64_FORMAT
      " expiredUpper=%"G_GUINT64_FORMAT" reordered=%"G_GUINT64_FORMAT
      " frameId=%"G_GUINT64_FORMAT" ageUpperP50Ns=%"G_GUINT64_FORMAT
      " ageUpperP95Ns=%"G_GUINT64_FORMAT" ageUpperMaxNs=%"G_GUINT64_FORMAT
      " submitUpperP95Ns=%"G_GUINT64_FORMAT" submitUpperMaxNs=%"G_GUINT64_FORMAT
      " clockUncertaintyMaxNs=%"G_GUINT64_FORMAT" sinkPushMaxNs=%"G_GUINT64_FORMAT
      " longestDecodedGapNs=%"G_GUINT64_FORMAT" quietNowNs=%"G_GUINT64_FORMAT" accepted=%"G_GUINT64_FORMAT
      " pacingDropped=%"G_GUINT64_FORMAT" scanoutVerified=false\n",
      self->channel,self->observed,self->matched,self->submit_matched,self->missing,self->unknown_clock,
      self->late_observed,self->reordered,self->previous_frame,
      histogram_percentile(self->age_buckets,self->matched,50,self->age_max),
      histogram_percentile(self->age_buckets,self->matched,95,self->age_max),self->age_max,
      histogram_percentile(self->submit_buckets,self->submit_matched,95,self->submit_age_max),self->submit_age_max,
      self->uncertainty_max,self->sink_push_max,self->longest_gap,
      self->last_observed ? now-self->last_observed : 0,self->rendered,self->dropped);
  self->stats_at=now;
}
static gboolean observe_frame(GstEufyPacing *self, guint64 pts, guint64 now, FreshStamp *stamp) {
  self->observed++;
  if (self->last_observed) self->longest_gap=MAX(self->longest_gap,now-self->last_observed);
  self->last_observed=now;
  if (!lookup(self->channel,pts,stamp)) {self->missing++;return FALSE;}
  if (self->have_observed_epoch && !memcmp(self->observed_epoch,stamp->epoch,8) &&
      stamp->frame<=self->previous_frame) self->reordered++;
  self->have_observed_epoch=TRUE;self->previous_frame=stamp->frame;
  memcpy(self->observed_epoch,stamp->epoch,8);
  guint64 upper;
  if (!observation_age(self,stamp,now,&upper)) {self->unknown_clock++;return FALSE;}
  self->matched++;self->age_max=MAX(self->age_max,upper);
  self->age_buckets[MIN(upper/25000000,100)]++;
  if (upper>(guint64)MIN(stamp->max_age_ms,self->max_age_ms)*1000000) self->late_observed++;
  return TRUE;
}

typedef struct {
  GstBaseTransformClass parent;
} GstEufyPacingClass;
G_DEFINE_TYPE(GstEufyPacing, gst_eufy_pacing, GST_TYPE_BASE_TRANSFORM)
static GstFlowReturn pace(GstBaseTransform *base, GstBuffer *buffer) {
  GstEufyPacing *self = (GstEufyPacing *)base;
  GstClock *clock = gst_element_get_clock(GST_ELEMENT(base));
  if (!clock)
    return GST_BASE_TRANSFORM_FLOW_DROPPED;
  GstClockTime now =
      gst_clock_get_time(clock) - gst_element_get_base_time(GST_ELEMENT(base));
  gst_object_unref(clock);
  GstClockTime raw_pts = GST_BUFFER_PTS(buffer);
  GstClockTime pts = gst_segment_to_running_time(
      &base->segment, GST_FORMAT_TIME, GST_BUFFER_PTS(buffer));
  FreshStamp diagnostic_stamp;
  gboolean diagnostic_found=self->timing.file && peek(self->channel,raw_pts,&diagnostic_stamp);
  FreshStamp stamp;
  guint64 deadline = 0, mono = (guint64)g_get_monotonic_time() * 1000;
  gboolean observed_stamp = self->observe_only && self->clock_file &&
      observe_frame(self,raw_pts,mono,&stamp);
  if (self->clock_file && !self->observe_only &&
      !frame_deadline(self, raw_pts, mono, &deadline, &stamp)) {
    self->dropped++;
    return GST_BASE_TRANSFORM_FLOW_DROPPED;
  }
  int64_t target;
  if (!pacing_target(&self->clock, pts, now,
                     GST_BUFFER_FLAG_IS_SET(buffer, GST_BUFFER_FLAG_DISCONT),
                     &target)) {
    self->dropped++;
    return GST_BASE_TRANSFORM_FLOW_DROPPED;
  }
  if (self->clock_file && !self->observe_only) {
    guint64 wait = target > (int64_t)now ? (guint64)target - now : 0;
    /* Keep scanout margin; never spend the freshness budget merely pacing. */
    if (deadline <= mono + wait + 20000000) {
      self->dropped++;
      return GST_BASE_TRANSFORM_FLOW_DROPPED;
    }
    guint64 target_mono = mono + wait;
    g_mutex_lock(&self->lock);
    while (!self->stopping &&
           (guint64)g_get_monotonic_time() * 1000 < target_mono)
      g_cond_wait_until(&self->changed, &self->lock,
                        (gint64)(target_mono / 1000));
    mono = (guint64)g_get_monotonic_time() * 1000;
    guint64 checked_deadline = 0;
    if (self->stopping ||
        !stamp_deadline(self, &stamp, mono, &checked_deadline) ||
        MIN(deadline, checked_deadline) <= mono + 20000000) {
      self->dropped++;
      g_mutex_unlock(&self->lock);
      return GST_BASE_TRANSFORM_FLOW_DROPPED;
    }
    self->deadline = MIN(deadline, checked_deadline) - 20000000;
    GST_BUFFER_PTS(buffer) = gst_segment_position_from_running_time(
        &base->segment, GST_FORMAT_TIME, MAX(target, 0));
    GST_BUFFER_DTS(buffer) = GST_CLOCK_TIME_NONE;
    /* Strict pipelines have no downstream queue and a sync=false sink. Push
     * synchronously under the same lock as plane clearing: a buffer cannot wait
     * in a sink clock queue and reappear after its deadline cleared the plane.
     */
    guint64 push_start = (guint64)g_get_monotonic_time() * 1000;
    GstFlowReturn flow =
        gst_pad_push(GST_BASE_TRANSFORM_SRC_PAD(base), gst_buffer_ref(buffer));
    guint64 push_end = (guint64)g_get_monotonic_time() * 1000;
    GST_DEBUG_OBJECT(base,
                     "fresh-output frame=%" G_GUINT64_FORMAT
                     " startNs=%" G_GUINT64_FORMAT " doneNs=%" G_GUINT64_FORMAT
                     " pushNs=%" G_GUINT64_FORMAT
                     " deadlineNs=%" G_GUINT64_FORMAT,
                     stamp.frame, push_start, push_end, push_end - push_start,
                     self->deadline);
    timing_record(&self->timing,self->clock_file,"kms-return",self->channel,
        raw_pts,push_end,&diagnostic_stamp,diagnostic_found,gst_buffer_get_size(buffer),0,0);
    if (flow == GST_FLOW_OK)
      self->rendered++;
    g_cond_broadcast(&self->changed);
    g_mutex_unlock(&self->lock);
    return flow == GST_FLOW_OK ? GST_BASE_TRANSFORM_FLOW_DROPPED : flow;
  }
  GST_BUFFER_PTS(buffer) = gst_segment_position_from_running_time(
      &base->segment, GST_FORMAT_TIME, MAX(target, 0));
  GST_BUFFER_DTS(buffer) = GST_CLOCK_TIME_NONE;
  GST_DEBUG_OBJECT(base,
                   "decoded rawPts=%" G_GUINT64_FORMAT " pts=%" G_GUINT64_FORMAT
                   " now=%" G_GUINT64_FORMAT " target=%" G_GINT64_FORMAT
                   " future=%" G_GINT64_FORMAT,
                   raw_pts, pts, now, target, target - (int64_t)now);
  if (self->observe_only) {
    guint64 push_start=(guint64)g_get_monotonic_time()*1000;
    GstFlowReturn flow=gst_pad_push(GST_BASE_TRANSFORM_SRC_PAD(base),gst_buffer_ref(buffer));
    guint64 push_end=(guint64)g_get_monotonic_time()*1000;
    self->sink_push_max=MAX(self->sink_push_max,push_end-push_start);
    timing_record(&self->timing,self->clock_file,"kms-return",self->channel,
        raw_pts,push_end,&diagnostic_stamp,diagnostic_found,gst_buffer_get_size(buffer),0,0);
    if (flow==GST_FLOW_OK) {
      self->rendered++;
      guint64 upper;
      if (observed_stamp && observation_age(self,&stamp,push_end,&upper)) {
        self->submit_matched++;
        self->submit_age_max=MAX(self->submit_age_max,upper);
        self->submit_buckets[MIN(upper/25000000,100)]++;
      }
    }
    observation_report(self,push_end,FALSE);
    return flow==GST_FLOW_OK?GST_BASE_TRANSFORM_FLOW_DROPPED:flow;
  }
  self->rendered++;
  return GST_FLOW_OK;
}
static gboolean query(GstBaseTransform *base, GstPadDirection direction,
                      GstQuery *query) {
  if (direction == GST_PAD_SRC && GST_QUERY_TYPE(query) == GST_QUERY_LATENCY) {
    /* PTS already includes our allowance. Do not add source/decoder latency
     * twice. */
    GST_DEBUG_OBJECT(base, "latency query: rebased PTS already contains "
                           "allowance; extra latency=0");
    gst_query_set_latency(query, TRUE, 0, 0);
    return TRUE;
  }
  return GST_BASE_TRANSFORM_CLASS(gst_eufy_pacing_parent_class)
      ->query(base, direction, query);
}
/* Downstream timing is rebased. Forwarding these QoS timestamps would make a
 * decoder still using source PTS treat valid encoded frames as seconds late.
 * This element already maps fresh late pictures to immediate output itself. */
static gboolean src_event(GstBaseTransform *base, GstEvent *event) {
  if (GST_EVENT_TYPE(event) == GST_EVENT_QOS) {
    gst_event_unref(event);
    return TRUE;
  }
  return GST_BASE_TRANSFORM_CLASS(gst_eufy_pacing_parent_class)
      ->src_event(base, event);
}
static gboolean start(GstBaseTransform *base) {
  GstEufyPacing *self = (GstEufyPacing *)base;
  if (!timing_open(&self->timing,self->diagnostic_file)) return FALSE;
  memset(&self->clock, 0, sizeof(self->clock));
  self->rendered = self->dropped = 0;
  self->observed = self->matched = self->missing = self->unknown_clock = 0;
  self->late_observed = self->reordered = self->submit_matched = 0;
  self->age_max = self->submit_age_max = self->uncertainty_max = 0;
  self->sink_push_max = self->longest_gap = self->last_observed = 0;
  self->stats_at = self->previous_frame = self->clock_read_at = 0;
  self->have_observed_epoch = self->observed_clock_valid = FALSE;
  memset(self->age_buckets, 0, sizeof(self->age_buckets));
  memset(self->submit_buckets, 0, sizeof(self->submit_buckets));
  if (self->clock_file && !self->observe_only) {
    self->stopping = FALSE;
    self->expired_count = 0;
    self->have_frame = FALSE;
    self->deadline = 0;
    clear_plane(self);
    self->timer = g_thread_new("fresh-expiry", expiry, self);
  }
  return TRUE;
}
static gboolean stop(GstBaseTransform *base) {
  GstEufyPacing *self = (GstEufyPacing *)base;
  g_mutex_lock(&self->lock);
  self->stopping = TRUE;
  g_cond_signal(&self->changed);
  g_mutex_unlock(&self->lock);
  if (self->timer) {
    g_thread_join(self->timer);
    self->timer = NULL;
  }
  if (self->clock_file && !self->observe_only)
    clear_plane(self);
  observation_report(self,(guint64)g_get_monotonic_time()*1000,TRUE);
  timing_close(&self->timing);
  GST_INFO_OBJECT(base,
                  "pacing accepted=%" G_GUINT64_FORMAT
                  " dropped=%" G_GUINT64_FORMAT " expiries=%" G_GUINT64_FORMAT,
                  self->rendered, self->dropped, self->expired_count);
  return TRUE;
}
enum {
  PROP_0,
  PROP_CHANNEL,
  PROP_CLOCK_FILE,
  PROP_DRM_FD,
  PROP_PLANE,
  PROP_MAX_AGE,
  PROP_OBSERVE_ONLY,
  PROP_DIAGNOSTIC_FILE
};
static void set_prop(GObject *object, guint prop, const GValue *value,
                     GParamSpec *spec) {
  GstEufyPacing *s = (GstEufyPacing *)object;
  switch (prop) {
  case PROP_CHANNEL:
    s->channel = g_value_get_uint(value);
    break;
  case PROP_CLOCK_FILE:
    g_free(s->clock_file);
    s->clock_file = g_value_dup_string(value);
    break;
  case PROP_DRM_FD:
    s->drm_fd = g_value_get_int(value);
    break;
  case PROP_PLANE:
    s->plane = g_value_get_uint(value);
    break;
  case PROP_DIAGNOSTIC_FILE:
    g_free(s->diagnostic_file);s->diagnostic_file=g_value_dup_string(value);break;
  case PROP_OBSERVE_ONLY:
    s->observe_only=g_value_get_boolean(value);
    break;
  case PROP_MAX_AGE:
    s->max_age_ms = g_value_get_uint(value);
    break;
  default:
    G_OBJECT_WARN_INVALID_PROPERTY_ID(object, prop, spec);
  }
}
static void finalize(GObject *object) {
  GstEufyPacing *s = (GstEufyPacing *)object;
  g_free(s->clock_file);
  timing_close(&s->timing);g_free(s->diagnostic_file);
  g_mutex_clear(&s->lock);
  g_cond_clear(&s->changed);
  G_OBJECT_CLASS(gst_eufy_pacing_parent_class)->finalize(object);
}
static void gst_eufy_pacing_class_init(GstEufyPacingClass *klass) {
  GObjectClass *object = G_OBJECT_CLASS(klass);
  object->set_property = set_prop;
  object->finalize = finalize;
  g_object_class_install_property(
      object, PROP_CHANNEL,
      g_param_spec_uint("channel", "channel", "Stamp registry channel", 1,
                        G_MAXUINT, 1, G_PARAM_WRITABLE));
  g_object_class_install_property(
      object, PROP_CLOCK_FILE,
      g_param_spec_string("clock-file", "clock-file",
                          "Atomic conservative monotonic clock sample", NULL,
                          G_PARAM_WRITABLE));
  g_object_class_install_property(
      object, PROP_DRM_FD,
      g_param_spec_int("drm-fd", "drm-fd",
                       "Shared DRM descriptor for expiration", -1, G_MAXINT, -1,
                       G_PARAM_WRITABLE));
  g_object_class_install_property(
      object, PROP_PLANE,
      g_param_spec_uint("plane-id", "plane-id",
                        "Owned overlay to disable on expiry", 0, G_MAXUINT, 0,
                        G_PARAM_WRITABLE));
  g_object_class_install_property(
      object, PROP_MAX_AGE,
      g_param_spec_uint("max-age-ms", "max-age-ms",
                        "Advertised ingress-age ceiling", 1, 65535, 500,
                        G_PARAM_WRITABLE));
  g_object_class_install_property(object,PROP_OBSERVE_ONLY,
      g_param_spec_boolean("observe-only","observe-only",
          "Report ingress age without deadlines, plane clearing or extra pacing",FALSE,G_PARAM_WRITABLE));
  g_object_class_install_property(object,PROP_DIAGNOSTIC_FILE,
      g_param_spec_string("diagnostic-file","diagnostic-file","Optional bounded private metadata file",NULL,G_PARAM_WRITABLE));
  GstElementClass *element = GST_ELEMENT_CLASS(klass);
  GstBaseTransformClass *transform = GST_BASE_TRANSFORM_CLASS(klass);
  GstCaps *caps = gst_caps_from_string("video/x-raw(ANY)");
  gst_element_class_add_pad_template(
      element,
      gst_pad_template_new("sink", GST_PAD_SINK, GST_PAD_ALWAYS, caps));
  gst_element_class_add_pad_template(
      element, gst_pad_template_new("src", GST_PAD_SRC, GST_PAD_ALWAYS, caps));
  gst_caps_unref(caps);
  gst_element_class_set_static_metadata(
      element, "Bounded decoded video pacing", "Filter/Video",
      "Rebase decoded PTS to a rolling local clock with a 67 ms allowance",
      "eufy-wall");
  transform->transform_ip = pace;
  transform->query = query;
  transform->src_event = src_event;
  transform->start = start;
  transform->stop = stop;
}
static void gst_eufy_pacing_init(GstEufyPacing *self) {
  g_mutex_init(&self->lock);
  g_cond_init(&self->changed);
  self->drm_fd = -1;
  self->channel = 1;
  self->max_age_ms = 500;
  gst_base_transform_set_in_place(GST_BASE_TRANSFORM(self), TRUE);
  gst_base_transform_set_passthrough(GST_BASE_TRANSFORM(self), FALSE);
}
typedef struct {
  GstBaseTransform parent;
  guint channel;
  gboolean hevc;
} GstEufyStamp;
typedef struct {
  GstBaseTransformClass parent;
} GstEufyStampClass;
G_DEFINE_TYPE(GstEufyStamp, gst_eufy_stamp, GST_TYPE_BASE_TRANSFORM)
static GstFlowReturn stamp_frame(GstBaseTransform *base, GstBuffer *buffer) {
  GstEufyStamp *s = (GstEufyStamp *)base;
  GstMapInfo map;
  FreshStamp stamp;
  if (gst_buffer_map(buffer, &map, GST_MAP_READ)) {
    if (fresh_extract(map.data, map.size, s->hevc, &stamp))
      remember(s->channel, GST_BUFFER_PTS(buffer), &stamp);
    gst_buffer_unmap(buffer, &map);
  }
  return GST_FLOW_OK;
}
static gboolean stamp_caps(GstBaseTransform *base, GstCaps *in, GstCaps *out) {
  (void)out;
  ((GstEufyStamp *)base)->hevc = g_str_equal(
      gst_structure_get_name(gst_caps_get_structure(in, 0)), "video/x-h265");
  return TRUE;
}
static void stamp_prop(GObject *object, guint prop, const GValue *value,
                       GParamSpec *spec) {
  if (prop == 1)
    ((GstEufyStamp *)object)->channel = g_value_get_uint(value);
  else
    G_OBJECT_WARN_INVALID_PROPERTY_ID(object, prop, spec);
}
static void gst_eufy_stamp_class_init(GstEufyStampClass *klass) {
  GstElementClass *e = GST_ELEMENT_CLASS(klass);
  GstBaseTransformClass *t = GST_BASE_TRANSFORM_CLASS(klass);
  G_OBJECT_CLASS(klass)->set_property = stamp_prop;
  g_object_class_install_property(
      G_OBJECT_CLASS(klass), 1,
      g_param_spec_uint("channel", "channel", "Paired decoded registry channel",
                        1, G_MAXUINT, 1, G_PARAM_WRITABLE));
  GstCaps *caps = gst_caps_from_string(
      "video/x-h264,stream-format=byte-stream,alignment=au;video/"
      "x-h265,stream-format=byte-stream,alignment=au");
  gst_element_class_add_pad_template(
      e, gst_pad_template_new("sink", GST_PAD_SINK, GST_PAD_ALWAYS, caps));
  gst_element_class_add_pad_template(
      e, gst_pad_template_new("src", GST_PAD_SRC, GST_PAD_ALWAYS, caps));
  gst_caps_unref(caps);
  gst_element_class_set_static_metadata(
      e, "Bridge freshness stamp reader", "Filter/Video",
      "Preserve per-AU ingress identity across hardware decode", "eufy-wall");
  t->transform_ip = stamp_frame;
  t->set_caps = stamp_caps;
}
static void gst_eufy_stamp_init(GstEufyStamp *s) {
  s->channel = 1;
  gst_base_transform_set_in_place(GST_BASE_TRANSFORM(s), TRUE);
}
/* The raw path reads only PTS and the registry; never maps decoded memory. */
typedef struct {
  GstBaseTransform parent;
  guint channel;
  gboolean raw,hevc,nal_aligned,pending_found;
  FreshStamp pending_stamp;
  guint64 pending_pts;
  gsize pending_bytes;
  guint nal_length;
  gint width,height;
  gchar *phase,*path,*clock_path;
  TimingObserver timing;
} GstEufyTrace;
typedef struct {GstBaseTransformClass parent;} GstEufyTraceClass;
G_DEFINE_TYPE(GstEufyTrace,gst_eufy_trace,GST_TYPE_BASE_TRANSFORM)
static gboolean trace_encoded_stamp(GstEufyTrace *s,const guint8 *data,gsize size,FreshStamp *stamp) {
  if (!s->nal_length) {
    if(s->nal_aligned) {
      gsize head=0;
      if(size>=4 && !data[0] && !data[1] && !data[2] && data[3]==1)head=4;
      else if(size>=3 && !data[0] && !data[1] && data[2]==1)head=3;
      if(head>=size || (s->hevc?((data[head]>>1)&63):(data[head]&31))!=(s->hevc?39:6))return FALSE;
    }
    return fresh_extract(data,size,s->hevc,stamp);
  }
  gsize pos=0;
  while(pos+s->nal_length<=size) {
    guint64 len=fresh_be(data+pos,s->nal_length);pos+=s->nal_length;
    if(!len || len>size-pos)return FALSE;
    guint type=s->hevc?((data[pos]>>1)&63):(data[pos]&31);
    if(type==(s->hevc?39:6) && len<=508) {
      guint8 nal[512]={0,0,0,1};memcpy(nal+4,data+pos,len);
      if(fresh_extract(nal,len+4,s->hevc,stamp))return TRUE;
    }
    pos+=len;
  }
  return FALSE;
}
static GstFlowReturn trace_frame(GstBaseTransform *base,GstBuffer *buffer) {
  GstEufyTrace *s=(GstEufyTrace *)base;
  if(!s->timing.file)return GST_FLOW_OK;
  guint64 now=(guint64)g_get_monotonic_time()*1000;
  FreshStamp stamp;gboolean found=FALSE;
  if(s->raw) found=peek(s->channel,GST_BUFFER_PTS(buffer),&stamp);
  else {
    GstMapInfo map;
    if(gst_buffer_map(buffer,&map,GST_MAP_READ)) {
      found=trace_encoded_stamp(s,map.data,map.size,&stamp);
      gst_buffer_unmap(buffer,&map);
    }
  }
  guint64 pts=GST_BUFFER_PTS(buffer);
  if(s->nal_aligned) {
    if(pts!=s->pending_pts || GST_BUFFER_FLAG_IS_SET(buffer,GST_BUFFER_FLAG_DISCONT)) {
      s->pending_found=FALSE;s->pending_bytes=0;s->pending_pts=pts;
    }
    gsize bytes=gst_buffer_get_size(buffer);
    if(bytes>4*1024*1024 || s->pending_bytes>4*1024*1024-bytes) {
      s->pending_found=FALSE;s->pending_bytes=0;
    } else s->pending_bytes+=bytes;
    if(found) {
      s->pending_stamp=stamp;s->pending_found=TRUE;
      timing_record(&s->timing,s->clock_path,"depay-sei",s->channel,pts,now,
          &stamp,TRUE,bytes,s->width,s->height);
    }
    if(GST_BUFFER_FLAG_IS_SET(buffer,GST_BUFFER_FLAG_MARKER)) {
      timing_record(&s->timing,s->clock_path,s->phase?s->phase:"unspecified",s->channel,
          pts,now,&s->pending_stamp,s->pending_found,s->pending_bytes,s->width,s->height);
      s->pending_found=FALSE;s->pending_bytes=0;
    }
  } else timing_record(&s->timing,s->clock_path,s->phase?s->phase:"unspecified",s->channel,
      pts,now,&stamp,found,gst_buffer_get_size(buffer),s->width,s->height);
  return GST_FLOW_OK;
}
static gboolean trace_caps(GstBaseTransform *base,GstCaps *in,GstCaps *out) {
  (void)out;GstEufyTrace *s=(GstEufyTrace *)base;
  const GstStructure *c=gst_caps_get_structure(in,0);
  const gchar *name=gst_structure_get_name(c);
  s->raw=g_str_equal(name,"video/x-raw");s->hevc=g_str_equal(name,"video/x-h265");
  s->nal_length=0;s->pending_found=FALSE;s->pending_bytes=0;s->pending_pts=GST_CLOCK_TIME_NONE;
  s->nal_aligned=g_strcmp0(gst_structure_get_string(c,"alignment"),"nal")==0;
  const gchar *format=gst_structure_get_string(c,"stream-format");
  if(!s->raw && format && !g_str_equal(format,"byte-stream")) {
    const GValue *value=gst_structure_get_value(c,"codec_data");
    if(!value || !GST_VALUE_HOLDS_BUFFER(value))return FALSE;
    GstMapInfo map;GstBuffer *config=gst_value_get_buffer(value);
    if(!gst_buffer_map(config,&map,GST_MAP_READ))return FALSE;
    guint offset=s->hevc?21:4;
    if(map.size>offset)s->nal_length=(map.data[offset]&3)+1;
    gst_buffer_unmap(config,&map);
    if(!s->nal_length)return FALSE;
  }
  s->width=s->height=0;gst_structure_get_int(c,"width",&s->width);gst_structure_get_int(c,"height",&s->height);
  return s->raw || s->hevc || g_str_equal(name,"video/x-h264");
}
static gboolean trace_start(GstBaseTransform *base) {
  GstEufyTrace *s=(GstEufyTrace *)base;return timing_open(&s->timing,s->path);
}
static gboolean trace_stop(GstBaseTransform *base) {
  timing_close(&((GstEufyTrace *)base)->timing);return TRUE;
}
static void trace_prop(GObject *o,guint prop,const GValue *v,GParamSpec *spec) {
  GstEufyTrace *s=(GstEufyTrace *)o;
  if(prop==1)s->channel=g_value_get_uint(v);
  else if(prop==2){g_free(s->phase);s->phase=g_value_dup_string(v);}
  else if(prop==3){g_free(s->path);s->path=g_value_dup_string(v);}
  else if(prop==4){g_free(s->clock_path);s->clock_path=g_value_dup_string(v);}
  else G_OBJECT_WARN_INVALID_PROPERTY_ID(o,prop,spec);
}
static void trace_finalize(GObject *o) {
  GstEufyTrace *s=(GstEufyTrace *)o;timing_close(&s->timing);
  g_free(s->phase);g_free(s->path);g_free(s->clock_path);
  G_OBJECT_CLASS(gst_eufy_trace_parent_class)->finalize(o);
}
static void gst_eufy_trace_class_init(GstEufyTraceClass *k) {
  GObjectClass *o=G_OBJECT_CLASS(k);o->set_property=trace_prop;o->finalize=trace_finalize;
  g_object_class_install_property(o,1,g_param_spec_uint("channel","channel","Registry channel",1,G_MAXUINT,1,G_PARAM_WRITABLE));
  g_object_class_install_property(o,2,g_param_spec_string("phase","phase","Phase name",NULL,G_PARAM_WRITABLE));
  g_object_class_install_property(o,3,g_param_spec_string("diagnostic-file","diagnostic-file","Bounded private metadata file",NULL,G_PARAM_WRITABLE));
  g_object_class_install_property(o,4,g_param_spec_string("clock-file","clock-file","Monotonic mapping",NULL,G_PARAM_WRITABLE));
  /* ANY preserves downstream caps ordering and existing depay negotiation.
   * set_caps validates the actual video format instead of forcing a new one. */
  GstElementClass *e=GST_ELEMENT_CLASS(k);GstCaps *caps=gst_caps_new_any();
  gst_element_class_add_pad_template(e,gst_pad_template_new("sink",GST_PAD_SINK,GST_PAD_ALWAYS,caps));
  gst_element_class_add_pad_template(e,gst_pad_template_new("src",GST_PAD_SRC,GST_PAD_ALWAYS,caps));gst_caps_unref(caps);
  gst_element_class_set_static_metadata(e,"Optional phase timing observer","Filter/Video",
      "Record metadata only; no decoded pixel access or pacing","eufy-wall");
  GstBaseTransformClass *t=GST_BASE_TRANSFORM_CLASS(k);t->transform_ip=trace_frame;
  t->set_caps=trace_caps;t->start=trace_start;t->stop=trace_stop;
  t->transform_ip_on_passthrough=TRUE;
}
static void gst_eufy_trace_init(GstEufyTrace *s) {
  s->channel=1;
  /* Inspection must not make shared decoder memory writable or copy pixels. */
  gst_base_transform_set_passthrough(GST_BASE_TRANSFORM(s),TRUE);
}
static gboolean plugin_init(GstPlugin *plugin) {
  GST_DEBUG_CATEGORY_INIT(eufy_pacing_debug, "eufypacing", 0,
                          "Decoded video pacing");
  return gst_element_register(plugin, "eufypacing", GST_RANK_NONE,
                              gst_eufy_pacing_get_type()) &&
         gst_element_register(plugin, "eufystamp", GST_RANK_NONE,
                              gst_eufy_stamp_get_type()) &&
         gst_element_register(plugin,"eufytrace",GST_RANK_NONE,gst_eufy_trace_get_type());
}
GST_PLUGIN_DEFINE(GST_VERSION_MAJOR, GST_VERSION_MINOR, eufypacing,
                  "Bounded decoded video pacing", plugin_init, "1.0",
                  "Proprietary", "eufy-wall",
                  "https://github.com/sapireli/eufy-p2p-rtsp-bridge")
