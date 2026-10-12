package com.eufywall.tv

import android.media.MediaCodecList
import android.media.MediaFormat
import android.os.Build
import org.json.JSONObject

data class NativeRendition(val rtsp: String, val codec: String, val width: Int, val height: Int,
    val freshness: FreshnessCapability? = null)

/** API 28 has no isHardwareAccelerated: reject known software implementations by name. */
internal fun hardwareCodecName(name: String): Boolean {
    val n = name.lowercase()
    return !(n.startsWith("omx.google.") || n.startsWith("c2.android.") || n.startsWith("c2.google.") ||
        n.contains(".sw.") || n.contains("software") || n.startsWith("omx.ffmpeg."))
}
internal fun nativeGeometryValid(codec: String, width: Int, height: Int): Boolean =
    codec.lowercase() in setOf("h265", "hevc", "video/hevc") && width > 0 && height > 0

internal fun nativeHardwareDecoder(rendition: NativeRendition): String? {
    if (!nativeGeometryValid(rendition.codec, rendition.width, rendition.height)) return null
    return try {
        MediaCodecList(MediaCodecList.ALL_CODECS).codecInfos.firstOrNull { info ->
            !info.isEncoder && hardwareCodecName(info.name) &&
                (Build.VERSION.SDK_INT < 29 || info.isHardwareAccelerated) &&
                info.supportedTypes.any { it.equals(MediaFormat.MIMETYPE_VIDEO_HEVC, true) } &&
                runCatching {
                    val caps = info.getCapabilitiesForType(MediaFormat.MIMETYPE_VIDEO_HEVC).videoCapabilities
                    caps != null && caps.isSizeSupported(rendition.width, rendition.height) &&
                        caps.areSizeAndRateSupported(rendition.width, rendition.height, 30.0)
                }.getOrDefault(false)
        }?.name
    } catch (_: Exception) { null }
}

internal fun parseFreshness(f: JSONObject?): FreshnessCapability? {
    if (f == null || f.optString("scope") != "bridge-ingress" || f.optInt("maxAgeMs") !in 100..60000) return null
    val mode = freshnessMode(f.optInt("version"), if (f.has("mode")) f.optString("mode") else null) ?: return null
    return f.optString("clockUrl").takeIf { it.isNotBlank() }?.let { FreshnessCapability(f.getInt("maxAgeMs"), it, mode) }
}
internal fun parseNativeRendition(f: JSONObject?): NativeRendition? {
    if (f == null) return null
    val codec = f.optString("codec")
    val width = f.optInt("width"); val height = f.optInt("height")
    val url = f.optString("rtsp")
    if (!nativeGeometryValid(codec, width, height) || !url.startsWith("rtsp://")) return null
    return NativeRendition(url, codec, width, height, parseFreshness(f.optJSONObject("freshness")))
}

/** A failed native decoder is not retried forever or replaced by a slow software decoder. */
internal class NativeFallback {
    private val failed = mutableSetOf<String>()
    fun permits(cameraId: String) = cameraId !in failed
    fun fail(cameraId: String) { failed.add(cameraId) }
}
