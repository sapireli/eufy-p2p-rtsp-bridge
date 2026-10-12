# go2rtc: complete encoded access-unit boundary proof

Date: 2026-10-12. Independent isolated test on the running bridge's Linux/amd64
Atom host, using **go2rtc 1.9.14, revision b5948cf**, from
`/opt/eufy-wall-bridge/bin/go2rtc`. Production encoders, camera settings, SDK and
client settings were not changed. The production service remained running.

## Why this change is needed

The bridge's whole-unit MPEG-TS writer represents an encoded access unit larger
than the PES length field with **PES length 0**. That expresses an unbounded video
PES: the consumer discovers its end from a later PES or source termination.
The native test below shows go2rtc retaining a complete large coded picture until
such a boundary arrives. A source pause can therefore become an additional
bridge→RTSP hold even though encoding that picture has already finished.

FLV provides an explicit finite tag length for the complete encoded access unit.
The change concerns the **post-encoder HTTP container**, not camera capture,
resolution, bitrate, decoding, encoding or a new playback buffer. H.264 uses AVC
FLV; H.265 uses Enhanced FLV `hvc1`. The separate complete-AU Matroska **encoder
input** boundary repair is covered by
[its own evidence](matroska-parser-latency-2026-10-12.md).

## Independent method

`server/scripts/verify-go2rtc-au-boundary.mjs` starts a separate go2rtc process and
loopback-only HTTP/RTSP/API listeners on ports 29930/29554/29084. Its RTSP reader
handles actual H.264/H.265 single-NAL, aggregation and fragmented RTP payloads,
checks packet sequence continuity, and declares a complete picture only at its
RTP marker with at least one VCL NAL. Configuration-only markers are ignored.

For each format/fixture, the source sends legitimate codec configuration first,
then **exactly one complete real coded picture**. MPEG-TS configuration is a
finite SPS/PPS[/VPS]-only PES, without a coded picture. FLV configuration uses its
standard metadata and sequence-header tags. This allows the RTSP receiver to
attach before picture delivery. A bounded fallback emits that same sole picture
if startup still needs the next PES start to expose configuration. It never emits
a second picture, empty PES, dummy boundary, filler or EOF.

The source HTTP response remains open throughout the measurement. No second coded
AU is sent. Large TS controls wait up to seven seconds without artificially
terminating the PES. Listeners/processes are closed only **after** the result.

The received VCL NAL lengths and bytes are hashed in order and compared with the
fixture. A fresh test SEI records a fixed epoch, source word, ingress timestamp
and frame ID; the receiver checks all of them exactly. The archived live fixture
already contained a historical timing SEI: the harness removes only that custom
SEI NAL before applying its test stamp. The coded picture bytes are unchanged.
Annex-B start-code lengths are normalized; they are not picture bytes.

Fixtures:

- **Real Garage H.264:** private previously captured encoded AU,
  `/tmp/eufy-real-garage-frame.h264`; original 594,882 bytes. Offline `ffprobe`
  counted exactly **one decoded picture**, 1280×1440. This proves a real large
  coded picture; it does not assert the camera's current capture resolution.
- **Large H.265:** locally generated real lossless noisy 512×512 picture,
  libx265 with one pool/thread and no B-frames; original 395,174 bytes. Offline
  `ffprobe` counted exactly **one decoded picture**. No encoder was added to the
  production streaming path.
- Small H.264/H.265: previously generated real 128×96 first-picture fixtures,
  providing finite-PES positive controls.

## Native results

The full native server test suite had finished before this isolated run, avoiding
that concurrent test load. These are individual measurements, **not latency
percentiles or glass-to-glass benchmarks**. Marker timing includes native host
scheduling and the observer's receive loop.

| Codec / fixture | AU bytes after timing-SEI normalization | Whole-unit MPEG-TS | FLV | FLV picture / stamp |
| --- | ---: | --- | --- | --- |
| H.264 small | 2,601 | Complete, 34.974 ms | Complete, 5.638 ms | Exact / exact |
| H.264 real large | **594,823** | **0 RTP packets; no complete AU in 7 s** | **410 RTP packets; complete in 53.108 ms** | Exact / exact |
| H.265 small | 3,980 | Complete, 3.776 ms | Complete, 5.285 ms | Exact / exact |
| H.265 large | **395,176** | **0 RTP packets; no complete AU in 7 s** | **274 RTP packets; complete in 45.466 ms** | Exact / exact |

All eight cases completed RTSP setup, emitted exactly one coded AU, and retained
an open source HTTP response **and connected, undestroyed socket** at the observation
boundary. The table is the strengthened socket-open verification repeat. An earlier
native pass measured large FLV H.264/HEVC markers at 41.525/19.728 ms with the same
pass/hold result; individual timing varies with host scheduling. Both small TS positive
controls preserved exact picture and stamp too. The large TS controls were held,
not corrupted or reported as decoded. Local macOS go2rtc 1.9.14 preflight reproduced
the same pass/hold distinction; it is separate from native Linux validation.

The initial native harness trial assumed a 500 ms process-start delay and got
connection refusals before any source opened. That trial is invalid for boundary
claims. The harness now polls actual isolated RTSP readiness with a ten-second
bound; the table is from the successful readiness-checked run.

## What is and is not established

This verifies removal of a specific unnecessary **future-PES dependency** for
large encoded pictures, including exact timing-metadata and coded-picture
preservation. It does not prove physical TV/Pi presentation, decoder deadlines,
continuous production behavior, source capture freshness or absence of unrelated
source pauses. The [separate source audit](sdk-source-pause-audit-2026-10-12.md)
records physical-interface quiet periods before SDK delivery; FLV cannot create
pictures the sender has not provided. Live deployment and client recovery remain
separate gates owned by the parent task.

## Reproduction and provenance

Prepare the private one-AU fixture directory with `small.h264`,
`garage-real-large.h264`, `small.h265` and `large.h265`, then run:

```sh
rtk proxy node server/scripts/verify-go2rtc-au-boundary.mjs \
  --go2rtc /opt/eufy-wall-bridge/bin/go2rtc \
  --fixtures /tmp/ewb-au-gate/fixtures
```

No production stream URL is referenced by this harness. Fixture/probe artifacts
remain private; no pixels are committed. Native result file:
`.evidence/frame-freshness-2026-10-12/go2rtc-oneau-native.jsonl`, SHA-256
`aa435cc47cbf1a612a13cd7882ce2842648e4a2ee8fd76e6dfc1b0cce84a4f9b`.
The excluded startup file ends in `-startup-invalid.jsonl`.

Tested implementation SHA-256:

| File | SHA-256 |
| --- | --- |
| `server/scripts/verify-go2rtc-au-boundary.mjs` | `5f923b7b3b78bb0146e4636d91bdaaa7bd619661a1ee3d11989e31e992c665ae` |
| `server/src/video-flv.mjs` | `33e8ebc91bc50326b7bc9a3b5e26a3b98444e1fcd9492f3a80826d2f4ba49395` |
| `server/src/video-matroska.mjs` | `3c65d4060cdb5578399fa2a30cefab7d6209e696a974d48d333a509d963ff046` |
| `server/src/video-mpegts.mjs` | `d836cc72bc6377e87dd8898e18276bb12109f1f21801f92bb06ba98097862ed8` |
| `server/src/frame-stamp.mjs` | `b15760bd149274355fc0cf167bdefee205b3484bdb48aa25a5f54ad70270f6a0` |

Private input fixture SHA-256:

| Fixture | SHA-256 |
| --- | --- |
| `large.h265` | `52749fab5e7420c59f11d137b7dac447d9a130af0e5fe4d0885ba2a2da5aec63` |
| `garage-real-large.h264` | `a744e562ef66ec4c378ff444c80054712d76df7637d5126b9edffc39bf15e9c2` |
| `small.h265` | `a4177976340c21e1b06487481dfef79aaa8f7bc5bd19c79e9e253d4a6b2f8c87` |
| `small.h264` | `9ea59cc22713e176099d1db3de8c1cd3762844ba12da0fe85c45045b88e90f63` |
