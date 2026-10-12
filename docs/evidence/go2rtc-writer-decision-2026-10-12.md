# RTSP writer decision — a default repair, not an experimental option

## Decision

**Keep the repair and make it the installation/update default. Remove the enable
flag.** The confirmed defect is continuation of length-framed RTSP/TCP after an
incomplete write. This repair closes only the affected transport; it does not
change encoding, resolution, buffering, frame pacing, or the five-second write
deadline. It does not claim to fix independent HomeBase/source pauses.

The pinned upstream is go2rtc v1.9.14. The reviewed, reproducibly built Linux
amd64 binary is
`7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585`.
The verified cache and atomic-install gates remain mandatory. A build/cache failure
preserves the installed binary; a newer unreviewed upstream version is rejected.
Normal updates verify/build on the build host and transfer the cache, keeping Go
compilation off the weaker bridge when using the update script.

Focused verification passed: 18 installer/cache/config tests, shell syntax checks,
the current cached binary's full provenance/hash verification, and the observer's
offline complete-AU digest/RTP identity fixture. The source patch and candidate
binary are unchanged from the earlier seven-case repair regression.

## Why this is a defect rather than a tuning choice

A failed socket write may transmit only the prefix of a `$`-framed RTP message.
The receiver still expects its declared tail. Upstream clears that buffer and
continues with the next message, whose header is then consumed as the missing
payload. Closing and redialing preserves protocol boundaries; appending unrelated
new messages cannot repair this TCP byte stream.

The [earlier proof](go2rtc-interleaved-write-failure-2026-10-12.md) includes
base-failing deterministic and actual `net.Pipe` partial-write regressions, a
controlled actual-camera witness, and ordinary consumer-removal ownership tests.
The latest RTSP/TCP/core package suites also passed under race checking.

## Mutex and cleanup audit

The guard is per `rtsp.Conn`, not per source, station, or process. Only deadline
setting and one complete TCP write use that mutex. Media and control messages
share it, preventing a control response from appending inside an incomplete
media packet. A queued writer sees the failed flag and exits.

The sender closes the low-level socket, never invokes `Conn.Close`, `OnClose`, or
`Sender.Close` inside its own handler. The ordinary read loop owns consumer
cleanup. No state/source mutex is acquired under the write mutex. Existing sender
fan-out uses independent per-viewer queues and nonblocking enqueue; a blocked
viewer does not hold another viewer's write mutex. The media-write deadline is
unchanged, so a queued control response can wait for the blocked write's existing
deadline, then fail cleanly. Dial resets failed state on a new socket.

## Additional bounded live gate: healthy sibling plus backpressure

Baseline and repaired sidecars were tested sequentially against the **same
already-running Garage compatible RTSP producer**. Each sidecar had one ordinary
viewer and one deliberately paused viewer; an ordinary main-stream observer
provided the same-window control. No new SDK session/encoder, camera setting, main
service restart, app action, or source-policy change occurred. Both existing
hardware encoder processes and Node PID 30219 remained in place.

The test-only paused viewer requested a 16,384-byte receive buffer, read for
approximately six seconds, stopped reading for 16 seconds, resumed, and redialed
after failure for eight seconds. These settings are not playback defaults.
Each ordinary viewer observed for 40 seconds. All reader handles completed;
both owned sidecar units were stopped, with runtime bounds as fallback protection.

| Gate | Baseline | Repair |
| --- | --- | --- |
| Main and healthy sidecar complete coded pictures | **601 each** | **598 each** |
| Exact VCL digest + RTP timestamp matches | **601/601**, no extras/missing | **598/598**, no extras/missing |
| Invalid transport units / healthy viewer errors | **0 / 0** | **0 / 0** |
| Main / healthy sidecar longest delivery gap | **1151.263 / 1181.125 ms** | **1058.237 / 1089.383 ms** |
| Median paired sidecar-minus-main receipt time | **+0.007 ms** | **−0.002 ms** |
| Failed write (UTC) | **23:01:54** | **23:02:44** |
| Accepted / expected bytes | **98,631 / 297,843** | **29,650 / 49,619** |
| Paused viewer after resume | **ValueError** | **ConnectionResetError** |
| New connection delivered pictures | **101** | **127** |

The source windows differ, so 601 versus 598 is not an FPS improvement claim.
Both healthy counterparts saw the same complete pictures in each window, even
when their sibling's write failed. The approximately one-second delivery gaps
also occurred on the main control; they are not a new writer-only freeze. These
are coded-picture receipt gaps, not physical panel freezes. For the repaired path,
paired receipt-delta p99/max were 27.444/53.778 ms; timestamps include observer
scheduling and distinct sockets, not physical display latency.

The baseline logged a real partial write plus `i/o timeout`, and continued its
damaged connection. The repaired writer logged its partial write plus timeout
and closed that connection; the peer observed a reset. Redial succeeded, while
the healthy sibling continued. Exact baseline exception text was not stored;
the `ValueError` alone is not relabeled as a particular GStreamer error.

### Corrected observer boundaries

An initial three-second attempt failed in the observer because a metadata hook
used `epoch` instead of the measurement API's `serverEpoch`. It yielded no valid
transport result and was excluded. A corrected intermediate run reproduced a
partial write and healthy counts, but recorded only stamped pictures. The RTSP
sidecar repacketizer retained per-picture SEI stamps on only 26/772 units in that
window, so stamp-only cadence/identity claims were invalid.

The final gate therefore recorded **every complete access unit**, with VCL hashes
and RTP timestamps. No raw pixels/RTP payloads were persisted. Full matching above
uses those encoded-picture identities, not the sparse surviving SEI stamps. The
sidecar stamp limitation does not occur on the main HTTP producer path and does
not imply that the writer changes encoded VCL bytes.

## Deployment and remaining limits

The earlier Fire TV disconnect report followed four intentional RTSP child
restarts; an independent paired Garage socket read timeout and later source
silence also occurred. There is no measured natural Fire TV partial-write witness
or quantified disconnect-rate attribution to this guard. The decision rests on
the real framing defect, isolated base-failing proofs, and the bounded healthy
sibling/redial gate—not a claim to explain all past disconnects.

The production owner subsequently performed one protected atomic go2rtc-child
replacement, preserving the SDK/Node process, source policies, client preferences,
and hardware paths. Both TVs and the parallel source API passed the short
post-recovery gate; the repair was retained and the rollback guard canceled.
See the [production gate](go2rtc-writer-production-gate-2026-10-12.md). This does
not establish sustained physical smoothness or resolve independent source pauses.

Reproducer: `server/tools/diagnostics/verify-rtsp-writer.py URL ROLE CLOCK_URL`.
Use the `paused` role only on an isolated sidecar. Private artifacts are under
`.evidence/frame-freshness-2026-10-12/writer-decision/`: `baseline-v3-*`,
`candidate-*`, journals, and summary files. No further live capture was started
after these gates.

| Retained metadata artifact | SHA-256 |
| --- | --- |
| Baseline main | `015b834fea4d990f4f3f6bbc9c153c03eb5dc9e463efc5d2e8f51fd4d15ab0d4` |
| Baseline healthy sibling | `61b6620fb6286722cfd383e68c7e0b7d74106c74a02b2d73065e712d94088def` |
| Baseline write journal | `fca9436a24bc6a431a517cfb035866dbcab9b094f090c072de42aca14c790dda` |
| Repair main | `19f0c6931c92c2a034d48651090013d60ec39fbdf8f56f2849d8f8d8cdc082d7` |
| Repair healthy sibling | `116be24586c4c3a246ee9057c414ca11b0d109cdaf39cdacacff2b211af67232` |
| Repair write journal | `f0d819317bcbd5005c36c86de74fe0ed552ffe9b06b9bc0ca7d6f9ae05ab9e17` |
