package com.eufywall.tv

import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Monotonic request/response clock interval. Device Unix time is irrelevant. */
internal class BridgeClockClient(url: String) : AutoCloseable {
    private val samples = BridgeClockWindow()
    fun currentSample(nowNs: Long = System.nanoTime()) = samples.current(nowNs)
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private val http = OkHttpClient.Builder().connectTimeout(1, TimeUnit.SECONDS).readTimeout(1, TimeUnit.SECONDS).callTimeout(1, TimeUnit.SECONDS).build()
    private val request = Request.Builder().url(url).header("Cache-Control", "no-cache").build()
    init {
        worker.scheduleWithFixedDelay({
            try {
                val start = System.nanoTime()
                http.newCall(request).execute().use { response ->
                    val value = JSONObject(response.body?.string() ?: "")
                    val end = System.nanoTime()
                    val epoch = value.getString("serverEpoch")
                    val server = value.getLong("monotonicUs")
                    if (response.isSuccessful && value.optString("scope") == "bridge-ingress" && epoch.matches(Regex("[0-9a-f]{16}")) && server >= 0 && end - start <= 1_000_000_000L)
                        samples.accept(BridgeClockSample(epoch, server, start, end))
                }
            } catch (_: Exception) { /* Previous sample expires; network failure cannot manufacture freshness. */ }
        }, 0, 1, TimeUnit.SECONDS)
    }
    override fun close() { worker.shutdownNow(); http.dispatcher.cancelAll(); samples.clear() }
}
