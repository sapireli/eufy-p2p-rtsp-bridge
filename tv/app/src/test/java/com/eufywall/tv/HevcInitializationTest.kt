package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test
import java.util.Base64
import java.security.MessageDigest

class HevcInitializationTest {
    private fun nal(type: Int) = byteArrayOf(0, 0, 0, 1, (type shl 1).toByte(), 1, 3)
    @Test fun reordersSdpAndDoesNotConsumePicture() {
        val input = nal(33) + nal(34) + nal(32)
        val result = hevcInitialization(input, 0, input.size)!!
        assertArrayEquals(nal(32) + nal(33) + nal(34), result.codecSpecificData)
        assertTrue(result.configOnly)
        val withPicture = input + nal(19)
        assertFalse(hevcInitialization(withPicture, 0, withPicture.size)!!.configOnly)
    }
    @Test fun incompleteConfigurationIsNotConsumed() {
        val input = nal(33) + nal(34)
        assertNull(hevcInitialization(input, 0, input.size))
        assertNull(hevcInitialization(input, -1, input.size))
    }
    @Test fun actualNativeSdpPreservesParameterBytesInCanonicalOrder() {
        // Bounded live SDP metadata: no camera identity or picture payload.
        val vps = Base64.getDecoder().decode("QAEMAf//IUAAAAMAkAAAAwAAAwCWJQJA")
        val sps = Base64.getDecoder().decode("QgEBIUAAAAMAkAAAAwAAAwCWoAKAgC0WeluRKFzcBAQEEAAAAwAQAAADAZCA")
        val pps = Base64.getDecoder().decode("RAHA98Dm2Q==")
        val start = byteArrayOf(0, 0, 0, 1)
        val input = start + sps + start + pps + start + vps
        val result = hevcInitialization(input, 0, input.size)!!
        assertEquals(88, result.codecSpecificData.size)
        assertArrayEquals(start + vps + start + sps + start + pps, result.codecSpecificData)
        val hash = MessageDigest.getInstance("SHA-256").digest(result.codecSpecificData).joinToString("") { "%02x".format(it) }
        assertEquals("d04a5e2bb2ac51e60a5d1713cf967b6053aec7beb30d8ff681907a81dd725d42", hash)
        assertTrue(result.configOnly)
    }
}
