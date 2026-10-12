package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class DecoderProgressWatchdogTest {
    private val watchdog = DecoderProgressWatchdog(15_000, 5_000)

    @Test fun healthyDecodeForSixSecondsSurvivesSilentRenderCallbacks() {
        // A render callback occurs at startup, then none arrive for six seconds.
        // The old live watchdog would expire at 5s. Reliable decoder output
        // nevertheless advances every 500ms and must keep this player alive.
        val started = 10_000L
        for (elapsed in 500L..6_000L step 500L) {
            val now = started + elapsed
            assertNull(watchdog.stalledForMs(now, started, elapsed / 500, now))
        }
    }

    @Test fun stoppedDecodeAndRenderReachLiveTimeoutExactly() {
        val started = 10_000L
        val lastOutput = started + 1_000
        assertNull(watchdog.stalledForMs(lastOutput + 4_999, started, 15, lastOutput))
        assertEquals(5_000L, watchdog.stalledForMs(lastOutput + 5_000, started, 15, lastOutput))
    }

    @Test fun incomingPacketsWithoutFirstDecodedOutputCannotExtendStartupDeadline() {
        val started = 10_000L
        // The decoder's timestamp is initialized at construction. It is not progress
        // until a real output exists; incoming packets/configuration don't count.
        assertNull(watchdog.stalledForMs(started + 14_999, started, 0, started + 14_999))
        assertEquals(15_000L, watchdog.stalledForMs(started + 15_000, started, 0, started + 15_000))
        assertNull(watchdog.stalledForMs(started + 15_000, started, 1, started + 15_000))
    }
}
