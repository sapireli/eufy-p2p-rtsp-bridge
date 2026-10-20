package com.eufywall.tv

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Rebalance only after a real hardware allocation failure, using parsed coded pixel area. */
internal class HardwareDecoderPool {
    class Lease internal constructor(val codec: String, val pixels: Long) {
        @Volatile var revoked = false
            internal set
        private val released = CountDownLatch(1)
        internal fun signalRelease() = released.countDown()
        fun awaitRelease(timeoutMs: Long): Boolean = released.await(timeoutMs, TimeUnit.MILLISECONDS)
    }
    val allocationLock = Any()
    private val leases = mutableSetOf<Lease>()

    @Synchronized fun register(codec: String, pixels: Long): Lease =
        Lease(codec, pixels).also { leases.add(it) }

    /** Equal-cost feeds never evict one another. The victim retains ownership until release. */
    @Synchronized fun reclaim(codec: String, pixels: Long): Lease? =
        leases.filter { it.codec == codec && !it.revoked && it.pixels < pixels }
            .minByOrNull { it.pixels }?.also { it.revoked = true }

    @Synchronized fun release(lease: Lease) {
        leases.remove(lease)
        lease.signalRelease()
    }

    companion object { val shared = HardwareDecoderPool() }
}

/** MediaCodec resource error plus native ENOMEM/EBUSY reported by older OMX drivers. */
internal fun hardwareResourcesExhausted(code: Int): Boolean = code == 1100 || code == -12 || code == -16
