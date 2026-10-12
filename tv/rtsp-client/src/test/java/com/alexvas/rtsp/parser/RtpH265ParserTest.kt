package com.alexvas.rtsp.parser

import org.junit.Assert.*
import org.junit.Test

class RtpH265ParserTest {
    private fun nal(type: Int) = byteArrayOf((type shl 1).toByte(), 1, 7, 9)
    private fun ap(vararg units: ByteArray): ByteArray = byteArrayOf(96, 1) + units.fold(byteArrayOf()) { result, n -> result + byteArrayOf((n.size ushr 8).toByte(), n.size.toByte()) + n }
    private fun annex(vararg units: ByteArray) = units.fold(byteArrayOf()) { result, n -> result + byteArrayOf(0, 0, 0, 1) + n }
    @Test fun deliversAllAggregatedNalBytesInOrder() {
        val input = ap(nal(39), nal(1), nal(1))
        assertArrayEquals(annex(nal(39), nal(1), nal(1)), RtpH265Parser().processRtpPacketAndGetNalUnit(input, input.size, true))
    }
    @Test fun validatesLengthsBeforeReturningAnything() {
        val good = ap(nal(1))
        val parser = RtpH265Parser()
        assertNull(parser.processRtpPacketAndGetNalUnit(good, good.size - 1, true))
        assertNull(parser.processRtpPacketAndGetNalUnit(good + byteArrayOf(0), good.size + 1, true))
        assertNull(parser.processRtpPacketAndGetNalUnit(byteArrayOf(96, 1, 0, 0), 4, true))
        assertNull(parser.processRtpPacketAndGetNalUnit(good, good.size + 1, true))
        assertArrayEquals(annex(nal(1)), parser.processRtpPacketAndGetNalUnit(good, good.size, true))
    }
    @Test fun rejectsNestedPacketizationAndInvalidNalHeader() {
        for (n in listOf(nal(48), nal(49), byteArrayOf(2, 0), byteArrayOf(0x82.toByte(), 1))) {
            val input = ap(n)
            assertNull(RtpH265Parser().processRtpPacketAndGetNalUnit(input, input.size, true))
        }
    }
    @Test fun malformedAggregationDoesNotBorrowAnOpenFragment() {
        val parser = RtpH265Parser()
        val startFu = byteArrayOf(98, 1, 0x93.toByte(), 5, 6)
        assertNull(parser.processRtpPacketAndGetNalUnit(startFu, startFu.size, false))
        val malformedAp = byteArrayOf(96, 1, 0, 9, 2, 1)
        assertNull(parser.processRtpPacketAndGetNalUnit(malformedAp, malformedAp.size, false))
        assertTrue(parser.lastPacketMalformed)
        val validAp = ap(nal(1))
        assertArrayEquals(annex(nal(1)), parser.processRtpPacketAndGetNalUnit(validAp, validAp.size, true))
        assertFalse(parser.lastPacketMalformed)
    }
    @Test fun truncatedFuPoisonsTheCurrentAccessUnitWithoutThrowing() {
        val parser = RtpH265Parser()
        assertNull(parser.processRtpPacketAndGetNalUnit(byteArrayOf(98, 1), 2, false))
        assertTrue(parser.lastPacketMalformed)
        assertNull(parser.processRtpPacketAndGetNalUnit(byteArrayOf(98, 1, 0x81.toByte()), 3, false))
        assertTrue(parser.lastPacketMalformed)
    }
    @Test fun markerDoesNotRemoveAggregationNalUnits() {
        val input = ap(nal(32), nal(33), nal(34), nal(19))
        assertArrayEquals(annex(nal(32), nal(33), nal(34), nal(19)), RtpH265Parser().processRtpPacketAndGetNalUnit(input, input.size, false))
    }
}
