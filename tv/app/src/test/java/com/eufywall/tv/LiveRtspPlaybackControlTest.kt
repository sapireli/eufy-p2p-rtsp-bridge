package com.eufywall.tv

import org.junit.Assert.assertEquals
import org.junit.Test

class LiveRtspPlaybackControlTest {
    @Test fun ordinaryJitterDoesNotStartCatchUp() {
        val control = LiveRtspPlaybackControl()
        listOf(200L, 850L, 1_400L, 1_000L).forEach {
            assertEquals(1f, control.speedFor(it, true), 0f)
        }
    }

    @Test fun burstBacklogDrainsAndReturnsToNormalSpeed() {
        val control = LiveRtspPlaybackControl()
        assertEquals(1.5f, control.speedFor(4_100, true), 0f)
        assertEquals(1.25f, control.speedFor(2_000, true), 0f)
        assertEquals(1.1f, control.speedFor(1_000, true), 0f)
        assertEquals(1f, control.speedFor(650, true), 0f)
        assertEquals(1f, control.speedFor(1_000, true), 0f)
    }

    @Test fun StarvationResetsCatchUpAndRecoveredBurstStartsItAgain() {
        val control = LiveRtspPlaybackControl()
        control.speedFor(4_100, true)
        assertEquals(1f, control.speedFor(4_100, false), 0f)
        assertEquals(1f, control.speedFor(1_000, true), 0f)
        assertEquals(1.5f, control.speedFor(4_100, true), 0f)
    }

    @Test fun CamerasHaveIndependentCatchUpState() {
        val door = LiveRtspPlaybackControl()
        val garage = LiveRtspPlaybackControl()
        door.speedFor(4_100, true)
        assertEquals(1.1f, door.speedFor(1_000, true), 0f)
        assertEquals(1f, garage.speedFor(1_000, true), 0f)
    }
}
