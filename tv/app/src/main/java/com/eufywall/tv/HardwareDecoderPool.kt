package com.eufywall.tv

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Rebalance only after a real hardware allocation failure, using parsed coded pixel area. */
internal class HardwareDecoderPool {
    class Lease internal constructor(val codec: String, val pixels: Long, val pinned: Boolean) {
        @Volatile var revoked = false
            internal set
        private val released = CountDownLatch(1)
        internal fun signalRelease() = released.countDown()
        fun awaitRelease(timeoutMs: Long): Boolean = released.await(timeoutMs, TimeUnit.MILLISECONDS)
    }
    private val allocationLock = Any()
    fun <T> allocate(block: () -> T): T = synchronized(allocationLock, block)
    private val leases = mutableSetOf<Lease>()

    @Synchronized fun register(codec: String, pixels: Long, pinned: Boolean = false): Lease =
        Lease(codec, pixels, pinned).also { leases.add(it) }

    /** Pinned owners cannot be evicted; equal-cost automatic feeds never evict each other. The victim retains ownership until release. */
    @Synchronized fun reclaim(codec: String, pixels: Long, pinned: Boolean = false): Lease? =
        leases.filter { it.codec == codec && !it.revoked && !it.pinned && (pinned || it.pixels < pixels) }
            .minWithOrNull(compareBy<Lease> { it.pinned }.thenBy { it.pixels })?.also { it.revoked = true }

    @Synchronized fun release(lease: Lease) {
        leases.remove(lease)
        lease.signalRelease()
    }

    companion object { val shared = HardwareDecoderPool() }
}

/** MediaCodec resource error plus native ENOMEM/EBUSY reported by older OMX drivers. */
internal fun hardwareResourcesExhausted(code: Int): Boolean = code == 1100 || code == -12 || code == -16
