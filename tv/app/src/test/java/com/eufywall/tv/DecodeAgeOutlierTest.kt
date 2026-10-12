package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class DecodeAgeOutlierTest {
    private val stamp = FrameStamp("0123456789abcdef", 123, 456, 789, 500)
    @Test fun upperOnlyMissAndBoundaryDoNotLogDefiniteOutlier() {
        assertNull(decodeAgeOutlier(stamp, 1, null, 900_000_000, 100_000_000, 600_000_000, 500_000_000, 0))
        assertNull(decodeAgeOutlier(stamp, 1, null, 900_000_000, 500_000_000, 600_000_000, 100_000_000, 0))
    }
    @Test fun exactFrameStagesAndNanosecondBoundsArePreserved() {
        val record = decodeAgeOutlier(stamp, 1000, DecodeFrameStages(700_000_000, 705_000_000), 720_000_000,
            510_000_000, 520_000_000, 10_000_000, 3)!!
        for (field in listOf("epoch=0123456789abcdef", "frameId=789", "ingressMonoUs=456", "rtpPtsUs=1000",
            "auReceivedNs=700000000", "decoderInputSubmitNs=705000000", "decoderOutputNs=720000000",
            "receiveToDecodeNs=20000000", "inputQueueDepth=3", "ageLowerNs=510000000", "clockIntervalWidthNs=10000000"))
            assertTrue(field, record.contains(field))
    }
    @Test fun missingOrInvalidTimesAreExplicitNotBorrowed() {
        val record = decodeAgeOutlier(stamp, 42, DecodeFrameStages(0, 1_000_000_001), 1_000_000_000,
            600_000_000, 610_000_000, 10_000_000, 0)!!
        assertTrue(record.contains("auReceivedNs=-1 decoderInputSubmitNs=-1"))
        assertTrue(record.contains("receiveToDecodeNs=-1"))
    }
}
