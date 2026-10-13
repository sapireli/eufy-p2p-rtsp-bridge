package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test

class CameraHoldClientTest {
    private data class Request(val url: String, val method: String, val complete: () -> Unit)
    private class Transport {
        val requests = mutableListOf<Request>()
        val coordinator = HoldRequestCoordinator { url, method, done -> requests.add(Request(url, method, done)) }
    }
    private val url = "http://bridge-a:3000/hold/battery?owner=wall"

    @Test fun renewalWhilePendingIsCoalescedAndIdleRenewalCreatesOneRequest() {
        val t = Transport()
        repeat(36) { t.coordinator.setDesired(url, true) }
        assertEquals(listOf("POST"), t.requests.map { it.method })
        t.requests[0].complete()
        assertEquals(1, t.requests.size)
        t.coordinator.setDesired(url, true)
        assertEquals(listOf("POST", "POST"), t.requests.map { it.method })
    }

    @Test fun deselectWaitsForPostTerminalThenDeletesWithoutStaleRenewals() {
        val t = Transport()
        t.coordinator.setDesired(url, true)
        t.coordinator.setDesired(url, true)
        t.coordinator.setDesired(url, false)
        assertEquals(listOf("POST"), t.requests.map { it.method })
        t.requests[0].complete()
        assertEquals(listOf("POST", "DELETE"), t.requests.map { it.method })
        repeat(36) { t.coordinator.setDesired(url, false) }
        assertEquals(2, t.requests.size)
        t.requests[1].complete()
        assertEquals(2, t.requests.size)
    }

    @Test fun reselectWhileDeletePendingOrdersNewPostAfterDelete() {
        val t = Transport()
        t.coordinator.setDesired(url, true)
        t.requests[0].complete()
        t.coordinator.setDesired(url, false)
        t.coordinator.setDesired(url, true)
        t.coordinator.setDesired(url, true)
        assertEquals(listOf("POST", "DELETE"), t.requests.map { it.method })
        t.requests[1].complete()
        assertEquals(listOf("POST", "DELETE", "POST"), t.requests.map { it.method })
    }

    @Test fun finalDesiredIntentWinsWhilePostPending() {
        val t = Transport()
        t.coordinator.setDesired(url, true)
        t.coordinator.setDesired(url, false)
        t.coordinator.setDesired(url, true)
        t.requests[0].complete()
        assertEquals(listOf("POST"), t.requests.map { it.method })
    }

    @Test fun closingOrdersAllReleasesAndRejectsAnyFuturePost() {
        val t = Transport()
        val other = "http://bridge-b:3000/hold/battery?owner=wall"
        t.coordinator.setDesired(url, true)
        t.coordinator.setDesired(other, true)
        t.requests[1].complete()
        t.coordinator.close()
        assertEquals(listOf("POST", "POST", "DELETE"), t.requests.map { it.method })
        t.coordinator.setDesired(url, true)
        t.requests[0].complete()
        assertEquals(listOf("POST", "POST", "DELETE", "DELETE"), t.requests.map { it.method })
        t.requests[2].complete()
        t.requests[3].complete()
        t.coordinator.close()
        t.coordinator.setDesired(other, true)
        assertEquals(4, t.requests.size)
    }

    @Test fun releaseAllAllowsLaterForegroundButDoesNotResurrectOldRequests() {
        val t = Transport()
        t.coordinator.setDesired(url, true)
        t.coordinator.releaseAll()
        t.requests[0].complete()
        t.requests[1].complete()
        t.coordinator.setDesired(url, true)
        assertEquals(listOf("POST", "DELETE", "POST"), t.requests.map { it.method })
        // Duplicate old completion cannot delete the new lease.
        t.requests[0].complete()
        t.requests[1].complete()
        assertEquals(3, t.requests.size)
    }

    @Test fun bridgeUrlsAreIndependentAndFailureWaitsForNextRenewal() {
        val t = Transport()
        val other = "http://bridge-b:3000/hold/battery?owner=wall"
        t.coordinator.setDesired(url, true)
        t.coordinator.setDesired(other, true)
        t.coordinator.setDesired(url, false)
        t.requests[1].complete() // Represents HTTP failure: still no tight retry loop.
        assertEquals(2, t.requests.size)
        t.requests[0].complete()
        assertEquals(url, t.requests[2].url)
        assertEquals("DELETE", t.requests[2].method)
        t.coordinator.setDesired(other, true)
        assertEquals(other, t.requests[3].url)
        assertEquals("POST", t.requests[3].method)
    }

    @Test fun thirtySixPendingCamerasHaveAtMostOneRequestPerUrlAndOrderedRelease() {
        val t = Transport()
        val urls = (1..36).map { "http://bridge:3000/hold/camera$it?owner=wall" }
        repeat(5) { urls.forEach { t.coordinator.setDesired(it, true) } }
        assertEquals(36, t.requests.size)
        t.coordinator.releaseAll()
        assertEquals(36, t.requests.size)
        t.requests.take(36).forEach { it.complete() }
        assertEquals(72, t.requests.size)
        assertTrue(t.requests.drop(36).all { it.method == "DELETE" })
        t.requests.drop(36).forEach { it.complete() }
        assertEquals(72, t.requests.size)
    }
}
