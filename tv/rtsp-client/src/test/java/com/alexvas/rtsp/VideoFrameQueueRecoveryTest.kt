package com.alexvas.rtsp

import com.alexvas.rtsp.codec.FrameQueue.VideoFrame
import com.alexvas.rtsp.codec.VideoCodecType
import com.alexvas.rtsp.codec.VideoFrameQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.*
import org.junit.Test

class VideoFrameQueueRecoveryTest {
    private fun nal(type: Int, value: Int = 1) = byteArrayOf(0, 0, 0, 1, type.toByte(), value.toByte())
    private fun frame(id: Long, key: Boolean = false, data: ByteArray = nal(if (key) 5 else 1), codec: VideoCodecType = VideoCodecType.H264) =
        VideoFrame(codec, key, data, 0, data.size, id, receivedMonoNs = id)

    @Test fun overflowClearsBacklogAndRejectsDependentPicturesUntilIdr() {
        val queue = VideoFrameQueue(2)
        assertTrue(queue.push(frame(1)))
        assertTrue(queue.push(frame(2)))
        assertFalse(queue.push(frame(3)))
        assertEquals(0, queue.size)
        assertFalse(queue.push(frame(4)))
        assertNull(queue.pop(0))
        val idr = frame(5, true)
        assertTrue(queue.push(idr))
        assertSame(idr, queue.pop(0))
        val next = frame(6)
        assertTrue(queue.push(next))
        assertSame(next, queue.pop(0))
    }

    @Test fun overflowingIdrCanResumeImmediatelyAndReissuesRetainedParameterSets() {
        val queue = VideoFrameQueue(1)
        val config = nal(7) + nal(8)
        queue.push(frame(0, true, config)) // SDP bootstrap, consumed before media.
        queue.pop(0)
        queue.push(frame(1))
        val idr = frame(2, true)
        assertTrue(queue.push(idr))
        val recovered = queue.pop(0)!!
        assertArrayEquals(config + idr.data, recovered.data)
        assertEquals(idr.timestampMs, recovered.timestampMs)
        assertEquals(idr.receivedMonoNs, recovered.receivedMonoNs)
        assertEquals(0, queue.size)
    }

    @Test fun latestInBandConfigurationWinsAndIsNotDuplicatedOnRecovery() {
        val queue = VideoFrameQueue(1)
        queue.push(frame(0, true, nal(7, 1) + nal(8, 1))); queue.pop(0)
        val latest = nal(7, 2) + nal(8, 2) + nal(5)
        queue.push(frame(1, true, latest)); queue.pop(0)
        queue.push(frame(2)); assertFalse(queue.push(frame(3)))
        val idr = frame(4, true, latest)
        queue.push(idr)
        assertSame(idr, queue.pop(0))
        queue.push(frame(5)); assertFalse(queue.push(frame(6)))
        queue.push(frame(7, true))
        assertArrayEquals(nal(7, 2) + nal(8, 2) + nal(5), queue.pop(0)!!.data)
    }

    @Test fun hevcRecoversWithVpsSpsPpsAndClearingStartsANewSession() {
        val queue = VideoFrameQueue(1)
        val config = nal(32 shl 1) + nal(33 shl 1) + nal(34 shl 1)
        queue.push(frame(0, true, config, VideoCodecType.H265)); queue.pop(0)
        queue.push(frame(1, codec = VideoCodecType.H265))
        assertFalse(queue.push(frame(2, codec = VideoCodecType.H265)))
        val idr = frame(3, true, nal(19 shl 1), VideoCodecType.H265)
        queue.push(idr)
        assertArrayEquals(config + idr.data, queue.pop(0)!!.data)
        queue.push(frame(4)); queue.push(frame(5))
        queue.clear()
        val next = frame(6)
        assertTrue(queue.push(next)) // New connection restores its normal bootstrap path.
        assertSame(next, queue.pop(0))
    }

    @Test fun waitingConsumerCannotTakeADependentPictureAfterOverflow() {
        val queue = VideoFrameQueue(1)
        queue.push(frame(1)); assertFalse(queue.push(frame(2)))
        val pool = Executors.newSingleThreadExecutor()
        val waiting = CountDownLatch(1)
        try {
            val read = pool.submit<VideoFrame?> { waiting.countDown(); queue.pop(1000) }
            assertTrue(waiting.await(1, TimeUnit.SECONDS))
            assertFalse(queue.push(frame(3)))
            val idr = frame(4, true)
            assertTrue(queue.push(idr))
            assertSame(idr, read.get(1, TimeUnit.SECONDS))
        } finally { pool.shutdownNow() }
    }
    @Test fun configurationOnlyAndFalselyFlaggedKeyframeCannotOpenRecoveryGate() {
        val queue = VideoFrameQueue(1)
        queue.push(frame(1)); assertFalse(queue.push(frame(2)))
        val config = nal(7, 3) + nal(8, 3)
        assertFalse(queue.push(frame(3, true, config)))
        assertFalse(queue.push(frame(4, true, nal(1))))
        assertNull(queue.pop(0))
        val idr = frame(5, true)
        assertTrue(queue.push(idr))
        assertArrayEquals(config + idr.data, queue.pop(0)!!.data)
    }

    @Test fun hevcCraDoesNotOpenGateThatRequiresAnIndependentIdrPicture() {
        val queue = VideoFrameQueue(1)
        queue.push(frame(1, codec = VideoCodecType.H265))
        assertFalse(queue.push(frame(2, codec = VideoCodecType.H265)))
        assertFalse(queue.push(frame(3, true, nal(21 shl 1), VideoCodecType.H265)))
        assertFalse(queue.push(frame(4, codec = VideoCodecType.H265)))
        val idr = frame(5, true, nal(20 shl 1), VideoCodecType.H265)
        assertTrue(queue.push(idr))
        assertSame(idr, queue.pop(0))
    }

}
