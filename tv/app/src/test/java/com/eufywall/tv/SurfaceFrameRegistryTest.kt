package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class SurfaceFrameRegistryTest {
    private fun stamp(id: Long) = FrameStamp("epoch",1,1,id,500)
    @Test fun coalescedBuffersUseExactNewestIdentity() {
        val r=SurfaceFrameRegistry()
        val old=r.register(stamp(1),100)
        val newest=r.register(stamp(2),200)
        assertEquals(2L,r.acquire(newest)!!.stamp.frameId)
        assertNull(r.acquire(old))
    }
    @Test fun repeatedNotificationKeepsIdentityOfSameAcquiredBuffer() {
        val r=SurfaceFrameRegistry()
        val target=r.register(stamp(1),100)
        val first=r.acquire(target)
        assertSame(first,r.acquire(target))
        assertNull(r.acquire(101))
        assertNull(r.acquire(101))
        // Once another timestamp was acquired, the old buffer is no longer current.
        assertNull(r.acquire(target))
    }
    @Test fun targetsAreUniqueEvenWhenPacingClampsMultipleFramesToNow() {
        val r=SurfaceFrameRegistry()
        assertEquals(100L,r.register(stamp(1),100))
        assertEquals(101L,r.register(stamp(2),100))
    }
    @Test fun unknownTimestampCannotBorrowLatestStamp() {
        val r=SurfaceFrameRegistry();r.register(stamp(1),100)
        assertNull(r.acquire(101))
    }
    @Test fun boundedRegistryDropsOldIdentityWithoutGuessing() {
        val r=SurfaceFrameRegistry(2)
        r.register(stamp(1),100);r.register(stamp(2),200);r.register(stamp(3),300)
        assertEquals(2,r.size());assertNull(r.acquire(100));assertEquals(3L,r.acquire(300)!!.stamp.frameId)
    }
    @Test fun replacementIsScheduledByPreviouslyVisiblePictureCutoff() {
        assertEquals(180L,surfacePresentationNs(250,200,100,20))
        assertEquals(200L,surfacePresentationNs(250,200,200,20))
        assertEquals(150L,surfacePresentationNs(150,200,100,20))
    }
    @Test fun ongoingFutureFramesRequireNoCpuWaitAndCannotExtendPriorCutoff() {
        val priorDeadline=500L
        for(now in 100L..480L step 35) {
            val submittedPresentation=surfacePresentationNs(now+67,priorDeadline,now,20)
            assertTrue(submittedPresentation>=now)
            assertTrue(submittedPresentation<=priorDeadline-20)
        }
    }
    @Test fun freshReplacementOfExpiredPictureIsImmediate() {
        assertEquals(600L,surfacePresentationNs(667,500,600,20))
        assertEquals(667L,surfacePresentationNs(667,0,600,20))
    }
}
