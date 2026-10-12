package com.eufywall.tv

import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder

data class FreshnessCapability(val maxAgeMs: Int, val clockUrl: String, val mode: String = "enforce") {
    val enforces: Boolean get() = mode == "enforce"
}
/** Capability revision is separate from the unchanged v1 binary frame stamp.
 * Old v1 clients enforced every recognized capability, so observe requires v2.
 */
internal fun freshnessMode(version: Int, advertisedMode: String?): String? = when(version) {
    1 -> if(advertisedMode == null || advertisedMode == "enforce") "enforce" else null
    2 -> advertisedMode?.takeIf { it == "observe" || it == "enforce" }
    else -> null
}

internal data class FrameStamp(val epoch: String, val sourceTimestamp: Long, val ingressMonoUs: Long, val frameId: Long, val maxAgeMs: Int)
internal data class BridgeClockSample(val epoch: String, val serverMonoUs: Long, val requestStartNs: Long, val responseNs: Long) {
    val expiresNs: Long get() = responseNs + 5_000_000_000L
    private fun ingressNs(stamp: FrameStamp, nowNs: Long): Long? {
        if (epoch != stamp.epoch || nowNs < responseNs || nowNs >= expiresNs || responseNs < requestStartNs || responseNs - requestStartNs > 1_000_000_000L) return null
        val delta = stamp.ingressMonoUs - serverMonoUs
        if (delta !in -86_400_000_000L..86_400_000_000L) return null
        // 1000 ppm relative clock-rate allowance (including clock slewing).
        // delta is microseconds, so this correction is delta nanoseconds.
        val earliestIngress = requestStartNs + delta * 1_000 - kotlin.math.abs(delta)
        if (earliestIngress > nowNs) return null
        return earliestIngress
    }
    fun ageUpperNs(stamp: FrameStamp, nowNs: Long): Long? = ingressNs(stamp, nowNs)?.let { nowNs - it }
    /** Diagnostic counterpart only: no admission or deadline is changed. */
    fun clockIntervalWidthNs(stamp: FrameStamp, nowNs: Long): Long? {
        if (ingressNs(stamp, nowNs) == null) return null
        val deltaUs = stamp.ingressMonoUs - serverMonoUs
        return responseNs - requestStartNs + 2 * kotlin.math.abs(deltaUs)
    }
    fun ageLowerNs(stamp: FrameStamp, nowNs: Long): Long? {
        val earliest = ingressNs(stamp, nowNs) ?: return null
        val width = clockIntervalWidthNs(stamp, nowNs) ?: return null
        return (nowNs - earliest - width).coerceAtLeast(0)
    }
    fun deadline(stamp: FrameStamp, nowNs: Long, allowedAgeMs: Int): Long? = ingressNs(stamp, nowNs)?.let {
        minOf(it + minOf(stamp.maxAgeMs, allowedAgeMs) * 1_000_000L, expiresNs).takeIf { deadline -> deadline > nowNs }
    }
}

/** Retain bounded, still-valid observations; a slow poll cannot replace a
 * better clock bound. Original sample leases are never extended. */
internal class BridgeClockWindow {
    private val samples = ArrayDeque<BridgeClockSample>()
    @Synchronized fun accept(sample: BridgeClockSample) {
        if (samples.firstOrNull()?.epoch?.let { it != sample.epoch } == true) samples.clear()
        samples.addLast(sample)
        while (samples.size > 16) samples.removeFirst()
    }
    @Synchronized fun current(nowNs: Long): BridgeClockSample? {
        samples.removeAll { nowNs >= it.expiresNs }
        return samples.filter { nowNs >= it.responseNs }.minByOrNull {
            (it.responseNs - it.requestStartNs) + (nowNs - it.responseNs) / 500
        }
    }
    @Synchronized fun clear() { samples.clear() }
}

/** Extract a per-picture stamp; no latest-frame or receipt-time substitution. */
internal object FrameStampReader {
    private val uuid = "EWFRESHNESS00001".toByteArray(Charsets.US_ASCII)
    fun read(data: ByteArray, offset: Int = 0, length: Int = data.size - offset, hevc: Boolean = false): FrameStamp? {
        if (offset < 0 || length < 0 || offset > data.size - length) return null
        val stop = offset + length
        var i = offset
        while (i + 3 < stop) {
            if (data[i] != 0.toByte() || data[i + 1] != 0.toByte() || data[i + 2] != 1.toByte()) { i++; continue }
            val start = i + 3
            val type = if (hevc) (data[start].toInt() ushr 1) and 63 else data[start].toInt() and 31
            if (if (hevc) type <= 31 else type in 1..5) return null
            var end = start
            while (end < stop && !(end + 2 < stop && data[end] == 0.toByte() && data[end + 1] == 0.toByte() && data[end + 2] == 1.toByte())) end++
            val header = if (hevc) 2 else 1
            if (type == (if (hevc) 39 else 6) && end - start >= header) {
                val bytes = ByteArrayOutputStream()
                var zeros = 0
                for (j in start + header until end) {
                    val b = data[j].toInt() and 255
                    if (zeros >= 2 && b == 3) { zeros = 0; continue }
                    bytes.write(b); zeros = if (b == 0) zeros + 1 else 0
                    if (bytes.size() > 65536) break
                }
                val rbsp = bytes.toByteArray()
                var p = 0
                while (p < rbsp.size && (rbsp[p].toInt() and 255) != 128) {
                    var payloadType = 0; var size = 0
                    while (p < rbsp.size && (rbsp[p].toInt() and 255) == 255) { payloadType += 255; p++ }
                    if (p >= rbsp.size) break
                    payloadType += rbsp[p++].toInt() and 255
                    while (p < rbsp.size && (rbsp[p].toInt() and 255) == 255) { size += 255; p++ }
                    if (p >= rbsp.size) break
                    size += rbsp[p++].toInt() and 255
                    if (size > rbsp.size - p) break
                    if (payloadType == 5 && size == 48 && uuid.indices.all { rbsp[p + it] == uuid[it] }) {
                        val b = ByteBuffer.wrap(rbsp, p, size).slice().order(ByteOrder.BIG_ENDIAN)
                        if (b.get(16).toInt() != 1 || b.get(17).toInt() != 1) return null
                        val age = b.getShort(18).toInt() and 65535
                        val epoch = (20 until 28).joinToString("") { "%02x".format(b.get(it).toInt() and 255) }
                        val ingress = b.getLong(32); val frame = b.getLong(40)
                        if (age == 0 || ingress < 0 || frame <= 0) return null
                        return FrameStamp(epoch, b.getInt(28).toLong() and 0xffffffffL, ingress, frame, age)
                    }
                    p += size
                }
            }
            i = end
        }
        return null
    }
}
