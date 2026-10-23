package com.eufywall.tv

import java.net.DatagramPacket
import java.net.Inet6Address
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.MulticastSocket
import java.net.NetworkInterface
import java.net.SocketTimeoutException
import java.util.Collections
import java.util.Locale
import java.util.concurrent.Callable
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Blocking discovery supplement for Android versions whose NSD resolver returns one address.
 * Invoke once from the discovery worker, never from playback or the main thread. */
internal object BridgeMdnsAddresses {
    fun discover(instanceName: String, serviceType: String, timeoutMs: Int = 1500): List<InetAddress> {
        val owner = mdnsOwner(instanceName, serviceType) ?: return emptyList()
        val timeout = timeoutMs.coerceIn(1, 3000)
        val deadline = System.nanoTime() + timeout * 1_000_000L
        val endpoints = try {
            Collections.list(NetworkInterface.getNetworkInterfaces()).filter {
                it.isUp && !it.isLoopback && it.supportsMulticast() && it.index > 0
            }.flatMap { iface -> Collections.list(iface.inetAddresses).filter {
                !it.isLoopbackAddress && !it.isAnyLocalAddress && !it.isMulticastAddress
            }.distinctBy { it is Inet6Address }.map { iface to it } }.take(8)
        } catch (_: Exception) { return emptyList() }
        if (endpoints.isEmpty()) return emptyList()
        val sockets = Collections.synchronizedList(mutableListOf<MulticastSocket>())
        val pool = Executors.newFixedThreadPool(endpoints.size)
        val collectors = endpoints.map { MdnsAddressCollector(owner) }
        return try {
            val tasks = endpoints.mapIndexed { index, (iface, local) -> Callable {
                val collector = collectors[index]
                if (System.nanoTime() >= deadline) return@Callable collector.addresses()
                try {
                    MulticastSocket(null).use { socket ->
                        sockets.add(socket)
                        socket.reuseAddress = true
                        socket.bind(InetSocketAddress(local, 0))
                        socket.networkInterface = iface
                        socket.timeToLive = 255
                        val group = if (local is Inet6Address) {
                            Inet6Address.getByAddress(null, byteArrayOf(0xff.toByte(), 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xfb.toByte()), iface.index)
                        } else InetAddress.getByName("224.0.0.251")
                        fun send(labels: List<String>, types: List<Int>) {
                            val bytes = mdnsQuery(labels, types)
                            socket.send(DatagramPacket(bytes, bytes.size, group, 5353))
                        }
                        send(owner, listOf(33))
                        var packets = 0
                        while (packets++ < 64) {
                            val remaining = deadline - System.nanoTime()
                            if (remaining <= 0) break
                            socket.soTimeout = ((remaining + 999_999) / 1_000_000).coerceIn(1, 3000).toInt()
                            val bytes = ByteArray(9216)
                            val packet = DatagramPacket(bytes, bytes.size)
                            try { socket.receive(packet) } catch (_: SocketTimeoutException) { break }
                            if (packet.port != 5353 || packet.length >= bytes.size) continue
                            collector.accept(bytes.copyOf(packet.length), iface.index).forEach { target -> send(target, listOf(1, 28)) }
                        }
                    }
                } catch (_: Exception) { /* Keep ordinary NSD/manual entry available on restricted networks. */ }
                collector.addresses()
            } }
            pool.invokeAll(tasks, timeout.toLong(), TimeUnit.MILLISECONDS)
            collectors.flatMap { it.addresses() }.distinctBy { it.hostAddress }
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt(); emptyList()
        } finally {
            synchronized(sockets) { sockets.forEach { it.close() } }
            pool.shutdownNow()
        }
    }
}

private fun dnsKey(labels: List<String>) = labels.joinToString("\u0000") { it.lowercase(Locale.ROOT) }
internal fun mdnsOwner(instance: String, service: String): List<String>? {
    val type = service.trimEnd('.').split('.')
    if (instance.isEmpty() || instance.toByteArray(Charsets.UTF_8).size > 63 || type.size !in 2..3 ||
        !type[0].startsWith("_") || type.take(2).any { it.toByteArray(Charsets.UTF_8).size !in 1..63 } || !type[1].equals("_tcp", true) || (type.size == 3 && !type[2].equals("local", true))) return null
    return listOf(instance) + type.take(2) + "local"
}
private fun mdnsQuery(labels: List<String>, types: List<Int>): ByteArray {
    val out = java.io.ByteArrayOutputStream()
    fun word(n: Int) { out.write(n ushr 8); out.write(n and 255) }
    word(0); word(0); word(types.size); word(0); word(0); word(0)
    for (type in types) {
        for (label in labels) { val bytes = label.toByteArray(Charsets.UTF_8); require(bytes.size in 1..63); out.write(bytes.size); out.write(bytes) }
        out.write(0); word(type); word(0x8001) // IN + unicast-response request, sent from an ephemeral port.
    }
    return out.toByteArray()
}

internal class MdnsAddressCollector(private val owner: List<String>) {
    private val targets = linkedMapOf<String, List<String>>()
    private val pending = linkedMapOf<String, MutableList<InetAddress>>()
    private var records = 0
    @Synchronized fun accept(packet: ByteArray, scopeId: Int): List<List<String>> {
        val parsed = try { MdnsPacket(packet).records() } catch (_: IllegalArgumentException) { return emptyList() }
        val added = mutableListOf<List<String>>()
        for (record in parsed) {
            if (++records > 512) break
            if (record.target != null && dnsKey(record.name) == dnsKey(owner) && targets.size < 4) {
                val key = dnsKey(record.target)
                if (!targets.containsKey(key)) { targets[key] = record.target; added.add(record.target) }
            }
            if (record.address != null && pending.size < 128) {
                val address = try {
                    val raw = InetAddress.getByAddress(record.address)
                    if (raw.isLoopbackAddress || raw.isAnyLocalAddress || raw.isMulticastAddress) continue
                    if (raw is Inet6Address && raw.isLinkLocalAddress) {
                        if (scopeId <= 0) continue
                        Inet6Address.getByAddress(null, record.address, scopeId)
                    } else raw
                } catch (_: Exception) { continue }
                val values = pending.getOrPut(dnsKey(record.name)) { mutableListOf() }
                if (values.size < 16 && values.none { it.hostAddress == address.hostAddress }) values.add(address)
            }
        }
        return added
    }
    @Synchronized fun addresses(): List<InetAddress> = targets.keys.flatMap { pending[it].orEmpty() }.distinctBy { it.hostAddress }
}
private data class MdnsRecord(val name: List<String>, val target: List<String>? = null, val address: ByteArray? = null)
private class MdnsPacket(private val data: ByteArray) {
    private fun word(at: Int): Int { require(at >= 0 && at + 2 <= data.size); return ((data[at].toInt() and 255) shl 8) or (data[at + 1].toInt() and 255) }
    private fun name(start: Int): Pair<List<String>, Int> {
        var at = start; var next = -1; var length = 0; val visited = hashSetOf<Int>(); val labels = mutableListOf<String>()
        repeat(128) {
            require(at in data.indices && visited.add(at))
            val n = data[at].toInt() and 255
            if (n == 0) return labels to if (next >= 0) next else at + 1
            if (n and 0xc0 == 0xc0) { val pointer = word(at) and 0x3fff; if (next < 0) next = at + 2; at = pointer }
            else {
                require(n in 1..63 && at + 1 + n <= data.size)
                length += n + 1; require(length <= 254)
                labels.add(String(data, at + 1, n, Charsets.UTF_8)); at += n + 1
            }
        }
        throw IllegalArgumentException("DNS name exceeds bounds")
    }
    fun records(): List<MdnsRecord> {
        require(data.size in 12..9215 && word(2) and 0x8000 != 0 && word(2) and 15 == 0 && word(2) and 0x7a00 == 0)
        val questions = word(4); val count = word(6) + word(8) + word(10)
        require(questions <= 32 && count <= 128)
        var at = 12
        repeat(questions) { at = name(at).second + 4; require(at <= data.size) }
        val out = mutableListOf<MdnsRecord>()
        repeat(count) {
            val (owner, end) = name(at); at = end
            val type = word(at); val klass = word(at + 2) and 0x7fff
            require(at + 10 <= data.size)
            val size = word(at + 8); val start = at + 10; val stop = start + size
            require(stop <= data.size); at = stop
            val ttl = (word(end + 4).toLong() shl 16) or word(end + 6).toLong()
            if (klass == 1 && ttl > 0) when {
                type == 33 && size >= 7 -> { val (target, used) = name(start + 6); require(used == stop && target.isNotEmpty() && word(start + 4) > 0); out.add(MdnsRecord(owner, target = target)) }
                type == 1 && size == 4 -> out.add(MdnsRecord(owner, address = data.copyOfRange(start, stop)))
                type == 28 && size == 16 -> out.add(MdnsRecord(owner, address = data.copyOfRange(start, stop)))
            }
        }
        return out
    }
}
