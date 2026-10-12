package com.eufywall.tv

internal data class HevcInitialization(val codecSpecificData: ByteArray, val configOnly: Boolean)

/** MediaCodec HEVC csd-0 is Annex B VPS, SPS, PPS in that order. */
internal fun hevcInitialization(data: ByteArray, offset: Int, length: Int): HevcInitialization? {
    if (offset < 0 || length < 0 || offset > data.size - length) return null
    val end = offset + length
    val starts = mutableListOf<Pair<Int, Int>>()
    var i = offset
    while (i + 3 <= end) {
        if (data[i] == 0.toByte() && data[i + 1] == 0.toByte()) {
            val prefix = if (data[i + 2] == 1.toByte()) 3 else if (i + 4 <= end && data[i + 2] == 0.toByte() && data[i + 3] == 1.toByte()) 4 else 0
            if (prefix != 0) { starts.add(i to (i + prefix)); i += prefix; continue }
        }
        i++
    }
    val parameterSets = mutableMapOf<Int, ByteArray>()
    var hasVcl = false
    starts.forEachIndexed { index, (_, body) ->
        val stop = if (index + 1 < starts.size) starts[index + 1].first else end
        if (stop - body < 2) return@forEachIndexed
        val type = (data[body].toInt() ushr 1) and 63
        if (type <= 31) hasVcl = true
        if (type in 32..34) parameterSets[type] = data.copyOfRange(body, stop)
    }
    if (!(32..34).all { parameterSets.containsKey(it) }) return null
    val size = (32..34).sumOf { 4 + parameterSets.getValue(it).size }
    val csd = ByteArray(size)
    var at = 0
    for (type in 32..34) {
        csd[at + 3] = 1; at += 4
        val nal = parameterSets.getValue(type); nal.copyInto(csd, at); at += nal.size
    }
    return HevcInitialization(csd, !hasVcl)
}
