package com.eufywall.tv

/** Short tile-keyed pulses; no playback state or continuously running animation. */
internal class MotionOutline {
    var enabled = true
        set(value) {
            field = value
            if (!value) clear()
        }
    private val started = mutableMapOf<String, Long>()
    private val phaseMs = 250L
    private val durationMs = phaseMs * 3

    fun motion(tile: String, nowMs: Long) {
        if (enabled && tile.isNotBlank()) started[tile] = nowMs
    }

    fun stroke(tile: String?, focused: Boolean, nowMs: Long): Int {
        val elapsed = tile?.let { started[it] }?.let { nowMs - it }
        val flashing = elapsed != null && elapsed >= 0 && elapsed < durationMs && (elapsed / phaseMs) % 2 == 0L
        return if (flashing) 8 else if (focused) 4 else 0
    }

    /** Schedule only the next visible transition, and discard expired events. */
    fun nextDelayMs(nowMs: Long): Long? {
        started.entries.removeAll { nowMs - it.value >= durationMs }
        return started.values.minOfOrNull { phaseMs - (nowMs - it).coerceAtLeast(0) % phaseMs }
    }

    fun cancel(tile: String) { started.remove(tile) }
    fun clear() = started.clear()
}
