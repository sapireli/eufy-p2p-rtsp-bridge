package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class TilePlaybackRecoveryTest {
    @Test fun repeatedDecoderFailuresRetainEveryUnaffectedPlayer() {
        val front = Any(); val balcony = Any(); val garage = Any()
        val players = mutableMapOf("front" to front, "balcony" to balcony, "garage" to garage)
        val pending = mutableListOf<Runnable>()
        val released = mutableListOf<String>()
        val recovery = TilePlaybackRecovery(
            current = players::get, wantsLive = { true }, schedule = pending::add,
            release = { released.add(it); players.remove(it) },
            reconnect = { players[it] = Any() },
        )
        // Repeated failures used to trip the activity-wide decoder restart.
        repeat(3) {
            recovery.retry("garage", players.getValue("garage"))
            pending.removeAt(0).run()
            assertSame(front, players["front"])
            assertSame(balcony, players["balcony"])
        }
        assertEquals(listOf("garage", "garage", "garage"), released)
        assertNotSame(garage, players["garage"])
    }

    @Test fun delayedRetryCannotReleaseReplacementOrHiddenTile() {
        val player = Any(); var current = player; var live = true
        val pending = mutableListOf<Runnable>(); var releases = 0; var reconnects = 0
        val recovery = TilePlaybackRecovery(
            current = { current }, wantsLive = { live }, schedule = pending::add,
            release = { releases++ }, reconnect = { reconnects++ },
        )
        recovery.retry("camera", player)
        current = Any()
        pending.removeAt(0).run()
        recovery.retry("camera", current)
        live = false
        pending.removeAt(0).run()
        assertEquals(0, releases)
        assertEquals(0, reconnects)
    }
}
