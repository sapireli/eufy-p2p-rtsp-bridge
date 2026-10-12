# Bridge and network timing audit — 2026-10-12

## Scope

Read-only measurements of the running bridge at `192.168.23.158`, its existing two VA-API FFmpeg producers, and RTSP/TCP connections to Fire TVs `.58` and `.85`. No source-quality, SDK-control, socket-buffer or service changes were made. Additional RTSP consumers reused the already-running producers; they did not create new encoders. GaragePi `.73` remained inactive and disabled (confirmed after capture).

Raw evidence is in `.evidence/bridge-network-audit-2026-10-12/`; the simultaneous SDK capture is in `.evidence/frame-freshness-2026-10-12/`. Those files include private device identifiers and process arguments; redact them before publishing externally.

## Bridge CPU and responsiveness

The existing Inspector was queried once per second for 90 samples, using only reads of event-loop utilization, CPU usage, RSS and monotonic time. Node PID 9556 did not change. Inspector round-trip median 7.8 ms, p95 = 14.6 ms, maximum 74 ms. One-second event-loop utilization median 0.266, p95 = 0.376, maximum 0.469. Node CPU median 36%, p95 = 55%, maximum 67% of one core. This is sampled responsiveness, **not** a full event-loop delay histogram; it cannot exclude a brief pause between samples.

Existing FFmpeg workers 10174/10175 consumed approximately 19–20% of one core each, p95 = 26–29%, max 31–33%, from `/proc` tick deltas. Both workers explicitly used VA-API hardware decoding, `scale_vaapi` and `h264_vaapi` encoding. The workers changed from 9936/9944 to 10174/10175 during the first measurement epoch, while the main bridge PID stayed fixed; do not treat the whole first window as one producer epoch.

Ethernet negotiated 1 Gb/s full duplex. Interface RX/TX error counters were zero; a single lifetime TX drop cannot identify a capture-time problem.

## RTSP/TCP TV differences

The corrected socket collector ran from 15:46:44.778 to 15:47:56.326 UTC (120 samples; nominal 0.5 s sleep plus collection overhead). It handles IPv4-mapped IPv6 addresses. The earlier collector omitted those RTSP sockets and only measured HTTP/WebSocket connections.

| Consumer | Camera | Sent bytes delta | Retransmitted bytes delta | Retransmission ratio | Minimum congestion window |
| --- | --- | ---: | ---: | ---: | ---: |
| `.58:57866` | Front Door |18,271,478|38,351|0.210%|19|
| `.85:42674` | Front Door |18,392,653|159,526|0.867%|6|
| `.58:57868` | Garage |39,933,225|13,401|0.034%|145|
| `.85:42676` | Garage |40,919,886|681,654|1.666%|1|

Median RTT was 5.8–14.6 ms; maximum RTT 44–132 ms. Sampled send queues reached 605 KB. TCP retransmission and congestion-window collapse were materially worse on `.85`, particularly Garage. This supports an additional per-device transport delay mechanism; it does **not** prove that one specific 2.25-second displayed pause was caused by TCP, because there is no matching frame-ID/render trace for that incident.

## Existing producer output cadence

Two simultaneous RTSP consumers on the Mac ran 60 seconds. Both feeds averaged 15 fps; RTP time/wall time ratios 1.0036 Front Door and 1.0010 Garage, with zero timestamp regressions. Nevertheless, frame-marker arrival gaps were irregular:

| Observer | Camera | Gaps counted | Median gap |95th percentile|Maximum|Gaps over 300 ms|
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Mac RTSP/TCP |Front Door|854|47.9 ms|204.7 ms|605.7 ms|28|
| Mac RTSP/TCP |Garage|853|51.4 ms|125.6 ms|669.8 ms|21|
| Bridge loopback RTSP/TCP |Front Door|397|48.3 ms|264.9 ms|409.7 ms|14|
| Bridge loopback RTSP/TCP |Garage|407|54.0 ms|128.8 ms|369.4 ms|3|

The subsequent 30-second loopback observer removes LAN/WiFi/client decoding from that measurement. Hundreds-of-milliseconds arrival gaps still occurred at the bridge RTSP boundary, while adjacent RTP timestamp increments continued to represent approximately 65–70 ms of media. Several longer gaps recur near the two-second keyframe cadence. That cadence is an observation, not proof that the encoder is responsible: source ingress, muxing, keyframe packet sizes and producer execution need simultaneous per-frame correlation.

The Mac and loopback windows are sequential, so their maxima cannot be subtracted as a measured network contribution.

## Conclusions and limits

There are at least two distinguishable measured effects: irregular arrival before the display player, and greater TCP recovery on `.85` than `.58`. No sustained RTP clock drift was observed. A 500 ms display expiry policy can blank otherwise recovering streams during the measured real arrival pauses; these measurements do not establish that strict enforcement is ready for production. Observation mode preserves playback while gathering ingress-age evidence. No Linux production deployment or physical KMS validation was performed in this audit.

## Simultaneous exact source-word correlation

A second capture joined SDK `emitted` records to the existing bridge loopback RTSP output. Source timestamps were not estimated from arrival order: every matched picture satisfies `RTP = (90 × sourceTimestamp + constantPhase) modulo 2^32`. The constant phase was 1,175,667,740 for Front Door and 270,357,585 for Garage. All 957 Front Door and 977 Garage output pictures inside the overlapping SDK capture interval matched exactly. SDK time and the RTSP observer used the same bridge host clock; SDK event `wallMs` has one-millisecond resolution.

The SDK capture ended at **15:57:18.199 UTC**. Exporting 101,981 metadata rows (~35 MB) through Inspector coincided with a subsequent ~7.8-second pause on both feeds. All output after that boundary was excluded from this correlation; that observer artifact is not evidence of ordinary playback behavior.

| Camera | SDK emit → RTSP completion median | p95 | Maximum within capture |
| --- | ---: | ---: | ---: |
| Front Door | 139.5 ms | 319.6 ms | 673.8 ms |
| Garage | 140.5 ms | 264.4 ms | 996.3 ms |

Exact neighboring-picture examples distinguish additional producer-path hold from an incoming SDK burst:

| Camera / output completion UTC | Source timestamp pair | SDK emission gap | RTSP completion gap | Increase in post-SDK hold |
| --- | --- | ---: | ---: | ---: |
| Front Door / 15:56:16.600 | 819215598 → 819215667 | 64 ms | 574.8 ms | 510.8 ms |
| Front Door / 15:56:50.561 | 819249602 → 819249667 | 61 ms | 529.1 ms | 468.1 ms |
| Front Door / 15:57:12.652 | 819271602 → 819271666 | 50 ms | 614.0 ms | 564.0 ms |
| Garage / 15:56:52.784 | 819246030 → 819246097 | 30 ms | 500.5 ms | 470.5 ms |
| Garage / 15:57:13.260 | 819266564 → 819266632 | 61 ms | 470.1 ms | 409.1 ms |

These pictures were small non-IDR output pictures, with only 0.02–0.22 ms between their first and final RTP packet at loopback. The extra delay therefore preceded their packet completion; it was not the time required to transfer a large IDR picture across WiFi. This establishes **additional delay between SDK emission and bridge RTSP output**. It does not yet isolate HTTP/MPEG-TS demuxing, hardware decode/encode, scheduling or output muxing as the specific cause.

Raw joins and packet metadata: `source-rtsp-correlation.json`, `sdk-wire-correlated-idr-loopback.jsonl`, and `sdk-wire-correlated-capture.json` in the evidence directories described above. No pixel data was stored in these correlation artifacts.

## Bounded WiFi performance-lock A/B/A

The Android agent ran one bounded, debug-only high-performance WiFi-lock experiment on `.85`; `.58` remained a simultaneous control. The same four RTSP sessions stayed connected throughout. No player-buffer, source-quality or SDK changes were made during the experiment. The lock automatically released after 120.001 seconds and debug logging was restored afterward. This is a diagnostic intervention, not a deployed playback fix.

Actual phases (one-second guard removed at each boundary):

- OFF1: 16:02:34.028–16:04:05.120 UTC.
- ON: 16:04:05.120–16:06:05.121 UTC.
- OFF2: 16:06:06.450–16:07:37.426 UTC.

| Device / camera | OFF1 retransmitted bytes / sent bytes | ON | OFF2 |
| --- | ---: | ---: | ---: |
| `.85` Front Door | 0.3157% | 0.3342% | 0.5358% |
| `.85` Garage | 0.2269% | 0.1162% | 1.1782% |
| `.58` Front Door control | 0.0663% | 0.0759% | 0.0805% |
| `.58` Garage control | 0.0087% | 0.0142% | 0.0193% |

Garage retransmission was lower with the lock than in both OFF phases, but Front Door did not improve relative to OFF1. The control also changed over time. A single sequential A/B/A run therefore does not establish a general WiFi-lock fix. The Android agent's receive/render measurements are needed alongside these socket counters; this document does not infer rendering from TCP acknowledgement.

A simultaneous bridge-loopback observer followed the new Matroska producer path and retained the same two passive consumers for all phases. It measured bridge ingress to observer receipt, not presentation or camera capture. All 7,124 Front Door and 7,154 Garage stable pictures carried valid unique stamps, with zero missing stamps, RTP-invalid pictures or frame-ID regressions. Conservative age bounds for the full 480-second observation were:

| Camera | Median | p95 | Maximum | Pictures with upper bound over 500 ms |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 124.9 ms | 329.1 ms | 1,196.3 ms | 21 / 7,124 |
| Garage | 92.8 ms | 213.4 ms | 1,190.7 ms | 4 / 7,154 |

The conservative clock bound includes HTTP sample uncertainty; maximum uncertainty was 73.5 ms Front Door / 89.6 ms Garage. The observer used an explicit 1,000 ppm drift allowance. Every clock sample's interval included zero offset between the two monotonic clocks on this same Linux host.

Loopback p99 age during OFF1 / ON / OFF2 was 406.9 / 423.4 / 399.2 ms Front Door and 279.3 / 334.8 / 249.2 ms Garage. Thus the bridge's tail did not worsen during OFF2 while `.85` transport and observed client tails worsened. This supports a separate downstream component; it does not eliminate remaining bridge-side stalls.

Two mechanisms remain measurable even on the new path:

- A Front Door output gap of 1,407.3 ms corresponded to 1,408.7 ms between the two stamped bridge-ingress times, while source timestamp advanced only 62 ms and the received pictures' conservative ages stayed about 146 / 139 ms. This is consistent with a pause before bridge ingress for neighboring source pictures. There was no simultaneous SDK hook in this later run; the observation alone cannot exclude omitted intermediate output pictures.
- A Garage output gap of 1,011.0 ms corresponded to only 81.0 ms between stamped ingress times. The age grew from about 199 to 1,191 ms: additional producer-path hold remained.

Raw artifacts: `wifi-aba-sockets-v2.jsonl`, `wifi-aba-phases.json`, `wifi-aba-summary.json`, `wifi-aba-loopback-freshness.jsonl` and `wifi-aba-loopback-phases.json`. The latter includes a same-host monotonic reconstruction for diagnosis; the published tables above retain conservative HTTP clock bounds. Neither observation proves strict 500 ms expiry is suitable for smooth production playback.

## Producer stage trace and actual driver mode

A bounded 90-second trace on the existing two encoders recorded 2,681 input frame objects and 1,676 timer samples; 2,680 completed frames joined their Matroska PTS to the actual final SEI frame ID/ingress stamp. Hooks were restored before export. Only metadata was exported to a private file on the bridge; no image bytes were retained. This window did not reproduce the earlier roughly 930 ms post-ingress hold.

| Stage | Garage median / p99 / maximum | Front Door median / p99 / maximum |
| --- | --- | --- |
| Bridge ingress → stdin write | 0.37 / 9.74 / 13.23 ms | 0.36 / 11.94 / 15.52 ms |
| stdin callback → encoded stats receipt | 71.30 / 184.18 / 245.67 ms | 96.60 / 348.43 / 428.47 ms |
| Encoded stats → complete reader frame | 0.48 / 7.54 / 53.31 ms | 0.49 / 3.71 / 43.15 ms |
| Complete reader frame → bridge HTTP write | 0.85 / 74.78 / 89.46 ms | 0.85 / 34.75 / 56.58 ms |
| Entire ingress → bridge HTTP write | 79.37 / 193.19 / 273.80 ms | 102.14 / 353.10 / 430.13 ms |

`readerReadyUs` was recorded before the original bridge callback; `httpWriteUs` before the tracer's prefix parsing. The tracer parses at most 4 KiB of output prefix; its parsing and original HTTP write together cost at most 5.4 ms, and do not account for the preceding 75–89 ms callback delays. Those larger delays coincided with roughly 590 KiB encoded Garage pictures and cover parameter-set inspection, SEI insertion and MPEG-TS packaging. This motivates the separate transport-allocation/prefix-scan optimization, with byte-equivalence tests; it is not the sole delay mechanism.

Per-process i915 engine accounting showed combined video-engine median utilization about 69%, p95 about 95%. GPU frequency was 500 MHz in 341 of 414 samples. Individual samples can slightly exceed 100% because the two counters and interval timestamp are read sequentially; do not interpret that as excess hardware capacity. Some Front Door 390–428 ms encoder-path holds occurred with about 64% sampled video-engine utilization and several subsequent source writes already accepted. These samples do not establish simple GPU saturation or a dependency on a future input picture.

Installed `h264_vaapi` exposes `async_depth=2`, but that option is **inactive on this driver path**. A query against the actual installed Intel i965 CherryView 2.4.1 driver returned `VA_STATUS_ERROR_UNIMPLEMENTED` (20) for `vaSyncBuffer`; no encode context was created. [FFmpeg 7.1 initializes its async FIFO only when that API is implemented](https://github.com/FFmpeg/FFmpeg/blob/n7.1/libavcodec/vaapi_encode.c#L2077). Consequently, changing async depth is not a supported diagnosis for this host.

## Confirmed large-PES demux dependency

The actual [go2rtc 1.9.14 MPEG-TS demuxer](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/mpegts/demuxer.go) completes a finite PES when its declared byte count arrives; a zero-length PES instead completes when the next payload-start packet arrives. The bridge's whole-AU MPEG-TS output uses length zero above the 16-bit PES limit.

Real frame-ID joins to bridge-loopback RTSP show that this boundary matters:

| Frame ID / camera | Encoded AU bytes | HTTP → RTSP completion | Time until next HTTP AU write |
| --- | ---: | ---: | ---: |
| 77477 / Front Door | 389,265 | 353.6 ms | 316.6 ms |
| 77916 / Front Door | 388,714 | 293.4 ms | 257.4 ms |
| 73920 / Garage | 397,973 | 367.8 ms | 331.4 ms |
| 75403 / Garage | 402,751 | 361.3 ms | 318.9 ms |

Those RTSP completions follow the next AU write by about 36–42 ms, consistent with the source-code dependency plus conversion/packetization. Smaller finite PES frames usually reached RTSP in about 2.9 ms. This explains an additional hold **after** the producer had already completed and stamped the large encoded picture. Splitting a picture into finite PES fragments is unsafe with this demuxer: it treats each PES as a complete AVCC packet.

The supported alternative under verification is one finite FLV video tag per complete AU. [go2rtc 1.9.14 recognizes FLV HTTP input](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/magic/producer.go), and its [FLV reader reads the declared 24-bit payload size and forwards the picture immediately](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/flv/producer.go). That length accommodates the existing 4 MiB AU bound and preserves all original NALs, the final SEI and source timestamps. It requires no filler, synthetic frames, extra encoder or next-frame wait. Native one-AU/open-response verification and live before/after tracing are required before claiming deployment acceptance.

Additional raw evidence: `producer-boundaries.json`, `producer-kernel.jsonl`, `producer-rtsp-join.json`, `producer-node-tail-cases.json`, and `vaapi-async-capability.json`.

## Live FLV and CPU optimization verification — two-camera load

After deployment, a bounded 90-second live producer trace joined 2,675 completed frames to their real SEI metadata; 2,631 also matched a simultaneous loopback RTSP receipt. Only Front Door and Garage encoders were active. All hooks were restored and observers closed afterward. No source-quality, resolution, encoder threading or hardware-fallback changes were made in this comparison.

| Measured stage | Front Door before → after | Garage before → after |
| --- | --- | --- |
| Reader callback → HTTP write, p99 | 34.75 → 2.30 ms | 74.78 → 3.81 ms |
| Reader callback → HTTP write, maximum | 56.58 → 5.59 ms | 89.46 → 8.40 ms |
| HTTP write → loopback RTSP completion, p99 | 147.32 → 29.51 ms | 121.31 → 47.25 ms |
| HTTP write → loopback RTSP completion, maximum | 353.58 → 45.33 ms | 367.83 → 72.66 ms |

Large AUs above 64 KiB reached RTSP in a median 22.0 ms Front Door / 20.5 ms Garage, with maxima 45.3 / 72.7 ms. These measurements, together with the independent single-AU/open-response test, verify removal of the next-PES dependency. The earlier Node packet-packaging tail also fell substantially. Comparison windows are sequential and scene content can differ, so the numerical ratios are observations, not universal performance bounds.

Remaining encoder-path hold was not eliminated: stdin callback → stats receipt reached 448.5 ms Front Door / 203.5 ms Garage. Total bridge ingress → loopback receipt in this two-camera window reached 463.3 / 235.6 ms. Combined video-engine utilization remained median 68.8%, p95 95.4%.

A separate three-camera observer saw much larger Balcony ages and additional delay on the other feeds. The third encoder was legitimately created by configured `transcode: always`, then stopped when the observer disconnected. This two-camera acceptance does **not** establish three-camera capacity or justify strict 500 ms display expiration. A three-camera stage trace is required to distinguish hardware backlog from transport or source pauses; no camera quality was reduced to conceal that issue.

After artifacts: `flv-producer-boundaries.json`, `flv-producer-trace-loopback.jsonl`, `flv-producer-rtsp-join.json`, `flv-producer-kernel.jsonl` and `flv-final-tv-sockets.jsonl`.


## Three-camera capacity trial: hardware encoder backlog reproduced

A subsequent 90-second stage trace (16:57:29.658–16:58:59.662 UTC) ran alongside a 110-second bridge-loopback observer of all three cameras. Camera quality and global `transcode: always` were unchanged. Source formats were Front Door AVC 1600×2200, Garage HEVC 1280×1440, and Balcony HEVC 1920×1080. Pulling Balcony legitimately created a third VA-API encoder.

This trial **failed** the smooth low-latency requirement. Balcony repeatedly reached the producer's 120-pending-frame bound and logged `encoder frame backlog exceeded bound`, ending its HTTP producer and retrying hardware encoding roughly every 11–13 seconds. Errors occurred at 16:57:17, :28, :41, :53 and 16:58:05, :20, :34, :47 UTC. This is direct producer-backlog evidence, independent of Mac network latency.

| Three-camera observation | Front Door | Garage | Balcony |
| --- | ---: | ---: | ---: |
| Loopback conservative ingress age, median | 1,340.6 ms | 332.1 ms | 2,704.3 ms |
| Loopback conservative ingress age, p95 | 2,559.3 ms | 505.5 ms | 4,607.6 ms |
| Loopback conservative ingress age, maximum | 2,957.7 ms | 656.6 ms | 4,940.6 ms |
| Producer ingress → HTTP, median / maximum | 1,209.1 / 2,108.6 ms | 317.5 / 626.3 ms | Not joined; child restarted |
| Oldest pending producer input, median / maximum | 1,152.1 / 2,061.9 ms | 276.5 / 570.9 ms | Not joined; child restarted |
| Maximum pending producer frames | 32 | 9 | Bound of 120 reached, per journal |

All loopback pictures had valid stamps. During the 47 measured intervals when the kernel sampler actually saw all three encoder processes, combined video-engine utilization had median 98.9%. Sequential counter reads produced some values above 100%; those are sampling artifacts, not additional hardware capacity. The busy-engine observation supports a capacity problem, while the bounded-input failures establish actual backlog. It does not establish that GPU execution alone accounts for every millisecond.

Instrumentation limitation: producer hooks attached only to processes present at trace startup. Balcony's initial encoder had already ended; its replacements were not wrapped. The kernel sampler tracked one replacement for about 11 seconds and also missed later replacement PIDs. Therefore, only Front Door and Garage have exact per-frame producer-stage joins in this trial. No complete three-camera per-frame GPU/encoder attribution is claimed.

The observers closed afterward, Balcony's extra encoder stopped, and the original two-camera load was restored. No quality reduction or software-only default was used to conceal the failure. A separate native HEVC rendition is being implemented so capable clients can use the original source pictures without requiring a third encoder; compatible H.264 URLs and configured default policy remain available. That candidate still requires live multi-camera acceptance.

Raw artifacts: `flv-threecam-manifest.json`, `flv-threecam-boundaries.json`, `flv-threecam-loopback.jsonl`, `flv-threecam-kernel.jsonl`, and `flv-threecam-backlog-journal.txt`.


## Native rollout regression: concurrent go2rtc config publication

The first native-rendition deployment exposed a config-publication race: simultaneous camera codec discoveries rewrote the same generated YAML while another generation read it. The running go2rtc API then contained only the two native aliases; compatible camera keys were absent and returned 404. The parent restored the previous server before further capacity testing. This was a real rollout failure, not a decoder capability failure.

The generator previously wrote the published path before the bridge reread, hardened, renamed camera keys and added native aliases. Concurrent writes could truncate the file or replace a completed configuration with the other writer's intermediate generator output. The repair serializes the entire generation/plan-check/restart transaction and performs generation in a unique file in the same directory. Only a completed configuration is atomically renamed over the published path. Failure removes its private staging file and preserves the last valid published config.

Two regression tests were run against the first native commit `de780fc`; both failed: the concurrent reader saw null YAML (no `streams`), and an injected registry failure replaced the prior hardened named config with raw intermediate generator output. The same tests pass with serialized atomic publication, while a concurrent reader verifies compatible keys remain present throughout 24 queued updates. The failed queue item also no longer prevents later successful updates. Live rollout and all-rendition capacity verification remain separate gates.


## Native rendition candidate: device trial failed; capacity acceptance withheld

After serialized atomic publication was redeployed, the actual go2rtc API retained all six compatible camera keys and both learned HEVC aliases. A four-consumer bridge-loopback observation began at 17:23:09.487 UTC: Front Door compatible, Garage compatible, Garage native and Balcony native. The compatible Garage consumer represented the eventual Pi viewer. A separate dynamic kernel sampler discovered replacement FFmpeg processes on every interval.

During this window, the Android agent temporarily added Balcony on both Fire TVs. Native HEVC input arrived, but each hardware decoder produced zero output; the existing 15-second watchdog correctly latched back to compatible H.264. That fallback created another bridge encoder, and the third AVC tile on one TV used software decoding after its hardware AVC instance limit. The resulting load invalidated the planned two-encoder capacity comparison. Both exact original two-camera preferences were restored at 17:24:30.500 / 17:24:31.366 UTC.

The loopback observer was explicitly stopped to close its additional consumers. It publishes its aggregate only at successful completion, so this aborted run produced **no receipt-age result**. The retained dynamic kernel metadata spans 17:23:09.487–17:25:19.442 UTC (518 rows): 378 rows had two FFmpeg processes, 135 had three, and five briefly had four during replacement/termination overlap. Process counts alone do not establish simultaneous engine utilization. This is not a successful native Fire TV playback or multi-camera capacity gate.

A later bounded read-only audit of the actual native Balcony URL confirmed RTSP SDP `H265/90000`, profile-id 1, and complete VPS/SPS/PPS of 24/45/7 bytes with NAL types 32/33/34. Native `ffprobe` reported HEVC Main, 1920×1080, 8-bit 4:2:0 full range, level 150 (5.0), 25 fps and 88-byte extradata. These observations establish advertised codec configuration; they do not prove successful hardware output on Fire TV. No camera quality, default transcode policy, decoder default or SDK behavior was changed to bypass the failed gate.

Raw artifacts: `native-fourconsumers-kernel.jsonl`, `native-balcony-sdp.json`, and `native-balcony-ffprobe.json`. The empty aborted `native-fourconsumers-loopback.jsonl` must not be treated as accepted output evidence.


## HEVC device capability boundary

The corrected Android single `csd-0` configuration subsequently produced some native Balcony output, but the actual device remained too slow: output began after about 11.5 seconds and then ran about 3 fps with multi-second pending age. This is not successful low-latency HEVC playback. The device advertised HEVC Main profile only through level 4.1; accepting its 1920×1080 size declaration alone was insufficient.

The actual captured SPS starts `42 01 01 21`. After removing the two-byte NAL header and the one-byte SPS header, PTL byte `0x21` gives profile 1 and **high tier (bit 0x20 set)**; its general_level_idc is 150 (level 5.0). The earlier main-tier assumption was wrong. The server now reads a bounded SPS RBSP prefix on parameter-set discovery and advertises numeric `profile`, `level` and `tier` with the native rendition. It never forks ffprobe for frames or scans picture payloads. Unknown/malformed values remain absent so clients can decline native selection. The exact SDP fixture, emulation-prevention cases and malformed prefix cases are tested.

Production remains the original two-camera compatible view with freshness observation, while native selection requires actual profile/level/tier capability. The three-camera native device gate and combined capacity gate remain unpassed; no buffers, source-quality changes or decoder defaults conceal that limit.


## HTTP high-water signal incorrectly terminated timed producers

A later live failure at 17:37:11 UTC logged `timed media consumer stopped draining` while go2rtc still listed all eight compatible/native aliases. The timed producer had treated `ServerResponse.writableNeedDrain` as terminal failure. A successful large HTTP write can set that flag simply because it exceeds the writable high-water threshold; another source picture or another completed FFmpeg packet can be delivered synchronously before the next drain event. Closing in that interval terminates a healthy producer, causing go2rtc EOF and a subsequent RTSP startup failure. This affected both native passthrough and compatible post-encoder output.

A real local Node HTTP regression reproduced the failure: a roughly 150 KiB keyframe (valid extra user-data SEI plus the coded picture) set the actual response's `writableNeedDrain` flag. The synchronous next reference picture then produced `ECONNRESET` against the previous implementation. The same test now delivers both whole FLV AUs, exact payloads and original frame IDs/source timestamps while the response remains open, before any third AU or EOF. A compatible-encoder test also drains two complete encoded packets in one callback with the high-water flag set.

The fix checks the actual queued response byte count plus the next whole FLV tag against an explicit 8 MiB bound. It does not wait for a timer, withhold the next picture, drop compressed references or add a playout buffer. The bound accommodates ordinary maximum-sized AU bursts; sustained non-draining output still closes instead of growing without limit. A real paused HTTP consumer test fills the response queue above 7 MiB and is closed when the next write would exceed 8 MiB. Both high-water acceptance regressions fail on the preceding implementation and pass on the repair. Live producer recovery remains a separate deployment gate owned by the parent agent.


## Final live HTTP repair verification and native receipt measurements

After the HTTP queued-byte repair was deployed, the actual Fire TV HEVC depayloader was corrected for aggregation packets that contain both prefix SEI and coded picture NALs. The Android agent then measured a bounded native Balcony hardware trial on both TVs with actual 1280×720 source pictures: about 25 decoded frames/s, no missing stamps, and no growing queue. Exact original two-camera preferences were restored at 17:46:19.226 / 17:46:20.026 UTC. This is separate device evidence at **720p**, not acceptance of the earlier 1080p overload window or an assertion that advertised decoder profile limits can be ignored.

The first requested four-URL diagnostic attempt exposed a measurement-tool limitation: its H.264-only observer rejected both native HEVC tracks before SETUP. Therefore, that 120-second receipt result establishes only compatible Front Door/Garage behavior while the TVs were temporarily displaying native Balcony. Native receipt ages were not inferred from it.

| Compatible receipt path, 120 seconds | Front Door | Garage |
| --- | ---: | ---: |
| Stable stamped pictures | 1,626 | 1,642 |
| Observed picture rate | 14.818 fps | 14.938 fps |
| Conservative ingress age, median / p99 / maximum | 94.0 / 345.2 / 393.1 ms | 97.1 / 219.9 / 252.3 ms |
| Missing/invalid stamps, transport errors, duplicate IDs | 0 / 0 / 0 | 0 / 0 / 0 |

The observer was extended with bounded RFC 7798 HEVC single-NAL/AP/FU parsing for DONL=0. Unit tests cover fragmented SEI, sequence loss/wrap, malformed aggregation, duplicate stamps and AU size limits. Independently, the real 842-byte AP fixture reconstructed the exact expected 844-byte Annex-B sequence (prefix SEI 57 bytes + VCL 779 bytes), SHA-256 `e7d56a46a4b51d70504b6b84b6dd9388a5d0f182ed0e6a679692003983f18857`, and recovered its original frame-ID stamp. A five-second live preflight on both native URLs verified stamps before the longer run.

The corrected native two-URL observer then ran for 120 seconds, with dynamic kernel accounting from 17:49:28.173–17:51:38.108 UTC. Both original compatible TV views continued running; neither a new encoder nor a new SDK camera session was created by the native observers.

| Native receipt path, 120 seconds | Garage HEVC | Balcony HEVC |
| --- | ---: | ---: |
| Total / stable stamped pictures | 1,803 / 1,723 | 3,001 / 2,873 |
| Observed picture rate | 14.986 fps | 24.975 fps |
| Conservative ingress age, median | 11.41 ms | 9.99 ms |
| Conservative ingress age, p99 | 48.60 ms | 46.48 ms |
| Conservative ingress age, maximum | 92.32 ms | 84.26 ms |
| Missing/invalid stamps, transport errors, duplicate/regressing IDs | 0 each | 0 each |
| Receipt-age upper bound above advertised 500 ms | 0 | 0 |

All 517 kernel samples had the same two encoder PIDs; no Balcony encoder was created. Combined video-engine utilization was median 66.7%, p95 95.9%. Values slightly above 100% remain sequential-counter sampling artifacts. Source dimensions stayed Front Door 1600×2200, Garage 1280×1440 and Balcony **1280×720** throughout both final windows. No camera quality or default transcoding configuration was changed by this measurement; this differs from the earlier 1920×1080 Balcony source and must not be described as an identical-resolution before/after capacity comparison.

Filtered production journal records from 17:44:50–17:51:40 UTC contained **no** timed-output failure, encoder-backlog or go2rtc-restart lines. The HTTP streams completed the measurements without EOF, all probe consumers closed afterward, and the two compatible production views remained restored.

Source-side pauses still exist. The largest native Garage/Balcony receipt gaps were 646 / 356 ms, compared with 629 / 348 ms between the neighboring original bridge-ingress stamps; source media timestamps advanced only 68 / 39 ms. Those pauses precede bridge receipt and were not cured by the output queue repair. The results verify stable HTTP producer flow and ingress-to-observer receipt, not camera sensor freshness, screen presentation age or a hard future latency bound. Production remains in observation mode.

Raw artifacts: `http-flow-fixed-fourconsumers-loopback.jsonl`, `http-flow-fixed-fourconsumers-kernel.jsonl`, `http-flow-fixed-compatible-summary.json`, `hevc-observer-actual-ap-proof.json`, `hevc-observer-preflight.jsonl`, `http-flow-fixed-native-two-loopback.jsonl`, `http-flow-fixed-native-two-kernel.jsonl`, `http-flow-fixed-native-summary.json`, and `http-flow-fixed-producer-journal.txt`.
