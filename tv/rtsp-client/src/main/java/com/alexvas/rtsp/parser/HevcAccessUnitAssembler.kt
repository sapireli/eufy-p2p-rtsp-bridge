package com.alexvas.rtsp.parser

/** Preserve one RTP timestamp's complete HEVC access unit until its marker. */
class HevcAccessUnitAssembler(private val maxBytes: Int = 4 * 1024 * 1024) {
    private val chunks = ArrayList<ByteArray>()
    private var bytes = 0
    private var timestamp: Long? = null
    private var invalid = false
    fun push(nal: ByteArray?, time: Long, marker: Boolean, malformed: Boolean = false): ByteArray? {
        if (timestamp != null && timestamp != time) clear()
        timestamp = time
        if (malformed) invalid = true
        if (nal != null) {
            if (nal.size > maxBytes - bytes || chunks.size >= 256) invalid = true
            if (!invalid) { chunks.add(nal); bytes += nal.size }
        } else if (marker) invalid = true
        if (!marker) return null
        if (invalid || chunks.isEmpty()) { clear(); return null }
        val result = ByteArray(bytes)
        var offset = 0
        chunks.forEach { it.copyInto(result, offset); offset += it.size }
        clear()
        return result
    }
    fun clear() { chunks.clear(); bytes = 0; timestamp = null; invalid = false }
}
