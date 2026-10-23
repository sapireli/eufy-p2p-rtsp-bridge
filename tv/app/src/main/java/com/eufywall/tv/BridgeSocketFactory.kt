package com.eufywall.tv

import com.alexvas.utils.ScopedAddressResolver
import java.net.*
import javax.net.SocketFactory

/** Retains the selected interface when OkHttp fast-paths a numeric IPv6 URL. */
internal object BridgeSocketFactory : SocketFactory() {
    private class ScopedSocket : Socket() {
        override fun connect(endpoint: SocketAddress, timeout: Int) {
            super.connect(if (endpoint is InetSocketAddress) ScopedAddressResolver.scopedEndpoint(endpoint) else endpoint, timeout)
        }
        override fun connect(endpoint: SocketAddress) = connect(endpoint, 0)
    }
    override fun createSocket(): Socket = ScopedSocket()
    override fun createSocket(host: String, port: Int): Socket = createSocket().apply { connect(InetSocketAddress(ScopedAddressResolver.resolve(host).first(), port)) }
    override fun createSocket(address: InetAddress, port: Int): Socket = createSocket().apply { connect(InetSocketAddress(address, port)) }
    override fun createSocket(host: String, port: Int, localAddress: InetAddress, localPort: Int): Socket = createSocket().apply {
        bind(InetSocketAddress(localAddress, localPort)); connect(InetSocketAddress(ScopedAddressResolver.resolve(host).first(), port))
    }
    override fun createSocket(address: InetAddress, port: Int, localAddress: InetAddress, localPort: Int): Socket = createSocket().apply {
        bind(InetSocketAddress(localAddress, localPort)); connect(InetSocketAddress(address, port))
    }
}
