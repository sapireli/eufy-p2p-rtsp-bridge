# Per-camera latency dots

## Change

Android and Linux display a small green/orange/red dot at each tile's bottom
right. The [indicator contract](../client-latency-indicator.md) defines the
thresholds, unavailable state and measurement limits. No server or SDK code,
buffering, reconnect policy, camera quality or decoder selection was changed.

Review covered missing stamps, clock expiry and epoch changes, late callbacks,
camera changes, retries and unsupported output measurement. Unknown timing
cannot produce green. Output age advances even when no new frames arrive.

## Android verification

- Version 0.7.1/code 20; 120 unit tests passed.
- Installed on Fire TVs `.58`, `.85`, `.100`. Final APK SHA-256:
  `c175a67e46deaeb399f356c98a1e50675bbb7135bfd6138e77dd1c2215c12031`.
- Wall preferences and private pairing files remained byte-identical.
- A screenshot on `.85` showed the dots in the intended corners, including green
  and orange states. Hardware-decoded video surfaces were black in the capture;
  this was not treated as proof of physical picture output.
- In a 45-second playback check before the accessibility-only follow-up, all
  four fixed feeds advanced on all three TVs, with no recorded RTSP timeouts,
  player releases or control disconnects. Front Door and Garage Doorbell retained
  hardware decoding; Balcony and Wyze retained software fallback.
- Updating accessibility descriptions every 250 ms prevented UI automation from
  reaching idle. Descriptions now change only with the status band or timing
  availability; UI automation subsequently reached idle and read five dot
  descriptions. A final short check confirmed every fixed feed still advanced
  on each TV and all final decoder queues were zero.

Private device evidence is retained under
`/tmp/eufy-latency-dot-tv-20261023/`. These counter checks do not establish sensor
capture latency or physical screen scanout.

## Linux verification

Focused tests cover threshold boundaries, stale output, invalid clock records,
malformed telemetry, callback/PID ordering, camera changes, retries, unsupported
resources and older plugins. Framebuffer tests check that the dot stays in its
reserved corner and coexists with motion outlines. Pipeline tests retain the
hardware decoder and gate new plugin properties by installed capabilities.
The complete `go test ./...` suite passed across all 12 packages.

Built off-device for ARMv6:

- Go binary SHA-256:
  `96948cfb10513d8d23c19c26ae969d2ea66a31edab81d1de8c865d0323740afd`.
- Native plugin SHA-256:
  `1565447acfb15e1a55026bffa747cabd44d42939a9ff3e8df33e02647724ba65`.
  ELF inspection confirmed 32-bit ARM, CPU 6KZ and VFPv2 hard-float attributes.
  Cross-compilation completed without warnings. The reusable
  `client/gst/build-armv6-cross.sh` uses an extracted ARM development sysroot,
  omits ARMv7 compiler startup objects and links the shared GCC runtime instead
  of embedding ARMv7 static helpers. No compilation ran on a Pi.

Compositor/window mode deliberately shows unknown orange because the existing
pipeline does not retain each camera's frame identity at final composed output.

Deployment is blocked: final SSH attempts to `garagepi.lan` (resolved `.73`) and
DietPi `.128` both timed out after five seconds. No Pi files or power state were
changed. Plugin loading, live sink telemetry and physical dot visibility on
these Pis remain unverified. The binary and plugin are staged privately at
`/tmp/eufy-wall-latency-dot-armv6` and
`/tmp/libgsteufypacing-latency-dot-armv6.so` for installation when reachable.

## GaragePi deployment follow-up

On the subsequent retry, `garagepi.lan` resolved to `.56` and presented the
already-trusted DietPi replacement's SSH key. Its hostname was `GaragePi`, its
architecture ARMv6, and its display configuration matched the prior hash.
The Go binary and native plugin above were installed with rollback copies under
`/root/eufy-wall-dot-20261023/rollback/`. Installed hashes matched both artifacts.
The replacement's GStreamer 1.26.2 loaded the plugin as the `wall` service user
and exposed its `output-status` property. The client restarted as PID 984 with
zero service restarts. Configuration and admin-token checksums remained
unchanged.

Live playback verification remains blocked by LAN DNS: both camera connections
and the control WebSocket fail because `homebridge.lan` has no DNS address.
From the Pi, the bridge's numeric address `.158` returns healthy authentication,
running go2rtc and active camera streams. No client URL or resolver override was
introduced to conceal this separate DNS failure. Physical dot visibility and
live frame progress are therefore still unverified on the Pi.

The bridge's installed SDK lock resolves to
`48e4f2f1d0dd5d255d928766cdae585bec6076d0`, exactly matching the current remote
`eufy-wall` branch at this check. Its installed `dist/index.js` SHA-256
`dd649e91323a3b2b5159d68b99c369cfa1cbffc64cfa302392830e482ff4da47`
matches the clean pinned build. No SDK update or bridge restart was needed.
