package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class HardwareDecoderPoolTest {
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
