package com.eufywall.tv

/** Diagnostic BOOT/MONO link. Never used for playback or freshness admission. */
internal data class ClockDomainBracket(val bootBeforeNs: Long, val monoNs: Long, val bootAfterNs: Long) {
    val valid: Boolean get() = bootBeforeNs >= 0 && monoNs >= 0 && bootAfterNs >= bootBeforeNs
    /** A BOOT read before/after MONO brackets the offset at the sampled instant. */
    val offsetLowerNs: Long get() = bootBeforeNs - monoNs
    val offsetUpperNs: Long get() = bootAfterNs - monoNs
    fun bootWindow(monoEventNs: Long): LongRange? = if (valid)
        (monoEventNs + offsetLowerNs)..(monoEventNs + offsetUpperNs) else null
    fun diagnostics(): String = if (valid)
        "clockBracketBootBeforeNs=$bootBeforeNs clockBracketMonoNs=$monoNs clockBracketBootAfterNs=$bootAfterNs bootMinusMonoLowerNs=$offsetLowerNs bootMinusMonoUpperNs=$offsetUpperNs clockBracketScope=sample-instant"
        else "clockBracket=invalid"
}
