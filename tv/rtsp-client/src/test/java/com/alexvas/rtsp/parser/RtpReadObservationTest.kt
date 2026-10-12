package com.alexvas.rtsp.parser

import org.junit.Assert.*
import org.junit.Test

class RtpReadObservationTest {
    @Test fun stagesAggregateOnlySameTimestampAndExcludeListenerFromParser() {
        val a = RtpReadObservation.Accumulator(123)
        a.packet(10, 100, 120, 150)
        a.packetCompleted(160)
        a.packet(10, 160, 170, 200)
        val snapshot = a.snapshot(220)
        assertEquals(100, snapshot.firstHeaderStartNs)
        assertEquals(200, snapshot.lastPayloadEndNs)
        assertEquals(30, snapshot.headerReadNs)
        assertEquals(60, snapshot.payloadReadNs)
        assertEquals(30, snapshot.parserBeforeCallbackNs)
        assertEquals(2, snapshot.packetCount)
        assertEquals(123, snapshot.networkThreadId)
        a.listenerCompleted(snapshot, 240)
        a.packetCompleted(245)
        assertEquals(20, snapshot.listenerElapsedNs)
        assertEquals(240, snapshot.listenerCompletedNs)
        a.packet(11, 245, 250, 260)
        val next = a.snapshot(270)
        assertEquals(245, next.firstHeaderStartNs)
        assertEquals(5, next.headerReadNs)
        assertEquals(10, next.payloadReadNs)
        assertEquals(10, next.parserBeforeCallbackNs)
        assertEquals(0, next.listenerElapsedNs)
        assertEquals(1, next.packetCount)
        // Subsequent timestamp cannot mutate identity or aggregate fields of previous snapshot.
        assertEquals(10, snapshot.rtpTimestamp)
        assertEquals(30, snapshot.parserBeforeCallbackNs)
    }
    @Test fun metadataDoesNotChangeSingleNalOrFragmentedParserBytes() {
        val packets = listOf(
            byteArrayOf(0x06, 1, 2, 3), byteArrayOf(0x61, 4, 5),
            byteArrayOf(0x7c, 0x85.toByte(), 6, 7), byteArrayOf(0x7c, 0x45, 8, 9))
        val base = RtpH264Parser()
        val observed = RtpH264Parser()
        val a = RtpReadObservation.Accumulator(123)
        for ((i, bytes) in packets.withIndex()) {
            val expected = base.processRtpPacketAndGetNalUnit(bytes, bytes.size, i == 3)
            a.packet(if(i < 2) i.toLong() else 2, i*100L, i*100L+10, i*100L+20)
            val actual = observed.processRtpPacketAndGetNalUnit(bytes, bytes.size, i == 3)
            if (actual != null) {
                val snapshot=a.snapshot(i*100L+30)
                a.listenerCompleted(snapshot,i*100L+35)
            }
            a.packetCompleted(i*100L+40)
            assertArrayEquals(expected, actual)
            when(i) {
                0 -> assertArrayEquals(byteArrayOf(0,0,0,1,0x06,1,2,3),actual)
                1 -> assertArrayEquals(byteArrayOf(0,0,0,1,0x61,4,5),actual)
                2 -> assertNull(actual)
                3 -> assertArrayEquals(byteArrayOf(0,0,0,1,0x65,6,7,8,9),actual)
            }
        }
    }
}
