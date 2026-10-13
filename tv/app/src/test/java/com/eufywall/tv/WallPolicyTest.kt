package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class WallPolicyTest {
    private val door = WallCameraState("door", mode = "always", state = "live")
    private val battery = WallCameraState("battery", hasStill = true)
    private val other = WallCameraState("other", hasStill = true)
    private fun policy() = WallPolicy().apply { applyHello(listOf(door, battery, other)) }
    private fun follow(watch: List<String> = emptyList(), dwell: Long = 5_000,
                       blank: Long = 30_000) = WallTileSpec("motion", motion = true,
        watch = watch, dwellMs = dwell, blankAfterMs = blank)

    @Test fun fixedBatteryStaysPassiveAndShowsStillDuringWake() {
        val policy = policy()
        val tile = WallTileSpec("fixed", camera = "battery")
        val asleep = policy.resolve(listOf(tile), 0).single()
        assertEquals(WallContent.SNAPSHOT, asleep.content)
        assertFalse(asleep.hold)
        policy.onStreamState("battery", "starting")
        val waking = policy.resolve(listOf(tile), 1).single()
        assertEquals(WallContent.SNAPSHOT, waking.content)
        assertTrue(waking.starting)
        assertFalse(waking.hold)
        policy.onStreamState("battery", "live")
        val live = policy.resolve(listOf(tile), 2).single()
        assertEquals(WallContent.LIVE, live.content)
        assertFalse(live.hold)
    }

    @Test fun explicitWakeLeaseExpiresAtBoundaryAndDoesNotPinUnselectedCamera() {
        val policy = policy()
        val tile = WallTileSpec("fixed", camera = "battery")
        assertTrue(policy.wake("battery", 120_000))
        assertTrue(policy.resolve(listOf(tile), 119_999).single().hold)
        assertFalse(policy.resolve(listOf(tile), 120_000).single().hold)
        assertFalse(policy.wake("door", 150_000))
        assertFalse(policy.wake("removed", 150_000))
    }

    @Test fun latestMotionHonorsWatchDwellAndExactBlankExpiry() {
        val policy = policy()
        val tile = follow(listOf("battery", "other"), blank = 10_000)
        policy.onMotion("door", 100)
        assertNull(policy.resolve(listOf(tile), 100).single().camera)
        policy.onMotion("battery", 200)
        assertEquals("battery", policy.resolve(listOf(tile), 200).single().camera)
        policy.onMotion("other", 300)
        assertEquals("battery", policy.resolve(listOf(tile), 5_199).single().camera)
        val switched = policy.resolve(listOf(tile), 5_200).single()
        assertEquals("other", switched.camera)
        assertTrue(switched.hold)
        assertEquals(WallContent.SNAPSHOT, switched.content)
        assertNotNull(policy.resolve(listOf(tile), 10_299).single().camera)
        val blank = policy.resolve(listOf(tile), 10_300).single()
        assertNull(blank.camera)
        assertFalse(blank.hold)
        assertEquals(WallContent.NONE, blank.content)
    }

    @Test fun alwaysOnMotionNeverRequestsHoldAndIdleKeepsLivePath() {
        val policy = policy()
        policy.onMotion("door", 0)
        policy.onStreamState("door", "idle")
        val tile = policy.resolve(listOf(follow()), 1).single()
        assertEquals("door", tile.camera)
        assertEquals(WallContent.LIVE, tile.content)
        assertFalse(tile.hold)
    }

    @Test fun reconnectRetainsMotionButRemovedCameraCannotReturnFromBufferedEvent() {
        val policy = policy()
        policy.onMotion("battery", 100)
        val tile = follow()
        policy.resolve(listOf(tile), 100)
        policy.applyHello(listOf(door, battery.copy(state = "starting")))
        assertEquals("battery", policy.resolve(listOf(tile), 200).single().camera)
        assertEquals(100L, policy.camera("battery")!!.lastMotionElapsedMs)
        policy.applyHello(listOf(door))
        assertFalse(policy.onMotion("battery", 300))
        assertFalse(policy.onStreamState("battery", "live"))
        assertNull(policy.resolve(listOf(tile), 300).single().camera)
        assertNull(policy.resolve(listOf(WallTileSpec("fixed", camera = "battery")), 300).single().camera)
    }

    @Test fun watchChangeAndRemovedSelectionOverrideDwell() {
        val policy = policy()
        policy.onMotion("battery", 100)
        policy.resolve(listOf(follow()), 100)
        policy.onMotion("other", 101)
        assertEquals("other", policy.resolve(listOf(follow(listOf("other"))), 102).single().camera)
    }

    @Test fun simultaneousEventsFollowLastReceivedAndIndependentTilesChooseSeparately() {
        val policy = policy()
        policy.onMotion("other", 0)
        policy.onMotion("battery", 0)
        val resolved = policy.resolve(listOf(follow(), follow(listOf("other")).copy(id = "second")), 0)
        assertEquals(listOf("battery", "other"), resolved.map { it.camera })
        assertTrue(resolved.all { it.hold })
    }

    @Test fun stoppedWallCannotRestoreOldWakeLeaseOrMotionAfterRestart() {
        val policy = policy()
        policy.onMotion("battery", 100)
        policy.wake("battery", 120_000)
        policy.resolve(listOf(follow()), 100)
        policy.clear()
        policy.applyHello(listOf(door, battery))
        assertNull(policy.resolve(listOf(follow()), 200).single().camera)
        assertFalse(policy.resolve(listOf(WallTileSpec("fixed", camera = "battery")), 200).single().hold)
    }

    @Test fun noStillMeansBlankUntilLiveAndNoMotionDoesNotWakeAnything() {
        val policy = WallPolicy().apply { applyHello(listOf(battery.copy(hasStill = false))) }
        assertFalse(policy.resolve(listOf(follow()), 0).single().hold)
        policy.onMotion("battery", 0)
        val starting = policy.resolve(listOf(follow()), 1).single()
        assertEquals(WallContent.NONE, starting.content)
        assertTrue(starting.hold)
        policy.onStreamState("battery", "live")
        assertEquals(WallContent.LIVE, policy.resolve(listOf(follow()), 2).single().content)
    }
}
