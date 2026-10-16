package com.eufywall.tv

/** Only properties used by the selected live rendition can invalidate its player. */
internal data class PlaybackContract(
    val rtsp: String,
    val freshness: FreshnessCapability?,
    val tcpPacketSize: Int?,
    val native: NativeRendition?,
    val hardwareDecoder: String?,
)

internal fun selectedPlayback(camera: Camera, nativePermitted: Boolean,
    hardwareDecoder: (NativeRendition) -> String?): Camera {
    val native = camera.nativeRendition?.takeIf { nativePermitted }
    val hardware = native?.let(hardwareDecoder)
    return if (native != null && hardware != null) camera.copy(rtsp = native.rtsp,
        width = native.width, height = native.height, freshness = native.freshness,
        nativeDecoderName = hardware, rtspTcpPacketSize = null) else camera
}

internal fun Camera.playbackContract() = PlaybackContract(rtsp, freshness, rtspTcpPacketSize,
    nativeRendition.takeIf { nativeDecoderName != null }, nativeDecoderName)
