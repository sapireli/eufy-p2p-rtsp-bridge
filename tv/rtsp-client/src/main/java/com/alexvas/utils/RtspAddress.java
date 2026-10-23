package com.alexvas.utils;

import java.net.URI;

/** Socket authority parsing must not use Android API 28 Uri.getHost for IPv6. */
public final class RtspAddress {
    private final String host;
    private final int port;
    private RtspAddress(String host, int port) { this.host = host; this.port = port; }
    public String getHost() { return host; }
    public int getPort() { return port; }
    public static RtspAddress parse(String value) {
        URI uri = URI.create(value);
        String host = uri.getHost();
        if (host == null || host.isEmpty()) throw new IllegalArgumentException("RTSP URL has no valid host");
        if (host.startsWith("[") && host.endsWith("]")) host = host.substring(1, host.length() - 1);
        int port = uri.getPort() < 0 ? 554 : uri.getPort();
        if (port < 1 || port > 65535) throw new IllegalArgumentException("RTSP port must be between 1 and 65535");
        return new RtspAddress(host, port);
    }
}
