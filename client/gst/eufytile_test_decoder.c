/* Synthetic one-slot decoder for offline contention tests only. Never installed. */
#include <gst/base/gstbasetransform.h>
#define PACKAGE "eufy-tile-test"
typedef struct { GstBaseTransform parent; gboolean held; } TestDecoder;
typedef struct { GstBaseTransformClass parent; } TestDecoderClass;
G_DEFINE_TYPE(TestDecoder,test_decoder,GST_TYPE_BASE_TRANSFORM)
G_LOCK_DEFINE_STATIC(slots);
static guint slots;
static gboolean decoder_start(GstBaseTransform *base) {
  TestDecoder *s=(TestDecoder *)base;
  G_LOCK(slots);
  if(slots) {
    G_UNLOCK(slots);
    GST_ELEMENT_ERROR(s,RESOURCE,BUSY,("Failed to allocate required memory."),("Synthetic single-slot capacity exhausted"));
    return FALSE;
  }
  slots++;s->held=TRUE;G_UNLOCK(slots);return TRUE;
}
static gboolean decoder_stop(GstBaseTransform *base) {
  TestDecoder *s=(TestDecoder *)base;
  G_LOCK(slots);if(s->held){slots--;s->held=FALSE;}G_UNLOCK(slots);return TRUE;
}
static GstFlowReturn decoder_frame(GstBaseTransform *base,GstBuffer *buffer) {(void)base;(void)buffer;return GST_FLOW_OK;}
static void test_decoder_class_init(TestDecoderClass *klass) {
  GstElementClass *element=GST_ELEMENT_CLASS(klass);GstCaps *caps=gst_caps_from_string("video/x-raw");
  gst_element_class_add_pad_template(element,gst_pad_template_new("src",GST_PAD_SRC,GST_PAD_ALWAYS,caps));
  gst_element_class_add_pad_template(element,gst_pad_template_new("sink",GST_PAD_SINK,GST_PAD_ALWAYS,caps));gst_caps_unref(caps);
  gst_element_class_set_static_metadata(element,"Synthetic decoder","Filter/Video","Offline capacity fixture","eufy-wall");
  GstBaseTransformClass *transform=GST_BASE_TRANSFORM_CLASS(klass);
  transform->start=decoder_start;transform->stop=decoder_stop;transform->transform_ip=decoder_frame;
}
static void test_decoder_init(TestDecoder *s){gst_base_transform_set_in_place(GST_BASE_TRANSFORM(s),TRUE);}
static gboolean init(GstPlugin *plugin) {
  /* Use a decoder-family name so production error ownership classification is exercised. */
  return gst_element_register(plugin,"v4l2testh264dec",GST_RANK_NONE,test_decoder_get_type());
}
GST_PLUGIN_DEFINE(GST_VERSION_MAJOR,GST_VERSION_MINOR,eufytiletest,"Offline synthetic resource fixture",init,"1.0","LGPL","eufy-wall","https://example.invalid")
