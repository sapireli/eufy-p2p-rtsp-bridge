package com.eufywall.tv

import java.io.ByteArrayOutputStream
import org.junit.Assert.*
import org.junit.Test

class BridgeMdnsAddressesTest {
    private val instance = "Wall on bridge"
    private val service = "_eufy-wall._tcp."
    private val owner = mdnsOwner(instance, service)!!
    private val target = listOf("bridge", "local")
    private fun name(labels: List<String>): ByteArray = ByteArrayOutputStream().apply {
        labels.forEach { val b = it.toByteArray(); write(b.size); write(b) }; write(0)
    }.toByteArray()
    private fun word(n: Int) = byteArrayOf((n ushr 8).toByte(), n.toByte())
    private fun record(labels: List<String>, type: Int, data: ByteArray, ttl: Int = 120): ByteArray =
        name(labels) + word(type) + word(0x8001) + byteArrayOf(0, 0, (ttl ushr 8).toByte(), ttl.toByte()) + word(data.size) + data
    private fun srv(labels: List<String> = owner, host: List<String> = target, ttl: Int = 120) = record(labels, 33, byteArrayOf(0, 0, 0, 0) + word(3000) + name(host), ttl)
    private fun packet(vararg records: ByteArray): ByteArray =
        byteArrayOf(0, 0, 0x84.toByte(), 0, 0, 0) + word(records.size) + byteArrayOf(0, 0, 0, 0) + records.fold(byteArrayOf()) { a, b -> a + b }
    private fun collector() = MdnsAddressCollector(owner)

    @Test fun exactServiceSrvAcceptsBothAddressFamiliesOnlyForItsTarget() {
        val c = collector()
        val unrelated = listOf("other", "local")
        c.accept(packet(record(target, 1, byteArrayOf(192.toByte(), 168.toByte(), 1, 4)),
            srv(mdnsOwner("Unrelated wall", service)!!, unrelated), record(unrelated, 1, byteArrayOf(10, 0, 0, 9)),
            srv(), record(target, 28, byteArrayOf(0xfd.toByte(), 0x23) + ByteArray(13) + byteArrayOf(4))), 7)
        val values = c.addresses()
        assertEquals(2, values.size)
        assertTrue(values.any { it.address.size == 4 && it.hostAddress == "192.168.1.4" })
        assertTrue(values.any { it.address.size == 16 })
        assertFalse(values.any { it.hostAddress == "10.0.0.9" })
    }

    @Test fun separatePacketsAndAddressBeforeSrvWorkWithoutUnrelatedFallback() {
        val c = collector()
        c.accept(packet(record(target, 1, byteArrayOf(10, 0, 0, 2))), 1)
        assertTrue(c.addresses().isEmpty())
        assertEquals(listOf(target), c.accept(packet(srv()), 1))
        assertEquals("10.0.0.2", c.addresses().single().hostAddress)
        assertTrue(c.accept(packet(srv()), 1).isEmpty())
    }

    @Test fun rejectsUnusableAddressesAndScopesIpv6LinkLocalToReceivingInterface() {
        val c = collector()
        val link = byteArrayOf(0xfe.toByte(), 0x80.toByte()) + ByteArray(13) + byteArrayOf(3)
        c.accept(packet(srv(), record(target, 1, byteArrayOf(127, 0, 0, 1)), record(target, 1, ByteArray(4)),
            record(target, 1, byteArrayOf(224.toByte(), 0, 0, 1)), record(target, 28, ByteArray(16)),
            record(target, 28, ByteArray(15) + byteArrayOf(1)),
            record(target, 28, byteArrayOf(0xff.toByte(), 2) + ByteArray(14)), record(target, 28, link)), 7)
        assertEquals(1, c.addresses().size)
        assertEquals(7, (c.addresses().single() as java.net.Inet6Address).scopeId)
        val unscoped = collector(); unscoped.accept(packet(srv(), record(target, 28, link)), 0)
        assertTrue(unscoped.addresses().isEmpty())
    }

    @Test fun compressionPointersAreSupportedAndCyclesOrBoundsFailClosed() {
        val full = packet(srv())
        // Owner at offset12 is reused by a compressed second SRV owner; target remains explicit.
        val compressed = byteArrayOf(0xc0.toByte(), 12) + word(33) + word(1) + byteArrayOf(0, 0, 0, 120) + word(6 + name(target).size) + byteArrayOf(0, 0, 0, 0) + word(3000) + name(target)
        val joined = full.copyOf().apply { this[7] = 2 } + compressed
        assertEquals(listOf(target), collector().accept(joined, 1))
        val cyclic = byteArrayOf(0, 0, 0x84.toByte(), 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xc0.toByte(), 12)
        assertTrue(collector().accept(cyclic, 1).isEmpty())
        assertTrue(collector().accept(cyclic.copyOf().apply { this[13] = 127 }, 1).isEmpty())
        assertTrue(collector().accept(packet(srv()).copyOf(20), 1).isEmpty())
        assertTrue(collector().accept(ByteArray(10000), 1).isEmpty())
        assertTrue(collector().accept(full.copyOf().apply { this[6] = 1; this[7] = 0 }, 1).isEmpty())
    }

    @Test fun goodbyeRecordsAndDnsQueriesCannotIntroduceTargets() {
        val c = collector()
        assertTrue(c.accept(packet(srv(ttl = 0)), 1).isEmpty())
        assertTrue(c.accept(packet(srv()).apply { this[2] = 0 }, 1).isEmpty())
        assertTrue(c.addresses().isEmpty())
    }

    @Test fun serviceOwnerKeepsInstanceDotsAsOneLabelAndRequiresLocalTcpService() {
        assertEquals(listOf("Wall.name", "_eufy-wall", "_tcp", "local"), mdnsOwner("Wall.name", service))
        assertEquals(owner, mdnsOwner(instance, "_eufy-wall._tcp.local."))
        assertNull(mdnsOwner(instance, "_eufy-wall._tcp.example.com"))
        assertNull(mdnsOwner(instance, "_eufy-wall._udp"))
        assertNull(mdnsOwner("x".repeat(64), service))
    }
}
