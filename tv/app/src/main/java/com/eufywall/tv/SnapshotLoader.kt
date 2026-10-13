package com.eufywall.tv

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.os.Handler
import android.os.Looper
import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit

/** Retained stills only: the bridge's GET /snapshot endpoint never wakes a camera. */
class SnapshotLoader(private val http: OkHttpClient) : AutoCloseable {
    private data class Pending(val generation: Long, var call: Call? = null, var decodeTask: Runnable? = null, var response: Response? = null)
    private val main = Handler(Looper.getMainLooper())
    private val lock = Any()
    private val pending = mutableMapOf<String, Pending>()
    private val cache = LinkedHashMap<String, Bitmap>(16, 0.75f, true)
    private var cacheBytes = 0L
    private var generation = 0L
    private var closed = false
    // Decode off the UI/network threads, with a hard bound on queued thumbnail work.
    private val decoder = ThreadPoolExecutor(1, 1, 30, TimeUnit.SECONDS,
        ArrayBlockingQueue(36), ThreadPoolExecutor.AbortPolicy())

    /** Replaces the tile's request. Cached content is shown immediately, then refreshed once. */
    fun load(tileId: String, url: String, onResult: (Bitmap?) -> Unit) {
        val cacheKey = SnapshotPolicy.cacheKey(url)
        val state: Pending
        val retained: Bitmap?
        synchronized(lock) {
            if (closed) return
            pending.remove(tileId)?.let { cancelPending(it) }
            if (pending.size >= 36) return
            state = Pending(++generation)
            pending[tileId] = state
            retained = cache[cacheKey]
        }
        if (retained != null) deliver(tileId, state, retained, onResult)
        val request = try { Request.Builder().url(url).header("Cache-Control", "no-cache").build() }
            catch (_: IllegalArgumentException) { deliver(tileId, state, null, onResult); return }
        val call = http.newCall(request)
        call.timeout().timeout(6, TimeUnit.SECONDS)
        synchronized(lock) {
            if (!current(tileId, state)) { call.cancel(); return }
            state.call = call
        }
        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                deliver(tileId, state, retained, onResult)
            }
            override fun onResponse(call: Call, response: Response) {
                val task = Runnable {
                    response.use {
                        if (!isCurrent(tileId, state)) return@Runnable
                        if (response.code == 404) {
                            synchronized(lock) { if (current(tileId, state)) removeCached(cacheKey) }
                            deliver(tileId, state, null, onResult)
                            return@Runnable
                        }
                        val body = response.body
                        if (!response.isSuccessful || body == null || body.contentLength() > SnapshotPolicy.MAX_BYTES) {
                            deliver(tileId, state, retained, onResult); return@Runnable
                        }
                        val bytes = try {
                            body.byteStream().use { input ->
                                val output = ByteArrayOutputStream()
                                val buffer = ByteArray(8192)
                                while (true) {
                                    if (!isCurrent(tileId, state)) return@Runnable
                                    val count = input.read(buffer)
                                    if (count < 0) break
                                    if (output.size().toLong() + count > SnapshotPolicy.MAX_BYTES) {
                                        deliver(tileId, state, retained, onResult); return@Runnable
                                    }
                                    output.write(buffer, 0, count)
                                }
                                output.toByteArray()
                            }
                        } catch (_: IOException) { deliver(tileId, state, retained, onResult); return@Runnable }
                        if (!isCurrent(tileId, state)) return@Runnable
                        val bitmap = decode(bytes)
                        synchronized(lock) {
                            if (current(tileId, state) && bitmap != null) putCached(cacheKey, bitmap)
                            state.response = null
                        }
                        deliver(tileId, state, bitmap ?: retained, onResult)
                    }
                }
                synchronized(lock) {
                    if (!current(tileId, state)) { response.close(); return }
                    state.response = response
                    state.decodeTask = task
                    try { decoder.execute(task) }
                    catch (_: java.util.concurrent.RejectedExecutionException) {
                        response.close()
                        state.response = null
                        deliver(tileId, state, retained, onResult)
                    }
                }
            }
        })
    }

    fun cancel(tileId: String) {
        synchronized(lock) { pending.remove(tileId)?.let { cancelPending(it) } }
    }

    override fun close() {
        synchronized(lock) {
            closed = true
            pending.values.forEach { cancelPending(it) }
            pending.clear()
            cache.clear()
            cacheBytes = 0
        }
        decoder.shutdownNow()
    }

    private fun current(tileId: String, state: Pending) = !closed && pending[tileId] === state
    private fun cancelPending(state: Pending) {
        state.call?.cancel()
        state.decodeTask?.let { decoder.remove(it) }
        state.response?.close()
        state.response = null
    }
    private fun isCurrent(tileId: String, state: Pending) = synchronized(lock) { current(tileId, state) }
    private fun deliver(tileId: String, state: Pending, bitmap: Bitmap?, callback: (Bitmap?) -> Unit) {
        main.post { if (isCurrent(tileId, state)) callback(bitmap) }
    }
    private fun removeCached(url: String) {
        cache.remove(url)?.let { cacheBytes -= it.allocationByteCount }
        // An ImageView may still own a bitmap after eviction; never recycle it here.
    }
    private fun putCached(url: String, bitmap: Bitmap) {
        removeCached(url)
        cache[url] = bitmap
        cacheBytes += bitmap.allocationByteCount
        while (cacheBytes > 6L * 1024 * 1024 || cache.size > 12) removeCached(cache.entries.first().key)
    }
    private fun decode(bytes: ByteArray): Bitmap? = try {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        val sample = SnapshotPolicy.sampleSize(bounds.outWidth, bounds.outHeight)
        if (sample == null) null else BitmapFactory.decodeByteArray(bytes, 0, bytes.size,
            BitmapFactory.Options().apply { inSampleSize = sample; inPreferredConfig = Bitmap.Config.RGB_565 })
    } catch (_: RuntimeException) { null } catch (_: OutOfMemoryError) { null }
}

/** Bounds dimensions before allocating decoded pixels, including malicious JPEG headers. */
internal object SnapshotPolicy {
    const val MAX_BYTES = 2L * 1024 * 1024
    // Motion version controls re-fetching, not camera identity. Preserve all other query parameters.
    fun cacheKey(url: String): String = url.toHttpUrlOrNull()?.newBuilder()
        ?.removeAllQueryParameters("v")?.build()?.toString() ?: url
    fun sampleSize(width: Int, height: Int): Int? {
        if (width <= 0 || height <= 0 || width > 32768 || height > 32768) return null
        var sample = 1
        while ((width.toLong() + sample - 1) / sample > 1024 ||
            (height.toLong() + sample - 1) / sample > 1024 ||
            ((width.toLong() + sample - 1) / sample) * ((height.toLong() + sample - 1) / sample) > 524288) {
            sample *= 2
        }
        return sample
    }
}
