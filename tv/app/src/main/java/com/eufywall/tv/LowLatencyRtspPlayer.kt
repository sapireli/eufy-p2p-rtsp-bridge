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
import android.view.View
import android.widget.FrameLayout
import com.alexvas.rtsp.codec.VideoDecodeThread
import com.alexvas.rtsp.codec.VideoDecodeThread.DecoderType
import com.alexvas.rtsp.codec.VideoFrameQueue
import com.alexvas.rtsp.codec.FrameQueue
import com.alexvas.rtsp.widget.RtspDataListener
import com.alexvas.rtsp.widget.RtspProcessor
import com.alexvas.rtsp.widget.RtspStatusListener
import com.limelight.binding.video.MediaCodecHelper
import java.util.concurrent.atomic.AtomicLong

/** A tile owns its display surface longer than any individual RTSP connection. */
internal class TileVideoSurface(context: Context) {
    val view = FrameLayout(context).apply { setBackgroundColor(0xff000000.toInt()); keepScreenOn = true }
    val surface = SurfaceView(context)
    val cover = View(context).apply { setBackgroundColor(0xff000000.toInt()) }
    init {
        view.addView(surface, FrameLayout.LayoutParams(-1, -1, Gravity.CENTER))
        view.addView(cover, FrameLayout.LayoutParams(-1, -1))
    }
    fun remove() { (view.parent as? FrameLayout)?.removeView(view) }
}

/** Independent RTSP connection and hardware-first decoder for one live tile. */
internal class LowLatencyRtspPlayer(
    context: Context,
    private val camera: Camera,
    private val onFirstFrame: () -> Unit,
    private val onFailure: (String, Boolean) -> Unit,
    private val onFreshnessState: (String?) -> Unit = {},
    private val hardwarePinned: Boolean = false,
    sharedSurface: TileVideoSurface? = null,
) {
    private val ownsSurface = sharedSurface == null
    private val tileSurface = sharedSurface ?: TileVideoSurface(context)
    val view = tileSurface.view
    private val surface = tileSurface.surface
    // Keep the decoder surface alive when masking expired video. Hiding a
    // SurfaceView destroys its surface and prevents the next fresh callback.
    private var freshRenderer: FreshVideoRenderer? = null
    private var freshFirstFrame = true
    private val freshnessCover = tileSurface.cover
    private lateinit var surfaceCallback: SurfaceHolder.Callback
    private val layoutListener = View.OnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> fitSurface() }
    private val ui = Handler(Looper.getMainLooper())
    @Volatile private var released = false
    private var started = false
    private var streamWidth = camera.width.takeIf { it > 0 } ?: 1920
    private var streamHeight = camera.height.takeIf { it > 0 } ?: 1080
    @Volatile private var queue: VideoFrameQueue? = null
    @Volatile private var decoderName = "pending"
    private val renderedTiming = RenderedTimingTracker()
    internal fun latencyObservation(nowNs: Long): OutputLatencyObservation? = renderedTiming.snapshot()?.let {
        OutputLatencyObservation(it, freshnessClock?.currentSample(nowNs), if (enforcesFreshness) "GPU-swap-admission" else "codec-render-callback")
    }
    val renderedFrames = AtomicLong()
    val decodedFrames = AtomicLong()
    val lastDecodedFrameAt = AtomicLong(SystemClock.elapsedRealtime())
    val enforcesFreshness: Boolean get() = camera.freshness?.enforces == true
    val receivedUnits = AtomicLong()
    val lastFrameAt = AtomicLong(SystemClock.elapsedRealtime())
    private val longestFrameGap = AtomicLong()
    private val previousRenderNs = AtomicLong()
    private val latestInputTimestamp = AtomicLong(-1)
    private val latestRenderedTimestamp = AtomicLong(-1)
    private val latestDecodedTimestamp = AtomicLong(-1)
    @Volatile private var frameClock = LiveFrameClock()
    private val freshnessClock = camera.freshness?.let { BridgeClockClient(it.clockUrl) }
    private val stamps = LinkedHashMap<Long, FrameStamp>()
    private val frameStages = LinkedHashMap<Long, DecodeFrameStages>()
    private val stampLock = Any()
    @Volatile private var shownDeadlineNs = 0L
    @Volatile private var shownFrameId = 0L
    @Volatile private var shownEpoch = ""
    @Volatile private var freshnessState = "Timing unavailable"
    val staleOutputFrames = AtomicLong()
    private val maximumGpuSubmissionAgeMs = AtomicLong()
    private val observedDecodeAgeMs = AtomicLong()
    private val observedRenderAgeMs = AtomicLong()
    private val observationDeadlineMisses = AtomicLong()
    private val observationDefiniteDecodeMisses = AtomicLong()
    private val lastDecodeAgeLowerMs = AtomicLong(-1)
    private val lastDecodeAgeUpperMs = AtomicLong(-1)
    private val lastDecodeClockWidthMs = AtomicLong(-1)
    private val observationRenderDeadlineMisses = AtomicLong()
    private val observationMissingStamp = AtomicLong()
    private val observationUnknownClock = AtomicLong()
    private fun observe(stamp: FrameStamp?, atNs: Long, stage: String, ptsUs: Long? = null) {
        if (camera.freshness?.mode != "observe") return
        if (stamp == null) { observationMissingStamp.incrementAndGet(); return }
        val sample = freshnessClock?.currentSample(atNs)
        val age = sample?.ageUpperNs(stamp, atNs)
        if (age == null) { observationUnknownClock.incrementAndGet(); return }
        val ageMs = age / 1_000_000
        val lower = sample?.ageLowerNs(stamp, atNs)
        val width = sample?.clockIntervalWidthNs(stamp, atNs)
        if(stage == "decode") {
            observedDecodeAgeMs.updateAndGet { maxOf(it,ageMs) }
            lastDecodeAgeLowerMs.set(lower?.div(1_000_000) ?: -1)
            lastDecodeAgeUpperMs.set(ageMs)
            lastDecodeClockWidthMs.set(width?.div(1_000_000) ?: -1)
            if(lower != null && lower >= minOf(stamp.maxAgeMs,camera.freshness.maxAgeMs)*1_000_000L) {
                observationDefiniteDecodeMisses.incrementAndGet()
                if (ptsUs != null) {
                    val stages = synchronized(stampLock) { frameStages[ptsUs] }
                    // Sparse diagnostic link only; BOOT and MONO are different clock domains.
                    val bootBefore = SystemClock.elapsedRealtimeNanos()
                    val linkMono = System.nanoTime()
                    val bootAfter = SystemClock.elapsedRealtimeNanos()
                    decodeAgeOutlier(stamp, ptsUs, stages, atNs, lower, age, width ?: -1, queue?.size ?: -1,
                        ClockDomainBracket(bootBefore, linkMono, bootAfter))?.let {
                        Log.i("EufyWallTV", "${camera.sn} $it")
                    }
                }
            }
            if(age >= minOf(stamp.maxAgeMs,camera.freshness.maxAgeMs)*1_000_000L) observationDeadlineMisses.incrementAndGet()
        } else {
            observedRenderAgeMs.updateAndGet { maxOf(it,ageMs) }
            if(age >= minOf(stamp.maxAgeMs,camera.freshness.maxAgeMs)*1_000_000L) observationRenderDeadlineMisses.incrementAndGet()
        }
        if(Log.isLoggable(TIMING_TAG,Log.DEBUG)) Log.d(TIMING_TAG,
            "${camera.sn} observe stage=$stage atNs=$atNs frameId=${stamp.frameId} ingressMonoUs=${stamp.ingressMonoUs} ageLowerMs=${lower?.div(1_000_000) ?: -1} ageUpperMs=$ageMs clockIntervalWidthNs=${width ?: -1} clockRttMs=${(sample.responseNs-sample.requestStartNs)/1_000_000} clockLeaseNs=${sample.expiresNs} budgetMs=${camera.freshness.maxAgeMs} captureClockValidated=false")
    }


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
        val initialWidth = if (camera.nativeDecoderName != null) camera.width else surface.width.takeIf { it > 0 } ?: 1920
        val initialHeight = if (camera.nativeDecoderName != null) camera.height else surface.height.takeIf { it > 0 } ?: 1080
        object : DrainingVideoDecoder(mime, initialWidth, initialHeight, rotation, frames, monitoredListener, type, camera.nativeDecoderName, hardwarePinned) {
            override fun hardwareReallocated() {
                fail("Hardware decoder reassigned to a larger stream", false)
            }
            override fun beforeInputFrame(frame: FrameQueue.VideoFrame) {
                if (camera.freshness == null) return
                FrameStampReader.read(frame.data, frame.offset, frame.length, mime == MediaFormat.MIMETYPE_VIDEO_HEVC)?.let { stamp ->
                    synchronized(stampLock) {
                        stamps[frame.timestampMs] = stamp
                        frameStages[frame.timestampMs] = DecodeFrameStages(frame.receivedMonoNs, System.nanoTime(), frame.rtpReadObservation)
                        while (stamps.size > 256) {
                            val oldest = stamps.keys.first()
                            stamps.remove(oldest)
                            frameStages.remove(oldest)
                        }
                    }
                }
            }
            override fun decoderCreated(mediaCodec: MediaCodec, mediaFormat: MediaFormat) {
                frameClock = LiveFrameClock()
                check(!released && surface.holder.surface.isValid) { "Video surface unavailable" }
                decoderName = mediaCodec.name
                mediaCodec.configure(mediaFormat, freshRenderer?.inputSurface ?: surface.holder.surface, null, 0)
                val outputGeneration = if (enforcesFreshness) -1L else renderedTiming.beginGeneration()
                var first = true
                mediaCodec.setOnFrameRenderedListener({ _, timestamp, renderNs ->
                    if (Log.isLoggable(TIMING_TAG, Log.DEBUG)) Log.d(TIMING_TAG, "${camera.sn} rendered ptsUs=$timestamp atNs=$renderNs")
                    if (!released) {
                        if (enforcesFreshness) return@setOnFrameRenderedListener
                        val stamp = synchronized(stampLock) { stamps[timestamp] }
                        renderedTiming.accept(outputGeneration, stamp, renderNs, System.nanoTime())
                        observe(stamp, renderNs, "codec-render-informational")
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
                if (render) {
                    decodedFrames.incrementAndGet()
                    latestDecodedTimestamp.set(bufferInfo.presentationTimeUs)
                    lastDecodedFrameAt.set(SystemClock.elapsedRealtime())
                }
                val now = System.nanoTime()
                val target = if (render && !released && surface.holder.surface.isValid)
                    frameClock.presentationTimeNs(bufferInfo.presentationTimeUs, now) else null
                val stamp = if (camera.freshness != null) synchronized(stampLock) { stamps[bufferInfo.presentationTimeUs] } else null
                val sample = freshnessClock?.currentSample(now)
                val until = stamp?.let { sample?.deadline(it,now,camera.freshness!!.maxAgeMs) }
                if(render) observe(stamp,now,"decode",bufferInfo.presentationTimeUs)
                val expired = enforcesFreshness && (until == null || target == null || target + DISPLAY_MARGIN_NS >= until)
                if (render && Log.isLoggable(TIMING_TAG, Log.DEBUG)) Log.d(TIMING_TAG,
                    "${camera.sn} decoded ptsUs=${bufferInfo.presentationTimeUs} atNs=$now targetNs=${target ?: -1}")
                if (target == null || expired) {
                    if (render && expired) {
                        staleOutputFrames.incrementAndGet()
                        val reason=when {
                            stamp==null -> "missingStamp"
                            sample==null -> "clockUnavailable"
                            sample.epoch!=stamp.epoch -> "epochMismatch"
                            sample.ageUpperNs(stamp,now)==null -> "clockBoundInvalid"
                            until==null -> "ingressExpired"
                            else -> "targetTooLate"
                        }
                        Log.i("EufyFreshness", "${camera.sn} outputSuppressed reason=$reason atNs=$now ptsUs=${bufferInfo.presentationTimeUs} frameId=${stamp?.frameId ?: -1} ingressMonoUs=${stamp?.ingressMonoUs ?: -1} ageUpperMs=${stamp?.let { sample?.ageUpperNs(it,now)?.div(1_000_000) } ?: -1} targetNs=${target ?: -1} deadlineNs=${until ?: -1} clockRttMs=${sample?.let { (it.responseNs-it.requestStartNs)/1_000_000 } ?: -1} budgetMs=${camera.freshness!!.maxAgeMs}")
                    }
                    mediaCodec.releaseOutputBuffer(outIndex, false)
                }
                else {
                    val registered = if (stamp != null) freshRenderer?.register(stamp, target) ?: target else target
                    mediaCodec.releaseOutputBuffer(outIndex, registered)
                }
            }

            override fun decoderDestroyed(mediaCodec: MediaCodec) {}
        }
    }

    init {
        MediaCodecHelper.initialize(context.applicationContext, "")
        freshnessCover.visibility = View.VISIBLE
        if(camera.freshness?.mode == "observe") {
            freshnessState="observe • capture time unverified"
        }
        // Strict freshness is gated and expired by the owned GL renderer.
        // MediaCodec render callbacks may be batched or omitted on older TVs.
        view.addOnLayoutChangeListener(layoutListener)
        processor.videoDecoderType = DecoderType.HARDWARE // Adapter falls back to software if hardware cannot start.
        processor.videoFrameRateStabilization = false
        processor.experimentalUpdateSpsFrameWithLowLatencyParams = false
        processor.statusListener = object : RtspStatusListener {
            override fun onRtspFirstFrameRendered() { if (!released) { if (!enforcesFreshness) freshnessCover.visibility = View.INVISIBLE; onFirstFrame() } }
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
        surfaceCallback = object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                if (!released && !started) {
                    if (enforcesFreshness) {
                        try {
                            val outputGeneration = renderedTiming.beginGeneration()
                            freshRenderer = FreshVideoRenderer(holder.surface, surface.width.coerceAtLeast(1), surface.height.coerceAtLeast(1),
                                cameraSn = camera.sn, maxAgeMs = camera.freshness!!.maxAgeMs,
                                clockSample = { now -> freshnessClock?.currentSample(now) },
                                onPresented = { stamp, swapNs, until ->
                                    if (!released) renderedTiming.accept(outputGeneration, stamp, swapNs, System.nanoTime())
                                    if (shownEpoch != stamp.epoch || shownFrameId != stamp.frameId) {
                                        shownEpoch = stamp.epoch; shownFrameId = stamp.frameId; shownDeadlineNs = until
                                        synchronized(stampLock) { stamps.entries.firstOrNull { it.value.epoch == stamp.epoch && it.value.frameId == stamp.frameId }?.key }?.let { latestRenderedTimestamp.set(it) }
                                        freshnessClock?.currentSample(swapNs)?.ageUpperNs(stamp, swapNs)?.let { age -> maximumGpuSubmissionAgeMs.updateAndGet { maxOf(it, age / 1_000_000) } }
                                        lastFrameAt.set(SystemClock.elapsedRealtime())
                                        val previous = previousRenderNs.getAndSet(swapNs)
                                        if (renderedFrames.getAndIncrement() > 0) longestFrameGap.updateAndGet { maxOf(it, (swapNs - previous).coerceAtLeast(0) / 1_000_000) }
                                        if (freshFirstFrame) { freshFirstFrame = false; ui.post { if (!released) onFirstFrame() } }
                                    }
                                },
                                onState = { message -> ui.post { if (!released) {
                                    freshnessState = message ?: "bridge-ingress admitted to GPU swap"
                                    freshnessCover.visibility = View.INVISIBLE
                                    onFreshnessState(message)
                                } } },
                                onFailure = { fail(it, true) })
                        } catch (error: Exception) { fail(error.message ?: "GPU freshness renderer unavailable", true); return }
                    }
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
            override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) { freshRenderer?.resize(width,height) }
            override fun surfaceDestroyed(holder: SurfaceHolder) {
                if (!released) { processor.stop(); processor.stopDecoders(); freshRenderer?.close(); freshRenderer = null; started = false }
            }
        }
        surface.holder.addCallback(surfaceCallback)
        // A retry inherits an already-created surface, so no new creation callback
        // is expected. Post startup until the caller has registered this player.
        ui.post { if (!released && surface.holder.surface.isValid) surfaceCallback.surfaceCreated(surface.holder) }
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
        val progress = PlaybackProgress(latestInputTimestamp.get(), latestDecodedTimestamp.get(), latestRenderedTimestamp.get())
        return "decoder=$decoderName receivedUnits=${receivedUnits.get()} decodedFrames=${decodedFrames.get()} renderedFrames=${renderedFrames.get()} queuedUnits=${queue?.size ?: 0} latestDecodedPtsUs=${progress.decodedPtsUs} pendingDecodeMediaMs=${progress.pendingDecodeMediaMs} callbackPendingMediaMs=${progress.callbackPendingMediaMs} callbackPendingScope=informational-render-callback longestFrameGapMs=${longestFrameGap.get()} pacingDropped=${frameClock.dropped} pacingResets=${frameClock.resets} maxPacingWaitMs=${frameClock.maxWaitNs / 1_000_000} freshness=$freshnessState staleOutputFrames=${staleOutputFrames.get()} frameId=$shownFrameId maxGpuSubmissionIngressAgeMs=${maximumGpuSubmissionAgeMs.get()} observeDecodeMaxAgeMs=${observedDecodeAgeMs.get()} observeRenderInformationalMaxAgeMs=${observedRenderAgeMs.get()} observeDecodeDeadlineMisses=${observationDeadlineMisses.get()} observeDecodeDefinitelyLateFrames=${observationDefiniteDecodeMisses.get()} lastDecodeAgeLowerMs=${lastDecodeAgeLowerMs.get()} lastDecodeAgeUpperMs=${lastDecodeAgeUpperMs.get()} lastDecodeClockIntervalMs=${lastDecodeClockWidthMs.get()} observeRenderInformationalDeadlineMisses=${observationRenderDeadlineMisses.get()} observeMissingStamp=${observationMissingStamp.get()} observeUnknownClock=${observationUnknownClock.get()} blankReasons=${freshRenderer?.diagnostics() ?: "none"} presentationScope=${if (enforcesFreshness) "GPU-swap-admission" else "codec-render-callback"}"
    }

    private companion object { const val TIMING_TAG = "EufyFrameTiming"; const val DISPLAY_MARGIN_NS = 20_000_000L }

    fun release() {
        if (released) return
        released = true
        surface.holder.removeCallback(surfaceCallback)
        view.removeOnLayoutChangeListener(layoutListener)
        freshnessCover.visibility = View.VISIBLE
        freshnessClock?.close()
        freshRenderer?.close(); freshRenderer = null
        processor.statusListener = null; processor.dataListener = null
        processor.stop(); processor.stopDecoders()
        if (ownsSurface) tileSurface.remove()
    }
}
