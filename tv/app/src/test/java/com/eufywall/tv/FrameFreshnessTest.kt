package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

class FrameFreshnessTest {
    private val epoch = "0001020304050607"
    private fun stamp(ingress: Long = 10_000_000) = FrameStamp(epoch, 42, ingress, 1, 500)
    @Test fun delayedPicturesExpireDespiteContinuingReceipt() {
        val sample = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_020_000_000)
        assertEquals(1_500_000_000L, sample.deadline(stamp(), 1_100_000_000, 500))
        assertNull(sample.deadline(stamp(), 1_600_000_000, 500))
    }
    @Test fun independentMonotonicOriginsDoNotNeedSynchronizedWallClocks() {
        val sample = BridgeClockSample(epoch, 500_000_000_000, 2_000_000_000, 2_030_000_000)
        val picture = stamp(499_999_900_000)
        assertEquals(2_399_900_000L, sample.deadline(picture, 2_100_000_000, 500))
    }
    @Test fun wrongEpochExpiredClockAndFutureStampCannotCertifyFreshness() {
        val sample = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_020_000_000)
        assertNull(sample.deadline(stamp().copy(epoch = "ffffffffffffffff"), 1_100_000_000, 500))
        assertNull(sample.deadline(stamp(15_500_000), 6_500_000_000, 500))
        assertNull(sample.deadline(stamp(12_000_000), 1_100_000_000, 500))
    }
    @Test fun advertisedAgeCapCannotBeExpandedByTheFrame() {
        val sample = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_020_000_000)
        assertNull(sample.deadline(stamp().copy(maxAgeMs = 10000), 1_600_000_000, 500))
    }
    @Test fun slowNewPollDoesNotDestroyAStillValidPreciseBound() {
        val window = BridgeClockWindow()
        val precise = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_010_000_000)
        val slow = BridgeClockSample(epoch, 11_000_000, 2_000_000_000, 2_700_000_000)
        window.accept(precise); window.accept(slow)
        assertEquals(precise, window.current(2_700_000_000))
        assertEquals(6_010_000_000L, window.current(2_700_000_000)!!.expiresNs)
    }
    @Test fun leaseExpirySelectsAnotherValidObservationWithoutExtendingOldLease() {
        val window = BridgeClockWindow()
        val first = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_010_000_000)
        val next = BridgeClockSample(epoch, 12_000_000, 3_000_000_000, 3_020_000_000)
        window.accept(first); window.accept(next)
        assertEquals(first, window.current(5_000_000_000))
        assertEquals(next, window.current(6_010_000_000))
        assertNull(window.current(8_020_000_000))
    }
    @Test fun newServerEpochInvalidatesOldPreciseSamplesImmediately() {
        val window = BridgeClockWindow()
        val old = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_010_000_000)
        val restarted = old.copy(epoch = "ffffffffffffffff", responseNs = 1_100_000_000)
        window.accept(old); window.accept(restarted)
        assertEquals(restarted, window.current(1_200_000_000))
        assertNull(window.current(1_200_000_000)!!.deadline(stamp(), 1_200_000_000, 500))
    }
    @Test fun relativeClockRateAllowanceIsIncludedInTheConservativeDeadline() {
        val sample = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_010_000_000)
        val picture = stamp(14_000_000)
        assertEquals(5_496_000_000L, sample.deadline(picture, 5_100_000_000, 500))
    }
    @Test fun wideRoundTripUpperMissDoesNotProveActualLateFrame() {
        val sample = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_500_000_000)
        val now = 1_600_000_000L
        assertEquals(600_000_000L, sample.ageUpperNs(stamp(), now))
        assertEquals(100_000_000L, sample.ageLowerNs(stamp(), now))
        assertEquals(500_000_000L, sample.clockIntervalWidthNs(stamp(), now))
        assertNull(sample.deadline(stamp(), now, 500))
    }
    @Test fun diagnosticLowerBoundPreservesLeaseEpochAndBothClockDriftDirections() {
        val sample = BridgeClockSample(epoch, 10_000_000, 1_000_000_000, 1_010_000_000)
        assertEquals(18_000_000L, sample.clockIntervalWidthNs(stamp(14_000_000), 5_100_000_000))
        assertEquals(86_000_000L, sample.ageLowerNs(stamp(14_000_000), 5_100_000_000))
        assertEquals(18_000_000L, sample.clockIntervalWidthNs(stamp(6_000_000), 1_100_000_000))
        assertNull(sample.ageLowerNs(stamp().copy(epoch = "wrong"), 1_100_000_000))
        assertNull(sample.ageLowerNs(stamp(), sample.expiresNs))
    }
    @Test fun annexBStampEscapingAndFrameIdentitySurvive() {
        val payload = ByteBuffer.allocate(48).order(ByteOrder.BIG_ENDIAN).apply {
            put("EWFRESHNESS00001".toByteArray()); put(1); put(1); putShort(500)
            put(byteArrayOf(0,1,2,3,4,5,6,7)); putInt(42); putLong(10_000_000); putLong(9)
        }.array()
        val rbsp = byteArrayOf(5,48) + payload + byteArrayOf(0x80.toByte())
        val escaped = ArrayList<Byte>(); var zeros = 0
        for (b in rbsp) {
            if (zeros >= 2 && (b.toInt() and 255) <= 3) { escaped.add(3); zeros = 0 }
            escaped.add(b); zeros = if (b == 0.toByte()) zeros + 1 else 0
        }
        val video = byteArrayOf(0,0,0,1,6) + escaped.toByteArray() + byteArrayOf(0,0,0,1,0x65,1,2,3)
        assertEquals(stamp().copy(frameId = 9), FrameStampReader.read(video))
        assertNull(FrameStampReader.read(byteArrayOf(0,0,0,1,0x65,1,2,3)))
        assertNull(FrameStampReader.read(video, 0, 20))
    }
}
