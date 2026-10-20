package com.eufywall.tv

/** Retry only the failed player, provided its tile still owns that instance. */
internal class TilePlaybackRecovery<T : Any>(
    private val current: (String) -> T?,
    private val wantsLive: (String) -> Boolean,
    private val schedule: (Runnable) -> Unit,
    private val release: (String) -> Unit,
    private val reconnect: (String) -> Unit,
) {
    fun retry(tile: String, player: T) {
        schedule(Runnable {
            if (wantsLive(tile) && current(tile) === player) {
                release(tile)
                reconnect(tile)
            }
        })
    }
}
