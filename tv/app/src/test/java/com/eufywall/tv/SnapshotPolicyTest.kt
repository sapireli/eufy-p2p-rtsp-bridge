package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class SnapshotPolicyTest {
    @Test fun motionVersionDoesNotEvictTheSameCameraStill() {
        assertEquals(SnapshotPolicy.cacheKey("http://bridge:3000/snapshot/A?v=1"),
            SnapshotPolicy.cacheKey("http://bridge:3000/snapshot/A?v=99&v=100"))
    }
    @Test fun cacheIdentityPreservesCameraHostAndOtherParameters() {
        assertEquals("http://bridge:3000/snapshot/A?token=secret&size=small",
            SnapshotPolicy.cacheKey("http://bridge:3000/snapshot/A?token=secret&v=3&size=small"))
        assertNotEquals(SnapshotPolicy.cacheKey("http://bridge:3000/snapshot/A?v=1"),
            SnapshotPolicy.cacheKey("http://bridge:3000/snapshot/B?v=1"))
        assertNotEquals(SnapshotPolicy.cacheKey("http://bridge:3000/snapshot/A?v=1"),
            SnapshotPolicy.cacheKey("http://other:3000/snapshot/A?v=1"))
        assertEquals("http://bridge/snapshot/A?V=keep&value=keep",
            SnapshotPolicy.cacheKey("http://bridge/snapshot/A?V=keep&v=1&value=keep"))
    }
    @Test fun smallThumbnailsKeepNativeDimensions() {
        assertEquals(1, SnapshotPolicy.sampleSize(640, 480))
    }
    @Test fun largePortraitAndWideFramesStayWithinPixelBudget() {
        for ((width, height) in listOf(1600 to 2200, 3840 to 2160, 32768 to 32768, 32768 to 1)) {
            val sample = SnapshotPolicy.sampleSize(width, height)!!
            val sampledWidth = (width.toLong() + sample - 1) / sample
            val sampledHeight = (height.toLong() + sample - 1) / sample
            assertTrue(sampledWidth <= 1024)
            assertTrue(sampledHeight <= 1024)
            assertTrue(sampledWidth * sampledHeight <= 524288)
            assertEquals(0, sample and (sample - 1))
        }
    }
    @Test fun corruptOrOversizedHeadersAreRejectedBeforeDecode() {
        assertNull(SnapshotPolicy.sampleSize(-1, 640))
        assertNull(SnapshotPolicy.sampleSize(640, 0))
        assertNull(SnapshotPolicy.sampleSize(Int.MAX_VALUE, Int.MAX_VALUE))
    }
}
