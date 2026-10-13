package com.eufywall.tv

import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Bounded asynchronous holds, independent of camera discovery's serial executor.
 * The complete URL includes bridge and owner, so a bridge change cannot redirect old releases.
 * Closing releases leases without cancelling an in-flight POST ahead of its ordered DELETE.
 */
class CameraHoldClient(http: OkHttpClient) : AutoCloseable {
    private val coordinator = HoldRequestCoordinator { url, method, complete ->
        try {
            val body = if (method == "POST") ByteArray(0).toRequestBody(null) else null
            val call = http.newCall(Request.Builder().url(url).method(method, body).build())
            call.timeout().timeout(6, TimeUnit.SECONDS)
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) = complete()
                override fun onResponse(call: Call, response: Response) {
                    response.close()
                    complete()
                }
            })
        } catch (_: RuntimeException) {
            complete()
        }
    }

    /** Repeated active calls renew a lease; calls while the same POST is pending are coalesced. */
    fun setDesired(url: String, active: Boolean) = coordinator.setDesired(url, active)
    fun releaseAll() = coordinator.releaseAll()
    override fun close() = coordinator.close()
}

/** HTTP-independent intent state machine. Transport calls complete exactly once on any outcome. */
internal class HoldRequestCoordinator(
    private val send: (url: String, method: String, complete: () -> Unit) -> Unit,
) : AutoCloseable {
    private data class State(var desired: Boolean, var pending: String? = null, var token: Long = 0)
    private val states = mutableMapOf<String, State>()
    private var nextToken = 0L
    private var closed = false

    @Synchronized
    fun setDesired(url: String, active: Boolean) {
        if (closed || url.isBlank()) return
        val state = states.getOrPut(url) { State(active) }
        state.desired = active
        if (state.pending == null) dispatch(url, state, if (active) "POST" else "DELETE")
    }

    /** Stops all current intents but allows a later foreground wall to take fresh leases. */
    @Synchronized
    fun releaseAll() = releaseAllLocked()

    @Synchronized
    override fun close() {
        if (closed) return
        closed = true
        releaseAllLocked()
    }

    private fun releaseAllLocked() {
        // A synchronous test transport may complete and remove entries during dispatch.
        states.toMap().forEach { (url, state) ->
            state.desired = false
            if (state.pending == null) dispatch(url, state, "DELETE")
        }
    }

    private fun dispatch(url: String, state: State, method: String) {
        state.pending = method
        val token = ++nextToken
        state.token = token
        send(url, method) { completed(url, state, token, method) }
    }

    @Synchronized
    private fun completed(url: String, state: State, token: Long, method: String) {
        if (states[url] !== state || state.token != token || state.pending != method) return
        state.pending = null
        when {
            method == "POST" && !state.desired -> dispatch(url, state, "DELETE")
            method == "DELETE" && state.desired -> dispatch(url, state, "POST")
            method == "DELETE" -> states.remove(url)
            // Active POST completed: wait for the next explicit renewal tick, including on error.
        }
    }
}
