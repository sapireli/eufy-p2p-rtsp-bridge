package com.eufywall.tv

import org.junit.Assert.*
import org.junit.Test
import java.net.*

class BridgeNetworkTest {
    @Test fun manualAddressesCoverBothFamiliesAndExplicitPorts() {
        assertEquals("http://192.168.23.158:3000", normalizeBridgeAddress("192.168.23.158"))
        assertEquals("http://homebridge.lan:80", normalizeBridgeAddress("http://homebridge.lan:80"))
        assertEquals("http://[2001:db8::1]:3000", normalizeBridgeAddress("2001:db8::1"))
        assertEquals("http://[2001:db8::1]:4000", normalizeBridgeAddress("[2001:db8::1]:4000"))
        assertEquals("https://[2001:db8::1]:4000", normalizeBridgeAddress("https://[2001:db8::1]:4000"))
        assertEquals("https://homebridge.lan:443", normalizeBridgeAddress("https://homebridge.lan"))
        assertEquals("wss://[2001:db8::1]:4000/ws", websocketBridgeUrl("https://[2001:db8::1]:4000"))
    }
    @Test fun malformedAndUnsafeDiscoveryAddressesAreRejected() {
        assertThrows(IllegalArgumentException::class.java) { normalizeBridgeAddress("http://user:password@host") }
        assertThrows(IllegalArgumentException::class.java) { bridgeEndpoint(InetAddress.getByName("127.0.0.1"), 3000) }
        assertThrows(IllegalArgumentException::class.java) { bridgeEndpoint(InetAddress.getByName("::1"), 3000) }
        assertEquals("http://[2001:db8::1]:3000", normalizeBridgeAddress(bridgeEndpoint(InetAddress.getByName("2001:db8::1"), 3000)))
    }
    @Test fun distinguishesDnsRefusedTimeoutRouteAndRecovers() {
        val state = BridgeConnectionStatus()
        state.failed(UnknownHostException("missing"), "missing.lan")
        assertTrue(state.problem!!.contains("Cannot resolve"))
        assertTrue(state.problem!!.contains("router/DNS"))
        state.failed(ConnectException("refused"), "host")
        assertTrue(state.problem!!.contains("Connection refused"))
        state.failed(SocketTimeoutException(), "host")
        assertTrue(state.problem!!.contains("No response"))
        state.failed(NoRouteToHostException(), "host")
        assertTrue(state.problem!!.contains("No network route"))
        state.recovered()
        assertNull(state.problem)
    }
    @Test fun unreachableFamilyFallsBackOnlyWithinSuppliedServiceAddresses() {
        val attempts = mutableListOf<String>()
        val result = tryBridgeEndpoints(listOf("http://[2001:db8::1]:3000", "http://192.0.2.1:3000")) {
            attempts += it
            if (it.contains('[')) throw ConnectException("Network unreachable")
            "same bridge response"
        }
        assertEquals("http://192.0.2.1:3000", result.first)
        assertEquals("same bridge response", result.second)
        assertEquals(2, attempts.size)
    }
    @Test fun emptyCameraResponseHasSpecificAuthenticationAndStartupStates() {
        assertTrue(bridgeReadinessProblem(org.json.JSONObject("""{"auth":{"state":"require_2fa"}}"""))!!.contains("verification code"))
        assertTrue(bridgeReadinessProblem(org.json.JSONObject("""{"auth":{"state":"ok"},"ready":false}"""))!!.contains("discovering"))
        assertNull(bridgeReadinessProblem(org.json.JSONObject("""{"auth":{"state":"ok"},"ready":true}""")))
    }

    @Test fun scopedIpv6SurvivesHttpNumericFastPathAndRtspResolver() {
        val scoped = java.net.Inet6Address.getByAddress(null, InetAddress.getByName("fe80::1234").address, 7)
        assertEquals("http://[fe80:0:0:0:0:0:0:1234%257]:3000", bridgeEndpoint(scoped, 3000))
        assertEquals("http://[fe80::1234]:3000", normalizeBridgeAddress(bridgeEndpoint(scoped, 3000)))
        val resolved = com.alexvas.utils.ScopedAddressResolver.resolve("fe80::1234").single() as java.net.Inet6Address
        assertEquals(7, resolved.scopeId)
        val endpoint = com.alexvas.utils.ScopedAddressResolver.scopedEndpoint(InetSocketAddress(InetAddress.getByName("fe80::1234"), 3000))
        assertEquals(7, (endpoint.address as java.net.Inet6Address).scopeId)
    }

    @Test fun linkLocalCandidatesRetainDistinctInterfacesAndIpv4NeedsNoScope() {
        val bytes = InetAddress.getByName("fe80::abcd").address
        val candidates = listOf(7, 8).map { bridgeEndpoint(java.net.Inet6Address.getByAddress(null, bytes, it), 3000) }
        assertEquals(2, candidates.distinct().size)
        normalizeBridgeAddress(candidates[0])
        assertEquals(7, (com.alexvas.utils.ScopedAddressResolver.resolve("fe80::abcd").single() as java.net.Inet6Address).scopeId)
        normalizeBridgeAddress(candidates[1])
        assertEquals(8, (com.alexvas.utils.ScopedAddressResolver.resolve("fe80::abcd").single() as java.net.Inet6Address).scopeId)
        assertEquals("http://169.254.1.2:3000", bridgeEndpoint(InetAddress.getByName("169.254.1.2"), 3000))
    }

    @Test fun learnedBridgeIdentityRejectsDhcpReuseAndMissingIdentity() {
        assertThrows(IllegalArgumentException::class.java) { verifyBridgeIdentity("bridge-a", "bridge-b", setOf("cam"), setOf("cam")) }
        assertThrows(IllegalArgumentException::class.java) { verifyBridgeIdentity("bridge-a", null, setOf("cam"), setOf("cam")) }
        verifyBridgeIdentity("bridge-a", "bridge-a", setOf("cam"), emptySet())
        assertThrows(IllegalArgumentException::class.java) { verifyBridgeIdentity(null, null, setOf("old-camera"), setOf("other-camera")) }
        verifyBridgeIdentity(null, null, setOf("cam"), setOf("cam"))
    }
    @Test fun authenticationFailureOutranksPreviouslyReadyBridge() {
        assertTrue(bridgeReadinessProblem(org.json.JSONObject("""{"auth":{"state":"require_2fa"},"ready":true}"""))!!.contains("verification code"))
    }

    @Test fun rtspSocketAuthorityPreservesCompleteIpv6HostAndPort() {
        val address = com.alexvas.utils.RtspAddress.parse("rtsp://[2001:db8:1::1234]:8554/front?pkt_size=8192")
        assertEquals("2001:db8:1::1234", address.host)
        assertEquals(8554, address.port)
        assertEquals("camera.lan", com.alexvas.utils.RtspAddress.parse("rtsp://camera.lan/live").host)
        assertEquals(554, com.alexvas.utils.RtspAddress.parse("rtsp://192.0.2.1/live").port)
    }

}
