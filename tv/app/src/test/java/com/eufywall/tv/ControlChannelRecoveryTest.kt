package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class ControlChannelRecoveryTest {
    @Test fun healthyMediaSurvivesFailureAndCloseWhileControlReconnects() {
        data class Media(var decoded: Long, val watchdog: DecoderProgressWatchdog)
        val front = Media(100, DecoderProgressWatchdog(15_000, 5_000))
        val garage = Media(100, DecoderProgressWatchdog(15_000, 5_000))
        val media = linkedMapOf("front" to front, "garage" to garage)
        val reported = mutableListOf<String>()
        var reconnects = 0
        // This is the production handler used by both WebSocket callback paths.
        // Its effect interface cannot release media or change decoder policy.
        val control = ControlChannelRecovery(reported::add, { reconnects++ })
        control.lost(true, "failure:IOException")
        for (now in 1_000L..6_000L step 500) {
            media.values.forEach { it.decoded++; assertNull(it.watchdog.stalledForMs(now, 0, it.decoded, now)) }
            if (now == 2_000L) control.lost(true, "closed:1001")
        }
        assertSame(front, media["front"]); assertSame(garage, media["garage"])
        assertTrue(front.decoded > 100 && garage.decoded > 100)
        assertEquals(listOf("failure:IOException", "closed:1001"), reported)
        assertEquals(2, reconnects)
        // Independent media inactivity still expires at the existing threshold.
        assertEquals(5_000L, front.watchdog.stalledForMs(11_000, 0, front.decoded, 6_000))
    }

    @Test fun obsoleteOrHiddenControlCallbacksCannotScheduleReconnects() {
        var reports = 0; var reconnects = 0
        val control = ControlChannelRecovery({ reports++ }, { reconnects++ })
        control.lost(false, "obsolete websocket failure")
        control.lost(false, "wall hidden")
        assertEquals(0, reports); assertEquals(0, reconnects)
    }
}
