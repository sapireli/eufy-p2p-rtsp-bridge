# Dual-stack deployment and on-screen errors

## Defects and changes

GaragePi showed its latency dot without video because both streams and the
control WebSocket could not resolve `homebridge.lan`. DNS-SD still resolved the
bridge, but initially selected an IPv6 address for an HTTP server bound only to
IPv4. The bridge now defaults to `::` with IPv4-mapped connections enabled.
Its deployed bootstrap was updated explicitly; changing the code default alone
would not have replaced the old `host: 0.0.0.0` setting.

Server-generated camera, clock, native rendition and internal pull URLs now
preserve IPv6 brackets. Explicit interface binds select a compatible internal
self-host unless overridden. HTTP, WebSockets and RTSP were checked on both
loopback families against the actual running bridge. Authentication was healthy,
each API returned eight cameras, both WebSockets returned ready hello messages,
and both RTSP OPTIONS requests returned `RTSP/1.0 200 OK`.

Review also found that a cached address could be reassigned by DHCP. The server
now persists a public UUID; clients require the known identity before automatic
address migration. Auth errors outrank a stale ready flag. Both clients show
connection errors as text and retain them through retries until recovery.

### Android URI defect found by actual playback

HTTP, WebSocket and raw RTSP OPTIONS tests passed over IPv6, but an actual
30-second wall test rendered zero frames. The API 28 RTSP processor used
`android.net.Uri.host`, which truncated a bracketed IPv6 address at its first
colon; the device logged an attempt to resolve `"[2603"`.

The processor now obtains socket host and port through `java.net.URI` without
rewriting the RTSP URL. The repeated actual IPv6 wall test passed: Front Door
rendered **440** frames and Garage Doorbell **426** over 30 seconds, both using
hardware decoding with unchanged player instances. Wyze rendered **452** and
Balcony **754** with their existing software fallback. Original bridge and wall
choices were restored afterward. The Android URI regression has a device test.

## Deployment and verification

### Bridge

- Runtime source `6e474d8c59eeae2925d5f0309cf9b2191f900403`, PID 7116,
  zero service restarts after the controlled deployment.
- **334 server tests passed**, no skips. Actual IPv4 and IPv6 HTTP, WebSocket,
  RTSP and clock checks passed. The Avahi record advertises both families again.
- Stored operating settings and admin-token checksums stayed unchanged. The
  new public identity file is owned by the service user with mode 0600.
- SDK remains pinned to `48e4f2f1d0dd5d255d928766cdae585bec6076d0`, which matched
  the remote `eufy-wall` head at final verification. No SDK source was changed.
- Private rollback sources are retained under
  `/var/backups/eufy-wall/dualstack-20261023/` and
  `/var/backups/eufy-wall/dualstack-identity-20261023/`.

### Fire TVs

- `.58`, `.85`, `.100`: **0.7.2/code 21** installed and hashes checked. Final APK:
  `ce2c1e25d9fe0c9d8c3293b2a5dc675456cbaaeac71ccd97a9a2934c80c2fb54`.
- **136 JVM tests passed**. Actual API 28 transport tests checked IPv4, global
  IPv6 and interface-scoped link-local IPv6 for HTTP, WebSockets, RTSP OPTIONS
  and freshness clocks. The selected DNS-SD instance returned A and AAAA records.
- Device UI tests displayed real DNS and refused-connection errors, then verified
  successful setup recovery. A locally cancelled WebSocket displayed the wall
  banner; the real reconnect/hello cleared it.
- Actual IPv6 media output passed as described above. The final normal-wall
  checks showed all four fixed feeds advancing on each TV, stable processes and
  preserved wall layout, original bridge, hardware pins and pairing.

### GaragePi

- Updated the current replacement at `.56`, whose trusted SSH identity matches
  the former DietPi host. The retired `.73` host is not claimed updated.
- Go ARMv6 SHA-256:
  `72557556f1c6542f1ac39c4dcc39fd4450065b793caf789b01421e0e7f86564b`.
- Native plugin SHA-256:
  `a304d7cfa2fe495e5a4c70592861cf2838ebce84fdfa46b7de4c058ebce3f72a`.
  Built off-Pi; native loading and a 640×22 RGBA caption pipeline passed.
- The full Go race suite passed across **13 packages**; `go vet` and formatting
  checks passed. Native caption glyph/buffer bounds tests passed and now run in CI.
- A controlled `.invalid` bridge trial displayed
  **DNS FAILED - CHECK BRIDGE ADDRESS** on both actual primary-framebuffer
  footers. The captured footer had 3,560 white text pixels. After exact config
  restoration, white error-text pixels were zero.
- Final 63-second IPv6 playback: stable PID 8383, zero service restarts, both
  `v4l2h264dec` players stable. Front Door advanced **676** accepted frames and
  Garage Doorbell **604**. Latest bridge-ingress-to-KMS-return upper ages were
  approximately **215 ms / 204 ms**; missing-stamp and unknown-clock counts were
  zero. These are latest samples, not capture latency or all-frame guarantees.
- Configuration checksum remained
  `79fb69cb5ac86226c35c97d1f8caee49787be673f81728717aac9631d6cca3f8`.
  Camera selection, hardware priorities, admin token and occupancy policy were
  preserved. Public identity pins persist privately across power cycles.

## Evidence and limits

Committed evidence: [GaragePi measurements](data/dual-stack-20261023/garagepi.json)
and the [actual DNS-error footer](data/dual-stack-20261023/garagepi-dns-footer.png).
The footer image contains only the owned status pixels, not camera pictures.

Private device results are retained in `/tmp/eufy-dualstack-tv-evidence-20261023/`,
`/tmp/eufy-dualstack-linux-final-evidence-20261023.json`,
`/tmp/eufy-dualstack-dns-footer.png` and
`/tmp/eufy-dualstack-live-server-final.jsonl`.

Family-outage migration was tested with injected network probes; a live family
outage was not imposed on the household bridge. Compositor/window caption paths
have unit and native-pipeline coverage; the deployed Pi uses direct DRM planes.
Decoder and sink-return progress do not prove sensor capture time or physical
panel scanout. Existing upstream camera pauses are not claimed fixed here.
