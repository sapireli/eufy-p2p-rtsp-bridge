package com.eufywall.tv

/**
 * RTSP samples keep loading independently of DefaultLoadControl's maximum buffer.
 * Recover excess playout delay without discarding encoded reference frames or seeking
 * an unseekable camera. The two thresholds keep ordinary arrival jitter from changing speed.
 */
internal class LiveRtspPlaybackControl {
    private var catchingUp = false

    fun speedFor(bufferedMs: Long, playing: Boolean): Float {
        if (!playing || bufferedMs < 0) {
            catchingUp = false
            return 1f
        }
        if (bufferedMs <= 700) catchingUp = false
        else if (bufferedMs >= 1_500) catchingUp = true
        if (!catchingUp) return 1f
        return when {
            bufferedMs >= 2_500 -> 1.5f
            bufferedMs >= 1_500 -> 1.25f
            else -> 1.1f
        }
    }
}
