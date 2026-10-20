package com.eufywall.tv

import android.os.SystemClock
import android.util.Log
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.atomic.AtomicLong

/** Uses the installed wall configuration; does not alter preferences or upstream streams. */
@RunWith(AndroidJUnit4::class)
class TilePlaybackRecoveryInstrumentedTest {
    private fun field(owner: Any, name: String): Any = owner.javaClass.getDeclaredField(name).let {
        it.isAccessible = true
        it.get(owner)!!
    }

    @Suppress("UNCHECKED_CAST")
    private fun players(activity: MainActivity): Map<String, Any> =
        (field(activity, "players") as Map<String, Any>).toMap()

    private fun decoded(player: Any) = (field(player, "decodedFrames") as AtomicLong).get()
    private fun pinned(player: Any) = field(player, "hardwarePinned") as Boolean

    private fun await(timeoutMs: Long, condition: () -> Boolean) {
        val deadline = SystemClock.elapsedRealtime() + timeoutMs
        while (SystemClock.elapsedRealtime() < deadline) {
            if (condition()) return
            SystemClock.sleep(100)
        }
        fail("Condition did not complete within ${timeoutMs}ms")
    }

    @Test fun twoDecoderFailuresRetainHealthyPlayersAndTheirProgress() {
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            var initial = emptyMap<String, Any>()
            await(30_000) {
                scenario.onActivity { initial = players(it) }
                initial.size == 4 && initial.values.all { decoded(it) > 0 }
            }
            val healthy = initial.filterValues(::pinned)
            val failed = initial.filterValues { !pinned(it) }
            assertEquals("This live regression requires two pinned healthy cameras", 2, healthy.size)
            assertEquals("This live regression requires two non-pinned failure targets", 2, failed.size)
            val before = healthy.mapValues { decoded(it.value) }
            Log.i("TileRecoveryTest", "Injecting two decoder failure callbacks; healthy=${healthy.size} targets=${failed.size}")
            scenario.onActivity {
                // Same UI turn: the old activity-wide counter reached two before
                // any successful first-frame callback could reset it.
                failed.values.forEach { player ->
                    @Suppress("UNCHECKED_CAST")
                    val callback = field(player, "onFailure") as (String, Boolean) -> Unit
                    callback("Instrumented decoder failure", true)
                }
            }
            var observedReplacements = false
            await(20_000) {
                var ready = false
                scenario.onActivity { activity ->
                    val current = players(activity)
                    healthy.forEach { (tile, player) ->
                        assertSame("Healthy tile was torn down by another decoder failure", player, current[tile])
                    }
                    observedReplacements = failed.all { (tile, old) -> current[tile]?.let { it !== old && decoded(it) > 0 } == true }
                    ready = observedReplacements && healthy.all { (tile, player) -> decoded(player) > before.getValue(tile) }
                }
                ready
            }
            assertTrue(observedReplacements)
            healthy.forEach { (tile, player) ->
                Log.i("TileRecoveryTest", "Healthy player retained tile=$tile identity=${System.identityHashCode(player)} decodedBefore=${before.getValue(tile)} decodedAfter=${decoded(player)} decodedDelta=${decoded(player) - before.getValue(tile)}")
            }
            Log.i("TileRecoveryTest", "PASS: both failed players replaced; both healthy instances retained and decoding")
        }
    }
}
