package com.eufywall.tv

import androidx.test.core.app.ActivityScenario
import android.widget.*
import android.view.*
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.alexvas.utils.NetUtils
import okhttp3.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Explicit operator-provided test endpoints only; never changes wall preferences or camera settings. */
@RunWith(AndroidJUnit4::class)
class BridgeTransportInstrumentedTest {
    @Test fun selectedIpv6EndpointCarriesHttpWebSocketAndRtsp() {
        val args = InstrumentationRegistry.getArguments()
        val supplied = args.getString("bridgeEndpoint")
        org.junit.Assume.assumeTrue("Explicit bridgeEndpoint argument required", !supplied.isNullOrBlank())
        supplied!!.split('|').forEach { probe(it) }
        args.getString("serviceInstance")?.let { instance ->
            val addresses = BridgeMdnsAddresses.discover(instance, "_eufy-wall._tcp", 2500)
            assertTrue("No IPv4 answer from selected service", addresses.any { it is java.net.Inet4Address })
            assertTrue("No IPv6 answer from selected service", addresses.any { it is java.net.Inet6Address })
        }
    }
    @Test fun connectionErrorsAreVisibleAndClearAfterRealRecovery() {
        org.junit.Assume.assumeTrue("Explicit bridgeEndpoint argument required", !InstrumentationRegistry.getArguments().getString("bridgeEndpoint").isNullOrBlank())
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val prefs = context.getSharedPreferences("wall", 0)
        val original = prefs.getString("bridge", null) ?: error("Saved bridge required")
        val originalWall = prefs.getString("configuration", null)
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
            fun connect(address: String) {
                scenario.onActivity { activity ->
                    MainActivity::class.java.getDeclaredMethod("showConnectionSettings", String::class.java).apply { isAccessible = true }.invoke(activity, "")
                    val all = views(activity.window.decorView)
                    all.filterIsInstance<EditText>().single().setText(address)
                    all.filterIsInstance<Button>().first { it.text.toString() == "Connect" }.performClick()
                }
            }
            fun awaitText(fragment: String, present: Boolean = true, timeoutMs: Long = 10000): Boolean {
                val until = SystemClock.elapsedRealtime() + timeoutMs
                while (SystemClock.elapsedRealtime() < until) {
                    var found = false
                    scenario.onActivity { activity -> found = views(activity.window.decorView).filterIsInstance<TextView>().any { it.visibility == View.VISIBLE && it.text.toString().contains(fragment) } }
                    if (found == present) return true
                    SystemClock.sleep(100)
                }
                return false
            }
            connect("bridge-does-not-exist.invalid")
            assertTrue("Missing visible DNS action", awaitText("Cannot resolve"))
            connect(original.toHttpUrl().newBuilder().port(1).build().toString())
            assertTrue("Missing visible refused action", awaitText("Connection refused"))
            connect(original)
            assertTrue("Bridge did not recover", awaitText("Bridge connected"))
            assertTrue("DNS error remained after recovery", awaitText("Cannot resolve", false))
            scenario.onActivity { activity -> views(activity.window.decorView).filterIsInstance<Button>().first { it.text.toString() == "Watch cameras" }.performClick() }
            SystemClock.sleep(1000)
            scenario.onActivity { activity ->
                val field = MainActivity::class.java.getDeclaredField("socket").apply { isAccessible = true }
                (field.get(activity) as WebSocket).cancel()
            }
            assertTrue("Missing visible wall connection error", awaitText("Cannot reach"))
            assertTrue("Wall error did not clear on WebSocket recovery", awaitText("Cannot reach", false, 12000))
        }
        assertEquals(original, prefs.getString("bridge", null))
        assertEquals(originalWall, prefs.getString("configuration", null))
    }

    @Test fun actualWallRendersOverSelectedIpv6AndRestoresOriginalConnection() {
        val supplied = InstrumentationRegistry.getArguments().getString("wallIpv6Endpoint")
        org.junit.Assume.assumeTrue("Explicit wallIpv6Endpoint argument required", !supplied.isNullOrBlank())
        val endpoint = supplied!!
        val prefs = InstrumentationRegistry.getInstrumentation().targetContext.getSharedPreferences("wall", 0)
        val original = prefs.getString("bridge", null) ?: error("Saved bridge required")
        val originalConfiguration = prefs.getString("configuration", null)
        val originalId = prefs.getString("bridgeNetworkId", null)
        val configured = JSONObject(originalConfiguration!!).getJSONArray("tiles")
        val fixedTileIds = (0 until configured.length()).map { configured.getJSONObject(it) }.filterNot { it.optBoolean("motion") }.map { it.getString("id") }.toSet()
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup) (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
            fun connect(address: String) {
                scenario.onActivity { activity ->
                    MainActivity::class.java.getDeclaredMethod("showConnectionSettings", String::class.java).apply { isAccessible = true }.invoke(activity, "")
                    val all = views(activity.window.decorView)
                    all.filterIsInstance<EditText>().single().setText(address)
                    all.filterIsInstance<Button>().first { it.text.toString() == "Connect" }.performClick()
                }
                val until = SystemClock.elapsedRealtime() + 10000
                var ready = false
                while (!ready && SystemClock.elapsedRealtime() < until) {
                    scenario.onActivity { activity -> ready = views(activity.window.decorView).filterIsInstance<Button>().any { it.text.toString() == "Watch cameras" } }
                    if (!ready) SystemClock.sleep(100)
                }
                assertTrue("Bridge setup did not complete", ready)
                scenario.onActivity { activity -> views(activity.window.decorView).filterIsInstance<Button>().first { it.text.toString() == "Watch cameras" }.performClick() }
            }
            fun players(): Map<String, LowLatencyRtspPlayer> {
                var result = mapOf<String, LowLatencyRtspPlayer>()
                scenario.onActivity { activity ->
                    @Suppress("UNCHECKED_CAST")
                    val current = MainActivity::class.java.getDeclaredField("players").apply { isAccessible = true }.get(activity) as Map<String, LowLatencyRtspPlayer>
                    result = current.filterKeys { it in fixedTileIds }
                }
                return result
            }
            try {
                connect(endpoint)
                assertEquals(originalId, prefs.getString("bridgeNetworkId", null))
                val until = SystemClock.elapsedRealtime() + 10000
                var current = players()
                while ((current.size < fixedTileIds.size || current.values.any { it.renderedFrames.get() == 0L }) && SystemClock.elapsedRealtime() < until) { SystemClock.sleep(100); current = players() }
                assertTrue("Four live players did not produce output", current.size == fixedTileIds.size && current.values.all { it.renderedFrames.get() > 0L })
                val start = current.mapValues { it.value.renderedFrames.get() }
                current.values.forEach { player ->
                    val camera = LowLatencyRtspPlayer::class.java.getDeclaredField("camera").apply { isAccessible = true }.get(player) as Camera
                    assertEquals(endpoint.toHttpUrl().host, java.net.URI(camera.rtsp).host.removePrefix("[").removeSuffix("]"))
                }
                SystemClock.sleep(30000)
                val latest = players()
                val report = current.map { (id, player) ->
                    assertSame("Player was replaced during IPv6 window", player, latest[id])
                    val delta = player.renderedFrames.get() - start.getValue(id)
                    assertTrue("IPv6 player stopped rendering", delta > 0)
                    "$id renderedDelta=$delta ${player.diagnostics().substringBefore(" receivedUnits") }"
                }.joinToString("; ")
                InstrumentationRegistry.getInstrumentation().sendStatus(0, android.os.Bundle().apply { putString("ipv6WallResult", report) })
            } finally { connect(original) }
        }
        assertEquals(original, prefs.getString("bridge", null))
        assertEquals(originalConfiguration, prefs.getString("configuration", null))
        assertEquals(originalId, prefs.getString("bridgeNetworkId", null))
    }

    @Test fun androidUriRepresentationDoesNotTruncateSocketHost() {
        val uri = android.net.Uri.parse("rtsp://[2001:db8:1::1234]:8554/front?pkt_size=8192")
        val endpoint = com.alexvas.utils.RtspAddress.parse(uri.toString())
        assertEquals("2001:db8:1::1234", endpoint.host)
        assertEquals(8554, endpoint.port)
    }

    private fun probe(supplied: String) {
        val bridge = normalizeBridgeAddress(supplied)
        val client = OkHttpClient.Builder().socketFactory(BridgeSocketFactory).callTimeout(8, TimeUnit.SECONDS).build()
        try {
            val cameras = client.newCall(Request.Builder().url("$bridge/api/cameras").build()).execute().use {
                assertEquals(200, it.code); JSONArray(it.body!!.string())
            }
            assertTrue(cameras.length() > 0)
            val hello = CountDownLatch(1)
            var failure: Throwable? = null
            val ws = client.newWebSocket(Request.Builder().url(websocketBridgeUrl(bridge)).build(), object : WebSocketListener() {
                override fun onMessage(webSocket: WebSocket, text: String) {
                    if (JSONObject(text).optString("type") == "hello") hello.countDown()
                }
                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { failure = t; hello.countDown() }
            })
            try { assertTrue("No bridge WebSocket hello", hello.await(8, TimeUnit.SECONDS)); failure?.let { throw it } }
            finally { ws.cancel() }
            val rtspUrl = cameras.getJSONObject(0).getString("rtsp")
            val uri = java.net.URI(rtspUrl)
            val host = uri.host.removePrefix("[").removeSuffix("]")
            NetUtils.createSocketAndConnect(host, if (uri.port < 0) 8554 else uri.port, 5000).use { socket ->
                socket.getOutputStream().write("OPTIONS $rtspUrl RTSP/1.0\r\nCSeq: 1\r\n\r\n".toByteArray())
                val line = socket.getInputStream().bufferedReader().readLine()
                assertTrue("RTSP reply: $line", line.contains("200"))
            }
            val clock = cameras.getJSONObject(0).optJSONObject("freshness")?.optString("clockUrl")
            if (!clock.isNullOrBlank()) {
                client.newCall(Request.Builder().url(clock).build()).execute().use {
                    assertEquals(200, it.code); assertEquals("bridge-ingress", JSONObject(it.body!!.string()).getString("scope"))
                }
            }
        } finally { client.dispatcher.cancelAll(); client.connectionPool.evictAll(); client.dispatcher.executorService.shutdown() }
    }
}
