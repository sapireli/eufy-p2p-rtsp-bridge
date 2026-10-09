package com.eufywall.tv

import android.annotation.SuppressLint
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Process
import android.util.Log
import com.alexvas.rtsp.codec.FrameQueue
import com.alexvas.rtsp.codec.VideoDecodeThread
import com.alexvas.rtsp.codec.VideoFrameQueue
import com.alexvas.utils.MediaCodecUtils
import com.limelight.binding.video.MediaCodecHelper

/**
 * RtspProcessor decoder adapter. Input and output are polled independently: a gap in RTP input
 * must not strand already decoded frames behind FrameQueue.pop()'s one-second default wait.
 * Compressed reference frames are never discarded to catch up.
 */
@SuppressLint("UnsafeOptInUsageError")
internal abstract class DrainingVideoDecoder(
    mime: String, width: Int, height: Int, rotation: Int,
    frames: VideoFrameQueue, listener: VideoDecoderListener, type: DecoderType,
) : VideoDecodeThread(mime, width, height, rotation, frames, listener, type) {
    private fun createDecoder(type: DecoderType): MediaCodec {
        val candidates = if (type == DecoderType.HARDWARE) MediaCodecUtils.getHardwareDecoders(mimeType)
            else MediaCodecUtils.getSoftwareDecoders(mimeType)
        val candidate = MediaCodecUtils.getLowLatencyDecoder(candidates) ?: candidates.firstOrNull()
        val decoder = candidate?.let { MediaCodec.createByCodecName(it.name) } ?: MediaCodec.createDecoderByType(mimeType)
        try {
            val caps = decoder.codecInfo.getCapabilitiesForType(mimeType).videoCapabilities
            val alignedWidth = caps?.let { ((width + it.widthAlignment - 1) / it.widthAlignment) * it.widthAlignment } ?: width
            val alignedHeight = caps?.let { ((height + it.heightAlignment - 1) / it.heightAlignment) * it.heightAlignment } ?: height
            // The surface viewport may exceed the coded stream and the decoder's size limit.
            val supported = caps == null || caps.isSizeSupported(alignedWidth, alignedHeight)
            val w = if (supported) alignedWidth else caps!!.supportedWidths.upper
            val h = if (supported) alignedHeight else caps!!.supportedHeights.upper
            val format = MediaFormat.createVideoFormat(mimeType, w, h).apply {
                setInteger(MediaFormat.KEY_ROTATION, rotation)
            }
            MediaCodecHelper.setDecoderLowLatencyOptions(format, decoder.codecInfo, 1)
            decoderCreated(decoder, format)
            decoder.start()
            videoDecoderType = type
            Log.i("EufyWallTV", "Live decoder ${decoder.name} started ($type)")
            return decoder
        } catch (error: Throwable) {
            decoder.release()
            throw error
        }
    }

    private fun closeDecoder(decoder: MediaCodec) {
        try { decoder.stop() } catch (_: Exception) {}
        try { decoder.release() } catch (_: Exception) {}
        decoderDestroyed(decoder)
    }

    override fun run() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_VIDEO)
        videoDecoderListener.onVideoDecoderStarted()
        var decoder: MediaCodec? = null
        try {
            decoder = try { createDecoder(videoDecoderType) } catch (error: Exception) {
                if (exitFlag.get() || videoDecoderType == DecoderType.SOFTWARE) throw error
                Log.w("EufyWallTV", "Hardware decoder unavailable; trying software", error)
                createDecoder(DecoderType.SOFTWARE)
            }
            var pending: FrameQueue.VideoFrame? = null
            val output = MediaCodec.BufferInfo()
            while (!exitFlag.get()) {
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
    }
}
