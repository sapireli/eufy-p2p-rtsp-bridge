# Frame freshness: evidence and guarantee boundary

## Requirement

Displayed video must remain smooth and near live. A stream delivering old frames
continuously must not be mistaken for fresh video merely because packets keep
arriving. When the verified freshness deadline expires, the client must clear
the picture and report stale/unavailable rather than leave an old image visible.

The first implementation measures **bridge ingress to client presentation**.
It does not claim that SDK receipt time is camera sensor capture time.

## What the existing camera timestamp proves

The SDK's `sourceTimestamp` is the raw unsigned 32-bit value read at offset
`0x0e` of the P2P video-frame header. Source cadence has been measured against
wall time on the current cameras; a universal epoch or sensor-capture meaning
has not been established. The timestamp wraps and can reset across sessions.

The private 300-second observer underlying
[source-timestamp transport evidence](source-timestamp-transport-2026-10-11.md)
recorded SDK frame timestamps and their local receipt wall time. Comparing the
raw word to `receiptUnixMs modulo 2^32` gives:

| Camera | First receipt minus source word | Last receipt minus source word |
| --- | ---: | ---: |
| Balcony | +788 ms | +761 ms |
| Garage | +1,156 ms | +1,128 ms |
| Front Door | **−1,682 ms** | **−1,716 ms** |

The closeness supports a truncated epoch-millisecond interpretation for these
samples. It does not calibrate the camera clock or prove sensor capture time.
Front Door's negative apparent age demonstrates why blindly reconstructing an
absolute timestamp would create a false guarantee. The reproducible source
file is `.evidence/frame-pacing-2026-10-11/source-clock.json`; its recorded digest
is `7c324db89872d9f65983142bff2532152ee6823ab905174d725c993d68f6b7d4`.
These are historical measurements, not a new live capture on October 12.

### Burned-in clock versus the wire clock

Earlier [direct SDK keyframe evidence](live-timing-2026-10-08.md) saved complete
keyframes at SDK emission before HTTP/fanout. Each finite file was decoded
offline to avoid streaming-decoder probe or queue delay:

| Camera | SDK receipt UTC | Burned-in clock EDT | Receipt minus numeric wire timestamp |
| --- | --- | --- | ---: |
| Front Door | 22:27:34.100 | 18:27:27 | 79 ms |
| Garage | 22:27:37.765 | 18:27:33 | 3,734 ms |
| Front Door | 22:27:40.101 | 18:27:33 | 80 ms |
| Garage | 22:27:43.971 | 18:27:39 | 3,940 ms |
| Front Door | 22:27:46.340 | 18:27:39 | 319 ms |

The clocks gave **opposite apparent age ordering**: the Door picture's printed
clock appeared older while its wire timestamp was near receipt time. The
printed clock is real image content, but its numerical offset from another
camera is not automatically a measurement of relative sensor-capture age.
The discrepancy already existed before bridge HTTP, encoding, RTSP and clients.
No downstream timing change can prove which upstream clock is correct.

## Exact association through the bridge

The earlier source timing transport established exact source/output timestamp
sequence matches: 1,309 Door and 1,306 Garage timestamps across their measured
common spans, with no extra or missing timestamps. That verifies relative
timeline preservation, not equality of compressed pictures or absolute age.
Current MPEG-TS consumers start their relative timeline at zero; the absolute
source word's phase is not conveyed by that timeline alone.

The freshness path registers each source unit's identity and ingress
monotonic time before passing it to the encoder. The output packet must join
that record by its **exact preserved PTS**, not by arrival proximity or a latest
frame heartbeat. FFmpeg now receives one complete access unit per finite
Matroska SimpleBlock, with proper `avcC`/`hvcC` configuration and four-byte
length-prefixed NALs. The one-millisecond container timebase preserves the
validated source increments exactly. Only the encoder input changed: outgoing
final encoded packets now enter go2rtc in finite FLV video tags; external RTSP
remains unchanged. FFmpeg output uses `-copyts`, no B frames, passthrough frame
timing and `-enc_time_base:v 1:90000`. Its encoded packet data is joined with
`-stats_mux_pre_fmt '{n} {pts} {tb} {size}'` packet records. Every output packet
must have the expected sequential record, exact integral 90 kHz PTS and complete
recorded byte length; mismatches and queue-limit violations fail the stream.

A local actual FFmpeg fixture found an important configuration dependency:
`-enc_time_base:v 1:90000` by itself with the data muxer yielded PTS `0,1,2`
for a 15 fps source. Adding `-fps_mode:v passthrough` yielded the expected
`0,6000,12000`. The integration test pins this behavior and compares decoded
frame MD5s before and after stamping.

## Inline timing stamp

Inject the stamp **after final encoding**, immediately before the first VCL NAL
and after existing encoder prefix SEIs. A pre-encode SEI or MPEG-TS private
metadata PID cannot be assumed to survive FFmpeg transcoding. Placement matters:
the Android RTSP dependency retains the last prefix SEI before a picture.

The user-data-unregistered SEI carries exactly 48 payload bytes:

| Byte offset | Field |
| --- | --- |
| 0–15 | UUID `455746524553484e4553533030303031` |
| 16 | Version 1 |
| 17 | Scope 1: bridge ingress |
| 18–19 | Maximum age, uint16 big-endian milliseconds; default 500 |
| 20–27 | Server epoch identifier, 8 bytes |
| 28–31 | Raw source timestamp, uint32 big-endian |
| 32–39 | Bridge ingress monotonic microseconds, uint64 big-endian |
| 40–47 | Frame ID, uint64 big-endian |

H.264 uses SEI NAL type 6; HEVC uses prefix SEI type 39. Standard RBSP emulation
prevention protects zeros and start-code-like payload bytes. Original encoder
NAL bytes remain unchanged. The server epoch prevents a restarted bridge from
reusing an old clock/identity mapping.

Client/server monotonic clocks have independent origins. The client therefore
needs a bounded clock-offset estimate, obtained from timed round trips, and
must include offset uncertainty in its conservative deadline. The client must
retain the metadata with the **same decoded picture** through actual output.
Malformed, missing, unmatched or expired metadata must not confer freshness.
Checking only the newest received stamp would incorrectly bless an older
picture still held by the decoder or display.

## Capture-clock validation tool

`server/scripts/capture-validation.mjs` uses an explicitly supplied, already
enabled loopback Node inspector (an SSH forward can provide access). It does
not enable an inspector, start a camera or alter camera settings. It temporarily
observes existing SDK video emission, restores its hook on completion and has
a remote cleanup guard if the diagnostic disconnects.

The bounded observer saves finite independently-decodable H.264/HEVC keyframes,
raw source timestamps, SDK-emission receipt times and anonymized stream labels.
H.264 requires SPS/PPS plus IDR; HEVC requires VPS/SPS/PPS plus IRAP type 19–21.
It saves at most 16 MiB of image bytes. Output is private camera material and is
stored with owner-only directory/file permissions. Optional local FFmpeg
decoding creates PNGs; optional Tesseract results are **unvalidated OCR text**,
not automatic camera-clock calibration.

Example after an inspector is already accessible through a loopback forward:

```sh
rtk proxy node server/scripts/capture-validation.mjs \
  --inspector http://127.0.0.1:19231 \
  --out .evidence/capture-validation-2026-10-12 --seconds 30 \
  --decode --ocr
```

On October 12, a read-only bridge check found service active, authentication OK,
three streaming slots, `stalled=false`, and go2rtc running. The two active
transcoder command lines showed VA-API decoding, VA-API scaling and H.264 VA-API
encoding. Historical stall counters were one for Door and Garage, zero for
Balcony. This health snapshot does not certify freshness or absence of pauses.
FFmpeg was available on the bridge; Tesseract was absent there. Both FFmpeg and
Tesseract were available on the local diagnostic host.

## Verification needed before calling it complete

1. Demonstrate exact source-record/output-packet correspondence on the live
   hardware encoder, including oversized keyframes and recovery/restarts.
2. Verify per-picture stamps survive go2rtc/RTSP, decoder submission and actual
   presentation on both Android and Linux.
3. Introduce controlled queue delay: expired decoded pictures must be rejected,
   and an already displayed expired picture must be cleared. Compressed
   reference packets retain their original expired stamps and reach the decoder,
   allowing later fresh pictures to recover without a manufactured GOP hole.
   Initial stream entry still requires an independent keyframe. Queue bounds
   and failure behavior must remain explicit.
4. Measure clock synchronization uncertainty and fail closed when it exceeds
   the freshness budget. Repeat after bridge/client restarts.
5. Measure actual rendered cadence and the added ingress-to-presentation delay;
   packet counters or decoder callbacks alone are insufficient.

For an absolute **sensor capture** guarantee, a trusted time reference must be
visible in the scene or an independently verified camera capture-clock mapping
must be obtained. Existing OSD/wire observations cannot resolve that missing
reference automatically. Until that is available, report and enforce bridge-
ingress freshness explicitly; do not relabel it end-to-end camera freshness.

The helper/unit/FFmpeg-fixture tests verify local serialization and packet
association. Deployment and physical client verification are separate gates;
this document must not be read as a claim that those gates have passed.

### Local production-path validation

`server/test/fresh-egress.test.mjs` passes eleven tests on the diagnostic host.
The actual production software fallback was run against complete-AU Matroska
input with source timestamps `1000,1067,1134,1300,1367,1434,1501,1568`.
All eight resulting encoded packets matched their **exact** input PTS record;
all eight outgoing stamps retained the corresponding source timestamp, frame
ID and ingress time. No unknown metadata occurred, and no PTS phase adjustment
or proximity heuristic was required. This is a software-fixture result; native
hardware container testing is recorded below, while live client presentation
remains a separate gate.

Other production-path tests verify hardware-first fallback order, expiry at
the 500 ms boundary, rejection of unmatched encoder PTS, exact expired-reference
stamps followed immediately by a fresh dependent packet, absent metadata,
stopped downstream drain, partial-packet
EOF, and fallback after classified hardware errors only. The helper tests also
verify 250,000-byte packet reconstruction, source wrap-scale values, malformed
stats/SEI rejection and encoder picture preservation by decoded-frame MD5.

### First deployment failure: encoder-pipe backpressure was treated as loss

The first live timed-stream trial failed startup even though finite-input tests
passed. The timed consumer exposed FFmpeg stdin's `writableNeedDrain` flag to
the shared consumer manager. That manager's recovery policy drops packets until
the next source keyframe. Large camera IDRs exceed the pipe's ordinary
high-water mark; normal transient flow control was therefore mistaken for loss,
discarding following reference pictures and starving encoder startup.

The repair stops exposing this generic drop signal. It preserves encoded
reference continuity and enforces a hard 4 MiB input-byte queue bound instead.
An oversized queue fails the stream rather than silently losing references.

The regression test crosses the actual stream high-water mark with a 250 KB
source IDR and verifies that subsequent inter frames remain admitted. A separate
real continuous-FFmpeg test uses valid 150 KB prefix-SEI-bearing IDRs and verifies
eight correctly stamped output packets **before stdin EOF**, while the process
remains alive. Finite EOF-only fixtures could not establish that behavior.

### Remaining parser hold: complete-AU Matroska input

MPEG-TS full Annex-B parsing retained the final picture until the next access
unit, even after the earlier finite-PES repair. Explicit Matroska packet
boundaries remove that rediscovery step. The checked-in tests verify real AVC
and HEVC codec configuration, exact irregular/wrapped timestamps, identical
decoded picture hashes, and in-band resolution changes. With a 300 KB IDR and
two P pictures, an open-input comparison delivered only two decoded pictures
from MPEG-TS but all three from Matroska without another packet or EOF.

An isolated Atom/i965 VA-API test ran the same 30-picture synthetic sequence at
approximately 67 ms intervals through full hardware decoding and encoding.
It did not enable the feature or change production configuration:

| Source / input | Median input-write→encoded output | P95 | Output before EOF / total |
| --- | ---: | ---: | ---: |
| AVC / MPEG-TS | 103.920 ms | 203.995 ms | 29 / 30 |
| AVC / Matroska | **32.171 ms** | **94.814 ms** | **30 / 30** |
| HEVC / MPEG-TS | 81.029 ms | 119.111 ms | 29 / 30 |
| HEVC / Matroska | **35.527 ms** | 178.442 ms | **30 / 30** |

All four native runs emitted thirty packets, zero unmatched PTS and no FFmpeg
errors. Matroska released the final packet before EOF for both codecs. HEVC's
P95 worsened between windows: the evidence establishes removal of a deterministic
parser hold and lower observed median stage delay, not universal tail improvement.
These fixture measurements do not establish real camera-to-screen latency.
Full details and private artifact paths are in
[the parser evidence](matroska-parser-latency-2026-10-12.md).

Encoder `async_depth=1` was considered and rejected. A live reversal comparison
2→1→2 produced medians 362.8→274.3→267.4 ms; the reversed baseline removed the
apparent advantage. Earlier direct encoder-stage measurements also found no
independent improvement. Hardware priority, source/output resolution, playback
buffers and the 500 ms deadline remain unchanged.

### Expired references are not expired displayed pictures

Dropping a compressed P picture at the bridge and waiting for another IDR can
manufacture up to a GOP-length gap from one short deadline miss. The bridge now
forwards that reference packet with its **unchanged expired stamp** and reports
`expiredReferenceFrames`. Strict clients decode dependencies, suppress expired
decoded output and clear an expired displayed picture. They can resume with a
fresh dependent picture without waiting for another IDR.

This does not claim every transmitted compressed packet is fresh. The enforced
contract concerns pictures actually displayed. Missing or mismatched stamps,
unknown epochs, invalid clock mappings and queue/backpressure failures cannot
bless stale output. Physical Android/Linux masking, presentation identity and
smoothness are still mandatory gates before calling the work complete.

### SDK boundary

No SDK code changes are part of this repair. A read-only runtime check found
SDK revision marker `35b9581d79d414602c63e7240677cb15aa09cf63` and loaded entrypoint
`dist/index.js` SHA-256
`3a15c296a54311782f242aa5446b23971671e4ee6016d43ecb927f4ec7fc3fc8`.
Bridge framing, per-picture metadata and client presentation enforcement are
consumer-side changes. Source capture-clock calibration remains unverified.

### Observation deployment and failed enforcement gate

The user reported repeated flashing followed by stale-video messages and
restarts during the strict experiment. It did not pass the live smoothness gate.
Strict display enforcement remains off. Capability version 2 requires an
explicit `observe` or `enforce` mode; v1 clients ignore version 2 rather than
accidentally applying strict expiry to observation. The binary SEI remains v1.

On October 12, the bridge and both Fire TVs were updated to observation mode.
Both selected hardware decoders, progressed decoded/rendered counters and
reported zero missing stamps. Observation does not drop, blank or restart
video based on receipt age. The bridge `/healthz` recovered with authenticated
streaming, all three streams and no stalls. All 155 server tests passed locally
and on the native Atom host; Android unit tests and APK assembly passed.

A subsequent 60-second Mac RTSP observer received 839 steady Front Door and
852 steady Garage pictures, with zero missing/invalid stamps, duplicates,
regressions or transport-invalid pictures. Ingress-to-observer upper age was:

| Camera | Median | p95 | Maximum | Pictures over 500 ms |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 142.992 ms | 352.286 ms | 529.035 ms | 1 / 839 |
| Garage | 113.740 ms | 234.660 ms | 845.173 ms | 2 / 852 |

These are receipt bounds, not sensor age or screen presentation. They are a
sequential window and cannot prove an A/B improvement over the old route.
Private full samples are `/tmp/eufy-freshness-observe-v2-live.jsonl`.
The [SDK/physical interface audit](sdk-source-pause-audit-2026-10-12.md) and
[bridge/network audit](bridge-network-timing-audit-2026-10-12.md) distinguish
upstream quiet intervals from additional downstream holds. Strict physical
Android and Linux presentation remains unverified and must not be advertised
as a completed guarantee.
