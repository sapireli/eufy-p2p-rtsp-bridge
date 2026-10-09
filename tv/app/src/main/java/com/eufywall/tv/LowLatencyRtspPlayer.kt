package com.eufywall.tv

import android.content.Context
import android.media.MediaCodec
import android.media.MediaFormat
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.util.Log
import android.view.Gravity
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.widget.FrameLayout
import com.alexvas.rtsp.codec.VideoDecodeThread
import com.alexvas.rtsp.codec.VideoDecodeThread.DecoderType
import com.alexvas.rtsp.codec.VideoFrameQueue
import com.alexvas.rtsp.widget.RtspDataListener
import com.alexvas.rtsp.widget.RtspProcessor
import com.alexvas.rtsp.widget.RtspStatusListener
import com.limelight.binding.video.MediaCodecHelper
import java.util.concurrent.atomic.AtomicLong

/** Independent RTSP connection and hardware-first decoder for one live tile. */
internal class LowLatencyRtspPlayer(
    context: Context,
    private val camera: Camera,
    private val onFirstFrame: () -> Unit,
    private val onFailure: (String, Boolean) -> Unit,
) {
    val view = FrameLayout(context).apply { setBackgroundColor(0xff000000.toInt()); keepScreenOn = true }
    private val surface = SurfaceView(context)
    private val ui = Handler(Looper.getMainLooper())
    @Volatile private var released = false
    private var started = false
    private var streamWidth = camera.width.takeIf { it > 0 } ?: 1920
    private var streamHeight = camera.height.takeIf { it > 0 } ?: 1080
    @Volatile private var queue: VideoFrameQueue? = null
    @Volatile private var decoderName = "pending"
    val renderedFrames = AtomicLong()
    val receivedUnits = AtomicLong()
    val lastFrameAt = AtomicLong(SystemClock.elapsedRealtime())
    private val longestFrameGap = AtomicLong()
    private val previousRenderNs = AtomicLong()
    private val latestInputTimestamp = AtomicLong(-1)
    private val latestRenderedTimestamp = AtomicLong(-1)

    // Keep the library's RTSP/RTP parsing and codec helpers. The decoder adapter drains
    // output independently of new input and adds actual-render monitoring.
    private val processor = RtspProcessor { mime, rotation, frames, listener, type, _ ->
        queue = frames
        val monitoredListener = object : VideoDecodeThread.VideoDecoderListener {
            override fun onVideoDecoderStarted() = listener.onVideoDecoderStarted()
            override fun onVideoDecoderStopped() = listener.onVideoDecoderStopped()
            override fun onVideoDecoderFailed(message: String?) {
                listener.onVideoDecoderFailed(message)
                fail(message ?: "Video decoder failed", true)
            }
            override fun onVideoDecoderFormatChanged(width: Int, height: Int) = listener.onVideoDecoderFormatChanged(width, height)
            override fun onVideoDecoderFirstFrameRendered() = listener.onVideoDecoderFirstFrameRendered()
        }
        // Bridge geometry describes the original camera, which may be transcoded for the TV.
        // For H.264 the adapter replaces this viewport hint with the SDP SPS coded size
        // before allocating the decoder. Bridge metadata can describe a different rendition.
        val initialWidth = surface.width.takeIf { it > 0 } ?: 1920
        val initialHeight = surface.height.takeIf { it > 0 } ?: 1080
        object : DrainingVideoDecoder(mime, initialWidth, initialHeight, rotation, frames, monitoredListener, type) {
            override fun decoderCreated(mediaCodec: MediaCodec, mediaFormat: MediaFormat) {
                check(!released && surface.holder.surface.isValid) { "Video surface unavailable" }
                decoderName = mediaCodec.name
                mediaCodec.configure(mediaFormat, surface.holder.surface, null, 0)
                var first = true
                mediaCodec.setOnFrameRenderedListener({ _, timestamp, renderNs ->
                    if (Log.isLoggable(TIMING_TAG, Log.DEBUG)) Log.d(TIMING_TAG, "${camera.sn} rendered ptsUs=$timestamp atNs=$renderNs")
                    if (!released) {
                        val now = SystemClock.elapsedRealtime()
                        lastFrameAt.set(now)
                        val previous = previousRenderNs.getAndSet(renderNs)
                        if (renderedFrames.getAndIncrement() > 0) longestFrameGap.updateAndGet {
                            maxOf(it, (renderNs - previous).coerceAtLeast(0) / 1_000_000)
                        }
                        latestRenderedTimestamp.set(timestamp)
                        if (first) {
                            first = false
                            monitoredListener.onVideoDecoderFirstFrameRendered()
                        }
                    }
                }, ui)
            }

            override fun releaseOutputBuffer(mediaCodec: MediaCodec, outIndex: Int, bufferInfo: MediaCodec.BufferInfo, render: Boolean) {
                if (render) if (Log.isLoggable(TIMING_TAG, Log.DEBUG)) Log.d(TIMING_TAG, "${camera.sn} decoded ptsUs=${bufferInfo.presentationTimeUs} atNs=${System.nanoTime()}")
                mediaCodec.releaseOutputBuffer(outIndex, render && !released && surface.holder.surface.isValid)
            }

            override fun decoderDestroyed(mediaCodec: MediaCodec) {}
        }
    }

    init {
        MediaCodecHelper.initialize(context.applicationContext, "")
        view.addView(surface, FrameLayout.LayoutParams(-1, -1, Gravity.CENTER))
        view.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> fitSurface() }
        processor.videoDecoderType = DecoderType.HARDWARE // Adapter falls back to software if hardware cannot start.
        processor.videoFrameRateStabilization = false
        processor.experimentalUpdateSpsFrameWithLowLatencyParams = false
        processor.statusListener = object : RtspStatusListener {
            override fun onRtspFirstFrameRendered() { if (!released) onFirstFrame() }
            override fun onRtspFrameSizeChanged(width: Int, height: Int) {
                if (!released && width > 0 && height > 0) {
                    streamWidth = width; streamHeight = height; fitSurface()
                }
            }
            override fun onRtspStatusFailedUnauthorized() = fail("RTSP authentication failed", false)
            override fun onRtspStatusFailed(message: String?) = fail(message ?: "RTSP connection failed", false)
            override fun onRtspStatusDisconnected() = fail("RTSP disconnected", false)
        }
        processor.dataListener = object : RtspDataListener {
            override fun onRtspDataVideoNalUnitReceived(data: ByteArray, offset: Int, length: Int, timestamp: Long) {
                if (Log.isLoggable(TIMING_TAG, Log.DEBUG)) Log.d(TIMING_TAG, "${camera.sn} received ptsUs=$timestamp atNs=${System.nanoTime()}")
                receivedUnits.incrementAndGet(); latestInputTimestamp.set(timestamp)
            }
        }
        surface.holder.addCallback(object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                if (!released && !started) {
                    started = true
                    val original = Uri.parse(camera.rtsp)
                    // Only a bridge-advertised capability enables larger interleaved TCP
                    // packets. Preserve explicit URL options and unrelated query values.
                    val hint = camera.rtspTcpPacketSize?.takeIf { it in 256..65535 }
                    val uri = if (hint != null && original.scheme == "rtsp" && original.getQueryParameter("pkt_size") == null)
                        original.buildUpon().appendQueryParameter("pkt_size", hint.toString()).build()
                    else original
                    Log.i("EufyWallTV", "${camera.sn} RTSP TCP packet size=${uri.getQueryParameter("pkt_size") ?: "server default"}")
                    val credentials = uri.userInfo?.split(':', limit = 2)
                    processor.init(uri, credentials?.getOrNull(0), credentials?.getOrNull(1), "EufyWallTV/0.5", 5_000)
                    processor.start(requestVideo = true, requestAudio = false, requestApplication = false)
                }
            }
            override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {}
            override fun surfaceDestroyed(holder: SurfaceHolder) {
                processor.stop(); processor.stopDecoders(); started = false
            }
        })
    }

    private fun fail(message: String, decoder: Boolean) {
        ui.post { if (!released) onFailure(message, decoder) }
    }

    private fun fitSurface() {
        if (view.width <= 0 || view.height <= 0) return
        val scale = minOf(view.width.toDouble() / streamWidth, view.height.toDouble() / streamHeight)
        val width = (streamWidth * scale).toInt().coerceAtLeast(1)
        val height = (streamHeight * scale).toInt().coerceAtLeast(1)
        if (surface.layoutParams.width != width || surface.layoutParams.height != height) {
            surface.layoutParams = FrameLayout.LayoutParams(width, height, Gravity.CENTER)
        }
    }

    fun diagnostics(): String {
        // The pinned library's getTimestampMsec()/timestampMs names are misleading:
        // RTP video ticks are converted to microseconds and passed unchanged to MediaCodec.
        val pending = if (latestRenderedTimestamp.get() < 0) -1 else
            (latestInputTimestamp.get() - latestRenderedTimestamp.get()).coerceAtLeast(0) / 1_000
        return "decoder=$decoderName receivedUnits=${receivedUnits.get()} renderedFrames=${renderedFrames.get()} queuedUnits=${queue?.size ?: 0} pendingMediaMs=$pending longestFrameGapMs=${longestFrameGap.get()}"
    }

    private companion object { const val TIMING_TAG = "EufyFrameTiming" }

    fun release() {
        released = true
        processor.statusListener = null; processor.dataListener = null
        processor.stop(); processor.stopDecoders()
        (view.parent as? FrameLayout)?.removeView(view)
    }
}
