package com.eufywall.tv

import android.media.MediaCodecList
import android.media.MediaFormat
import android.os.Build
import android.util.Log
import org.json.JSONObject

data class NativeRendition(val rtsp: String, val codec: String, val width: Int, val height: Int,
    val freshness: FreshnessCapability? = null, val profile: Int? = null, val level: Int? = null, val tier: Int? = null)

/** API 28 has no isHardwareAccelerated: reject known software implementations by name. */
internal fun hardwareCodecName(name: String): Boolean {
    val n = name.lowercase()
    return !(n.startsWith("omx.google.") || n.startsWith("c2.android.") || n.startsWith("c2.google.") ||
        n.contains(".sw.") || n.contains("software") || n.startsWith("omx.ffmpeg."))
}
internal fun nativeGeometryValid(codec: String, width: Int, height: Int): Boolean =
    codec.lowercase() in setOf("h265", "hevc", "video/hevc") && width > 0 && height > 0

internal fun supportsHevcProfileLevel(profile: Int?, level: Int?, tier: Int?, supportedProfile: Int, supportedLevel: Int): Boolean {
    if (profile !in listOf(1, 2) || tier !in listOf(0, 1)) return false
    val levels = listOf(30, 60, 63, 90, 93, 120, 123, 150, 153, 156, 180, 183, 186)
    val requiredIndex = levels.indexOf(level)
    if (requiredIndex < 0 || supportedProfile != profile || supportedLevel <= 0 || supportedLevel and (supportedLevel - 1) != 0) return false
    val bit = Integer.numberOfTrailingZeros(supportedLevel)
    return bit / 2 >= requiredIndex && (tier == 0 || bit % 2 == 1)
}

internal fun nativeHardwareDecoder(rendition: NativeRendition, debugProfileProbe: Boolean = false): String? {
    if (!nativeGeometryValid(rendition.codec, rendition.width, rendition.height)) return null
    return try {
        MediaCodecList(MediaCodecList.ALL_CODECS).codecInfos.firstOrNull { info ->
            !info.isEncoder && hardwareCodecName(info.name) &&
                (Build.VERSION.SDK_INT < 29 || info.isHardwareAccelerated) &&
                info.supportedTypes.any { it.equals(MediaFormat.MIMETYPE_VIDEO_HEVC, true) } &&
                runCatching {
                    val codecCaps = info.getCapabilitiesForType(MediaFormat.MIMETYPE_VIDEO_HEVC)
                    Log.i("EufyWallTV", "Native HEVC profiles ${info.name}: ${codecCaps.profileLevels.joinToString { "${it.profile}:${it.level}" }}")
                    val caps = codecCaps.videoCapabilities
                    val size = caps != null && caps.isSizeSupported(rendition.width, rendition.height)
                    val rate = size && caps!!.areSizeAndRateSupported(rendition.width, rendition.height, 30.0)
                    val profile = codecCaps.profileLevels.any { supportsHevcProfileLevel(rendition.profile, rendition.level, rendition.tier, it.profile, it.level) }
                    Log.i("EufyWallTV", "Native HEVC candidate ${info.name}: ${rendition.width}x${rendition.height} size=$size rate30=$rate streamProfile=${rendition.profile} level=${rendition.level} tier=${rendition.tier} profileSupported=$profile")
                    rate && (profile || debugProfileProbe)
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
    return NativeRendition(url, codec, width, height, parseFreshness(f.optJSONObject("freshness")), f.optInt("profile").takeIf { f.has("profile") }, f.optInt("level").takeIf { f.has("level") }, f.optInt("tier").takeIf { f.has("tier") })
}

/** A failed native decoder is not retried forever or replaced by a slow software decoder. */
internal class NativeFallback {
    private val failed = mutableSetOf<String>()
    fun permits(cameraId: String) = cameraId !in failed
    fun fail(cameraId: String) { failed.add(cameraId) }
}
