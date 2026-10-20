package com.eufywall.tv

import android.annotation.SuppressLint
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Process
import android.util.Log
import java.nio.ByteBuffer
import com.alexvas.rtsp.codec.FrameQueue
import com.alexvas.rtsp.codec.VideoDecodeThread
import com.alexvas.rtsp.codec.VideoFrameQueue
import com.alexvas.utils.MediaCodecUtils
import com.alexvas.utils.VideoCodecUtils
import com.limelight.binding.video.MediaCodecHelper

/**
 * RtspProcessor decoder adapter. Input and output are polled independently: a gap in RTP input
 * must not strand already decoded frames behind FrameQueue.pop()'s one-second default wait.
 * Normal playback preserves compressed references; queue overflow recovery waits for an IDR.
 */
@SuppressLint("UnsafeOptInUsageError")
internal abstract class DrainingVideoDecoder(
    mime: String, width: Int, height: Int, rotation: Int,
    frames: VideoFrameQueue, listener: VideoDecoderListener, type: DecoderType,
    private val requiredHardwareName: String? = null,
    private val hardwarePinned: Boolean = false,
) : VideoDecodeThread(mime, width, height, rotation, frames, listener, type) {
    private var codedWidth = width
    private var codedHeight = height
    private var hasCodedSize = requiredHardwareName != null
    private var hevcCodecSpecificData: ByteArray? = null
    private var hardwareLease: HardwareDecoderPool.Lease? = null
    protected open fun hardwareReallocated() {}
    private fun hardwareCodecNameForRequest(): String? {
        val candidates = MediaCodecUtils.getHardwareDecoders(mimeType)
        return requiredHardwareName ?: (MediaCodecUtils.getLowLatencyDecoder(candidates) ?: candidates.firstOrNull())?.name
    }
    protected open fun beforeInputFrame(frame: FrameQueue.VideoFrame) {}
    private fun createDecoder(type: DecoderType): MediaCodec =
        if (type == DecoderType.HARDWARE) HardwareDecoderPool.shared.allocate { createDecoderUnlocked(type) }
        else createDecoderUnlocked(type)

    private fun createDecoderUnlocked(type: DecoderType): MediaCodec {
        val candidates = if (type == DecoderType.HARDWARE) MediaCodecUtils.getHardwareDecoders(mimeType)
            else MediaCodecUtils.getSoftwareDecoders(mimeType)
        val candidate = MediaCodecUtils.getLowLatencyDecoder(candidates) ?: candidates.firstOrNull()
        val decoder = if (requiredHardwareName != null) {
            check(type == DecoderType.HARDWARE) { "Native rendition requires hardware decoding" }
            MediaCodec.createByCodecName(requiredHardwareName)
        } else candidate?.let { MediaCodec.createByCodecName(it.name) } ?: MediaCodec.createDecoderByType(mimeType)
        try {
            val caps = decoder.codecInfo.getCapabilitiesForType(mimeType).videoCapabilities
            val alignedWidth = caps?.let { ((codedWidth + it.widthAlignment - 1) / it.widthAlignment) * it.widthAlignment } ?: codedWidth
            val alignedHeight = caps?.let { ((codedHeight + it.heightAlignment - 1) / it.heightAlignment) * it.heightAlignment } ?: codedHeight
            // Retain parsed H.264 SPS dimensions even outside the declared limits:
            // some drivers can decode them. Without SPS, keep the legacy safe allocation.
            val supported = caps == null || caps.isSizeSupported(alignedWidth, alignedHeight)
            Log.i("EufyWallTV", "Decoder capabilities ${decoder.name}: coded=${codedWidth}x$codedHeight aligned=${alignedWidth}x$alignedHeight supported=$supported widths=${caps?.supportedWidths} heights=${caps?.supportedHeights} rate15=${caps?.areSizeAndRateSupported(alignedWidth, alignedHeight, 15.0)}")
            val w = if (hasCodedSize || supported) alignedWidth else caps!!.supportedWidths.upper
            val h = if (hasCodedSize || supported) alignedHeight else caps!!.supportedHeights.upper
            val format = MediaFormat.createVideoFormat(mimeType, w, h).apply {
                setInteger(MediaFormat.KEY_ROTATION, rotation)
                hevcCodecSpecificData?.let { setByteBuffer("csd-0", ByteBuffer.wrap(it)) }
            }
            MediaCodecHelper.setDecoderLowLatencyOptions(format, decoder.codecInfo, 1)
            decoderCreated(decoder, format)
            decoder.start()
            videoDecoderType = type
            if (type == DecoderType.HARDWARE) hardwareLease = HardwareDecoderPool.shared.register(decoder.name, codedWidth.toLong() * codedHeight, hardwarePinned)
            Log.i("EufyWallTV", "Live decoder ${decoder.name} started ($type, pinned=$hardwarePinned)")
            return decoder
        } catch (error: Throwable) {
            decoder.release()
            throw error
        }
    }

    private fun closeDecoder(decoder: MediaCodec) {
        try { decoder.stop() } catch (_: Exception) {}
        try { decoder.release() } catch (_: Exception) {}
        hardwareLease?.let { HardwareDecoderPool.shared.release(it) }
        hardwareLease = null
        decoderDestroyed(decoder)
    }

    override fun run() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_VIDEO)
        videoDecoderListener.onVideoDecoderStarted()
        var decoder: MediaCodec? = null
        var reallocated = false
        try {
            // RtspProcessor queues SDP SPS/PPS before starting the decoder. For H.264,
            // allocate for the coded picture; some decoders cannot grow the viewport allocation.
            var pending: FrameQueue.VideoFrame? = videoFrameQueue.pop(0)
            if (mimeType == MediaFormat.MIMETYPE_VIDEO_AVC) pending?.let { frame ->
                VideoCodecUtils.getWidthHeightFromArray(frame.data, frame.offset, frame.length, false)?.let { size ->
                    codedWidth = size.first
                    codedHeight = size.second
                    hasCodedSize = true
                }
            }
            if (mimeType == MediaFormat.MIMETYPE_VIDEO_HEVC) pending?.let { frame ->
                hevcInitialization(frame.data, frame.offset, frame.length)?.let { initialization ->
                    hevcCodecSpecificData = initialization.codecSpecificData
                    Log.i("EufyWallTV", "HEVC initialization: ordered VPS/SPS/PPS csd-0=${initialization.codecSpecificData.size} bytes configOnly=${initialization.configOnly}")
                    // MediaCodec automatically submits csd-0 on start. Preserve a first
                    // access unit containing VCL; only consume pure SDP configuration.
                    if (initialization.configOnly) pending = null
                }
            }
            // Keep the reclaimed slot reserved through release and retry; another
            // stream must not steal it from a pinned camera during that handoff.
            decoder = HardwareDecoderPool.shared.allocate {
              try { createDecoder(videoDecoderType) } catch (error: Exception) {
                if (exitFlag.get() || videoDecoderType == DecoderType.SOFTWARE || requiredHardwareName != null) throw error
                Log.w("EufyWallTV", "Hardware allocation failed for ${codedWidth}x$codedHeight; checking smaller hardware owners", error)
                val codecName = hardwareCodecNameForRequest()
                val victim = if (error is MediaCodec.CodecException && hardwareResourcesExhausted(error.errorCode))
                    codecName?.let { HardwareDecoderPool.shared.reclaim(it, codedWidth.toLong() * codedHeight, hardwarePinned) } else null
                val retried = if (victim != null) {
                    Log.i("EufyWallTV", "Rebalancing hardware $codecName: ${codedWidth}x$codedHeight replaces ${victim.pixels} coded pixels")
                    if (victim.awaitRelease(3_000)) runCatching { createDecoder(DecoderType.HARDWARE) }.getOrNull() else null
                } else null
                retried ?: run {
                    Log.w("EufyWallTV", "Hardware decoder unavailable; trying software", error)
                    createDecoder(DecoderType.SOFTWARE)
                }
            }
            }
            val output = MediaCodec.BufferInfo()
            while (!exitFlag.get()) {
                if (hardwareLease?.revoked == true) { reallocated = true; break }
                try {
                    val codec = decoder!!
                    // Drain every available output, including when no new compressed input arrives.
                    while (!exitFlag.get()) {
                        val index = codec.dequeueOutputBuffer(output, 0)
                        if (index == MediaCodec.INFO_TRY_AGAIN_LATER) break
                        if (index == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                            val format = codec.outputFormat
                            val cropped = format.containsKey("crop-right") && format.containsKey("crop-left") &&
                                format.containsKey("crop-bottom") && format.containsKey("crop-top")
                            val w = if (cropped) format.getInteger("crop-right") - format.getInteger("crop-left") + 1
                                else format.getInteger(MediaFormat.KEY_WIDTH)
                            val h = if (cropped) format.getInteger("crop-bottom") - format.getInteger("crop-top") + 1
                                else format.getInteger(MediaFormat.KEY_HEIGHT)
                            Log.i("EufyWallTV", "Decoded format ${codec.name}: visible=${w}x$h format=$format")
                            uiHandler.post {
                                if (rotation == 90 || rotation == 270) videoDecoderListener.onVideoDecoderFormatChanged(h, w)
                                else videoDecoderListener.onVideoDecoderFormatChanged(w, h)
                            }
                        } else if (index >= 0) {
                            releaseOutputBuffer(codec, index, output, output.size > 0 && !exitFlag.get())
                        }
                    }
                    if (exitFlag.get()) break
                    // A short poll bounds output starvation to 5 ms instead of one second.
                    if (pending == null) pending = videoFrameQueue.pop(5)
                    val frame = pending ?: continue
                    val index = codec.dequeueInputBuffer(0)
                    if (index < 0) { Thread.sleep(1); continue }
                    val buffer = checkNotNull(codec.getInputBuffer(index))
                    buffer.clear()
                    buffer.put(frame.data, frame.offset, frame.length)
                    beforeInputFrame(frame)
                    codec.queueInputBuffer(index, 0, frame.length, frame.timestampMs,
                        if (frame.isKeyframe) MediaCodec.BUFFER_FLAG_KEY_FRAME else 0)
                    pending = null
                } catch (error: InterruptedException) {
                    if (exitFlag.get()) break
                } catch (error: Exception) {
                    if (exitFlag.get()) break
                    if (error is MediaCodec.CodecException && error.isTransient) { Thread.sleep(5); continue }
                    // Reopen RTSP after a decoder failure so SDP codec configuration is reissued.
                    // Recreating only the codec can lose SPS/PPS/VPS on sources that do not repeat it.
                    throw error
                }
            }
        } catch (error: Exception) {
            if (!exitFlag.get()) videoDecoderListener.onVideoDecoderFailed(error.message)
        } finally {
            decoder?.let { closeDecoder(it) }
            videoFrameQueue.clear()
            videoDecoderListener.onVideoDecoderStopped()
        }
        if (reallocated && !exitFlag.get()) hardwareReallocated()
    }
}
