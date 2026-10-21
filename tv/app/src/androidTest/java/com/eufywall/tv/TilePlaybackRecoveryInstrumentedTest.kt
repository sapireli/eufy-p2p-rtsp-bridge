package com.eufywall.tv

import android.os.SystemClock
import android.util.Log
import android.view.SurfaceHolder
import android.view.SurfaceView
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

    @Test fun twoDecoderFailuresRetainHealthyPlayersAndTheirProgress() =
        verifyRecovery(failAll = false, decoderFailure = true)

    @Test fun simultaneousTransportFailuresRetainAllFourSurfacesAndResumeDecoding() =
        verifyRecovery(failAll = true, decoderFailure = false)

    private fun verifyRecovery(failAll: Boolean, decoderFailure: Boolean) {
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            var initial = emptyMap<String, Any>()
            await(30_000) {
                scenario.onActivity { initial = players(it) }
                initial.size == 4 && initial.values.all { decoded(it) > 0 }
            }
            val healthy = if (failAll) emptyMap() else initial.filterValues(::pinned)
            val failed = if (failAll) initial else initial.filterValues { !pinned(it) }
            assertEquals("Unexpected healthy camera count", if (failAll) 0 else 2, healthy.size)
            assertEquals("Unexpected failure target count", if (failAll) 4 else 2, failed.size)
            val before = healthy.mapValues { decoded(it.value) }
            val destroyedSurfaces = AtomicLong()
            val surfaceCallbacks = mutableMapOf<SurfaceView, SurfaceHolder.Callback>()
            scenario.onActivity {
                failed.values.forEach { player ->
                    val surface = field(player, "surface") as SurfaceView
                    val callback = object : SurfaceHolder.Callback {
                        override fun surfaceCreated(holder: SurfaceHolder) {}
                        override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {}
                        override fun surfaceDestroyed(holder: SurfaceHolder) { destroyedSurfaces.incrementAndGet() }
                    }
                    surface.holder.addCallback(callback)
                    surfaceCallbacks[surface] = callback
                }
            }
            Log.i("TileRecoveryTest", "Injecting simultaneous failure callbacks decoder=$decoderFailure; healthy=${healthy.size} targets=${failed.size}")
            scenario.onActivity {
                // One UI turn models a shared upstream interruption before any
                // replacement player can report successful playback.
                failed.values.forEach { player ->
                    @Suppress("UNCHECKED_CAST")
                    val callback = field(player, "onFailure") as (String, Boolean) -> Unit
                    callback("Instrumented ${if (decoderFailure) "decoder" else "transport"} failure", decoderFailure)
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
                    failed.forEach { (tile, old) ->
                        current[tile]?.takeIf { it !== old }?.let { replacement ->
                            assertSame("Retry replaced the tile display surface", field(old, "surface"), field(replacement, "surface"))
                            assertSame("Retry replaced the attached tile view", field(old, "view"), field(replacement, "view"))
                            assertNotNull("Retry detached the tile view", (field(replacement, "view") as android.view.View).parent)
                        }
                    }
                    assertEquals("Retry destroyed an existing surface", 0L, destroyedSurfaces.get())
                    observedReplacements = failed.all { (tile, old) -> current[tile]?.let { it !== old && decoded(it) > 0 } == true }
                    ready = observedReplacements && healthy.all { (tile, player) -> decoded(player) > before.getValue(tile) }
                }
                ready
            }
            assertTrue(observedReplacements)
            scenario.onActivity {
                surfaceCallbacks.forEach { (surface, callback) -> surface.holder.removeCallback(callback) }
            }
            healthy.forEach { (tile, player) ->
                Log.i("TileRecoveryTest", "Healthy player retained tile=$tile identity=${System.identityHashCode(player)} decodedBefore=${before.getValue(tile)} decodedAfter=${decoded(player)} decodedDelta=${decoded(player) - before.getValue(tile)}")
            }
            Log.i("TileRecoveryTest", "PASS: failed players replaced on the same attached surfaces without surface destruction; all players decoding")
        }
    }
}
