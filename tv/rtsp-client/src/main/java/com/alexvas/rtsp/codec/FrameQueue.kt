package com.alexvas.rtsp.codec

import com.alexvas.rtsp.codec.FrameQueue.VideoFrame
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

enum class VideoCodecType {
    H264, H265, UNKNOWN
}

enum class AudioCodecType {
    AAC_LC, G711_ALAW, G711_MLAW, UNKNOWN
}

/** Compressed pictures cannot be dropped independently of their reference chain. */
class VideoFrameQueue(frameQueueCapacity: Int): FrameQueue<FrameQueue.VideoFrame>(frameQueueCapacity) {
    private val lock = ReentrantLock()
    private val available = lock.newCondition()
    private var awaitingKeyframe = false
    private val parameters = linkedMapOf<Int, ByteArray>()
    private var parameterCodec = VideoCodecType.UNKNOWN

    override fun push(frame: VideoFrame): Boolean = lock.withLock {
        if (parameterCodec != frame.codecType) {
            parameters.clear()
            parameterCodec = frame.codecType
        }
        // Assemblers include current in-band parameter sets on IDRs. SDP's initial
        // configuration is also submitted as a keyframe before media starts.
        val info = if (frame.isKeyframe) nalInfo(frame) else NalInfo(emptyMap(), false)
        val nals = info.parameters
        nals.forEach { (type, bytes) -> parameters[type] = bytes }
        if (queue.remainingCapacity() == 0) {
            queue.clear()
            awaitingKeyframe = true
        }
        if (awaitingKeyframe && !info.randomAccess) return false
        val next = if (awaitingKeyframe) {
            val missing = parameters.toSortedMap().filterKeys { it !in nals }.values
            val prefixSize = missing.sumOf { it.size }
            if (prefixSize > 0) {
                val data = ByteArray(prefixSize + frame.length)
                var offset = 0
                missing.forEach { it.copyInto(data, offset); offset += it.size }
                frame.data.copyInto(data, offset, frame.offset, frame.offset + frame.length)
                frame.copy(data = data, offset = 0, length = data.size)
            } else frame
        } else frame
        awaitingKeyframe = false
        check(queue.offer(next)) // Admission and overflow recovery share this lock.
        available.signal()
        true
    }

    override fun pop(timeout: Long): VideoFrame? = lock.withLock {
        var remaining = TimeUnit.MILLISECONDS.toNanos(timeout.coerceAtLeast(0))
        try {
            while (queue.isEmpty()) {
                if (remaining <= 0) return null
                remaining = available.awaitNanos(remaining)
            }
            queue.poll()
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            null
        }
    }

    override fun clear() = lock.withLock {
        queue.clear()
        awaitingKeyframe = false
        parameters.clear()
        parameterCodec = VideoCodecType.UNKNOWN
    }

    private data class NalInfo(val parameters: Map<Int, ByteArray>, val randomAccess: Boolean)

    private fun nalInfo(frame: VideoFrame): NalInfo {
        val found = linkedMapOf<Int, ByteArray>()
        var randomAccess = false
        val starts = mutableListOf<Pair<Int, Int>>()
        val end = frame.offset + frame.length
        var i = frame.offset
        while (i + 3 < end) {
            val prefix = if (frame.data[i] == 0.toByte() && frame.data[i + 1] == 0.toByte()) {
                if (frame.data[i + 2] == 1.toByte()) 3
                else if (frame.data[i + 2] == 0.toByte() && frame.data[i + 3] == 1.toByte()) 4 else 0
            } else 0
            if (prefix > 0) { starts += i to prefix; i += prefix } else i++
        }
        starts.forEachIndexed { index, (start, prefix) ->
            val nalEnd = starts.getOrNull(index + 1)?.first ?: end
            if (start + prefix >= nalEnd) return@forEachIndexed
            val header = frame.data[start + prefix].toInt() and 255
            val type = if (frame.codecType == VideoCodecType.H265) (header shr 1) and 63 else header and 31
            if (frame.codecType == VideoCodecType.H265 && type in 19..20 || frame.codecType == VideoCodecType.H264 && type == 5) randomAccess = true
            val config = if (frame.codecType == VideoCodecType.H265) type in 32..34 else type == 7 || type == 8
            if (config && nalEnd - start <= 65536) found[type] = frame.data.copyOfRange(start, nalEnd)
        }
        return NalInfo(found, randomAccess)
    }
}
class AudioFrameQueue(frameQueueCapacity: Int): FrameQueue<FrameQueue.AudioFrame>(frameQueueCapacity)

/**
 * Queue for concurrent adding/removing audio/video frames.
 */
open class FrameQueue<T>(private val frameQueueCapacity: Int) {

    interface Frame {
        val data: ByteArray
        val offset: Int
        val length: Int
        val timestampMs: Long  // presentation time in msec
    }

    data class VideoFrame(
        /** Only H264 codec supported */
        val codecType: VideoCodecType,
        /** Indicates whether it is a keyframe or not */
        val isKeyframe: Boolean,
        override val data: ByteArray,
        override val offset: Int,
        override val length: Int,
        /** Video frame timestamp (msec) generated by camera */
        override val timestampMs: Long,
        /** Captured (received) video frame timestamp (msec). If -1, not supported. */
        val capturedTimestampMs: Long = -1,
        /** Monotonic time at complete parsed AU callback entry, before queue insertion. */
        val receivedMonoNs: Long = 0,
        val rtpReadObservation: com.alexvas.rtsp.parser.RtpReadObservation? = null
    ) : Frame

    data class AudioFrame(
        val codecType: AudioCodecType,
//      val sampleRate: Int,
        override val data: ByteArray,
        override val offset: Int,
        override val length: Int,
        override val timestampMs: Long,
    ) : Frame

    protected val queue = ArrayBlockingQueue<T>(frameQueueCapacity)

    val size: Int
        get() = queue.size

    val capacity: Int
        get() = frameQueueCapacity

    @Throws(InterruptedException::class)
    open fun push(frame: T): Boolean {
        if (queue.offer(frame, 5, TimeUnit.MILLISECONDS)) {
            return true
        }
//        Log.w(TAG, "Cannot add frame, queue is full")
        return false
    }

    @Throws(InterruptedException::class)
    open fun pop(timeout: Long = 1000): T? {
        try {
            val frame: T? = queue.poll(timeout, TimeUnit.MILLISECONDS)
//            if (frame == null) {
//                Log.w(TAG, "Cannot get frame within 1 sec, queue is empty")
//            }
            return frame
        } catch (e: InterruptedException) {
            Thread.currentThread().interrupt()
        }
        return null
    }

    open fun clear() {
        queue.clear()
    }

    fun copyInto(dstFrameQueue: FrameQueue<T>) {
        dstFrameQueue.queue.addAll(queue)
    }

    companion object {
        private val TAG: String = FrameQueue::class.java.simpleName
    }

}
