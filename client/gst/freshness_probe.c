/* Native GStreamer integration test: encoded/raw PTS association and no-input
 * expiry. Two appsrc branches isolate metadata from raw GstBuffer creation: no
 * decoder GstMeta is reused. */
#include <gst/gst.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
static gint encoded_count, raw_count;
static gboolean last_age;
static gsize raw_bytes=4;
static GstBuffer *identity_buffer;
static GstMemory *identity_memory;
static gint identity_count;
static GstPadProbeReturn identity_input(GstPad *pad,GstPadProbeInfo *info,gpointer unused) {
  (void)pad;(void)unused;
  identity_buffer=gst_buffer_ref(GST_PAD_PROBE_INFO_BUFFER(info));
  identity_memory=gst_buffer_peek_memory(identity_buffer,0);
  GST_MINI_OBJECT_FLAG_SET(identity_memory,GST_MEMORY_FLAG_NO_SHARE);
  return GST_PAD_PROBE_OK;
}
static void identity_handoff(GstElement *s,GstBuffer *b,GstPad *p,gpointer unused) {
  (void)s;(void)p;(void)unused;
  g_assert(b==identity_buffer);
  g_assert(gst_buffer_peek_memory(b,0)==identity_memory);
  g_atomic_int_inc(&identity_count);
}
static void handoff(GstElement *s, GstBuffer *b, GstPad *p, gpointer count) {
  (void)s;
  (void)b;
  (void)p;
  g_atomic_int_inc((gint *)count);
  if(last_age && count==&raw_count)g_usleep(50000);
}
static void be(uint8_t *p, uint64_t v, unsigned n) {
  while (n) {
    p[--n] = (uint8_t)v;
    v >>= 8;
  }
}
static void push_format(GstElement *s, guint64 pts, uint64_t ingress,
                         uint64_t frame, gboolean avc) {
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
  if (avc) be(nal,len-4,4);
  GstBuffer *b = gst_buffer_new_allocate(NULL, len, NULL);
  gst_buffer_fill(b, 0, nal, len);
  GST_BUFFER_PTS(b) = pts;
  GstFlowReturn r;
  g_signal_emit_by_name(s, "push-buffer", b, &r);
  gst_buffer_unref(b);
  g_assert(r == GST_FLOW_OK);
}
static void push_encoded(GstElement *s, guint64 pts, uint64_t ingress, uint64_t frame) {
  push_format(s,pts,ingress,frame,FALSE);
}
static void push_raw(GstElement *s, guint64 pts) {
  GstBuffer *b = gst_buffer_new_allocate(NULL, raw_bytes, NULL);
  GST_BUFFER_PTS(b) = pts;
  GstFlowReturn r;
  g_signal_emit_by_name(s, "push-buffer", b, &r);
  gst_buffer_unref(b);
  g_assert(r == GST_FLOW_OK);
}
int main(int argc, char **argv) {
  last_age=argc>1 && !strcmp(argv[1],"--last-age");
  gboolean trace = argc > 1 && !strcmp(argv[1], "--trace");
  gboolean observe = last_age || trace || (argc > 1 && !strcmp(argv[1], "--observe"));
  const char *diagnostic="/tmp/eufy-timing-probe";
  if (trace) {remove(diagnostic);raw_bytes=0;}
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
  gchar *description = g_strdup_printf(
      "appsrc name=encoded is-live=true format=time "
      "caps=video/x-h264,stream-format=byte-stream,alignment=au %s ! eufystamp "
      "channel=7 %s ! fakesink name=encoded_sink sync=false async=false "
      "signal-handoffs=true appsrc name=raw is-live=true format=time "
      "caps=video/x-raw,format=RGB,width=1,height=1,framerate=15/1 %s ! "
      "eufypacing channel=7 clock-file=/tmp/eufy-freshness-probe-clock observe-only=%s %s ! "
      "fakesink name=raw_sink sync=false async=false signal-handoffs=true",
      trace ? "! eufytrace phase=depay-au channel=7 diagnostic-file=/tmp/eufy-timing-probe clock-file=/tmp/eufy-freshness-probe-clock" : "",
      trace ? "! eufytrace phase=parsed-au channel=7 diagnostic-file=/tmp/eufy-timing-probe clock-file=/tmp/eufy-freshness-probe-clock" : "",
      trace ? "! eufytrace phase=decoder-out channel=7 diagnostic-file=/tmp/eufy-timing-probe clock-file=/tmp/eufy-freshness-probe-clock ! eufytrace phase=post-queue channel=7 diagnostic-file=/tmp/eufy-timing-probe clock-file=/tmp/eufy-freshness-probe-clock" : "",
      observe ? "true" : "false", trace ? "diagnostic-file=/tmp/eufy-timing-probe" : "");
  GstElement *pipe = gst_parse_launch(description, &err);
  g_free(description);
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
  if(!last_age)push_raw(raw, 134000000);
  g_usleep(30000);
  GstBus *bus = gst_element_get_bus(pipe);
  GstMessage *m;
  if (last_age) {
    g_usleep(300000);g_assert(g_atomic_int_get(&raw_count)==2);
  } else if (observe) {
    /* Old and missing stamps must still reach the sink; no no-input expiry. */
    g_usleep(700000);
    g_assert(g_atomic_int_get(&raw_count) == 3);
    m = gst_bus_timed_pop_filtered(bus, 0, GST_MESSAGE_ELEMENT | GST_MESSAGE_ERROR);
    g_assert(m == NULL);
    /* Invalid clock cannot turn observation into enforcement. */
    remove(path);
    push_encoded(encoded, 200000000, now / 1000, 3);
    for (int i=0;i<100 && g_atomic_int_get(&encoded_count)<3;i++) g_usleep(1000);
    push_raw(raw,200000000);
    g_usleep(100000);
    g_assert(g_atomic_int_get(&raw_count)==4);
    g_print("observe integration: expired/missing/unknown-clock pictures forwarded; no expiry event\n");
  } else {
    g_assert(g_atomic_int_get(&raw_count) == 1);
    m = gst_bus_timed_pop_filtered(bus, GST_SECOND, GST_MESSAGE_ELEMENT | GST_MESSAGE_ERROR);
    g_assert(m != NULL);
    g_assert(GST_MESSAGE_TYPE(m) == GST_MESSAGE_ELEMENT);
    g_assert(gst_structure_has_name(gst_message_get_structure(m), "freshness-expired"));
    gst_message_unref(m);
    uint64_t recovered = (uint64_t)g_get_monotonic_time() * 1000;
    push_encoded(encoded, 200000000, recovered / 1000, 3);
    for (int i=0;i<100 && g_atomic_int_get(&encoded_count)<3;i++) g_usleep(1000);
    push_raw(raw,200000000);
    g_usleep(100000);
    g_assert(g_atomic_int_get(&raw_count)==2);
    m = gst_bus_timed_pop_filtered(bus,0,GST_MESSAGE_ERROR);
    g_assert(m==NULL);
    g_print("strict integration: stale/missing suppressed; expiry and recovery verified\n");
  }
  gst_object_unref(bus);
  gst_element_set_state(pipe, GST_STATE_NULL);
  gst_object_unref(encoded);
  gst_object_unref(raw);
  gst_object_unref(es);
  gst_object_unref(rs);
  gst_object_unref(pipe);
  remove(path);
  if (trace) {
    GstElement *avc=gst_parse_launch("appsrc name=avc is-live=true format=time caps=video/x-h264,stream-format=avc,alignment=au,codec_data=(buffer)01000000ff ! eufytrace phase=avc-sei channel=7 diagnostic-file=/tmp/eufy-timing-probe ! fakesink sync=false async=false",&err);
    g_assert_no_error(err);GstElement *source=gst_bin_get_by_name(GST_BIN(avc),"avc");
    gst_element_set_state(avc,GST_STATE_PLAYING);gst_element_get_state(avc,NULL,NULL,GST_SECOND);
    push_format(source,300000000,now/1000,9,TRUE);g_usleep(100000);
    gst_element_set_state(avc,GST_STATE_NULL);gst_object_unref(source);gst_object_unref(avc);
    GstElement *nalpipe=gst_parse_launch("appsrc name=nal is-live=true format=time caps=video/x-h264,stream-format=byte-stream,alignment=nal ! eufytrace phase=depay-au channel=7 diagnostic-file=/tmp/eufy-timing-probe ! fakesink sync=false async=false",&err);
    g_assert_no_error(err);source=gst_bin_get_by_name(GST_BIN(nalpipe),"nal");
    gst_element_set_state(nalpipe,GST_STATE_PLAYING);gst_element_get_state(nalpipe,NULL,NULL,GST_SECOND);
    push_format(source,400000000,now/1000,10,FALSE);
    GstBuffer *vcl=gst_buffer_new_allocate(NULL,6,NULL);
    const guint8 picture[]={0,0,0,1,0x65,0x80};gst_buffer_fill(vcl,0,picture,sizeof(picture));
    GST_BUFFER_PTS(vcl)=400000000;GST_BUFFER_FLAG_SET(vcl,GST_BUFFER_FLAG_MARKER);
    GstFlowReturn flow;g_signal_emit_by_name(source,"push-buffer",vcl,&flow);gst_buffer_unref(vcl);g_assert(flow==GST_FLOW_OK);
    g_usleep(100000);gst_element_set_state(nalpipe,GST_STATE_NULL);gst_object_unref(source);gst_object_unref(nalpipe);
    /* Retain the input buffer: it is deliberately non-writable, and its memory
     * cannot be shared through a subbuffer. A modifying transform would copy it. */
    GstElement *identity=gst_parse_launch("appsrc name=identity-source is-live=true format=time caps=video/x-raw,format=RGB,width=1,height=1,framerate=15/1 ! eufytrace name=identity-trace phase=raw-identity channel=7 diagnostic-file=/tmp/eufy-timing-probe ! fakesink name=identity-sink sync=false async=false signal-handoffs=true",&err);
    g_assert_no_error(err);source=gst_bin_get_by_name(GST_BIN(identity),"identity-source");
    GstElement *identity_sink=gst_bin_get_by_name(GST_BIN(identity),"identity-sink");
    g_signal_connect(identity_sink,"handoff",G_CALLBACK(identity_handoff),NULL);
    GstElement *identity_trace=gst_bin_get_by_name(GST_BIN(identity),"identity-trace");
    GstPad *identity_pad=gst_element_get_static_pad(identity_trace,"sink");
    gst_pad_add_probe(identity_pad,GST_PAD_PROBE_TYPE_BUFFER,identity_input,NULL,NULL);
    gst_object_unref(identity_pad);gst_object_unref(identity_trace);
    gst_element_set_state(identity,GST_STATE_PLAYING);gst_element_get_state(identity,NULL,NULL,GST_SECOND);
    GstAllocationParams allocation;gst_allocation_params_init(&allocation);allocation.flags=GST_MEMORY_FLAG_NO_SHARE;
    GstBuffer *identity_source_buffer=gst_buffer_new();
    gst_buffer_append_memory(identity_source_buffer,gst_allocator_alloc(NULL,4,&allocation));
    GST_BUFFER_PTS(identity_source_buffer)=500000000;
    g_signal_emit_by_name(source,"push-buffer",identity_source_buffer,&flow);g_assert(flow==GST_FLOW_OK);
    gst_buffer_unref(identity_source_buffer);
    for(guint n=0;n<1000 && !g_atomic_int_get(&identity_count);n++)g_usleep(1000);
    g_assert(g_atomic_int_get(&identity_count)==1);
    gst_element_set_state(identity,GST_STATE_NULL);gst_buffer_unref(identity_buffer);
    gst_object_unref(identity_sink);gst_object_unref(source);gst_object_unref(identity);
    gchar *contents=NULL;gsize len=0;g_assert(g_file_get_contents(diagnostic,&contents,&len,NULL));
    g_assert(strstr(contents,"\"phase\":\"depay-au\"")!=NULL);
    g_assert(strstr(contents,"\"phase\":\"decoder-out\"")!=NULL);
    g_assert(strstr(contents,"\"phase\":\"kms-return\"")!=NULL);
    gchar **lines=g_strsplit(contents,"\n",-1);guint count=0,matched_phases=0;gboolean avc_found=FALSE,nal_sei=FALSE,nal_complete=FALSE;
    for(guint i=0;lines[i];i++) {
      if(!*lines[i])continue;
      count++;
      if(strstr(lines[i],"\"frameId\":\"10\"") && strstr(lines[i],"\"stampFound\":true")) {
        if(strstr(lines[i],"\"phase\":\"depay-sei\""))nal_sei=TRUE;
        if(strstr(lines[i],"\"phase\":\"depay-au\""))nal_complete=TRUE;
      }
      if(strstr(lines[i],"\"frameId\":\"1\"") && strstr(lines[i],"\"stampFound\":true")) {
        if(strstr(lines[i],"\"phase\":\"depay-au\""))matched_phases|=1;
        if(strstr(lines[i],"\"phase\":\"parsed-au\""))matched_phases|=2;
        if(strstr(lines[i],"\"phase\":\"decoder-out\""))matched_phases|=4;
        if(strstr(lines[i],"\"phase\":\"post-queue\""))matched_phases|=8;
        if(strstr(lines[i],"\"phase\":\"kms-return\""))matched_phases|=16;
      }
      if(strstr(lines[i],"\"phase\":\"decoder-out\"") && strstr(lines[i],"\"frameId\":\"1\"")) {
        g_assert(strstr(lines[i],"\"stampFound\":true"));g_assert(strstr(lines[i],"\"bytes\":0"));
      }
      if(strstr(lines[i],"\"phase\":\"avc-sei\"")) {
        g_assert(strstr(lines[i],"\"stampFound\":true"));g_assert(strstr(lines[i],"\"frameId\":\"9\""));avc_found=TRUE;
      }
    }
    g_assert(count==22);g_assert(matched_phases==31);g_assert(avc_found);g_assert(nal_sei && nal_complete);g_strfreev(lines);g_free(contents);remove(diagnostic);
    g_print("phase integration: exact encoded/raw/KMS identities; AVC/NAL metadata parsed without format/alignment conversion\n");
  }
  return 0;
}
