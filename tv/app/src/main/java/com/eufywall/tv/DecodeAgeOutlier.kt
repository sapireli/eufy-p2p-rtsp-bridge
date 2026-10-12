package com.eufywall.tv

/** Times belong to this exact PTS; absence is reported, never borrowed from another frame. */
internal data class DecodeFrameStages(val receivedNs: Long, val inputSubmitNs: Long)

/** Sparse observation only. Does not admit, drop, pace, or render a picture. */
internal fun decodeAgeOutlier(
    stamp: FrameStamp, ptsUs: Long, stages: DecodeFrameStages?, outputNs: Long,
    lowerAgeNs: Long, upperAgeNs: Long, clockWidthNs: Long, queueDepth: Int,
): String? {
    if (lowerAgeNs <= 500_000_000L) return null
    val received = stages?.receivedNs?.takeIf { it > 0 && it <= outputNs }
    val input = stages?.inputSubmitNs?.takeIf { it > 0 && it <= outputNs }
    val elapsed = received?.let { outputNs - it }
    return "decodeAgeOutlier epoch=${stamp.epoch} frameId=${stamp.frameId} ingressMonoUs=${stamp.ingressMonoUs} sourceTimestamp=${stamp.sourceTimestamp} rtpPtsUs=$ptsUs auReceivedNs=${received ?: -1} decoderInputSubmitNs=${input ?: -1} decoderOutputNs=$outputNs receiveToDecodeNs=${elapsed ?: -1} inputQueueDepth=$queueDepth ageLowerNs=$lowerAgeNs ageUpperNs=$upperAgeNs clockIntervalWidthNs=$clockWidthNs captureClockValidated=false presentationScope=codec-output"
}
