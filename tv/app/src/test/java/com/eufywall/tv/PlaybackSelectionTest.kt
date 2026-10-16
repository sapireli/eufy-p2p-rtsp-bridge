package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class PlaybackSelectionTest {
    private val native = NativeRendition("rtsp://bridge/native", "h265", 1280, 720,
        profile = 1, level = 150, tier = 1)
    private val camera = Camera("balcony", "Balcony", "rtsp://bridge/compat", "always", true,
        false, null, 1920, 1080, nativeRendition = native)
    private fun contract(cam: Camera, permitted: Boolean = true,
        decoder: (NativeRendition) -> String? = { null }) = selectedPlayback(cam, permitted, decoder).playbackContract()

    @Test fun unsupportedNativeGeometryChangesDoNotReconnectCompatibility() {
        val original = contract(camera)
        val changed = camera.copy(nativeRendition = native.copy(width = 1920, height = 1080))
        assertEquals(original, contract(changed))
        assertEquals(original, contract(changed.copy(nativeRendition = null)))
    }

    @Test fun selectedNativeChangesInvalidateItsContract() {
        val hardware: (NativeRendition) -> String? = { "OMX.hardware.hevc" }
        val original = contract(camera, decoder = hardware)
        assertNotEquals(original, contract(camera.copy(nativeRendition = native.copy(width = 1920, height = 1080)), decoder = hardware))
        assertNotEquals(original, contract(camera.copy(nativeRendition = native.copy(rtsp = "rtsp://bridge/new-native")), decoder = hardware))
        assertNotEquals(original, contract(camera.copy(nativeRendition = null), decoder = hardware))
    }

    @Test fun eligibilityIsReevaluatedOnMetadataChange() {
        val decoder: (NativeRendition) -> String? = { if (it.level == 123) "OMX.hardware.hevc" else null }
        val unsupported = contract(camera, decoder = decoder)
        val supportedCamera = camera.copy(nativeRendition = native.copy(level = 123))
        val supported = contract(supportedCamera, decoder = decoder)
        assertNotEquals(unsupported, supported)
        assertEquals(native.rtsp, supported.rtsp)
        assertEquals(unsupported, contract(camera, decoder = decoder))
    }

    @Test fun failedNativeDoesNotGetReselectedAfterMetadataChange() {
        var probes = 0
        val hardware: (NativeRendition) -> String? = { probes++; "OMX.hardware.hevc" }
        val original = contract(camera, false, hardware)
        val changed = contract(camera.copy(nativeRendition = native.copy(width = 1920)), false, hardware)
        assertEquals(original, changed)
        assertEquals(0, probes)
        assertEquals(camera.rtsp, changed.rtsp)
    }

    @Test fun compatibilityConnectionAndFreshnessChangesStillInvalidatePlayer() {
        val original = contract(camera)
        assertNotEquals(original, contract(camera.copy(rtsp = "rtsp://bridge/new-compat")))
        assertNotEquals(original, contract(camera.copy(rtspTcpPacketSize = 4096)))
        assertNotEquals(original, contract(camera.copy(freshness = FreshnessCapability(500, "http://bridge/clock", "observe"))))
    }

    @Test fun unselectedCompatibilityChangesDoNotInvalidateNative() {
        val hardware: (NativeRendition) -> String? = { "OMX.hardware.hevc" }
        val original = contract(camera, decoder = hardware)
        assertEquals(original, contract(camera.copy(rtsp = "rtsp://bridge/new-compat", rtspTcpPacketSize = 4096), decoder = hardware))
        assertEquals(original, contract(camera.copy(name = "New label", mode = "motion"), decoder = hardware))
    }
}
