# Bridge frame formatting on Atom — 2026-10-12

## Observed need

The live producer trace measured Garage's time from receiving an encoded FFmpeg
packet to writing HTTP output at approximately 75 ms p99 and 89 ms maximum on a
large I-frame. Processing is synchronous on the bridge's Node event loop; other
camera work and UDP delivery cannot execute while it is busy. This investigation
addresses that formatting work, not all upstream pauses or transport delays.

## Changes under test

- `firstVclNal` searches Annex-B prefix start codes using native `Buffer.indexOf`
  and returns at the first coded-slice header. It does not walk the encoded slice
  payload. The same reader identifies H.264 IDR type 5 and H.265 random-access
  types 16–21, instead of parsing full parameter sets just to obtain a keyframe flag.
- Stamp insertion uses that prefix reader. Its location remains after existing
  prefix SEIs and immediately before the first VCL. All original bytes are retained.
- MPEG-TS formatting allocates one transport output backing store instead of
  allocating and concatenating thousands of individual 188-byte packets.

No resolution, frame rate, codec, playout buffer, timestamp, or freshness deadline
was changed by these formatting repairs. No SDK change was made.

## Actual native frame and pixel equivalence

The running Garage RTSP feed supplied one 594,882-byte H.264 IDR through an FFmpeg
`-c:v copy` capture. No additional encoder was started. `ffprobe` identified an
I-frame at 1280×1440; the first VCL started at byte 100.

- Input SHA256: `a744e562ef66ec4c378ff444c80054712d76df7637d5126b9edffc39bf15e9c2`.
- Old and new stamped output bytes were identical.
- Eight consecutive old/new complete MPEG-TS outputs were identical, including
  continuity counters, timestamps, tables and packet layout.
- Original, old-stamped and new-stamped decoded pixel MD5 all equaled
  `5a960c214a051d3f3bb0f93468361b03`.

Generated H.264 and H.265 fixtures also passed FFmpeg decoded-pixel equivalence
tests. Prefix tests cover mixed three/four-byte start codes, existing encoder SEIs,
a 590 KB coded slice, malformed headers and actual VCL keyframe types. Synthetic
MPEG-TS comparisons cover both codecs, whole/finite PES variants and clock wrapping.

## Native CPU measurements

The isolated benchmark ran on the actual Atom bridge with Node 26.11.1, at nice
priority 19, after all producer/NIC captures had completed. Each case had ten
warm-up calls and 100 measured calls. The same real IDR was used throughout.
The bridge remained running, so scheduling and allocator variation remain possible.

| Work | Before median / p95 / max | After median / p95 / max |
|---|---|---|
| Stamp insertion | 6.607 / 11.025 / 20.527 ms | 0.356 / 0.627 / 0.853 ms |
| Key detection + stamp + whole-PES MPEG-TS | 31.930 / 56.303 / 73.698 ms | 3.968 / 8.395 / 10.284 ms |

Whole formatting consumed 4106 ms CPU across the old 100-call batch and 552 ms
across the new batch. This reproduces formatting costs on the scale observed in
the live trace and establishes a substantial reduction with byte-identical output.
It does not establish an end-to-end latency guarantee or eliminate upstream silence.
Live deployment and joined-stage verification must be recorded separately.

## Separate SDK audit: measured candidate, not changed

The unchanged SDK bundle SHA256 was
`3a15c296a54311782f242aa5446b23971671e4ee6016d43ecb927f4ec7fc3fc8`,
corresponding to the `eufy-wall` revision under investigation.

A separate isolated synthetic benchmark fed its actual `P2PSession.reassemble`
method 1400-byte fragments. It did not open sockets, decrypt video, call live
consumers, or modify the SDK. The comparison was a linear chunk-list fixture,
not a replacement SDK implementation.

| Payload | Current copies / allocations | Current median / p95 / max | Linear fixture median / p95 / max |
|---|---|---|---|
| 64,000 bytes | 1,510,896 bytes / 45 | 3.964 / 7.332 / 10.780 ms | 0.130 / 0.215 / 0.452 ms |
| 234,000 bytes | 19,869,144 bytes / 167 | 15.415 / 49.397 / 57.553 ms | 0.293 / 0.407 / 0.544 ms |

The logical transport length is uint32, so 64 KB is not a transport-wide maximum;
64,000 bytes is one model-specific video chunk size. The assembler for completed
access units already stores chunks and concatenates once. The repeated copying
occurs in logical-frame reassembly across UDP continuations.

The SDK parameter-set sniff also scans a normal single-VCL delta's full payload:
its generator checks a 64-byte bound only after yielding another start code.
Native synthetic calls took 1.181 ms for 64 KB and 4.002 ms for 234 KB, compared
with 0.0044/0.0022 ms for a bounded-head fixture. Timing ran before Proxy-based
indexed-read counts, avoiding receiver deoptimization as a benchmark confound.

These synthetic results identify avoidable work but do not prove its cost in the
live packet path. Kernel-first-packet to whole-frame callback includes normal
assembly time; proof needs the final completing datagram, callback entry and
assembly/consumer stages joined correctly. No GC timing conclusion is drawn from
the incomplete performance-observer samples. No SDK patch or upstream claim is
justified solely by this benchmark.

## Reproduction artifacts

Private actual frames, old/new modules and benchmark results are under `/tmp/` on
the bridge and the local analysis machine. The harnesses are
`eufy-frame-stamp-bench.mjs`, `eufy-whole-format-bench.mjs` and
`eufy-sdk-reassembly-bench.mjs`; result names end in `native-before-after.json`
or `bench-native.json`. Raw device video is not committed.
