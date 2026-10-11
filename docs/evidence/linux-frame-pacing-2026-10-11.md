# Linux bounded frame pacing — 2026-10-11

## Why flags alone were rejected

GaragePi was reachable, with `eufy-wall.service` inactive and disabled. Its intended
display state was preserved throughout the tests: headless `fakesink` runs did not
open DRM or turn on the display.

A 40-second hardware-decoded Garage test used `rtspsrc latency=67`, TCP receive
timestamps, a one-frame decoded queue, and a clocked sink with 67 ms maximum
lateness. It accepted 443 decoded frames over 37.745 seconds. Sink interval median
was 74.67 ms, p95 167.16 ms, maximum 575.00 ms. The maximum **clock wait was
277.27 ms**, median 59.83 ms. No sink lateness drops occurred.

This is evidence against claiming that `latency=67` and `max-lateness=67ms`
provide a 67 ms future scheduling bound. GStreamer adds upstream reported latency;
maximum lateness only discards overdue frames, not frames scheduled too far ahead.
The initial flag-only candidate was rejected and never deployed.

The [official RTSP source documentation](https://gstreamer.freedesktop.org/documentation/rtsp/rtspsrc.html#rtspsrc:tcp-timestamp)
identifies TCP timestamp interpolation without drift compensation as a cause
of latency growth, but also warns that receive-time timestamping can distort timing
when a sender bursts. The initial experiment enabled it; the final native path
preserves RTP media timestamps and owns drift compensation in the decoded mapper.

## Implementation

`client/gst/gsteufypacing.c` is an in-place raw-video `GstBaseTransform`. It does
not inspect or copy pixels. A rolling two-second minimum arrival/PTS offset maps
decoded presentation timestamps to local running time. The mapping includes a
67 ms allowance and clamps scheduling to the interval `now` through `now + 67 ms`.
A newest picture arriving late renders immediately; lateness alone cannot discard it.
Repeated/out-of-order timestamps are rejected. Explicit discontinuities reset the mapping.

The post-decoder queue retains one raw frame and replaces older queued raw frames.
Encoded references always reach the decoder. Plane paths pace each tile; composite
paths pace the final composite once. The element reports zero additional latency
because its allowance is already incorporated into rebased PTS. KMS clock syncing
uses `processing-deadline=0`; shared DRM planes retain `skip-vsync=true`.

The transform consumes downstream QoS events. Sink QoS timestamps describe the
rebased clock, while the upstream decoder still uses source timestamps. Forwarding
them could make it drop valid encoded pictures using the wrong clock domain. The
fresh-picture timing policy belongs to the transform instead. This follows the
[GStreamer QoS event contract](https://gstreamer.freedesktop.org/documentation/plugin-development/advanced/qos.html).

Default RTSP jitter latency is zero over TCP. Existing explicitly configured jitter
latency is preserved and can add another allowance. Missing native plugin support
logs a warning and falls back to immediate display without a stabilization claim.
The installer builds the plugin for the destination CPU and installed GStreamer ABI.

## Verification boundaries

Pure C tests cover normal cadence, one hour / 54,000 frames with slow source clock
drift and periodic delivery jitter, a large forward timestamp jump, discontinuity
reset, out-of-order/repeated timestamps, and 30 newest pictures arriving after a
518 ms source pause. They assert the future bound and immediate late-picture output and pass
on the development host. All nine Go package checks, race tests, vet, and ARMv6
cross build pass.

These tests establish the scheduler's clock policy, not physical glass-to-glass
latency or removal of upstream pauses. A source gap longer than the allowance
remains visible. Decoder/display overhead remains separate. This does not establish
the root cause or resolution of reported brightness flashing.

## Native build and headless allocation control

GaragePi is an ARMv6 Raspberry Pi Model B. The native plugin and clock tests built
on its installed GStreamer 1.26.2 ABI; plugin discovery succeeded as the service's
`wall` user. Build dependencies were installed with 74 new packages, zero upgrades
and zero removals. Neither its service nor display was started for these tests.

The first concurrent `fakesink` tests were misleading. Door's predecode delay reached
seconds even with the original immediate `sync=false` output and no decoded queue
or pacing element: 90 seconds delivered 1,305 pictures, with matched raw-PTS
encoded-to-decoded median 273 ms, p95 1,297 ms, maximum 2,032 ms. Garage's median
was 38 ms. Door process CPU was approximately 51% of the single core.

An ordinary `fakesink` does not advertise the video metadata and minimum two
buffers that the actual KMS sink supplies. The
[KMS allocation implementation](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-bad/sys/kms/gstkmssink.c)
advertises both. The
[V4L2 allocation implementation](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2object.c)
requires video metadata to share its capture pool when the output needs stride
metadata; otherwise it selects a copying path. Door negotiated 1222×1680 raw
video, while Garage negotiated 1280×1440. This makes a plain headless sink an
inadequate substitute for the KMS allocation contract.

`client/gst/pacing_probe.c` can respond to the headless sink's allocation query
with the KMS video-meta/minimum-two-buffer contract, without opening DRM. With
that contract and the original immediate output, a concurrent 90-second baseline
delivered Door 1,320 of 1,327 encoded pictures and Garage 1,320 of 1,328. Matched
raw-PTS predecode latency was Door median 45.1 ms / p95 173.0 ms / maximum 446.2 ms,
Garage 44.5 / 132.5 / 576.1 ms. Door CPU fell to approximately 16%. These controlled
measurements support an allocation/copy artifact in the initial headless tests;
they do not establish that the production KMS path had the same defect.

Compile the probe with `cc -O2 -Wall -Wextra -Werror pacing_probe.c -o pacing-probe
$(pkg-config --cflags --libs gstreamer-1.0) -ldl`. Set
`EUFY_PROBE_KMS_ALLOCATION=1` and `EUFY_PROBE_SECONDS=180`. It records encoded
arrival, raw decoded arrival, pacer input, rendered output, and upstream/downstream QoS counts.
Raw PTS, rather than rewritten output PTS, matches the predecode measurements.

## 180-second candidate gate: bounded, but not yet smooth

Both actual camera streams ran concurrently through their V4L2 hardware decoders,
the one-frame raw queue, native pacer, and clocked headless sinks with the KMS
allocation response. Door supplied 2,689 encoded pictures; 2,441 reached the pacer
and 2,115 were rendered. Garage supplied 2,686; 2,534 reached the pacer and 2,103
were rendered. The decoded lateness policy dropped 326 Door and 430 Garage
pictures; additional pictures were replaced in the raw queue or remained in flight.

The scheduling target never exceeded 67 ms into the future. Actual pacer-input to
render waits were Door median 13.5 ms / p95 68.0 / maximum 91.3 ms, Garage 12.1 /
67.5 / 97.5 ms. This distinguishes the scheduler's bound from actual thread/clock
wakeup overhead on the single-core Pi. Pacer-input timing was matched by raw PTS
before rewriting; it includes decoder and raw queue time, not just decoder time.
The first version of the probe stamped pacer input as `decoded`; the final probe
separately stamps decoder output and pacer input for subsequent measurements.

| 30-second window start | Door median encoded→pacer input | Garage median |
| --- | ---: | ---: |
| 0 s | 47.6 ms | 42.2 ms |
| 30 s | 38.0 ms | 31.3 ms |
| 60 s | 36.7 ms | 29.3 ms |
| 90 s | 40.2 ms | 32.7 ms |
| 120 s | 37.0 ms | 35.9 ms |
| 150 s | 41.6 ms | 43.1 ms |

There was no accumulating seconds-long queue in this controlled path. Sampled TCP
receive queues had median zero, p95 67,704 bytes and maximum 415,791 bytes.
Sink QoS generated 2,114 Door / 2,102 Garage events; zero reached the upstream
encoded pad, validating the timestamp-domain boundary in the native transform.

However, rendered interval p95 was 247 ms Door / 231 ms Garage, with long gaps
up to approximately two seconds. The corresponding immediate baseline p95 was
243 / 194 ms. This candidate therefore **did not pass a smoothness improvement
gate** against the current bridge output. It was not deployed. The service remains
inactive and disabled, its configuration and installed client unchanged. Native
plugin SHA-256 was `670dfb5ee7679da31d1b38b5264cc104cdac102627fff426f1ef771721d4b782`.
Full native traces are retained in `/tmp/eufy-kms-baseline-final.tar.gz` and
`/tmp/eufy-kms-paced-final.tar.gz` on the development machine. These are headless
hardware measurements; actual KMS scanout and physical smoothness remain unverified.

## Correct media clock and rejected late-picture gating

After the bridge preserved source PTS through MPEG-TS, repeating the 180-second
gate with receive-time timestamping still produced Door interval p95 587 ms and
only 1,346 rendered pictures from 2,674 decoded pictures. Garage rendered 1,929
of 2,669, interval p95 285 ms. Decoder latency medians stayed 40.5 / 37.7 ms.

Switching only the RTSP clock policy to `buffer-mode=none`, with TCP receive-time
timestamping disabled, removed duplicated decoded PTS and restored the media cadence.
Door interval p95 improved to 127 ms; Garage to 238 ms. However, the initial
decoded late-picture gate still dropped 618 Door / 494 Garage pictures, with a
Door output freeze of 2.07 seconds. An older minimum clock offset remained active
after source delivery slowed, so discarding every newer picture against that old
deadline could extend a source pause until the two-second clock window aged out.
That policy was rejected. It was never installed in the running client.

The corrected helper retains the clock estimate and renders the newest late picture
immediately. The raw one-frame queue already replaces superseded pictures when output
is blocked; it does not need another lateness-only discard policy. A pure C regression
uses a 518 ms source pause followed by 30 regularly arriving new pictures. All are
accepted, and the first 22 render immediately while the old offset remains in the
two-second window. Previously they would have been discarded, extending the freeze
by approximately 1.5 seconds. Only an explicit discontinuity permits a source
timestamp restart; out-of-order/repeated pictures cannot replace newer output.

## Corrected concurrent hardware gate

The corrected helper ran for 180 seconds on both actual feeds, with media RTP
timestamps, zero RTSP jitter allowance, V4L2 hardware decoding and KMS-equivalent
allocation. No newest picture was rejected solely for lateness:

| Stage | Door | Garage |
| --- | ---: | ---: |
| Encoded pictures | 2,645 | 2,667 |
| Decoder output | 2,625 | 2,640 |
| Pacer input after one-frame raw queue | 2,485 | 2,568 |
| Headless rendered output | 2,484 | 2,567 |
| Sink QoS events | 2,483 | 2,566 |
| QoS events reaching upstream encoded pad | 0 | 0 |

One picture per stream remained in flight at shutdown. The raw queue replaced
approximately 5.3% Door / 2.7% Garage decoded pictures during bursts. These were
superseded pictures, not newest pictures hidden by an old clock deadline.
Typical delivered output remained near 14 / 14.5 pictures per second. Scheduling
targets stayed at most 67 ms ahead; actual pacer-to-render p95 was 68.9 / 69.3 ms,
maximum 114.4 / 93.0 ms, including OS wakeup overhead. Decoder latency medians in
successive 30-second windows stayed 37–45 ms Door / 29–37 ms Garage, with no growth.
TCP receive queues had median zero, p95 38,634 bytes and maximum 241,714 bytes.

Rendered interval median was 65.7 / 66.8 ms, p95 181.2 / 124.6 ms. These are
different live runs, not a controlled identical-frame replay: source delivery
varies. The source pause remains a real limitation. Matching raw PTS established
that Door's longest 1,636.6 ms rendered gap corresponded to a 1,732.8 ms
encoded-arrival gap, while media PTS advanced only 70 ms. The next newest picture
rendered 42.4 ms after arrival. Another 467 ms output gap corresponded to 490 ms
arrival gap and resumed within 34.3 ms. The corrected client resumed promptly;
it did not extend those pauses until the clock window expired.

The tested plugin was `5d30786cf746f48c39c77acb50703606b31f7ba1c65b624899446798dfd8c042`.
A subsequent guard handles missing PTS without inserting local-clock timestamps
into source ordering; a missing→valid timestamp regression passes. The valid-PTS
path measured above is unchanged. Full corrected traces are retained in
`/tmp/eufy-fresh-paced-final.tar.gz` on the development machine. Physical KMS
scanout, brightness flicker, and elimination of upstream source pauses are not
established by this headless test.

## Deployment checkpoint

The updated ARMv6 binary and native plugin were installed on GaragePi after the
corrected gate. Backup: `/root/eufy-wall-client-backup-20261011T051721Z`.
Only `latency_ms: 200` changed to `latency_ms: 0` in the existing configuration;
decoder auto selection, camera URLs, layout and display settings were retained.
Plugin discovery succeeded as `wall`. The service remained inactive and disabled;
its next normal display start will use the installed implementation.
The installed binary's `-dry-run` selected V4L2 and printed zero RTSP latency,
`buffer-mode=none`, the one-frame raw queue, native pacing, and clocked KMS sinks.
This inspected pipeline generation without acquiring DRM. Configured serial names
are resolved to the advertised RTSP stream keys by WebSocket hello during normal
execution; standalone serial-path playback was not the headless test target.

Installed binary SHA-256:
`4dcdba5aca441acf9f02b008778d574542e4f6c72a98a2508f01b8cda74caf11`.
Final plugin including the missing-PTS regression guard:
`e2f7e6a8993bfd6bc8289699552fc14cb4c15dab15a658ae98d4cf4fe6572c64`.

## Final finite-PES bridge transport gate

After the final bridge deployment on `192.168.23.158` (PID 11104), both live feeds
were tested concurrently for another 90 seconds using the **installed** final
native plugin. The production path now carries source PTS in finite MPEG-TS PES
fragments: `/stream/<serial>?format=mpegts&pes=fragmented` → FFmpeg demux/copy or
hardware transcode → go2rtc RTSP → client hardware decoder and bounded pacer.
RTSP keys remain `front_door_cle` and `garage_cle`. See the
[transport evidence](source-timestamp-transport-2026-10-11.md) for the oversized
picture/PES delimiting correction; no SDK source change was needed.

| Final 90-second gate | Door | Garage |
| --- | ---: | ---: |
| Encoded pictures | 1,328 | 1,285 |
| V4L2 decoder output | 1,320 | 1,260 |
| Pacer input / rendered output | 1,219 / 1,219 | 1,197 / 1,197 |
| Pacer rejected pictures | 0 | 0 |
| Active rendered frame rate | 13.91 fps | 13.83 fps |
| Render interval median / p95 | 65.9 / 193.9 ms | 66.3 / 203.5 ms |
| Decoder latency median / p95 | 39.0 / 130.9 ms | 41.6 / 116.4 ms |
| Decoder→pacer queue median / p95 | 1.8 / 42.1 ms | 0.8 / 39.3 ms |
| Pacer→render wait median / p95 | 46.0 / 72.4 ms | 27.1 / 67.7 ms |
| Maximum scheduled future wait | 67 ms | 67 ms |
| Sink QoS events / upstream QoS events | 1,218 / 0 | 1,196 / 0 |

Decoder medians in consecutive 30-second windows were Door 39 / 34 / 46 ms and
Garage 48 / 33 / 44 ms. They did not develop a growing seconds-long hold.
TCP receive queues were usually empty (median zero); bursts reached p95 185,395
bytes and maximum 420,680 bytes. Every picture reaching the pacer rendered;
the one-frame raw queue still replaces superseded pictures during delivery bursts.

The longest Door output gap was 1,000.7 ms and corresponded to a 930.2 ms
encoded-arrival gap; its next picture rendered 156.0 ms after arrival. Garage's
631.0 ms output gap corresponded to 622.8 ms between encoded arrivals and resumed
within 57.1 ms. Remaining source/decode interruptions are visible, but the client
does not extend them by rejecting fresh pictures against the old clock estimate.
This is evidence of the installed low latency clock policy and hardware delivery,
not a claim of perfectly constant 15 fps or elimination of upstream pauses.

The service remained inactive and disabled before and after this gate, and the
screen stayed untouched. Hardware verification used headless KMS-equivalent
allocation; physical KMS scanout remains unverified.

Final private artifacts are retained under `.evidence/frame-pacing-2026-10-11/`:

| Artifact | SHA-256 |
| --- | --- |
| `linux-finite-pes-headless.tar.gz` | `db6acfd7001264f5a6331aa87db1f1681e95f0bfd041bb9adc0ef8aff995e3e9` |
| `linux-finite-pes-headless-summary.jsonl` | `430c7e2aabad885d55d6020a9b7c35485d3b654c5c834684fdd86ab294c5035a` |
