# Source timing lost at raw HTTP egress — 2026-10-11

## Finding and scope

The camera's SDK video events carried substantially more regular frame timing than
the outgoing RTP stream in a simultaneous live window. The bridge opened the SDK
Readable in byte mode, transported Annex-B video over HTTP without timestamps,
and instructed FFmpeg to timestamp that input by arrival time. Its outgoing RTP
PTS therefore retained delivery jitter rather than the camera's original timing.

This identifies a timing-metadata loss in the bridge. It does not establish the
cause of upstream packet pauses, physical-display brightness flicker, or absolute
camera-to-screen latency. The timestamp-preserving muxer and production outgoing
timing are verified below; actual client presentation remains a separate gate.

## Passive live observation

Capture: **2026-10-11 04:52:47.278–04:57:47.280 UTC**, 300.002 seconds.
The existing bridge process, PID **616**, stayed running throughout.

The observer enabled Node's loopback-only inspector using `SIGUSR1`, reached it
through an SSH localhost forward, and temporarily wrapped
`LiveStream.prototype.emit`. For existing `video` events it retained only monotonic
arrival time, wall time, source/channel identity, source timestamp, size, codec,
dimensions and keyframe flag. It immediately invoked the original emitter with
the original arguments. It added no consumer and made no live-start, mode,
quality or camera-setting request. No video bytes were changed or retained.

The wrapper restored the original property descriptor in `finally`; a separate
330-second timer also guarded cleanup. Capture finished normally, removed the
temporary global cleanup handle, closed the inspector, and terminated the SSH
forward. The process PID was unchanged afterward. Both tested cameras remained
streaming, with their pre-existing stall counters unchanged at one. The independent
Balcony stream changed source-session identity during the window; its two groups
are retained in the private capture but are not used to claim continuous playback.

The capture retained **15,905** video events across all existing live sources with
zero observer overflow. Front Door is source channel 2; Garage is channel 1.

| Full five-minute capture | Garage | Front Door |
| --- | ---: | ---: |
| Video events | 4,501 | 4,500 |
| Source timestamp increment median / P95 / P99, ms | 67 / 68 / 70 | 67 / 71 / 73 |
| Largest source increment, ms | 79 | 77 |
| Local arrival increment median / P95 / P99, ms | 61.191 / 121.814 / 151.873 | 60.446 / 123.789 / 262.927 |
| Largest local arrival gap, ms | 370.964 | 1,654.864 |
| Source progression / local monotonic progression | 1.000096 | 1.000113 |
| Missing, zero, negative or wrapped source steps | 0 | 0 |

The largest Front Door arrival gap advanced the source timestamp by only **65 ms**.
Later source frames caught up. Thus delivery gaps were not uniformly capture gaps
in this observed window. Source timestamps are uint32 milliseconds; their absolute
clock offset is not a physical capture-time measurement.

## Concurrent outgoing stream

A Fire TV receive trace, PID **12730**, overlaps approximately
04:54:45.57–04:57:47.25 UTC. Rows were selected using log wall time, and source
events were cropped to each camera's corresponding receive window. This is a
same-time comparison, **not exact coded-picture/hash matching**. The source and
TV clocks were not independently calibrated for absolute latency.

| Overlapping window | Garage | Front Door |
| --- | ---: | ---: |
| Outgoing RTP frames / source events | 2,726 / 2,725 | 2,726 / 2,726 |
| Source increment median / P95 / P99, ms | 67 / 68 / 70 | 67 / 71 / 73 |
| Outgoing RTP PTS increment median / P95 / P99, ms | 61.167 / 123.112 / 158.445 | 60.200 / 124.912 / 279.178 |
| Largest source timestamp increment, ms | 79 | 77 |
| Largest outgoing RTP PTS increment, ms | 362.889 | 1,665.700 |

The outgoing timing closely reflects irregular arrival, while source increments
remain regular. Increasing a client pacing allowance does not recover metadata
that was already discarded upstream.

## Code path and compatibility

`server/src/sdk-adapter.mjs` previously called `cam.openReadable()` without
`objectMode`. The installed SDK's `readable-egress` implementation emits
`frame.data` in that mode, or the full frame—including `sourceTimestampMs`—with
`objectMode: true`. `server/src/stream-manager.mjs` then wrote bare video bytes
to HTTP. `server/src/go2rtc.mjs` explicitly used
`-use_wallclock_as_timestamps 1`, `-fps_mode:v passthrough` and
`-enc_time_base:v 1:90000`.

The earlier [clock correction](live-timing-2026-10-08.md) remains justified:
nominal-rate synthesis advanced Garage's media clock by 18.200 seconds during
11.879 seconds of delivery. Reverting to nominal 15 FPS synthesis would reintroduce
that measured backlog fault. Preserving actual source timestamps is a different
correction; it must retain discontinuity and reconnect handling.

Primary go2rtc 1.9.14 sources show that its
[HTTP producer](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/internal/http/http.go)
uses [format detection](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/magic/producer.go)
to recognize MPEG-TS. Its
[MPEG-TS producer](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/mpegts/producer.go)
supports H.264 and HEVC, and its
[demuxer](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/mpegts/demuxer.go)
uses PES timestamps directly for 90 kHz video RTP timestamps.

An important latency limitation: that demuxer emits finite-length PES immediately
when complete, but holds length-zero PES until the next PES begins. MPEG-TS video
PES must use length zero above the 16-bit length limit. The new muxer uses a finite
length for access units up to **65,527 bytes**, but oversized access units can
therefore add next-unit waiting in the direct go2rtc demux path. This must be
measured on the actual transcoding path; it is not hidden by a zero-buffer claim.

## Muxer verification

`server/src/video-mpegts.mjs` emits each input access unit synchronously, with
PAT/PMT at startup/keyframes, correct MPEG-2 section CRC, transport continuity,
PES PTS, and a PCR on each unit. It preserves millisecond source increments as
exact 90 kHz increments and handles normal uint32 source wrap. It never adds a
frame queue, sleeps, changes coded pictures, or synthesizes a nominal frame rate.

Negative source steps and an optional caller-defined maximum positive step throw
`TimestampDiscontinuityError` before changing clock or packet-counter state.
The caller chooses whether to reconnect; reset is explicit rather than silently
turning a source-clock discontinuity into a fabricated timeline.

On the Mac, **FFmpeg 9.0.1** generated 30 real H.264 pictures at 15 FPS without B
frames. The test supplied explicit source timestamps including an additional
**217 ms** gap after frame ten. `ffprobe` recovered all 30 exact expected PTS
values. One unit included a valid 100 KB filler-data NAL, exercising oversized PES
without changing its coded picture. FFmpeg decoded the MPEG-TS output and the original Annex-B fixture into
identical per-frame MD5 lists, proving that the timestamp container did not alter
their pictures. A separate six-frame real HEVC fixture with an explicit 100 ms
step also retained all expected PTS and identical decoded-picture hashes.
Both fixtures now exercise whole-unit and finite-fragment PES, including a valid
100 KB HEVC filler-data NAL. Six muxer tests passed, none skipped:

```sh
node --test server/test/video-mpegts.test.mjs
```

The other tests independently verify section CRC, PCR, byte-complete 100 KB
payload, continuity-counter wrap, H.264/HEVC stream types, per-consumer startup
tables, source wrap and rejected discontinuities.

## Live production integration

The bridge was restarted with timestamp-preserving transport at **05:03:44 UTC**,
PID **9496**. The deployment's rollback directory is
`/opt/eufy-wall-bridge/frame-clock-backup-20261011`. Existing raw Annex-B access
remains available; go2rtc's producer URLs request `?format=mpegts`. Runtime FFmpeg
PIDs **9541 / 9542** used MPEG-TS input, VA-API decode/scale/H.264 encode,
`-fps_mode:v passthrough` and `-enc_time_base:v 1:90000`, without the old wall-clock
input timestamp override. This retained hardware processing and the existing
picture dimensions. No SDK source change was required.

A concurrent HTTP-MPEG-TS/RTSP observer ran
**05:09:26.595–05:10:58.404 UTC**, approximately 90 seconds per connection. Initial
three seconds were omitted from clock statistics. It opened two additional HTTP
and two RTSP consumers of the already-running camera sources, without creating
another decoder/encoder producer.

| After timestamp-preserving transport | Garage | Front Door |
| --- | ---: | ---: |
| RTSP frames, whole observation | 1,349 | 1,350 |
| Stable RTSP timestamps | 1,304 | 1,303 |
| RTSP PTS increment median / P95 / P99 / max, ms | 67 / 68 / 70 / 74 | 67 / 71 / 73 / 76 |
| HTTP-MPEG-TS PTS increment median / P95 / P99 / max, ms | 67 / 68 / 70 / 74 | 67 / 71 / 73 / 76 |
| RTSP arrival increment median / P95 / P99 / max, ms | 60.489 / 136.201 / 184.453 / 365.291 | 57.433 / 137.489 / 308.501 / 528.426 |
| RTSP media/wall ratio | 0.9996 | 0.9982 |
| RTSP arrival rate, fps | 14.99 | 14.97 |
| RTSP timestamp regressions | 0 | 0 |
| HTTP source PES units, finite / oversized | 1,352 / 0 | 1,305 / 46 |

The outgoing RTP clock now has the same interval distribution as source MPEG-TS,
while actual delivery still arrives in bursts. That is the intended correction:
clients receive original timing with which to pace their pictures. The near-one
clock-rate ratio establishes no growing synthetic-clock backlog in this bounded
observation; it does not measure a constant physical delay.

Source and encoder-consumer clocks have different initial offsets. Matching exact
sequences of 12 consecutive timestamp increments found a single constant offset
supported by **1,290** Front Door windows / **1,291** Garage windows. Within the
common timeline, all **1,303** Front Door and **1,304** Garage source timestamps
appeared in RTSP, with **zero missing or extra timestamps**. The reproducer is
`match-post-transport.py`. This checks timestamp-sequence delivery, not equality
of pictures across the transcoder; its generated-fixture picture check is separate.

The first attempted 180-second observer failed because the existing RTSP clock
script sent no session keepalive. go2rtc's
[RTSP connection implementation](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/rtsp/conn.go)
advertises a 60-second timeout and applies that read deadline to passive consumers.
Media received by the observer does not renew the server's incoming read deadline.
The corrected `measure-rtsp-clock.py` sends OPTIONS every 20 seconds and parses
responses interleaved with RTP. Both 90-second connections received all **four**
keepalive responses. A parser test exercises every fragmented-header/body and RTP
boundary, bodies beginning with `$`, mixed control/media, rejected status responses
and source clock wrap. The failed initial collector discarded its result objects;
no usable 180-second measurement or playback failure is claimed from that attempt.

Near six minutes after deployment, process lifetime CPU readings were Node **38.7%**
and FFmpeg **19.1% / 19.2%**; these `ps` values are percentages of one CPU, not total
host utilization. Available memory was approximately **938 MB**. The process and
both encoder PIDs were unchanged. Service/kernel journal inspection showed no new
decoder error or GPU hang in the inspected interval. The independent Balcony source
reopened once; that is retained rather than describing every source as uninterrupted.

### Oversized PES limitation measured

A separate 20-second metadata observation retained each unit's last received TS
packet and the next PES start. Front Door supplied **10** fully observed oversized
units, approximately **145.6–146.8 KB**. Last-packet-to-next-PES waits were median
**13.896 ms**, P95 **178.453 ms**, maximum **221.427 ms**. Garage supplied **two**
oversized units, **74.7 KB / 214.0 KB**, with corresponding waits **7.114 / 37.622 ms**.
The final unit was excluded because its next boundary was not observed.

These are observer receive intervals, **not measured FFmpeg queue residence**.
They quantify how long a demuxer that waits for the next PES boundary could hold
an otherwise complete oversized unit. Actual FFmpeg residence and physical display
latency are not established by them. The 90-second timestamp match includes every
Front Door timestamp across its common span despite oversized units, so no timestamp
loss occurred there. The finite-PES refinement below corrects the demonstrated
extra boundary wait for the FFmpeg-facing transport.

### Controlled boundary-delay comparison and finite-PES correction

Review of primary FFmpeg **7.1.5** source showed that
[the MPEG-TS muxer](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavformat/mpegtsenc.c)
normally prefixes H.264/HEVC access-unit delimiters (AUD), but
[the demuxer](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavformat/mpegts.c)
selects full elementary-stream parsing. Its finite PES completion emits promptly;
length-zero PES emits when the next PES begins.
[The generic parser path](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavformat/demux.c)
does not treat the video's PES alignment flag as a complete-frame declaration.
Missing AUD was therefore not accepted as the cause without a timed comparison.

A local paced HTTP fixture supplied the same 90 unique H.264 pictures at nominal
15 FPS. Picture 30 included a valid 100 KB filler NAL. Input decoder slice mode
and one thread, plus output rawvideo encoder one thread, removed incidental frame
threading from the timing observer. The observer matched decoded-picture hashes
to source sends. Every mode delivered each of the 90 pictures once and in order.

| Implemented-API controlled comparison, FFmpeg 9.0.1 | Median / P95 hold, ms | Picture 29 hold, ms | Maximum hold, ms |
| --- | ---: | ---: | ---: |
| Legacy raw Annex-B, no AUD | 67.078 / 68.121 | 67.964 | 68.337 |
| Whole-unit MPEG-TS, no AUD | 67.110 / 68.028 | 135.842 | 135.842 |
| Whole-unit MPEG-TS with AUD | 67.101 / 68.187 | 135.118 | 135.118 |
| Finite-fragment MPEG-TS, no AUD | 67.098 / 68.192 | 69.132 | 78.572 |

The extra frame occurred **before** the oversized picture: the elementary parser
could not see picture 30's start until its length-zero PES was released by picture
31. Adding AUD did not remove it. Finite PES fragments removed that extra full-frame
hold, retaining the approximately one-frame parser wait already present on raw
Annex-B. The fragmented run's 78.572 ms maximum was picture 31 and is retained
rather than claiming identical worst-case scheduling across separate fixture runs.
These are local HTTP-send-to-software-decoded-output measurements, not physical
camera/display measurements. Each HTTP fixture ended with an FFmpeg input I/O
warning; exit status was zero and all 90 decoded pictures were recovered in order.

`newVideoMpegTsMuxer({ maxPesPayloadBytes: 63000, codec })` now splits oversized
units into finite PES fragments synchronously. Every fragment retains the original
unit's PTS; only the first fragment sets data alignment. No fake delimiter/frame,
new wait, synthesized FPS, or change to coded pictures is introduced. One `push()`
still returns all bytes of that source unit immediately. Default zero retains
whole-unit PES for compatibility with direct go2rtc MPEG-TS demuxing, which treats
each PES as a unit and therefore **cannot** consume this fragmentation mode directly.
FFmpeg-facing HTTP uses `?format=mpegts&pes=fragmented`; direct compatibility remains
explicit. Copy mode must therefore also traverse FFmpeg with video stream copy,
preserving source timing without re-encoding.

The six muxer tests and RTSP observer parser test were copied to an isolated
`/tmp/eufy-ts-fragment-test.3KrwlS` on the Atom bridge and run with its installed
FFmpeg **7.1.5**. **7/7 passed, none skipped**, including oversized real H.264 and
HEVC fixtures in both modes, exact once-per-AU PTS, identical decoded-picture hashes,
finite fragment lengths/alignment and uint32 source wrap. The staging directory
was removed afterward; this test did not alter the running service or source files.
The following production gate verifies the finite-fragment transport separately
from the earlier whole-unit implementation. Actual client presentation is measured
in the client evidence, rather than inferred from packet timestamps.


### Final finite-fragment production gate

The final bridge deployment started at **2026-10-11 05:23:18 UTC**, PID **11104**,
with rollback files under
`/opt/eufy-wall-bridge/frame-clock-fragment-backup-20261011`. The parent agent's
native Atom server suite passed **123/123**. All internal go2rtc sources use
`?format=mpegts&pes=fragmented` through FFmpeg; copy sources add a remux process
with `-codec:v copy`, without decoding or encoding.

The concurrent source-HTTP and output-RTSP observation ran from **05:23:43.652 to
05:25:15.733 UTC**. Each RTSP observer received four successful keepalive replies.
No timestamp regression occurred in any of the four paths.

| Camera | Stable RTSP timestamps | RTP step median / P95 / P99 / max, ms | RTP arrival median / P95 / P99 / max, ms | RTP media/wall ratio |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 1,309 | 67 / 71 / 73 / 77 | 54.637 / 152.068 / 276.816 / 547.828 | 1.0054 |
| Garage | 1,306 | 67 / 68 / 70 / 77 | 61.642 / 132.636 / 190.980 / 639.220 | 1.0002 |

Matching exact 12-step sequences established constant source-to-RTP offsets with
1,296 agreeing windows for Front Door and 1,293 for Garage. Across their common
spans, **all 1,309 Front Door and 1,306 Garage source timestamps appeared in RTSP,
with zero missing or extra timestamps**. These are exact timing-sequence matches;
they do not establish picture hashes through the live transcoder or physical
camera-to-screen delay. Boundary arrival bursts explain the short-window Front
Door media/wall ratio; its PTS steps remain the source's measured steps.

The HTTP observer reconstructed 1,351 Front Door units from 1,441 finite PES packets
and 1,353 Garage units from 1,402 finite PES packets. **Zero length-zero PES packets**
were observed. This verifies that oversized live units use finite fragments,
addressing the specific extra demux boundary wait demonstrated by the fixture.
Network arrival pauses still occur; the transport preserves their camera timing
instead of encoding those pauses into the presentation clock.

A later read-only checkpoint found PID 11104 still active, both production FFmpeg
children using VA-API decode and `h264_vaapi` encode, slice input threading, and
source PTS passthrough with the 90 kHz encoder timebase. Lifetime process CPU was
19.4% and 19.0% of one logical CPU for Garage and Front Door respectively; this is
not a matched before/after CPU comparison. No Front Door or Garage reopen appeared
in the sampled journal. Balcony independently reopened at 05:25:35–05:25:41;
that unrelated source interruption is retained as a limitation of the checkpoint.

### Live HEVC copy integration

At **05:26:19 UTC**, a bounded isolated go2rtc 1.9.14 instance read the existing
Balcony camera's fragmented HTTP transport. Its temporary configuration used the
production `ewb_timestamped_http` input template and `#video=copy`, with separate
loopback RTSP/API listeners on ports 8566/1986. The FFmpeg child command explicitly
used `-c:v copy` and published RTSP to that isolated instance; it had no decoder,
filter, or video encoder configuration. FFprobe read **201 HEVC Main packets** over
eight seconds at the camera's then-native **1920×1080**, with exit status zero and
empty stderr. Production stream registration was unchanged.

An earlier direct FFmpeg copy probe saw native 1280×720 and completed 199 packets
in 8.08 seconds, but emitted a startup PTS-before-DTS warning to a null muxer. The
subsequent actual go2rtc RTSP integration was warning-free. The camera's resolution
changed between these probes, and FFprobe reported B-frame capability; no fixed
Balcony resolution or permanently error-free copy behavior is inferred. The
isolated process group was terminated in `finally`, its temporary directory removed,
and a final process query found no isolated copy process or listener remaining.
This proves live registration/remux compatibility, rather than hardware decode or
physical playback of that HEVC feed. Copy mode intentionally uses neither encoder
nor decoder.

## Private artifacts and reproduction

Private artifacts remain under `.evidence/frame-pacing-2026-10-11/`. They are
not committed because receive logs contain device identities. The bounded
inspector observer is `capture-source-clock.mjs`; analysis is
`analyze-source-clock.py`, which handles uint32 source differences using signed
modulo arithmetic. To reproduce, use the loopback inspector of an already-running
bridge and an SSH forward at local port 19231; run that observer for five minutes,
then the analyzer. No additional camera session is needed.

| Immutable artifact | SHA-256 |
| --- | --- |
| `isolated-copy.py` | `0a7b53a69e698c673be3cfcad1f8137f5f7893acde2af4b01768a24ffc611f87` |
| `isolated-copy.json` | `b574986543066f518572500ba670e3a2789e6f921a09236cd3b3a668fd8ebe1e` |
| `final-transport-clock.json` | `14bed6fd2f603054d20908d5e0eb9a0c79cad40b2d330344ddaff73857195779` |
| `final-transport-clock-summary.json` | `50ab225fac3116ec40c6456a536843e1a07b71bfaea85143bcb5c7cdcee8255e` |
| `final-transport-matched-timestamps.json` | `3962ccbd003492fa0055fd640af1181018ad25593e55df9f07f0c378a0bab4eb` |
| `probe-post-transport.py` | `13744d62b8929b2d413beda6da6800f1875e191c96fd37f3d2232519fd4b3ac3` |
| `match-final-transport.py` | `111cfe36f6fa1471d252419014485e3be5f6e41279cdfae528ecb09eb728602b` |
| `source-clock.json` | `7c324db89872d9f65983142bff2532152ee6823ab905174d725c993d68f6b7d4` |
| `source-clock-summary.json` | `897282a23eb5174b13e4b6f7cdfb663f67368a755c8f743c2a504273b50020a6` |
| `capture-source-clock.mjs` | `cb1c80963d343ad6e967d6043954dfc297614b83b999b08d347224bfcfdde750` |
| `analyze-source-clock.py` | `4c8b25a4e5555a50764fd5029e16ee672a6ed602079e85887eac3ec2d7b46392` |
| `post-transport-clock.json` | `3718926760bae6f08af6464fa450c9e7b7ad9a795264068c4c85a7cc808e99fb` |
| `post-transport-clock-summary.json` | `123c41afcae6bd4cef70cebf6198e98ce94eb7adf08991a29346cf049b574549` |
| `post-transport-matched-timestamps.json` | `74a6632eefc614ac099862714efb0a99c16b76329227887651070757aa74f8aa` |
| `oversize-transport-clock.json` | `ff399a89681568782c0c0f0ab5c138ea3d69f4ba432098e6474fbd0f7b0f1dd4` |
| `demux-hold.json` | `154375c9d8a4d4692dff4a3fbccbe0f3d4e9a4f686db4b9fa3d1a41d76372878` |
| `measure-demux-hold.mjs` | `d622e0a26bdc23b9db78413ec203ec58856af0fd719374ae936a32746df9b89a` |
| `source-overlap-85.log` | `f9b0a6dd088eb6cc23c310051287e219e6b1a6d55d80b7391ab1af5bd2617c2f` |
| `source-overlap-summary.json` | `ba66b156edba69ed5e1c345a1696be78c185c9abed14db2dbc253c21fa453579` |

`source-overlap-85.log` is the concurrent TV trace and
`source-overlap-summary.json` records its selected interval statistics. The collector
was stopped before the final hashes above were recorded.

The final live gate is reproducible with
`python3 .evidence/frame-pacing-2026-10-11/probe-post-transport.py 90 fragmented`,
then `match-final-transport.py`. The probe uses two HTTP consumers and two RTSP
consumers of the already-active camera feeds; it sends no camera mode or stream
start commands. The matcher checks that the observed RTP span does not cross its
uint32 wrap before applying its common-span comparison.
