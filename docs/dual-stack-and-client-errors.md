# IPv4, IPv6 and visible connection errors

The default bridge listener uses `host: "::"` with IPv4-mapped connections
enabled. HTTP, WebSockets and the go2rtc RTSP listener accept both families.
The default Avahi record advertises both. Existing startup files that explicitly
set `host: 0.0.0.0` continue to request IPv4-only binding; change that bootstrap
setting to `host: "::"` to enable both families.

Explicit interface addresses still restrict the bind. Without an explicit
`self_host`, internal go2rtc pulls follow a specific bind address. Wildcard
listeners use loopback for internal pulls. The go2rtc management API stays on
loopback and WebRTC remains disabled.

## Client addresses and discovery

IPv6 literal URLs use brackets, for example `http://[2001:db8::1]:3000` and
`rtsp://[2001:db8::1]:8554/front_door`. Hostnames may resolve to either family.
Clients retain the selected bridge's service identity and try its available
addresses; they do not switch to an unrelated advertised bridge after a failed
connection. Manual addresses remain explicit choices.

Linux supports `rtsp_base: auto`. Android discovers `_eufy-wall._tcp`; older
Android NSD implementations that return only one address are supplemented by
bounded queries for the exact service instance's SRV and A/AAAA records.
Link-local IPv6 transport retains the client interface scope. That scope is
local to the client, so the server's interface number is never copied to it.

## Bridge identity and intentional replacement

The server stores a persistent public UUID in its private data directory's
`bridge-id` file. HTTP responses expose it as `X-Eufy-Wall-Bridge-Id`, and
`/healthz` and WebSocket hello messages include `bridgeId`. Address migration
must match the previously selected bridge's identity. A mismatched or missing
identity on a previously identified bridge is rejected visibly. Older servers
without this field require matching known camera inventory before migration.
The UUID identifies an instance; it is not a password or an authentication token.

Preserve the bridge's data directory when moving the same instance. For an
intentional replacement, Android's explicit bridge selection establishes the
new choice. On Linux, clear the display's public identity pins explicitly as
the service user, then start the client using its normal TV power controller:

```sh
sudo -u wall env HOME=/var/lib/eufy-wall eufy-wall -reset-bridge-identity
```

This keeps the display configuration and admin pairing token. A new bridge may
still require its own admin pairing token before shared settings can be edited.

## On-screen errors

Connection failures are shown as text, alongside the latency dot. The clients
distinguish DNS lookup failure, refused connections, timeout, unreachable
network, bridge authentication/startup and individual stream failures. Messages
identify the bridge or camera and show retry/recovery state. They clear after
the relevant connection or video output recovers; a started process alone is
not treated as successful playback.

Linux uses its existing black background and reserved footer in DRM mode.
Compositor/window mode uses a small independent caption surface; video frames
are not copied to paint a message. Discovery failures can be displayed before
the first camera connection. Android uses a bridge connection banner and camera
tile error labels.

Changing the selected address after a bridge-wide network failure may require
reconnecting its video streams. An individual camera or decoder failure does
not trigger migration to another bridge address. No video quality, buffering
or hardware-first/software-fallback policy is changed by this feature.
