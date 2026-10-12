package com.eufywall.tv
import org.junit.Assert.*
import org.junit.Test
class PlaybackProgressTest {
    @Test fun delayedInformationalCallbackDoesNotManufactureDecoderBacklog() {
        val progress = PlaybackProgress(3_000_000, 3_000_000, 1_000_000)
        assertEquals(0L, progress.pendingDecodeMediaMs)
        assertEquals(2_000L, progress.callbackPendingMediaMs)
    }
    @Test fun realMediaProgressLagHasIndependentUnitsAndUnknownStartup() {
        val progress = PlaybackProgress(3_000_000, 2_800_000, 2_500_000)
        assertEquals(200L, progress.pendingDecodeMediaMs)
        assertEquals(500L, progress.callbackPendingMediaMs)
        assertEquals(-1L, PlaybackProgress(3_000_000, -1, -1).pendingDecodeMediaMs)
        assertEquals(-1L, PlaybackProgress(-1, 0, 0).callbackPendingMediaMs)
    }
}
