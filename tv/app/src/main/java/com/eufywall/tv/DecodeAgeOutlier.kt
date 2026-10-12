package com.eufywall.tv

/** Times belong to this exact PTS; absence is reported, never borrowed from another frame. */
internal data class DecodeFrameStages(val receivedNs: Long, val inputSubmitNs: Long, val rtp: com.alexvas.rtsp.parser.RtpReadObservation? = null)

/** Sparse observation only. Does not admit, drop, pace, or render a picture. */
internal fun decodeAgeOutlier(
    stamp: FrameStamp, ptsUs: Long, stages: DecodeFrameStages?, outputNs: Long,
    lowerAgeNs: Long, upperAgeNs: Long, clockWidthNs: Long, queueDepth: Int,
): String? {
    if (lowerAgeNs <= 500_000_000L) return null
    val received = stages?.receivedNs?.takeIf { it > 0 && it <= outputNs }
    val input = stages?.inputSubmitNs?.takeIf { it > 0 && it <= outputNs }
    val elapsed = received?.let { outputNs - it }
    val rtp = stages?.rtp
    val read = if (rtp == null) "rtpObservation=missing" else
        "rtpObservation=present rtpTimestamp=${rtp.rtpTimestamp} networkThreadId=${rtp.networkThreadId} rtpPacketCount=${rtp.packetCount} firstHeaderStartNs=${rtp.firstHeaderStartNs} lastPayloadEndNs=${rtp.lastPayloadEndNs} auCallbackNs=${rtp.auCallbackNs} headerReadElapsedNs=${rtp.headerReadNs} payloadReadElapsedNs=${rtp.payloadReadNs} parserBeforeCallbackElapsedNs=${rtp.parserBeforeCallbackNs} listenerElapsedNs=${rtp.listenerElapsedNs} listenerCompletedNs=${rtp.listenerCompletedNs} readElapsedScope=blocking-plus-scheduling"
    return "decodeAgeOutlier epoch=${stamp.epoch} frameId=${stamp.frameId} ingressMonoUs=${stamp.ingressMonoUs} sourceTimestamp=${stamp.sourceTimestamp} rtpPtsUs=$ptsUs auReceivedNs=${received ?: -1} decoderInputSubmitNs=${input ?: -1} decoderOutputNs=$outputNs receiveToDecodeNs=${elapsed ?: -1} inputQueueDepth=$queueDepth ageLowerNs=$lowerAgeNs ageUpperNs=$upperAgeNs clockIntervalWidthNs=$clockWidthNs captureClockValidated=false presentationScope=codec-output $read"
}
