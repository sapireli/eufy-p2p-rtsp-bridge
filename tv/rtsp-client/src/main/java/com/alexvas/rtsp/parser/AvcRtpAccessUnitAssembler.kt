package com.alexvas.rtsp.parser

/** RFC 6184 non-interleaved AVC: deliver complete pictures, never individual slices. */
class AvcRtpAccessUnitAssembler(
    sps: ByteArray? = null, pps: ByteArray? = null,
    private val maxBytes: Int = 4 * 1024 * 1024,
    private val maxPackets: Int = 4096,
) {
    private var cachedSps = sps
    private var cachedPps = pps
    private var timestamp: Long? = null
    private var sequence: Int? = null
    private var invalid = false
    private var packets = 0
    private var bytes = 0
    private var fragment: java.io.ByteArrayOutputStream? = null
    private val nals = ArrayList<ByteArray>()

    fun push(payload: ByteArray, length: Int, time: Long, seq: Int, marker: Boolean): ByteArray? {
        if (timestamp != time) clearUnit()
        timestamp = time
        if (sequence != null && seq != ((sequence!! + 1) and 65535)) invalid = true
        sequence = seq
        packets++
        if (packets > maxPackets || length < 1 || length > payload.size) invalid = true
        if (!invalid) {
            val type = payload[0].toInt() and 31
            when (type) {
                in 1..23 -> {
                    if (fragment != null) invalid = true
                    else addNal(payload.copyOf(length))
                }
                24 -> {
                    if (fragment != null) invalid = true
                    var offset = 1
                    val aggregate = ArrayList<ByteArray>()
                    while (!invalid && offset < length) {
                        if (offset + 2 > length) { invalid = true; break }
                        val size = ((payload[offset].toInt() and 255) shl 8) or (payload[offset + 1].toInt() and 255)
                        offset += 2
                        if (size < 1 || size > length - offset || (payload[offset].toInt() and 31) !in 1..23) {
                            invalid = true; break
                        }
                        aggregate.add(payload.copyOfRange(offset, offset + size)); offset += size
                    }
                    if (aggregate.isEmpty()) invalid = true
                    if (!invalid) aggregate.forEach { addNal(it) }
                }
                28 -> {
                    if (length < 3) invalid = true
                    else {
                        val start = payload[1].toInt() and 128 != 0
                        val end = payload[1].toInt() and 64 != 0
                        val nalType = payload[1].toInt() and 31
                        if (nalType !in 1..23 || payload[1].toInt() and 32 != 0 || (start && end)) invalid = true
                        else if (start) {
                            if (fragment != null) invalid = true
                            else fragment = java.io.ByteArrayOutputStream().apply {
                                write((payload[0].toInt() and 224) or nalType)
                            }
                        } else if (fragment == null) invalid = true
                        if (!invalid) {
                            val f = fragment!!
                            if (length - 2 > maxBytes - bytes - f.size()) invalid = true
                            else {
                                f.write(payload, 2, length - 2)
                                if (end) { fragment = null; addNal(f.toByteArray()) }
                            }
                        }
                    }
                }
                else -> invalid = true
            }
        }
        if (!marker) return null
        if (fragment != null) invalid = true
        if (invalid) { clearUnit(); return null }
        // Update configuration only from an intact timestamp; retain it for every IDR.
        nals.forEach {
            when (it[4].toInt() and 31) { 7 -> cachedSps = it; 8 -> cachedPps = it }
        }
        val hasVcl = nals.any { (it[4].toInt() and 31) in 1..5 }
        if (!hasVcl) { clearUnit(); return null }
        val idr = nals.any { (it[4].toInt() and 31) == 5 }
        val resultNals = ArrayList<ByteArray>()
        if (idr) {
            if (nals.none { (it[4].toInt() and 31) == 7 }) cachedSps?.let { resultNals.add(it) }
            if (nals.none { (it[4].toInt() and 31) == 8 }) cachedPps?.let { resultNals.add(it) }
        }
        resultNals.addAll(nals)
        val total = resultNals.sumOf { it.size }
        if (total > maxBytes) { clearUnit(); return null }
        val result = ByteArray(total)
        var offset = 0
        resultNals.forEach { it.copyInto(result, offset); offset += it.size }
        clearUnit()
        return result
    }

    private fun addNal(nal: ByteArray) {
        if (nals.size >= 256 || nal.size + 4 > maxBytes - bytes) { invalid = true; return }
        val annex = byteArrayOf(0, 0, 0, 1) + nal
        nals.add(annex); bytes += annex.size
    }
    private fun clearUnit() {
        timestamp = null; invalid = false; packets = 0; bytes = 0; fragment = null; nals.clear()
    }
}
