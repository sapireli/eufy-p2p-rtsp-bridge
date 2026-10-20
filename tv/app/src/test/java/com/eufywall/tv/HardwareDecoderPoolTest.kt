package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class HardwareDecoderPoolTest {
    @Test fun reclaimedSlotIsReservedWhileVictimReleasesWithoutDeadlock() {
        val pool = HardwareDecoderPool()
        val victim = pool.register("avc", 100)
        val waiting = java.util.concurrent.CountDownLatch(1)
        val contenderStarted = java.util.concurrent.CountDownLatch(1)
        val competitorEntered = java.util.concurrent.atomic.AtomicBoolean(false)
        val failure = java.util.concurrent.atomic.AtomicReference<Throwable?>(null)
        val pinned = Thread {
            try {
                pool.allocate {
                    assertSame(victim, pool.reclaim("avc", 50, true))
                    waiting.countDown()
                    assertTrue(victim.awaitRelease(1000))
                    assertFalse(competitorEntered.get())
                    pool.register("avc", 50, true)
                }
            } catch (e: Throwable) { failure.set(e) }
        }
        pinned.start()
        assertTrue(waiting.await(1, java.util.concurrent.TimeUnit.SECONDS))
        val contender = Thread {
            contenderStarted.countDown()
            pool.allocate { competitorEntered.set(true) }
        }
        contender.start()
        assertTrue(contenderStarted.await(1, java.util.concurrent.TimeUnit.SECONDS))
        assertFalse(competitorEntered.get())
        // Releasing an existing decoder must not require the allocation lock.
        pool.release(victim)
        pinned.join(1500); contender.join(1500)
        assertFalse(pinned.isAlive); assertFalse(contender.isAlive)
        failure.get()?.let { throw AssertionError(it) }
        assertTrue(competitorEntered.get())
    }

    @Test fun pinnedCameraKeepsHardwareAgainstLargerAutomaticStream() {
        val pool = HardwareDecoderPool()
        val door = pool.register("avc", 784L * 1080, true)
        val balcony = pool.register("avc", 1920L * 1080)
        assertSame(balcony, pool.reclaim("avc", 3840L * 2160))
        assertFalse(door.revoked)
        pool.release(balcony)
        val replacement = pool.register("avc", 1920L * 1080)
        assertSame(replacement, pool.reclaim("avc", 640L * 480, true))
        assertFalse(door.revoked)
        assertTrue(balcony.revoked)
    }
    @Test fun pinCanTakeHardwareFromLargerAutomaticOwnerButNotEqualPriorityEqualSize() {
        val pool = HardwareDecoderPool()
        val large = pool.register("avc", 1920L * 1080)
        assertSame(large, pool.reclaim("avc", 784L * 1080, true))
        pool.release(large)
        pool.register("avc", 784L * 1080, true)
        assertNull(pool.reclaim("avc", 784L * 1080, true))
        assertNull(pool.reclaim("avc", 3840L * 2160, true))
    }
    @Test fun onlyResourceFailuresCanEvictAnotherDecoder() {
        for (code in listOf(1100, -12, -16)) assertTrue(hardwareResourcesExhausted(code))
        for (code in listOf(1101, -22, 0, -1)) assertFalse(hardwareResourcesExhausted(code))
    }
    @Test fun hardwareFailureReclaimsOnlySmallerMatchingCodecAndWaitsForActualRelease() {
        val pool = HardwareDecoderPool()
        val door = pool.register("avc", 784L * 1080)
        val wyze = pool.register("avc", 1920L * 1080)
        val hevc = pool.register("hevc", 640L * 480)
        assertNull(pool.reclaim("avc", 784L * 1080))
        assertSame(door, pool.reclaim("avc", 1920L * 1080))
        assertTrue(door.revoked)
        assertFalse(wyze.revoked)
        assertFalse(hevc.revoked)
        assertFalse(door.awaitRelease(0))
        assertNull(pool.reclaim("avc", 1920L * 1080))
        pool.release(door)
        assertTrue(door.awaitRelease(0))
        assertNull(pool.reclaim("avc", 1920L * 1080))
    }
    @Test fun choosesLowestCostAndDoesNotReuseReleasedOwner() {
        val pool = HardwareDecoderPool()
        val mid = pool.register("avc", 100)
        val small = pool.register("avc", 50)
        assertSame(small, pool.reclaim("avc", 200))
        pool.release(small)
        assertSame(mid, pool.reclaim("avc", 200))
        pool.release(mid)
        assertNull(pool.reclaim("avc", 200))
    }
}
