package com.eufywall.tv

internal data class SurfacePicture(val stamp: FrameStamp, val targetNs: Long)

/** Maps producer Surface timestamps to exact acquired buffers, including coalescing. */
internal class SurfaceFrameRegistry(private val capacity: Int = 256) {
    private val pictures = LinkedHashMap<Long, SurfacePicture>()
    private var lastTargetNs = 0L
    private var currentTimestampNs = Long.MIN_VALUE
    private var currentPicture: SurfacePicture? = null
    @Synchronized fun register(stamp: FrameStamp, targetNs: Long): Long {
        val unique = maxOf(targetNs, lastTargetNs + 1)
        lastTargetNs = unique
        pictures[unique] = SurfacePicture(stamp, unique)
        while (pictures.size > capacity) pictures.remove(pictures.keys.first())
        return unique
    }
    @Synchronized fun acquire(timestampNs: Long): SurfacePicture? {
        if (timestampNs == currentTimestampNs) return currentPicture
        val picture = pictures.remove(timestampNs)
        currentTimestampNs = timestampNs
        currentPicture = picture
        // updateTexImage acquired the latest buffer; earlier entries can never be displayed.
        pictures.keys.removeAll { it < timestampNs }
        return picture
    }
    @Synchronized fun size() = pictures.size
}

/** Output SurfaceView performs pacing; never defer the CPU draw for future input.
 * A replacement must be scheduled by the prior picture's cutoff. If it has already
 * expired, a valid replacement is scheduled immediately rather than holding old pixels.
 */
internal fun surfacePresentationNs(targetNs: Long, visibleDeadlineNs: Long, nowNs: Long, marginNs: Long): Long {
    val desired = maxOf(nowNs, targetNs)
    return if (visibleDeadlineNs > 0) minOf(desired, maxOf(nowNs, visibleDeadlineNs - marginNs)) else desired
}
