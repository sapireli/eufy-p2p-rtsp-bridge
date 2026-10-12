package com.alexvas.rtsp.parser
import org.junit.Assert.*
import org.junit.Test
class HevcAccessUnitAssemblerTest {
    @Test fun groupsPrefixAndSlicesOnlyUntilSameTimestampMarker() {
        val a = HevcAccessUnitAssembler()
        assertNull(a.push(byteArrayOf(1, 2), 100, false))
        assertNull(a.push(byteArrayOf(3), 100, false))
        assertArrayEquals(byteArrayOf(1, 2, 3, 4), a.push(byteArrayOf(4), 100, true))
        assertArrayEquals(byteArrayOf(5), a.push(byteArrayOf(5), 101, true))
    }
    @Test fun doesNotBorrowOldPrefixesAfterMissingMarker() {
        val a = HevcAccessUnitAssembler()
        assertNull(a.push(byteArrayOf(1), 100, false))
        assertArrayEquals(byteArrayOf(2), a.push(byteArrayOf(2), 101, true))
    }
    @Test fun malformedAggregationPoisonsWholeTimestampWithoutBorrowingNextFrame() {
        val a = HevcAccessUnitAssembler()
        assertNull(a.push(byteArrayOf(1), 100, false))
        assertNull(a.push(null, 100, false, true))
        assertNull(a.push(byteArrayOf(2), 100, true))
        assertArrayEquals(byteArrayOf(3), a.push(byteArrayOf(3), 101, true))
    }
    @Test fun rejectsMalformedCompletionAndBoundsWholeUnit() {
        val a = HevcAccessUnitAssembler(3)
        assertNull(a.push(byteArrayOf(1, 2), 100, false))
        assertNull(a.push(byteArrayOf(3, 4), 100, true))
        assertArrayEquals(byteArrayOf(5), a.push(byteArrayOf(5), 101, true))
        assertNull(a.push(byteArrayOf(6), 102, false))
        assertNull(a.push(null, 102, true))
        assertArrayEquals(byteArrayOf(7), a.push(byteArrayOf(7), 103, true))
    }
}
