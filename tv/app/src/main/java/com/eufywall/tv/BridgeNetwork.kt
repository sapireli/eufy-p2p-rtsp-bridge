package com.eufywall.tv

import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

internal fun normalizeBridgeAddress(input: String): String {
    var value = input.trim()
    require(value.isNotEmpty()) { "Enter a bridge hostname or IP address." }
    if (!value.contains("://")) {
        if (!value.startsWith("[") && value.count { it == ':' } > 1) value = "[$value]"
        value = "http://$value"
    }
    val authority = value.substringAfter("://").substringBefore('/').substringBefore('?').substringBefore('#')
    if (authority.startsWith('[') && authority.substringBefore(']').contains('%')) {
        val encodedHost = authority.substringAfter('[').substringBefore(']')
        val scoped = java.net.InetAddress.getByName(encodedHost.replace("%25", "%"))
        require(scoped is java.net.Inet6Address && scoped.scopeId > 0 && scoped.isLinkLocalAddress) { "Enter a valid local IPv6 interface scope." }
        com.alexvas.utils.ScopedAddressResolver.register(scoped)
        value = value.replace("[$encodedHost]", "[${encodedHost.substringBefore('%')}]")
    }
    val explicitHttps = value.startsWith("https://")
    val url = value.toHttpUrlOrNull() ?: error("Invalid address. Use hostname:port, IPv4, or [IPv6]:port.")
    require(url.username.isEmpty() && url.password.isEmpty() && url.encodedPath == "/" && url.query == null && url.fragment == null) { "Enter only the bridge address, without a path or login." }
    val port = if (!explicitHttps && url.port == 80 && !explicitBridgePort(value)) 3000 else url.port
    val host = if (url.host.contains(':')) "[${url.host}]" else url.host
    return "${url.scheme}://$host:$port"
}
private fun explicitBridgePort(value: String): Boolean {
    val authority = value.substringAfter("://").substringBefore('/').substringBefore('?').substringBefore('#')
    return if (authority.startsWith('[')) authority.substringAfter(']').startsWith(':') else authority.contains(':')
}
internal fun websocketBridgeUrl(bridge: String): String = bridge.replaceFirst(Regex("^http:"), "ws:").replaceFirst(Regex("^https:"), "wss:") + "/ws"
internal fun bridgeEndpoint(address: java.net.InetAddress, port: Int): String {
    require(port in 1..65535)
    require(!address.isLoopbackAddress && !address.isAnyLocalAddress && !address.isMulticastAddress) { "Use a LAN address, not localhost or multicast." }
    require(address !is java.net.Inet6Address || !address.isLinkLocalAddress || address.scopeId > 0) { "Link-local IPv6 needs its local interface scope." }
    val host = requireNotNull(address.hostAddress)
    // Retain each interface in the candidate identity. Bind only when that candidate is attempted.
    return if (host.contains(':')) "http://[${host.replace("%", "%25")}]:$port" else "http://$host:$port"
}

internal fun connectionProblem(error: Throwable, address: String): String {
    val causes = generateSequence(error) { it.cause }.take(12).toList()
    return when {
        causes.any { it is UnknownHostException } -> "Cannot resolve $address. Check the hostname in your router/DNS, or enter the bridge IP."
        causes.any { it is NoRouteToHostException } -> "No network route to $address. Check this TV's LAN connection and the bridge address."
        causes.any { it is ConnectException && (it.message?.contains("refused", true) == true || it.message?.contains("ECONNREFUSED") == true) } -> "Connection refused at $address. Check the bridge service and its HTTP/RTSP port."
        causes.any { it is ConnectException } -> "Cannot connect to $address. Check the bridge address, service and network route."
        causes.any { it is SocketTimeoutException } -> "No response from $address. Check bridge availability, firewall and LAN connection."
        else -> "Cannot reach $address: ${error.message ?: error.javaClass.simpleName}. Check the bridge address and service."
    }
}
internal class BridgeConnectionStatus {
    var problem: String? = null; private set
    fun failed(error: Throwable, address: String) { problem = connectionProblem(error, address) }
    fun recovered() { problem = null }
}

internal fun <T> tryBridgeEndpoints(endpoints: List<String>, fetch: (String) -> T): Pair<String, T> {
    var failure: Exception? = null
    for (candidate in endpoints.distinct()) {
        try { return candidate to fetch(candidate) } catch (error: Exception) { failure = error }
    }
    throw failure ?: IllegalArgumentException("No usable address for this bridge service")
}
internal fun bridgeReadinessProblem(health: org.json.JSONObject): String? {
    val auth = health.optJSONObject("auth")?.optString("state")
    return when {
        auth == "require_2fa" -> "Bridge login needs a verification code. Complete the bridge's server login."
        auth == "require_captcha" -> "Bridge login needs a captcha. Complete the bridge's server login."
        auth != null && auth != "ok" -> "Bridge login state: $auth. Check the server login; retrying."
        health.has("ready") && !health.optBoolean("ready") -> "Bridge is discovering cameras; waiting for startup."
        else -> null
    }
}

internal fun verifyBridgeIdentity(expectedId: String?, receivedId: String?, expectedCameras: Set<String>, receivedCameras: Set<String>, allowEmptyCurrent: Boolean = false) {
    if (expectedId != null) require(receivedId == expectedId) { "Bridge identity changed. Rediscover and confirm the intended bridge; no automatic switch was made." }
    else if (expectedCameras.isNotEmpty() && !(allowEmptyCurrent && receivedCameras.isEmpty()))
        require(expectedCameras.intersect(receivedCameras).isNotEmpty()) { "Discovered address reports a different camera inventory. Confirm the intended bridge; no automatic switch was made." }
}
