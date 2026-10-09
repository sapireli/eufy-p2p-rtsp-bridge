# Fire TV latency investigation — 2026-10-09

## Report and verification boundary

The user reports Front Door trailing Garage by about ten seconds on a Fire TV,
and subsequently confirms the same difference on the Linux display. The goal
is smooth, current video, with a measured root cause before changing playback.
The user subsequently withdrew the latency report, saying the feeds were
fine and the difference appeared to be their internal clocks, and requested
work on choppiness. The measurements below isolate parts of the live path.
They are **not** a sensor-to-display latency measurement. No latency repair
is claimed or justified by the inter-camera timestamp difference.

## Installed Android players

Both Fire TVs, `192.168.23.58` and `.85`, have the same installed APK:

```text
f0f251cf2eec7629fad2b4357e8db547a6bb7bfbef35568d0f3821cf5e3b88b9
```

This matches the direct RTSP player documented in
[the October 8 decoder investigation](tv-low-latency-rtsp-2026-10-08.md).
Both live tiles use `OMX.amlogic.avc.decoder.awesome`. The existing player
drains decoded output independently, releases it immediately to its surface,
and has timestamp stabilization disabled. No periodic catch-up, compressed
frame discard, seek, or playback-speed adjustment was added for this investigation.

On first connecting, `.85` was at the Amazon launcher and had no bridge RTSP
consumers. Eufy Wall was launched for its baseline. Later, at approximately
11:00:33 EDT, it returned to the launcher and both RTSP connections closed.
That lifecycle interval is excluded from ongoing playback measurements.

## Matched Android frames before bridge instrumentation

Enable the existing `EufyFrameTiming` debug tag. Match each camera's received,
decoded, and rendered events by its exact RTP presentation timestamp. Receive
and decoder-release events record `System.nanoTime()`; rendered events record
the codec's actual render timestamp, rather than when its main-thread callback
was delivered. The approximately 20-second windows are sequential across TVs.

| TV | Camera | Matched receive→render frames | Median | p95 | Maximum |
|---|---|---:|---:|---:|---:|
| .58 | Front Door | 305 | 58.8 ms | 240.6 ms | 525.5 ms |
| .58 | Garage | 294 | 68.8 ms | 171.2 ms | 459.5 ms |
| .85 | Front Door | 260 | 57.3 ms | 243.0 ms | 2,102.3 ms |
| .85 | Garage | 289 | 61.5 ms | 157.1 ms | 1,892.0 ms |

Receive→decoded-release medians are 5.7–5.8 ms on both TVs. The `.58` live
diagnostics repeatedly report compressed queue depth zero, with most
received-versus-rendered media timestamp distances around 100–500 ms. These
samples do not show a persistent ten-second backlog inside the Android player.
The longer `.85` outliers and irregular input still require attention; the
medians do not prove uninterrupted smooth playback.

## Upstream investigation

A separate capture compares Front Door's raw HTTP Annex-B feed with its
production transcoded RTSP output. Matching encoded camera timestamps are
visible in near-simultaneous images, within approximately 400 ms in sampled
pairs. The reported large Front Door/Garage timestamp difference is present
in the raw feed as well. Raw-versus-transcoded timing therefore does not
support a ten-second delay introduced by this transcoder in the sampled path.

Temporary, bounded instrumentation of the **deployed** SDK bundle records
datagram receipt, frame reassembly, accepted video frames, and access-unit
emission. This required bridge restarts at 14:59:31 and 15:01:00 UTC, so the
later samples cannot prove the exact state of a pre-restart queue. In the
first approximately 100 seconds after the second restart:

- Front Door: 60 emission samples, median 1 ms/max 5 ms from accepted frame
  to access-unit emission; 88 sampled datagram batches, maximum age 19 ms.
- Garage: 63 emission samples, median 1 ms/max 6 ms; 88 sampled datagram
  batches, maximum age 30 ms.
- No reorder-hole abandonment occurred in this window; sampled reorder
  queues were empty. Early point samples of UDP receive queues and kernel
  socket drop counts were zero; those point samples did not establish the
  absence of later burst loss.

Front Door and Garage use separate source sessions, channels 2 and 1,
respectively. The SDK's embedded video timestamp is epoch milliseconds
modulo 2^32. Its clock differs from the timestamp burned into the picture:
Front Door's header is about 0.56 seconds ahead of server wall time while
its encoded image timestamp is about 12–13 seconds behind. Garage's header
is about 3.25 seconds behind server wall time, while its image timestamp is
about 4–5 seconds behind. Neither clock alone establishes capture latency.

A same-camera comparison with the Eufy phone app and a timed physical event
is needed to distinguish clock differences from actual stale camera frames.
Restarting did not eliminate the encoded inter-camera timestamp difference.

The original SDK bundle was restored exactly, with SHA-256
`5ae12418fb1a7048bdff04c55d44d647009dcd57274897f990bf63453023f193`.
The production bridge restarted at 15:05:42 UTC with PID 16322. The temporary
instrumentation is not a production change.

## Independent receiver-backlog bound

Two packet-only RTSP connections on the Mac record the production producer's
RTP timestamps and wall-clock arrival. Match those exact timestamps with
`.58`'s Android receive callback. Eight ADB nanosecond `date` queries map
the TV clock to the Mac clock; the shortest round trip is 66.953 ms, which
leaves approximately ±33.5 ms uncertainty in the midpoint mapping.

In a 15-second window, 224 Front Door and 226 Garage frames match. The TV's
callback precedes the reference Mac arrival by approximately 26 ms at the
median for both cameras. The maximum TV-minus-Mac difference is −14.2 ms
for Front Door and −9.3 ms for Garage, before accounting for clock-map
uncertainty. This independently excludes a ten-second TCP/socket/parser
backlog on `.58` in this window. It does not establish long-run behavior.
The planned long-run latency capture was stopped when the user corrected
the report.

## Android choppiness observations

On `.58`, the same 15-second capture has approximately 15 frames/s per
camera. Largest receive, decoded-release, and actual-render gaps are:

| Camera | Receive | Decode release | Actual render |
|---|---:|---:|---:|
| Front Door | 535.5 ms | 535.5 ms | 533.4 ms |
| Garage | 658.7 ms | 658.7 ms | 666.9 ms |

The p95 gaps are approximately 183 ms for Front Door and 150 ms for Garage
through all three stages. In `.85`'s earlier baseline, Front Door's largest
2,087 ms receive gap is followed by a 2,087 ms decode-release gap and a
2,083 ms render gap. The pauses already exist at compressed frame reception;
the independent decoder drain is not adding a wait for another input frame.
The next useful distinction is irregular upstream emission versus network
delivery, together with any additional Linux display stutter. No Android
buffering or scheduling change was made without that evidence.

The user explicitly requires **no added playout latency**. A proposal to
preserve source timestamps and add a presentation margin was stopped before
deployment. Android still releases decoded output immediately. No new wait,
frame-rate stabilizer, periodic restart, or catch-up loop was installed.
The SDK metadata candidate was prepared separately; the deployed SDK still
omits the source timestamp from its public video frame. Timestamped bridge
transport and paced client output are not deployed features from this work.

## Per-frame source cadence, without restarting the bridge

A subsequent 60-second diagnostic temporarily wraps the running SDK's
video callbacks through a localhost-only Node inspector connection. It
records every source header, emitted access unit, UDP callback, callback
duration, and event-loop delay in a bounded in-memory ring. The 30,000-row
limit retains the first approximately 53.6 seconds of the window beginning
15:09:49.856 UTC. The original functions were restored and inspector closed;
the bridge retained PID 16322 throughout.

| Camera | Emitted units | Source timestamp step, median / max | Largest AU delivery gap | UDP callback gap at that pause |
|---|---:|---:|---:|---:|
| Front Door | 805 | 67 / 77 ms | 1,719 ms | 1,670 ms |
| Garage | 805 | 67 / 74 ms | 1,247 ms | 1,196 ms |

During Front Door's 15:10:20.558→15:10:22.277 pause, the next source frame's
timestamp advances only 66 ms. Garage's corresponding next source frame
advances 67 ms. Their source frame clocks remain regular while delivery
stops and later resumes in bursts. The event-loop maximum is 156.5 ms
(p99 54.2 ms), and the longest measured video callback is 55.6 ms for Front
Door and 38 ms for Garage. These do not explain the approximately one-to-two
second ingress gaps. The irregularity is present before SDK UDP callbacks.

The bridge currently serves Annex-B bytes without the source frame clock,
and the FFmpeg input substitutes wall-clock timestamps. Consequently
transport arrival jitter becomes irregular RTSP media timing. Correctly
preserving the source timestamps is a distinct issue from inventing a
constant frame rate or periodically restarting a late player. Any intended
playout change must also keep buffering bounded: preserving timestamps
cannot produce frames during a genuine ingress pause.

The exact bounded ring and metadata are retained in
`.evidence/latency-root-2026-10-09/au-cadence-inspector.json`.

A concurrent 45-second kernel-socket capture subsequently records receive
queue peaks of 361,984 and 360,384 bytes, with 36 and 8 socket drops on two
video ports. **These drops occur at 15:10:51.259 UTC, after the inspector
window closes at 15:10:49.870, during debugger cleanup.** The simultaneous
queue rise across all three sessions may be caused by the diagnostic
disconnect itself. There are no socket drops during the measured Front
Door 1.7-second gap at 15:10:20–22. This drop window cannot establish ordinary
production loss or its relationship to that pause. A passive capture with
no inspector is required.

The subsequent uninterrupted 60-second passive capture records **zero new
UDP socket drops**, with queue peaks of 229,440 bytes for Garage, 217,728
for Front Door, and 64,768 for Balcony. The kernel ceiling remains a real
limit, but a production loss fix has not been established by this window.

## Passive wire and HomeBase A/B observations

A diagnostic packet socket captures only the active SDK UDP ports with a
kernel filter and 96-byte snapshots. `SO_TIMESTAMPNS` supplies packet times
independently of the Python reader's scheduling. The diagnostic socket uses
its own 16 MiB ring; this does not change the production sockets or host
receive-buffer ceiling. Earlier diagnostic runs with capture-ring losses
are excluded. The final capture records 28,918 packets with **zero capture
drops**.

Front Door and Garage both receive from HomeBase `192.168.23.233` on separate
port pairs; Balcony receives from `192.168.23.89`. A Front Door pause spans
890.7 ms between consecutive transport sequences 14950 and 14951. The
preceding packet's ACK leaves 7.2 ms after receipt, approximately 883 ms
before the next packet. Another 400 ms pause follows an ACK sent in 0.95 ms.
Garage's largest 439.8 ms pause follows an ACK sent in 0.87 ms. Balcony's
largest ingress gap in this window is 72.6 ms. These examples do not show
the sender waiting for the last packet's ACK during the observed silence.
They do not identify an unobserved sender-side queue or network loss.

Three sequential, 40-second captures compare the ordinary bridge with
Garage temporarily disabled and then with the exact original configuration
restored. Every diagnostic capture has zero packet-socket drops.

| Bridge phase | Front Door source-header arrival p95 | Maximum | Source-clock step p95 |
|---|---:|---:|---:|
| Door + Garage, before | 218 ms | 486 ms | 71 ms |
| Door only | 241 ms | 826 ms | 71 ms |
| Door + Garage, restored | 212 ms | 1,190 ms | 71 ms |

Disabling Garage does not eliminate Front Door's irregular delivery in
these windows. This does not support changing the SDK's camera-session
architecture as a repair. The phases include bridge restarts and changing
scenes; whether the phone's live view remained open throughout is not
confirmed. “Door only” describes the bridge configuration, not proof that
it was the only HomeBase client. The original configuration was restored
byte-for-byte, SHA-256
`ce3b70bc726ae641cf89efdab8f598708e88d28ce7762bb9e372869ead6a7c3b`.

The effective receive buffer is only 425,984 bytes despite the
SDK requesting 4 MiB, because the host's kernel receive-buffer ceiling
clamps the request. The passive capture above does not establish it as the cause of ordinary
production pauses; no receive-buffer configuration change was deployed. The
zero-queue point samples above must not be used to claim there was no burst
pressure or kernel loss.

## Hardware decoder frame holds: measured repair

Four sequential live Door side-transcoder runs retained the production
VA-API decode/scale/encode path and emitted 90 H.264 frames each, all with
exit status zero. Per-process i915 video-engine counters advanced, confirming
GPU work. `-debug_ts` stage records were timestamped as they arrived and
matched by packet/frame PTS. The tested input-only change is `-threads:v 1`
**before `-i`**, in the full GPU attempt. The software fallback is unchanged.
There is no new buffer, scheduling margin, or waiting policy.

For the steady subset, the first 15 and last six input packets are excluded.
Some records cannot be paired because printed timestamp precision rounds
differently; the matched counts are recorded in the private JSON summary.

| Full GPU variant | Input to decoded median / p95 | Input to encoded median / p95 | Minimum / median later input packets received before encoded output |
|---|---:|---:|---:|
| Production arguments | 263 / 472 ms | 291 / 585 ms | 4 / 4 |
| Input `-threads:v 1` | 1.95 / 31.4 ms | 26.6 / 372 ms | 0 / 0 |

This identifies an additional decoder frame hold in the existing FFmpeg
hardware path: the baseline needs at least four later input packets before
output, while the changed path can deliver without a later packet. The
change removes that hold instead of concealing source stalls with more
playout latency. These are stage timings, not a glass-to-glass measurement.

An encoder `-async_depth 1` variant was also tested. It did not independently
improve encoder submission-to-output timing: medians were 19.5 ms for the
baseline and 20.7 ms for that variant. It is not included in the repair.
The sequential live windows have different upstream jitter; their output
cadence percentiles do not prove smoothing or elimination of the previously
observed HomeBase delivery pauses. All four variants produced 90 frames;
this alone is not a claim that every source frame was delivered.

The exact commands and wall-timestamped stage records are in
`ffmpeg-probe.py` and `ffmpeg-low-delay-probes.jsonl`. Calculated stage and
cadence summaries are in `ffmpeg-stage-summary.json` and
`ffmpeg-output-cadence.json` under the private evidence directory below.

### Intermediate production verification with one decoder thread

The deployed wrapper SHA-256 is
`c5533e9ceff45ed7ed1f3f9c509b40d0757fa40e57925c1b0bd817c15f9d027b`.
The SDK bundle remains unchanged at
`5ae12418fb1a7048bdff04c55d44d647009dcd57274897f990bf63453023f193`.
The phone live view is closed for this postdeployment capture, as confirmed
by the user; that does not establish its state during the earlier A/B test.

Both production FFmpeg processes use `-threads:v 1` before the input, VA-API
decode, `scale_vaapi`, and `h264_vaapi`. Their i915 video-engine counters
advance by 263 and 330 ms over a three-second interval. A 30-second RTSP
capture receives 452 Door and 451 Garage frames, approximately 15 fps each;
media-time/wall-time ratios are 1.0008 and 1.0011. This confirms sustained
production output using hardware after the change, without a software
fallback in this interval.

| Camera, Fire TV .58 | Receive to decoded median / p95 | Receive to rendered median / p95 | RTSP maximum arrival gap | TV receive / render maximum gap |
|---|---:|---:|---:|---:|
| Front Door | 5.63 / 7.20 ms | 59.8 / 183 ms | 1,949 ms | 1,952 / 1,967 ms |
| Garage | 5.67 / 7.40 ms | 64.1 / 146 ms | 811 ms | 811 / 817 ms |

The hardware decoder hold has been removed, but these continuing pauses
are already present at RTSP receipt and propagate to the display. This
capture therefore **does not establish that all choppiness is repaired**.
No added playout buffer was deployed. The Android APK and player remain
unchanged, and diagnostic timing logging was restored to INFO afterward.
`postdeploy-rtp.jsonl`, `postdeploy-tv58.log`, `postdeploy-gpu.jsonl`, and
`postdeploy-summary.json` retain the underlying measurements.

### Narrower repair: keep slice parallelism

The single-thread input test establishes the frame-hold mechanism, but the
repair can target frame threading specifically. FFmpeg's
[codec threading documentation](https://ffmpeg.org/ffmpeg-codecs.html#Codec-Options)
states that frame threading adds a frame of decoding delay per thread and
that slice threading processes parts of one frame concurrently. The narrower
input option is `-thread_type:v slice` before `-i`, retaining the automatic
thread count. Actual parallel work still depends on codec/stream support.

A second live Door comparison emits 90 frames per variant with zero exit
status and active i915 GPU counters. Slice-only input-to-decoded median/p95
is 1.82/18.4 ms, compared with 2.50/24.8 ms for one decoder thread. Both can
emit output without any later input packet. A live Garage **HEVC** slice-only
check also emits 90 H.264 frames, exit zero: input-to-decoded median/p95 is
2.52/20.9 ms, input-to-encoded 30.9/128 ms, and minimum/median later input
packets before encoded output are both zero. This tests both current input
codecs, rather than assuming the Door H.264 result applies to Garage.

A same-recorded-input replay provides a separate comparison without changing
live scenes. The captured Door input contains I/P pictures and no B pictures.
All three variants deliver the requested 60 frames and exit zero. Pacing uses
`-re`; wall-clock stamping is omitted **only for this file replay** to avoid
feedback between reader pacing and arrival-generated timestamps. An earlier
replay retaining that combination timed out and is excluded.

| Same input, 60-frame replay | Input to decoded median | Input to encoded median | Process CPU seconds / elapsed seconds |
|---|---:|---:|---:|
| Automatic frame + slice threading | 333 ms | 353 ms | 2.54 / 5.21 |
| One decoder thread | 67.5 ms | 87.6 ms | 2.99 / 5.65 |
| Slice threading, automatic count | 67.9 ms | 87.2 ms | 2.51 / 5.04 |

The file demuxer emits NOPTS, so this comparison matches packet/frame order
for the known I/P-only fixture; decoder-generated PTS advance sequentially.
The first 15 and last six output frames are excluded from timing. The one
remaining frame in both reduced modes belongs to this parser/replay path;
the baseline holds four additional later packets. These short runs establish
removal of that extra hold and sustained output, not a general CPU benchmark
or a promise to hide source outages. The full GPU attempt is the scope of the
change; mixed/software fallback decoding keeps its prior parallelism.

### Final slice-only production verification

The final wrapper replaces the intermediate input `-threads:v 1` with
`-thread_type:v slice`, retaining automatic thread count in the full GPU
attempt. Wrapper SHA-256 is
`3f227e6e0df162ee8073b19b64a6b85007f995fd463ea5edf2dd76199ad112f1`;
the SDK bundle remains unchanged. No Android APK update, encoder async-depth
change, playout buffer, or software fallback thread restriction is included.

Startup was **not clean**: Garage's raw SDK feed delivered no bytes for 31
seconds and then reported an awaiting-first-frame warm timeout; Balcony's raw
feed also ended and reopened. Garage's RTSP producer timed out during this
source outage. The first diagnostic DESCRIBE request timed out and is retained
as a failed capture, not presented as a successful production check. These
raw-source events occur before the transcoder and do not establish a slice
threading regression.

After raw feeds resumed, a subsequent 20-second capture receives 302 Door
and 300 Garage RTP frames with no additional bridge log error in that window.
Both current production processes use VA-API decode/scale/H.264 encoding
and slice-only input; their i915 video-engine counters advance by 367 and
394 ms over three seconds. This verifies output on both input codecs after
warm-up. Garage remains bursty: in the subset after the first three seconds,
287 frames span 16.61 seconds, while RTP media time spans 17.96 seconds.
This is not proof of uniform pacing or elimination of all source jitter.
The observed raw startup outage and remaining incoming pauses still require
separate transport investigation; no concealment by additional waiting was
introduced. Evidence is in `slice-warm-rtp.jsonl`, `slice-warm-gpu.jsonl`, and
the initial failed `slice-postdeploy-rtp.jsonl` capture.

### Historical first-party wire cadence

The existing authenticated phone capture `eufy-hb3.pcap` is rechecked against
SHA-256 `5290131e60869fa2950045854afd520cf6ac328a1941995648ee91448305c344`.
Within the documented active start window, 2.168–12.921 seconds after the
September 19 start, unique video-unit header starts have arrival gaps of
41.6 ms median, 82.3 ms p95, and 540.5 ms maximum. Source timestamp steps are
67 ms median and 76 ms maximum. The longest arrival gap is between unit
identities 5352 and 5353 while source time advances only 67 ms.

This shows that the archived first-party wire delivery is also irregular;
it does not measure the phone renderer, prove a phone buffering policy, or
identify the current 1–2 second pauses. This is historical header-only evidence,
with a different scene/quality and possible additional packed/fragmented
headers. Retransmitted headers are deduplicated by timestamp and unit identity;
the subsequent stop/restart gap is excluded. It must not substitute for a
current paired comparison. Results are retained in
`first-party-header-cadence.json` and `first-party-header-starts.json`.

## Private reproducible evidence

The installed APKs, raw Android timing logs, calculated summaries, and hashes
are preserved in `.evidence/firetv-latency-2026-10-09/`. SDK logs, raw/RTSP
image captures, and their capture times are preserved separately in
`.evidence/latency-root-2026-10-09/`. These directories are ignored by Git.

```sh
adb -s 192.168.23.58:5555 shell setprop log.tag.EufyFrameTiming DEBUG
adb -s 192.168.23.58:5555 logcat -v threadtime -s EufyFrameTiming EufyWallTV
# Restore the normal logging level after capture.
adb -s 192.168.23.58:5555 shell setprop log.tag.EufyFrameTiming INFO
```

Match timestamps only within a camera connection; exclude startup,
disconnection, and timestamp reuse across a reconnect. The raw/header/OSD
clock distinction must remain in any upstream PR or handoff.
