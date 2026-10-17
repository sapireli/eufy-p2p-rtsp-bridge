package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class MotionOutlineTest {
    @Test fun flashesOnlyMatchingCameraAndRestoresFocus() {
        val pulse = MotionOutline()
        pulse.motion("door", 1000)
        assertEquals(8, pulse.stroke("door", false, 1000))
        assertEquals(0, pulse.stroke("garage", false, 1000))
        assertEquals(0, pulse.stroke(null, false, 1000))
        assertEquals(0, pulse.stroke("door", false, 1250))
        assertEquals(4, pulse.stroke("door", true, 1250))
        assertEquals(8, pulse.stroke("door", true, 1500))
        assertEquals(8, pulse.stroke("door", false, 1700))
        assertEquals(0, pulse.stroke("door", false, 1750))
        assertEquals(4, pulse.stroke("door", true, 1750))
        assertNull(pulse.nextDelayMs(1750))
    }

    @Test fun repeatedMotionRestartsPulseWithoutCancellingOtherCamera() {
        val pulse = MotionOutline()
        pulse.motion("door", 1000)
        pulse.motion("garage", 1100)
        assertEquals(0, pulse.stroke("door", false, 1300))
        pulse.motion("door", 1300)
        assertEquals(8, pulse.stroke("door", false, 1300))
        assertEquals(8, pulse.stroke("garage", false, 1300))
        assertEquals(50L, pulse.nextDelayMs(1300))
        assertEquals(0, pulse.stroke("garage", false, 1350))
        assertEquals(8, pulse.stroke("door", false, 1350))
    }

    @Test fun reassignedTileUsesItsCurrentCameraAndStoppingClearsPendingWork() {
        val pulse = MotionOutline()
        pulse.motion("door", 1000)
        // Changing a tile's camera cancels its old event instead of carrying it over.
        assertEquals(8, pulse.stroke("door", false, 1000))
        pulse.cancel("door")
        assertEquals(0, pulse.stroke("door", false, 1001))
        pulse.motion("garage", 1001)
        pulse.clear()
        assertEquals(0, pulse.stroke("door", false, 1001))
        assertNull(pulse.nextDelayMs(1001))
        pulse.motion("", 1001)
        assertNull(pulse.nextDelayMs(1001))
    }
    @Test fun disablingClearsPulseAndRejectsEventsUntilEnabledWithoutReplayingHistory() {
        val pulse = MotionOutline()
        pulse.motion("tile", 1000)
        assertEquals(8, pulse.stroke("tile", true, 1000))
        pulse.enabled = false
        assertEquals(4, pulse.stroke("tile", true, 1001))
        assertEquals(0, pulse.stroke("tile", false, 1001))
        pulse.motion("tile", 1002)
        assertNull(pulse.nextDelayMs(1002))
        pulse.enabled = true
        assertEquals(0, pulse.stroke("tile", false, 1003))
        pulse.motion("tile", 1004)
        assertEquals(8, pulse.stroke("tile", false, 1004))
    }

    @Test fun configurationDefaultsOnAndPersistsExplicitOffAcrossRestartAndEdits() {
        assertTrue(WallConfiguration().motionFlash)
        val old = """{"version":1,"layout":"AUTO","columns":2,"rows":2,"tiles":[]}"""
        assertTrue(WallConfiguration.decode(old)!!.motionFlash)
        val disabled = WallConfiguration(motionFlash = false)
        val restarted = WallConfiguration.decode(disabled.encode())!!
        assertFalse(restarted.motionFlash)
        assertFalse(restarted.copy(columns = 3).motionFlash)
        assertTrue(WallConfiguration.decode(restarted.copy(motionFlash = true).encode())!!.motionFlash)
    }

}
