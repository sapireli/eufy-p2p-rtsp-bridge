package com.eufywall.tv

/** Decoder liveness only; neither physical presentation nor ingress/capture freshness. */
internal class DecoderProgressWatchdog(
    private val firstOutputTimeoutMs: Long,
    private val liveOutputTimeoutMs: Long,
) {
    fun stalledForMs(nowMs: Long, startedAtMs: Long, decodedFrames: Long, lastDecodedAtMs: Long): Long? {
        val hasOutput = decodedFrames > 0
        val silentForMs = (nowMs - if (hasOutput) lastDecodedAtMs else startedAtMs).coerceAtLeast(0)
        val timeout = if (hasOutput) liveOutputTimeoutMs else firstOutputTimeoutMs
        return silentForMs.takeIf { it >= timeout }
    }
}
