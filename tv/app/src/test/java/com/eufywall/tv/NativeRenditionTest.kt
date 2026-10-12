package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class NativeRenditionTest {
    @Test fun rejectsSoftwareNamesOnOlderFireTv() {
        listOf("OMX.google.hevc.decoder", "c2.android.hevc.decoder", "c2.google.hevc.decoder", "OMX.vendor.sw.hevc", "OMX.ffmpeg.hevc").forEach { assertFalse(it, hardwareCodecName(it)) }
        assertTrue(hardwareCodecName("OMX.MTK.VIDEO.DECODER.HEVC"))
        assertTrue(hardwareCodecName("OMX.amlogic.hevc.decoder.awesome"))
    }
    @Test fun onlyKnownHevcGeometryIsEligible() {
        assertTrue(nativeGeometryValid("h265", 1280, 1440))
        assertTrue(nativeGeometryValid("hevc", 1536, 2048))
        assertFalse(nativeGeometryValid("h264", 1280, 1440))
        assertFalse(nativeGeometryValid("h265", 0, 1440))
        assertFalse(nativeGeometryValid("h265", 1280, -1))
    }
    @Test fun failedNativeStaysOnCompatibilityWithoutAffectingOtherCameras() {
        val policy = NativeFallback()
        assertTrue(policy.permits("door"))
        policy.fail("door")
        assertFalse(policy.permits("door"))
        assertTrue(policy.permits("garage"))
        policy.fail("door")
        assertFalse(policy.permits("door"))
    }
}
