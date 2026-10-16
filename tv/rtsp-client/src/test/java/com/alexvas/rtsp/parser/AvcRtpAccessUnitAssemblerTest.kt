package com.alexvas.rtsp.parser

import org.junit.Assert.*
import org.junit.Test

class AvcRtpAccessUnitAssemblerTest {
    private fun nal(type: Int, value: Int = 1) = byteArrayOf(type.toByte(), value.toByte())
    private fun annex(vararg nals: ByteArray) = nals.fold(byteArrayOf()) { a, n -> a + byteArrayOf(0,0,0,1) + n }
    @Test fun fourSlicesProduceExactlyOnePictureAtMarker() {
        val a = AvcRtpAccessUnitAssembler()
        for (i in 0..2) assertNull(a.push(nal(1,i),2,100,10+i,false))
        assertArrayEquals(annex(nal(1,0),nal(1,1),nal(1,2),nal(1,3)), a.push(nal(1,3),2,100,13,true))
    }
    @Test fun parameterSetsAreRetainedAndPrependedToEveryCompleteIdr() {
        val a = AvcRtpAccessUnitAssembler()
        assertNull(a.push(nal(7),2,100,1,true))
        assertNull(a.push(nal(8),2,101,2,true))
        assertArrayEquals(annex(nal(7),nal(8),nal(5)),a.push(nal(5),2,102,3,true))
        assertArrayEquals(annex(nal(7),nal(8),nal(5,2)),a.push(nal(5,2),2,103,4,true))
    }
    @Test fun sequenceLossPoisonsWholePictureButRecoversNextTimestamp() {
        val a = AvcRtpAccessUnitAssembler()
        assertNull(a.push(nal(1),2,100,1,false))
        assertNull(a.push(nal(1),2,100,3,true))
        assertArrayEquals(annex(nal(1)),a.push(nal(1),2,101,4,true))
    }
    @Test fun sequenceAndTimestampRolloverRemainValid() {
        val a = AvcRtpAccessUnitAssembler()
        assertNull(a.push(nal(1),2,0xffffffffL,65535,false))
        assertArrayEquals(annex(nal(1),nal(1)),a.push(nal(1),2,0xffffffffL,0,true))
        assertArrayEquals(annex(nal(1)),a.push(nal(1),2,0,1,true))
    }
    @Test fun missingMarkerDoesNotJoinTwoPictures() {
        val a = AvcRtpAccessUnitAssembler()
        assertNull(a.push(nal(1,8),2,100,1,false))
        assertArrayEquals(annex(nal(1,9)),a.push(nal(1,9),2,101,2,true))
    }
    @Test fun fragmentedSliceAndSecondSliceShareOnePicture() {
        val a = AvcRtpAccessUnitAssembler()
        assertNull(a.push(byteArrayOf(0x7c,0x81.toByte(),10),3,100,1,false))
        assertNull(a.push(byteArrayOf(0x7c,0x41,11),3,100,2,false))
        assertArrayEquals(annex(byteArrayOf(0x61,10,11),nal(1)),a.push(nal(1),2,100,3,true))
    }
    @Test fun markerCannotCompleteUnfinishedFuAndOrphanFuIsDropped() {
        val a = AvcRtpAccessUnitAssembler()
        assertNull(a.push(byteArrayOf(0x7c,0x81.toByte(),10),3,100,1,true))
        assertNull(a.push(byteArrayOf(0x7c,0x41,11),3,101,2,true))
        assertArrayEquals(annex(nal(1)),a.push(nal(1),2,102,3,true))
    }
    @Test fun stapWithParametersAndSlicesIsOnePicture() {
        val a = AvcRtpAccessUnitAssembler()
        val stap = byteArrayOf(24,0,2,7,1,0,2,8,1,0,2,5,1,0,2,5,2)
        assertArrayEquals(annex(nal(7),nal(8),nal(5),nal(5,2)),a.push(stap,stap.size,100,1,true))
    }
    @Test fun malformedStapDoesNotCachePartialParameters() {
        val a = AvcRtpAccessUnitAssembler()
        val broken = byteArrayOf(24,0,2,7,1,0,9,8)
        assertNull(a.push(broken,broken.size,100,1,true))
        assertArrayEquals(annex(nal(5)),a.push(nal(5),2,101,2,true))
    }
    @Test fun boundsBytesAndPacketsAndRecovers() {
        val a = AvcRtpAccessUnitAssembler(maxBytes=10,maxPackets=2)
        assertNull(a.push(nal(1),2,100,1,false))
        assertNull(a.push(nal(1),2,100,2,true))
        assertArrayEquals(annex(nal(1)),a.push(nal(1),2,101,3,true))
        val b = AvcRtpAccessUnitAssembler(maxPackets=1)
        assertNull(b.push(nal(1),2,100,1,false))
        assertNull(b.push(nal(1),2,100,2,true))
    }
}
