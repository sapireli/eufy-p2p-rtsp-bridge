package com.alexvas.utils;

import java.net.*;
import java.util.concurrent.ConcurrentHashMap;

/** Explicitly selected LAN scope bindings; never discovers or substitutes an unrelated host. */
public final class ScopedAddressResolver {
    private static final ConcurrentHashMap<String, Inet6Address> scopes = new ConcurrentHashMap<>();
    private ScopedAddressResolver() {}
    private static String key(InetAddress address) {
        StringBuilder value = new StringBuilder();
        for (byte b : address.getAddress()) value.append(String.format("%02x", b & 255));
        return value.toString();
    }
    public static void register(InetAddress address) {
        if (address instanceof Inet6Address && address.isLinkLocalAddress() && ((Inet6Address) address).getScopeId() > 0)
            scopes.put(key(address), (Inet6Address) address);
    }
    public static InetAddress[] resolve(String host) throws UnknownHostException {
        InetAddress[] addresses = InetAddress.getAllByName(host);
        for (int i = 0; i < addresses.length; i++) {
            Inet6Address selected = scopes.get(key(addresses[i]));
            if (selected != null) addresses[i] = selected;
        }
        return addresses;
    }
    public static InetSocketAddress scopedEndpoint(InetSocketAddress endpoint) {
        InetAddress address = endpoint.getAddress();
        Inet6Address selected = address == null ? null : scopes.get(key(address));
        return selected == null ? endpoint : new InetSocketAddress(selected, endpoint.getPort());
    }
    public static String scopedHost(String host) {
        try {
            for (InetAddress address : InetAddress.getAllByName(host)) {
                Inet6Address selected = scopes.get(key(address));
                if (selected != null) return selected.getHostAddress();
            }
        } catch (UnknownHostException ignored) {}
        return null;
    }
}
