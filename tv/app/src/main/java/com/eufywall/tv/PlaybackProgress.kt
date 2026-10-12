package com.eufywall.tv

/** Media-time progress only, not sensor age or physical display latency. */
internal data class PlaybackProgress(val receivedPtsUs: Long, val decodedPtsUs: Long, val callbackPtsUs: Long) {
    private fun pending(progressPtsUs: Long): Long = if (receivedPtsUs < 0 || progressPtsUs < 0) -1
        else (receivedPtsUs - progressPtsUs).coerceAtLeast(0) / 1_000
    val pendingDecodeMediaMs: Long get() = pending(decodedPtsUs)
    // OnFrameRendered may be batched/omitted on older Android. Its callback's
    // latest PTS does not identify the picture currently visible on the panel.
    val callbackPendingMediaMs: Long get() = pending(callbackPtsUs)
}
