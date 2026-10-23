package com.eufywall.tv

internal enum class LatencyColor { GREEN, ORANGE, RED }
internal data class RenderedFrameTiming(val stamp: FrameStamp?, val outputNs: Long)
internal data class OutputLatencyObservation(val frame: RenderedFrameTiming, val clock: BridgeClockSample?, val scope: String)
internal data class LatencyIndicatorState(val color: LatencyColor, val ageUpperMs: Long?, val silenceMs: Long?, val scope: String) {
    fun description(): String {
        val measurement = ageUpperMs?.let { "latest output age upper bound $it milliseconds" } ?: "timing unavailable"
        val position = when (scope) {
            "codec-render-callback" -> "measured at the video render callback"
            "GPU-swap-admission" -> "measured at graphics submission"
            else -> "output unavailable"
        }
        return "Bridge-to-display estimate, camera capture time unavailable. ${color.name.lowercase()}: $measurement; $position" +
            (silenceMs?.let { "; no new output for $it milliseconds" } ?: "")
    }
}

/** Only accepted output callbacks advance this state; input/decode progress cannot make it green. */
internal class RenderedTimingTracker {
    private var generation = 0L
    private var latest: RenderedFrameTiming? = null
    @Synchronized fun beginGeneration(): Long = ++generation
    @Synchronized fun accept(callbackGeneration: Long, stamp: FrameStamp?, outputNs: Long, nowNs: Long): Boolean {
        if (callbackGeneration != generation || outputNs <= 0 || outputNs > nowNs || latest?.let { outputNs <= it.outputNs } == true) return false
        // A newer unstamped output must replace the old stamped output too.
        latest = RenderedFrameTiming(stamp, outputNs)
        return true
    }
    @Synchronized fun snapshot(): RenderedFrameTiming? = latest
}

internal fun latencyIndicatorState(live: Boolean, nowNs: Long, liveSinceNs: Long, observation: OutputLatencyObservation?): LatencyIndicatorState {
    val scope = observation?.scope ?: "output unavailable"
    if (!live) return LatencyIndicatorState(LatencyColor.ORANGE, null, null, "idle / output unavailable")
    val frame = observation?.frame
    val outputNs = frame?.outputNs ?: liveSinceNs
    if (nowNs < outputNs || nowNs < liveSinceNs) return LatencyIndicatorState(LatencyColor.ORANGE, null, null, scope)
    val silenceNs = nowNs - outputNs
    val ageNs = frame?.stamp?.let { observation.clock?.ageUpperNs(it, nowNs) }
    val color = when {
        silenceNs > 1_500_000_000L -> LatencyColor.RED
        ageNs == null -> LatencyColor.ORANGE
        ageNs > 1_500_000_000L -> LatencyColor.RED
        ageNs > 500_000_000L -> LatencyColor.ORANGE
        else -> LatencyColor.GREEN
    }
    return LatencyIndicatorState(color, ageNs?.div(1_000_000), silenceNs / 1_000_000, scope)
}
