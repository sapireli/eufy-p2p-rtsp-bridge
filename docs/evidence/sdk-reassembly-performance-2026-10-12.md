# SDK logical-frame reassembly performance — live proof, 2026-10-12

## Need and boundary

Large video frames require repeated synchronous buffer copying on a small bridge
host. This capture measured that work in the unchanged running SDK, separately
from network arrival time and frame consumers. It establishes avoidable host work;
it does not explain the separately observed multi-second upstream silence.

No SDK code, camera setting, connection policy or service configuration was changed.
The ephemeral observer restored the exact original methods after 60 seconds.

## Runtime and measurement

- Unchanged SDK runtime SHA256:
  `3a15c296a54311782f242aa5446b23971671e4ee6016d43ecb927f4ec7fc3fc8`.
- Capture start/end epoch milliseconds: 1791824748637 / 1791824808640
  (2026-10-12 17:05:48.637–17:06:48.640 UTC).
- Duration 60.003 seconds; three active SDK sessions; 3382 completed video-command
  records; overflow zero. One frame already partial at observer installation was
  excluded from the results below.
- The observer reused per-call contexts and counters. It allocated rows only for
  completed video commands, retaining no video bytes or per-UDP trace records.
- `reassemble` was timed in synchronous segments. Timing stopped before
  `handleFrame` dispatch and resumed only after it returned. RSA/video decode,
  SDK/client listeners and bridge formatting therefore were excluded.
- Indexed sizes and pending-buffer lengths estimate the bytes passed to the
  existing concatenation. A final datagram can also contain a following frame's
  prefix, so the attribution may include that small packed tail.
- Monotonic elapsed time includes OS preemption and timer-read overhead. It is not
  a profiler-derived exclusive CPU measurement or proof of zero observer overhead.

Two deterministic observer tests verify that handler work is excluded, original
return values and prototype methods are preserved, records are bounded, and an
already-partial frame is identified.

## Live keyframe results

Session labels are opaque handles. Each row summarizes command frames whose video
header marked them as keyframes; it does not infer a camera model from the handle.

| Session | Keyframes | Median logical bytes | Median copied bytes | Median / p95 / max assembly time |
|---|---:|---:|---:|---|
| A | 34 | 150,685 | 11,286,397 | 12.692 / 16.913 / 25.951 ms |
| B | 30 | 90,370 | 4,097,938 | 5.731 / 13.740 / 19.497 ms |
| C | 30 | 234,299 | 26,962,187 | 24.154 / 29.760 / 34.339 ms |

Session C's largest actual logical frame was 237,400 bytes. These are whole
transport commands larger than 64 KB. The transport length is uint32; the SDK's
64,000-byte access-unit chunk threshold is not a universal transport frame limit.

For all completed frames, time from the final completing datagram's reassembly
entry to pre-handler dispatch was small:

| Session | Median / p95 / max final-datagram time |
|---|---|
| A | 0.031 / 0.087 / 1.486 ms |
| B | 0.029 / 0.074 / 3.266 ms |
| C | 0.037 / 0.087 / 1.412 ms |

The larger keyframe cost accumulates across its continuations. Measuring from the
first kernel packet to a whole access unit also includes normal network delivery
and assembly waiting. It cannot be reported as SDK execution time.

## Candidate mechanism and falsification

`P2PSession.reassemble` concatenates the accumulated partial payload with every
incoming continuation. The copied byte count grows quadratically with a frame's
fragment count. `AccessUnitAssembler` already accumulates chunks and concatenates
once; it is a different layer and does not contain this repeated-copy pattern.

The earlier isolated native benchmark in
[bridge formatting evidence](bridge-frame-formatting-2026-10-12.md) compared the
actual method with a linear chunk-list fixture. It found approximately 15.4 ms
median for a synthetic 234 KB frame versus 0.293 ms for the fixture. The live
capture now confirms large logical frames and substantial repeated-copy work in
production. The fixture is not a correct replacement SDK parser.

Before proposing an SDK change, an implementation must preserve fragmented
headers, packed adjacent frames, retransmission/reordering and sequence wrapping;
then repeat the same bounded live measurement on the same runtime and concurrent
streams. Delivered frames and freezes must also be counted. Removing allocation
work is not evidence that camera-generated pauses or client TCP stalls are fixed.

## Artifacts

The private capture is
`.evidence/bridge-network-audit-2026-10-12/sdk-reassembly-live-60s.json`.
The bounded reproducible observer is `server/scripts/capture-sdk-reassembly.mjs`,
with tests in `server/test/capture-sdk-reassembly.test.mjs`. Raw video and device
identifiers are not part of this public evidence document.
