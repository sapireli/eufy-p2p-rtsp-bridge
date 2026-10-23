package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class LatencyIndicatorTest {
    private val base = 10_000_000_000L
    private val stamp = FrameStamp("epoch", 1, 0, 1, 500)
    private val clock = BridgeClockSample("epoch", 0, base, base)
    private fun state(deltaNs: Long, observation: OutputLatencyObservation? = OutputLatencyObservation(RenderedFrameTiming(stamp, base), clock, "codec-render-callback"), live: Boolean = true) =
        latencyIndicatorState(live, base + deltaNs, base, observation)

    @Test fun thresholdBoundariesUseNanosecondsWithoutTruncating() {
        assertEquals(LatencyColor.GREEN, state(500_000_000).color)
        assertEquals(LatencyColor.ORANGE, state(500_000_001).color)
        assertEquals(LatencyColor.ORANGE, state(1_500_000_000).color)
        assertEquals(LatencyColor.RED, state(1_500_000_001).color)
    }

    @Test fun agesLatestOutputRatherThanHoldingItsInitialGreenValue() {
        val output = OutputLatencyObservation(RenderedFrameTiming(stamp, base), clock, "codec-render-callback")
        assertEquals(100L, state(100_000_000, output).ageUpperMs)
        assertEquals(LatencyColor.GREEN, state(100_000_000, output).color)
        assertEquals(900L, state(900_000_000, output).ageUpperMs)
        assertEquals(LatencyColor.ORANGE, state(900_000_000, output).color)
        assertEquals(LatencyColor.RED, state(1_600_000_000, output).color)
    }

    @Test fun expiredClockAndMissingStampNeverBecomeGreen() {
        val recentOutput = base + 5_000_000_000
        val expired = OutputLatencyObservation(RenderedFrameTiming(stamp, recentOutput), clock, "codec-render-callback")
        assertEquals(LatencyColor.ORANGE, latencyIndicatorState(true, recentOutput, base, expired).color)
        val unknown = OutputLatencyObservation(RenderedFrameTiming(null, base), clock, "codec-render-callback")
        assertEquals(LatencyColor.ORANGE, state(100_000_000, unknown).color)
        assertEquals(LatencyColor.RED, state(1_500_000_001, unknown).color)
        assertEquals(LatencyColor.ORANGE, state(2_000_000_000, unknown, live = false).color)
    }

    @Test fun startupBecomesRedWithoutAnyOutputAfterBudget() {
        assertEquals(LatencyColor.ORANGE, state(1_500_000_000, null).color)
        assertEquals(LatencyColor.RED, state(1_500_000_001, null).color)
    }

    @Test fun freshCallbackForOldFrameIsRedEvenWithNoOutputGap() {
        val now = base + 2_000_000_000
        val output = OutputLatencyObservation(RenderedFrameTiming(stamp, now), clock, "codec-render-callback")
        assertEquals(LatencyColor.RED, latencyIndicatorState(true, now, base, output).color)
    }

    @Test fun invalidEpochAndFutureOutputAreUnknown() {
        val wrong = OutputLatencyObservation(RenderedFrameTiming(stamp.copy(epoch = "other"), base), clock, "codec-render-callback")
        assertEquals(LatencyColor.ORANGE, state(100_000_000, wrong).color)
        val future = OutputLatencyObservation(RenderedFrameTiming(stamp, base + 500_000_000), clock, "codec-render-callback")
        assertEquals(LatencyColor.ORANGE, state(100_000_000, future).color)
    }

    @Test fun trackerRejectsOldGenerationFutureAndOutOfOrderButReplacesWithUnstampedOutput() {
        val tracker = RenderedTimingTracker()
        val old = tracker.beginGeneration()
        assertTrue(tracker.accept(old, stamp, base, base))
        val current = tracker.beginGeneration()
        assertFalse(tracker.accept(old, stamp, base + 1, base + 1))
        assertFalse(tracker.accept(current, stamp, base + 100, base + 99))
        assertFalse(tracker.accept(current, stamp, base - 1, base + 1))
        assertTrue(tracker.accept(current, null, base + 1, base + 1))
        assertNull(tracker.snapshot()!!.stamp)
        assertEquals(base + 1, tracker.snapshot()!!.outputNs)
    }

    @Test fun accessibilityNamesScopeAndBoundRatherThanCaptureTime() {
        val status = state(100_000_000)
        assertTrue(status.description().contains("upper bound 100 milliseconds"))
        assertTrue(status.description().contains("video render callback"))
        assertTrue(status.description().contains("camera capture time unavailable"))
    }
}
