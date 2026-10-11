package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class LiveFrameClockTest {
    @Test fun smoothsArrivalJitterWithoutMovingTheClockLater() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        val arrivalJitter = listOf(0L, 20_000_000L, 5_000_000L, 30_000_000L, 0L)
        arrivalJitter.forEachIndexed { i, jitter ->
            val pts = i * 66_667L
            val now = start + pts * 1_000 + jitter
            assertEquals(start + pts * 1_000 + 67_000_000L, clock.presentationTimeNs(pts, now))
        }
        assertEquals(0L, clock.dropped)
    }

    @Test fun showsEveryNewestPictureAfterAContinuousDeliveryDelayWithoutManufacturingAFreeze() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        clock.presentationTimeNs(0, start)
        // A half-second arrival delay is less than the timestamp-reset threshold. The
        // earlier minimum remains in the two-second window throughout this sequence.
        repeat(20) { i ->
            val pts = (i + 1) * 66_667L
            val now = start + pts * 1_000 + 500_000_000L
            assertEquals(now, clock.presentationTimeNs(pts, now))
        }
        assertEquals(0L, clock.dropped)
        assertEquals(0L, clock.resets)
    }

    @Test fun rejectsDuplicateAndOutOfOrderPicturesWhileRetainingNewLatePictures() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        clock.presentationTimeNs(0, start)
        val late = start + 500_000_000L
        assertEquals(late, clock.presentationTimeNs(66_667, late))
        assertNull(clock.presentationTimeNs(66_667, late + 1_000_000L))
        assertEquals(1L, clock.dropped)
        assertNull(clock.presentationTimeNs(65_667, late + 2_000_000L))
        assertEquals(2L, clock.dropped)
        assertNotNull(clock.presentationTimeNs(133_334, late + 66_667_000L))
    }

    @Test fun staysBoundedOverAnHourWithClockDriftAndBursts() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        repeat(54_000) { i ->
            val pts = i * 66_667L
            val now = start + pts * 1_000 + i * 10_000L + (i % 7) * 3_000_000L
            val target = clock.presentationTimeNs(pts, now)
            assertNotNull(target)
            assertTrue(target!! >= now)
            assertTrue(target - now <= 67_000_000)
        }
        assertEquals(0L, clock.dropped)
    }

    @Test fun recoversFromTimestampResetAndLongSourcePause() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        clock.presentationTimeNs(5_000_000, start)
        assertNotNull(clock.presentationTimeNs(0, start + 66_667_000))
        assertNotNull(clock.presentationTimeNs(66_667, start + 5_000_000_000))
        assertEquals(2L, clock.resets)
    }
}
