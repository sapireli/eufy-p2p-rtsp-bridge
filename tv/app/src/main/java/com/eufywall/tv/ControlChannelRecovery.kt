package com.eufywall.tv

/** Control transport loss is not evidence that independent RTSP media failed. */
internal class ControlChannelRecovery(
    private val reportLoss: (String) -> Unit,
    private val scheduleReconnect: () -> Unit,
) {
    // Deliberately has no player/decoder action: explicit camera state and the
    // media's own error/progress handlers retain ownership of media recovery.
    fun lost(isCurrentAndVisible: Boolean, reason: String) {
        if (!isCurrentAndVisible) return
        reportLoss(reason)
        scheduleReconnect()
    }
}
