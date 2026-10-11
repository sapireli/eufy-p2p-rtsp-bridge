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

    @Test fun discardsLateDecodedPicturesAndRecoversWithoutAQueue() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        clock.presentationTimeNs(0, start)
        assertNull(clock.presentationTimeNs(66_667, start + 300_000_000))
        assertNotNull(clock.presentationTimeNs(333_335, start + 333_335_000))
        assertEquals(1L, clock.dropped)
    }

    @Test fun staysBoundedOverAnHourWithClockDriftAndBursts() {
        val clock = LiveFrameClock()
        val start = 10_000_000_000L
        repeat(54_000) { i ->
            val pts = i * 66_667L
            val now = start + pts * 1_000 + i * 10_000L + (i % 7) * 3_000_000L
            clock.presentationTimeNs(pts, now)?.let {
                assertTrue(it >= now)
                assertTrue(it - now <= 67_000_000)
            }
        }
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
