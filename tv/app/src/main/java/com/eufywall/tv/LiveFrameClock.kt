package com.eufywall.tv

import java.util.ArrayDeque

/** Maps media time to monotonic display time without sleeping in the decoder thread.
 * The fastest decoded arrival in a rolling window establishes the clock offset.
 * Waiting and lateness are bounded; this is not a growing playback queue.
 */
internal class LiveFrameClock(private val budgetNs: Long = 67_000_000L) {
    private data class Sample(val atNs: Long, val offsetNs: Long)
    private val samples = ArrayDeque<Sample>()
    private var previousPtsUs: Long? = null
    private var previousAtNs = 0L
    private var lastPresentedPtsUs: Long? = null
    @Volatile var dropped = 0L
        private set
    @Volatile var resets = 0L
        private set
    @Volatile var maxWaitNs = 0L
        private set

    /** Null means discard this already decoded picture, preserving decoder reference state. */
    fun presentationTimeNs(ptsUs: Long, nowNs: Long): Long? {
        val previous = previousPtsUs
        if (previous != null && (ptsUs < previous - 1_000_000L ||
                kotlin.math.abs((ptsUs - previous) * 1_000L - (nowNs - previousAtNs)) > 1_000_000_000L)) {
            samples.clear()
            lastPresentedPtsUs = null
            resets++
        }
        previousPtsUs = ptsUs
        previousAtNs = nowNs
        while (samples.isNotEmpty() && nowNs - samples.first.atNs > 2_000_000_000L) samples.removeFirst()
        val offset = nowNs - ptsUs * 1_000L
        // Monotonic minimum deque: discard samples superseded by an earlier arrival.
        while (samples.isNotEmpty() && samples.last.offsetNs >= offset) samples.removeLast()
        samples.addLast(Sample(nowNs, offset))
        val target = ptsUs * 1_000L + samples.first.offsetNs + budgetNs
        if (lastPresentedPtsUs?.let { ptsUs <= it } == true || nowNs - target > budgetNs) {
            dropped++
            return null
        }
        lastPresentedPtsUs = ptsUs
        val bounded = target.coerceIn(nowNs, nowNs + budgetNs)
        maxWaitNs = maxOf(maxWaitNs, bounded - nowNs)
        return bounded
    }
}
