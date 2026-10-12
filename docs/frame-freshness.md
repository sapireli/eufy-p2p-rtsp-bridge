# Frame freshness protocol

The bridge can attach a receipt identity to every encoded video picture. Clients
use that picture's identity and a bounded clock mapping to measure downstream
backlog. Optional enforcement rejects expired output and clears expired still
pictures. Observation is the default and preserves established playback;
enforcement has not passed the live smoothness gate. Neither mode adds a playback buffer.

## Scope

`scope: bridge-ingress` starts at the SDK video callback on this bridge. Camera
capture time, camera-side queues and transport time before that callback are
**unverified**. A camera's printed clock or raw `sourceTimestamp` cannot be used
as a calibrated capture clock. See the [evidence and limits](evidence/frame-freshness-2026-10-12.md).

## Configuration and discovery

```yaml
freshness:
  enabled: true
  mode: observe # enforce is opt-in and requires hardware validation
  max_age_ms: 500
```

The age limit is a rejection deadline, not a requested buffer duration. A device
whose normal processing exceeds it will show unavailable video; increasing a
limit would permit older pictures, not accelerate that device. Measure the live
path before enabling enforcement. `enabled: false` restores the established
egress and omits the capability, while retaining the clock diagnostic endpoint.

Supported cameras advertise this optional capability in `/api/cameras` and
WebSocket `hello`:

```json
{"freshness":{"version":2,"mode":"observe","scope":"bridge-ingress","maxAgeMs":500,"captureClockValidated":false,"clockUrl":"http://bridge:3000/api/clock"}}
```

Version 2 requires an explicit `observe` or `enforce` mode. Version 1 clients
reject it, avoiding accidental enforcement of an observation capability. The SEI
binary stamp remains version 1. New clients also accept legacy version 1
capabilities, whose absent mode means enforcement.

Observation retains the direct Android SurfaceView and legacy Linux pipeline;
age counters are informational and cannot blank, drop or restart playback.
Explicit RTSP URLs and bridges without this capability retain legacy playback.
Linux enforcement requires independent KMS planes; strict compositor/window
playback is refused because it cannot clear one expired picture independently.
On non-Linux bridge hosts, only non-transcoded streams advertise the capability.

## Association and clock

An H.264 user-data SEI or HEVC prefix SEI carries the version, scope, maximum age,
server epoch, source word, ingress monotonic microseconds and frame ID. It is
inserted after final encoding, immediately before the first video slice and
after encoder SEIs. Video NAL payloads remain unchanged. The binary layout is
specified in the evidence document.

Transcoding joins encoded packets to their exact preserved input PTS. Unknown
PTS, incomplete packet records and bounded-queue overruns close the producer;
there is no nearest-frame or latest-heartbeat substitution. Hardware decoding
and encoding are attempted first, with software fallback for hardware failures.
Source dimensions follow the existing configured decoder ceiling.

Complete frame boundaries survive both bridge container transitions: finite
Matroska SimpleBlocks feed FFmpeg, and finite FLV video tags feed go2rtc after
final encoding/stamping. Large MPEG-TS PES packets have no finite length and
made go2rtc wait for a following picture. FLV removes that dependency without
filler frames, another encoder or an added buffer. The external RTSP URLs and
picture bytes remain unchanged. See the [native one-frame proof](evidence/go2rtc-flv-access-unit-boundary-2026-10-12.md).

`GET /api/clock` returns `serverEpoch`, `monotonicUs`, scope and the unverified
capture flag. Clients time the request and complete response with their own
monotonic clock. If request start is `t0`, server time is `s`, and frame receipt
is `f`, the conservative local receipt bound is `t0 + (f - s)`. The complete
network uncertainty is included rather than assuming a symmetric connection.
Mappings expire after five seconds, are refreshed each second, and cannot be
reused across server epochs. The best valid bounded round-trip sample is used;
a slower new poll cannot displace a more precise still-valid mapping. Missing,
malformed or expired metadata fails closed only in enforcement mode.

## Display behavior

The frame's stamp survives into decoder output; checking only newest network
input would incorrectly bless an older decoded picture. The bridge may deliver
stamped expired compressed reference packets; their original receipt time is
retained, so they cannot acquire a fresh identity. Those references are still
decoded so following fresh pictures can recover. Expired
decoded pictures are not displayed. A separate deadline check masks or clears
the retained picture even if no subsequent frame arrives.

Android enforcement owns an EGL output surface and checks the acquired texture
identity and its deadline before presentation. Informational codec render callbacks
can arrive hundreds of milliseconds late and cannot control visibility. Repeated
texture notifications retain the current frame identity. Presentation timestamps
schedule admitted frames without a CPU wait that continually restarts on new input. Linux clears the camera's own KMS
plane, revealing the owned black background. Expiry is not a decoder failure
and must not cause a cold reconnect. Actual transport/decoder failures still
use the normal recovery path.

These are application deadline checks on general-purpose operating systems,
not hard real-time display guarantees. Clock oscillator drift, UI scheduling,
decoder callbacks and display scanout have practical uncertainty. The clients
reserve a display margin and expire mappings; tests must report those bounds
and physical presentation evidence separately from packet receipt statistics.

## Verify

Run the server tests, TV build/unit tests and Linux tests, then verify the
actual hardware decoder, surface/plane progression, clock-outage masking and
recovery. Record added delay and cadence. The read-only observer:

```sh
rtk proxy python3 server/scripts/measure-frame-freshness.py \
  --clock-url http://bridge:3000/api/clock --duration 30 \
  rtsp://bridge:8554/front_door_cle rtsp://bridge:8554/garage_cle
```

The observer accepts H.264 and H.265 video tracks, including bounded HEVC
RFC 7798 single-NAL, aggregation and fragmentation packets with DONL disabled.
Loss, incomplete units or duplicate stamps do not confer verified receipt age.
Its scope is ingress-to-observer receipt, not screen presentation or sensor
capture. Private captures should remain outside Git; commit only measurements
and reproducible commands.

## Live deployment status

The 500 ms enforcement experiment produced repeated blank intervals and did not
pass the user acceptance test. Display enforcement is disabled on the live bridge;
version 2 observation mode is running on both Fire TVs. Stable legacy
playback also contains source arrival pauses longer than the remaining display
budget: a strict deadline necessarily clears a held frame during such a pause.
Neither increasing the deadline nor repeatedly restarting the decoder establishes
a root-cause fix. See the [SDK pause audit](evidence/sdk-source-pause-audit-2026-10-12.md)
and [network audit](evidence/bridge-network-timing-audit-2026-10-12.md).
