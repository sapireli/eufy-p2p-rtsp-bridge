package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class ClockDomainBracketTest {
    @Test fun pairedReadsBracketActualOffsetAndEventWithoutDirectClockSubtraction() {
        // BOOT offset40; MONO sample102 happens between BOOT reads140 and144.
        val bracket = ClockDomainBracket(140,102,144)
        assertEquals(38,bracket.offsetLowerNs)
        assertEquals(42,bracket.offsetUpperNs)
        val range=bracket.bootWindow(53)!!
        assertEquals(91L..95L,range)
        assertTrue(93 in range) // eventMONO53 with actual offset40.
    }
    @Test fun reversedOrInvalidClockSampleCannotBeUsedForAlignment() {
        assertNull(ClockDomainBracket(144,102,140).bootWindow(53))
        assertNull(ClockDomainBracket(140,-1,144).bootWindow(53))
        assertNull(ClockDomainBracket(-1,102,144).bootWindow(53))
    }
    @Test fun sparseRecordContainsExplicitClockDomainsOnlyOnRealOutlier() {
        val stamp=FrameStamp("epoch",1,2,3,500)
        val bracket=ClockDomainBracket(140,102,144)
        assertNull(decodeAgeOutlier(stamp,1,null,200,100_000_000,600_000_000,500_000_000,0,bracket))
        val record=decodeAgeOutlier(stamp,1,null,200,600_000_000,610_000_000,10_000_000,0,bracket)!!
        assertTrue(record.contains("clockBracketBootBeforeNs=140 clockBracketMonoNs=102 clockBracketBootAfterNs=144"))
        assertTrue(record.contains("bootMinusMonoLowerNs=38 bootMinusMonoUpperNs=42"))
        assertTrue(record.contains("clockBracketScope=sample-instant"))
    }
}
