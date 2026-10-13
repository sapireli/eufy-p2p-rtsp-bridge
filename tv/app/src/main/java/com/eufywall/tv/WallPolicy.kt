package com.eufywall.tv

/** Tile behavior is independent of geometry so changing a layout does not wake cameras. */
data class WallTileSpec(
    val id: String,
    val camera: String = "",
    val motion: Boolean = false,
    val watch: List<String> = emptyList(),
    val dwellMs: Long = 5_000,
    val blankAfterMs: Long = 30_000,
)

data class WallCameraState(
    val sn: String,
    val name: String = sn,
    val mode: String = "on_demand",
    val state: String = "idle",
    val hasStill: Boolean = false,
    val lastMotionElapsedMs: Long? = null,
)

enum class WallContent { NONE, SNAPSHOT, LIVE }

data class WallResolvedTile(
    val id: String,
    val camera: String?,
    val content: WallContent,
    val hold: Boolean,
    val starting: Boolean,
)

/**
 * Event-driven selection using the caller's monotonic clock (SystemClock.elapsedRealtime on Android).
 * No wall-clock/camera timestamp can extend a dwell or wake lease. Resolve periodically for expiry.
 * Call on the UI thread, including hello/event updates, to keep selection and hold updates atomic.
 */
class WallPolicy {
    private val cameras = linkedMapOf<String, WallCameraState>()
    private val selected = mutableMapOf<String, String>()
    private val switchedAt = mutableMapOf<String, Long>()
    private val wakeUntil = mutableMapOf<String, Long>()
    private val motionOrder = mutableMapOf<String, Long>()
    private var nextMotionOrder = 0L

    fun camera(sn: String): WallCameraState? = cameras[sn]

    /** Stop/bridge change drops leases and motion from the old connection. */
    fun clear() {
        cameras.clear()
        selected.clear()
        switchedAt.clear()
        wakeUntil.clear()
        motionOrder.clear()
        nextMotionOrder = 0
    }

    /** A reconnect refreshes state without forgetting motion observed on this client. */
    fun applyHello(snapshot: List<WallCameraState>) {
        val previous = cameras.toMap()
        cameras.clear()
        snapshot.filter { it.sn.isNotBlank() }.forEach { incoming ->
            val lastMotion = listOfNotNull(incoming.lastMotionElapsedMs,
                previous[incoming.sn]?.lastMotionElapsedMs).maxOrNull()
            cameras[incoming.sn] = incoming.copy(lastMotionElapsedMs = lastMotion)
        }
        wakeUntil.keys.retainAll(cameras.keys)
        motionOrder.keys.retainAll(cameras.keys)
        selected.entries.removeAll { (_, sn) -> sn !in cameras }
        switchedAt.keys.retainAll(selected.keys)
    }

    /** Ignore removed/unknown cameras; an old buffered event must not recreate them. */
    fun onMotion(sn: String, nowElapsedMs: Long, hasStill: Boolean = false): Boolean {
        val camera = cameras[sn] ?: return false
        cameras[sn] = camera.copy(lastMotionElapsedMs = nowElapsedMs,
            hasStill = camera.hasStill || hasStill)
        motionOrder[sn] = ++nextMotionOrder
        return true
    }

    fun onStreamState(sn: String, state: String): Boolean {
        val camera = cameras[sn] ?: return false
        cameras[sn] = camera.copy(state = state)
        return true
    }

    /** An explicit user wake is bounded; merely selecting a fixed battery tile never calls this. */
    fun requestWake(sn: String, nowElapsedMs: Long, durationMs: Long = 30_000): Boolean {
        val camera = cameras[sn] ?: return false
        if (camera.mode == "always" || durationMs <= 0) return false
        wakeUntil[sn] = if (nowElapsedMs > Long.MAX_VALUE - durationMs) Long.MAX_VALUE
            else nowElapsedMs + durationMs
        return true
    }

    fun wake(sn: String, untilElapsedMs: Long): Boolean {
        val camera = cameras[sn] ?: return false
        if (camera.mode == "always") return false
        wakeUntil[sn] = untilElapsedMs
        return true
    }

    fun resolve(tiles: List<WallTileSpec>, nowElapsedMs: Long): List<WallResolvedTile> {
        require(tiles.map { it.id }.distinct().size == tiles.size) { "Tile ids must be unique" }
        val ids = tiles.map { it.id }.toSet()
        selected.keys.retainAll(ids)
        switchedAt.keys.retainAll(ids)
        wakeUntil.entries.removeAll { (_, until) -> nowElapsedMs >= until }
        return tiles.map { tile ->
            val sn = if (tile.motion) chooseMotion(tile, nowElapsedMs)
                else tile.camera.takeIf { it in cameras }
            if (selected[tile.id] != sn) {
                if (sn == null) {
                    selected.remove(tile.id)
                    switchedAt.remove(tile.id)
                } else {
                    selected[tile.id] = sn
                    switchedAt[tile.id] = nowElapsedMs
                }
            }
            val camera = sn?.let(cameras::get)
            val content = when {
                camera == null -> WallContent.NONE
                camera.state == "live" || camera.mode == "always" -> WallContent.LIVE
                camera.hasStill && camera.mode != "always" -> WallContent.SNAPSHOT
                else -> WallContent.NONE
            }
            WallResolvedTile(tile.id, sn, content,
                camera != null && camera.mode != "always" &&
                    (tile.motion || wakeUntil.containsKey(sn)),
                camera?.state == "starting")
        }
    }

    private fun chooseMotion(tile: WallTileSpec, now: Long): String? {
        val watch = if (tile.watch.isEmpty()) cameras.keys else tile.watch.toSet()
        val candidates = cameras.values.filter { it.sn in watch && it.lastMotionElapsedMs != null }
        val best = candidates.maxWithOrNull(compareBy<WallCameraState> { it.lastMotionElapsedMs }
            .thenBy { motionOrder[it.sn] ?: 0 }.thenBy { it.sn }) ?: return null
        val bestAt = best.lastMotionElapsedMs!!
        // At the expiry boundary release the hold, even if a dwell would otherwise retain selection.
        if (tile.blankAfterMs > 0 && now - bestAt >= tile.blankAfterMs) return null
        val current = selected[tile.id]
        val since = switchedAt[tile.id]
        if (current != null && current in cameras && current in watch && current != best.sn &&
            since != null && now - since < tile.dwellMs.coerceAtLeast(0)) return current
        return best.sn
    }
}
