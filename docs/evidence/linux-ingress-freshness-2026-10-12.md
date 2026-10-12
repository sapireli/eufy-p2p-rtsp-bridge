# Linux bridge-ingress freshness enforcement

## Scope and boundaries

This enforces age **since the bridge received the access unit**, not camera capture age. The camera's source clock origin remains unvalidated. A camera continuously supplying already-delayed video can pass this ingress check; the implementation does not claim a capture-to-display guarantee.

Startup `/api/cameras` and subsequent `/ws` hello snapshots can activate the capability, so a bridge unavailable during client startup can recover without a client restart. Once seen, the strict capability remains enforced through temporary empty snapshots.

`mode: observe` preserves the legacy Linux pacing/rendering pipeline without clock/deadline enforcement. `mode: enforce` activates guards; an absent mode retains older v1 enforcement compatibility. Unknown modes are unsupported. The candidate was not deployed on the Pi following the flashing regression.

Only a camera advertising `freshness.version=1`, `scope=bridge-ingress`, a positive `maxAgeMs`, and `captureClockValidated=false` activates strict enforcement **only for that camera’s advertised RTSP media URL**. URL comparison preserves host, port, escaped path, credentials and every query value except the documented client-only `pkt_size` transport override. It does not resolve hostname aliases. Reusing a camera serial with an explicit different source URL cannot opt that legacy source into strict mode. Legacy explicit RTSP URLs without that advertised capability retain existing playback. Strict mode currently requires independent DRM overlay planes and the shared DRM descriptor; compositor/window pipelines reject strict mode rather than retaining potentially expired composite pictures.

## Protocol and implementation

The bridge stamps each final encoded access unit with a user-data-unregistered SEI payload. `eufystamp`, immediately after the codec parser and before hardware/software decode, extracts the 48-byte payload. A process-local, mutex-protected 256-entry registry associates original encoded PTS with ingress time, server epoch, source timestamp and frame identity. `eufypacing` looks up the original decoded PTS before rebasing it. No decoder-preserved GstMeta is required. Compressed reference pictures continue reaching the decoder; only decoded output is suppressed.

The Go process polls `/api/clock` every second, retaining at most16 samples within their original five-second leases. It chooses the lowest round-trip-plus-projected-rate-error score, preferring at least2.1 seconds of remaining lease (one-second poll, one-second maximum HTTP request,100 ms scheduling allowance). This prevents a worse recent RTT from unnecessarily inflating frame age while avoiding a healthy-clock lease gap. A server-epoch change discards all previous samples; selecting an old sample never refreshes its expiration. It atomically renames a 48-byte clock record into place:

| Bytes | Meaning |
|---|---|
| 0–7 | ASCII `EWCLK001` |
| 8–15 | Server epoch, 8 bytes |
| 16–23 | Server monotonic microseconds, BE u64 |
| 24–31 | Local request-start CLOCK_MONOTONIC nanoseconds, BE u64 |
| 32–39 | Local response-end CLOCK_MONOTONIC nanoseconds, BE u64 |
| 40–47 | Local sample-valid-until nanoseconds, BE u64 |

For a bridge ingress timestamp, the earliest possible local ingress is `requestStart + (ingressUs - serverSampleUs) * 1000`. This deliberately includes round-trip uncertainty conservatively. The mapped earliest ingress is additionally moved earlier by1000 ppm of the absolute server sample-to-frame delta, covering two opposing500 ppm clock slews; this is approximately5 ms across a five-second projection, plus the bounded request interval. Unix time does not participate. Samples older than five seconds, epoch mismatches, malformed stamps, absent PTS associations, duplicate/older frame IDs and expired deadlines suppress output. The advertised deadline clamps the stamp's deadline, and output cannot outlive the clock sample's validity lease; future pacing must fit inside that deadline with a 20 ms scanout margin.

A separate timer sleeps on the exact monotonic deadline, waking on deadline updates or expiry even if frames stop. It disables **only its own overlay plane** through the shared DRM descriptor, exposing the static primary plane with a centered `VIDEO UNAVAILABLE` caption beneath each video rectangle, and marks the tile unavailable while decoding continues. A genuinely fresh picture can resume without discarding compressed reference state or waiting for a new keyframe. Strict pacing owns its bounded wait, rechecks age immediately before rendering, and pushes synchronously to an unqueued `sync=false` sink under the same mutex as plane clearing; a delayed sink-clock queue cannot re-show an expired buffer. Actual decoder/transport/DRM failures still reconnect.

The deadline timer and 20 ms allowance are scheduling margins, not a measured physical scanout guarantee under arbitrary OS scheduling. Plane-disable failures are logged and force pipeline teardown. Live KMS clearing and actual stamped V4L2 decoding must be verified before deployment is called complete.

## Verification completed before deployment

- Full Go client test suite passes, including atomic clock records, invalid-clock preservation, strict-capability validation, encoded/raw registry pairing in generated pipelines and refusal to silently downgrade advertised strict feeds.
- Linux ARMv6 Go binary builds.
- Unavailable-caption tests cover RGB565/XRGB8888 buffers, writing only inside the intended tile rectangle without additional DRM ioctls or per-frame rendering.
- Native ARMv6 GStreamer plugin compiles on the installed target ABI, with `-Wall -Wextra -Werror`; both `eufystamp` and `eufypacing` register.
- Portable C tests exercise SEI extraction/emulation-prevention, truncated payloads, different monotonic clock origins, old frames, clock-sample expiry and server-epoch mismatch.
- Native `freshness_probe.c` creates separate encoded and raw buffers. One fresh frame is rendered, two stale/missing-metadata frames are suppressed, and the no-input expiry timer clears output without further incoming buffers and resumes with a new fresh frame in the same pipeline.

The native test plugin and probe reside under `/tmp`; production playback was not modified during these tests. This isolated test does not establish actual KMS plane clearing or preservation of encoded PTS through the hardware decoder.


## Live gate rejection and decoder timing

The first strict Garage headless V4L2 run used the actual bridge-stamped RTSP feed. It processed 31 encoded and 30 decoded pictures; 27 reached the raw pacer. Seven stamped pictures were accepted and actually reached the output sink, proving the encoded-PTS registry survives this hardware decoder path. Twenty pictures were suppressed, and the original expiry architecture terminated the pipeline after 3.79 seconds. This was a **failed sustained-playback gate**, not a successful deployment. Production Linux binaries remained unchanged and its service remained inactive/disabled.

Matched encoded/decode timestamps in that short run showed startup queued work: encoded-to-decoded median 202.9 ms, maximum 359.8 ms. The final seven steady pictures took 26.4–29.4 ms to decode. The bridge's separately observed ingress-to-receipt ages near 484 ms left insufficient budget for decoding, pacing and presentation inside a 500 ms limit.

A separate 30-second headless legacy capture, with strict bridge enforcement disabled, delivered 434 encoded, 419 decoded and 382 output pictures. After the first five seconds, encoded-to-decoded delay was median 82.7 ms, p95 183.3 ms and maximum 265.8 ms; encoded-to-pacer delay was median 83.1 ms, p95 183.9 ms and maximum 266.7 ms. The longest output gap was 1,067 ms. These measure queued work plus decode in the real delivery path, rather than only the fastest single-frame decode time. Headless allocation requested the same video-meta/minimum-two-buffer contract as KMS; it does not establish physical screen presentation timing.

A freshness miss must therefore clear expired output without unnecessarily making the decoder cold again. The revised native integration test verifies expiry and recovery with the same pipeline/reference state and no GStreamer error. Its synchronous push/clear serialization requires the generated strict pipeline's unqueued `sync=false` sink. Actual KMS driver execution and timer scheduling still need a bounded live test after the bridge age issue is fixed. Strict activation is withheld; the 500 ms limit was not silently increased.


### Optimized bridge follow-up, withheld from production

With the optimized bridge, a 30-second Garage headless run using default RTSP packet size rendered only28 of389 decoded pictures. Adding the actual production hint `pkt_size=8192` increased acceptance to289 of440 decoded pictures. These remained insufficient sustained-playback results; neither was a production Linux deployment. The validation helper now reports settled render ratio and explicitly fails its continuous-playback gate below90%, rather than passing merely because a few pictures rendered.

A subsequent diagnostic instrumentation build failed before installing the new diagnostic plugin; the shell incorrectly continued with the prior plugin. Its87/205 decoded-picture result cannot establish per-drop causes. The missing diagnostic variable was fixed and native validation commands were changed to fail fast. Production activation was halted after the TV regression, before any Linux production binary or service policy changed. Exact age-versus-clock-versus-missing-stamp reasons require a successfully instrumented isolated candidate capture.

## Deployment checkpoint — observation compatibility only

On 2026-10-12 at 16:07:30 UTC the latest Go ARMv6 binary and a plugin compiled against GaragePi's native GStreamer ABI were installed on `192.168.23.73`. Before and after installation, `eufy-wall.service` remained **inactive and disabled**. No CEC, display-power, service-unit or configuration changes were made. The current bridge advertises capability version 2 with explicit observation mode; this Linux mode uses the exact legacy playback chain and does not activate clock polling, freshness drops or expiry blanking. Version 2 requires an explicit mode; old version 1 absent-mode capabilities retain enforcement compatibility. The binary SEI stamp is still version 1.

Installed artifacts:

- `/usr/local/bin/eufy-wall`: SHA-256 `fe9518309bd432019d5ab39691eb0a179b00d52c1452e8177f359ebb870a1987`.
- `/usr/local/lib/eufy-wall/gstreamer-1.0/libgsteufypacing.so`: SHA-256 `c5b0e3cb7086697aec3bc258e9eedf7797510ad2455aad8d65365eb991214e8f`.

Rollback copies and the exact previous artifact hashes are in `/var/backups/eufy-wall/freshness-observe-20261012T160730Z/deployment.json`. Latest Go tests, ARMv6 build, native portable pacing/freshness tests, registration of both plugin elements and installed-binary `-h` execution passed. This installation was **not activated** and does not establish physical strict-freshness playback acceptance. The incomplete live enforcement gates described above still apply.
