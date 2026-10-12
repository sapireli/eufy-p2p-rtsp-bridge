# Exact picture age in low-rate Linux telemetry

The five-second `eufy-observe` report now includes separate last decoded-admission
and last successful KMS-sink-return samples. Each valid sample carries its own
source epoch, frame ID, local CLOCK_MONOTONIC observation time and ingress-age
lower/upper bounds. Histograms remain cumulative and must not be attributed to
the most recently logged frame ID. `lastDecoded` is **after decoded admission**,
not the decoder's earlier output. KMS return is not verified physical scanout.

These fields use existing age calculations and stamp lookup. They add no packet
capture, raw-memory mapping, per-frame JSON or file reads. Missing/invalid clock
bounds have validity false and zero bounds; readers must not treat zeros as a
fresh frame. An invalid KMS sample may also have no identity when its admission
age was unverified. All pictures still follow the existing observe-only playback
policy; no deadline rejection, extra buffering or clock correction is introduced.

## Native verification

The actual GStreamer fixture sends two stamped synthetic pictures. The second is
deliberately more than 600 ms old. A fakesink performs 50 ms of work. The report
must identify picture 2 and epoch `epoch123` independently at both stages, preserve
ordered bounds, and increase both bounds by the observed downstream duration.
The verifier rejects an identity mismatch, invalid sample, missing report, an age
relabelled from another frame, or a duration/bounds mismatch.

On the Pi, decoded bounds were 762.380–763.580 ms; KMS-return bounds were
812.967–814.167 ms. Local observation timestamps differed by 50.587 ms, matching
the increase in both bounds. The automated verifier passed a second native run.
Pure C tests also check independent sample identities and clearing unknown/missing
bounds. Native build passed; plugin SHA-256:
`0b54bb70ac0c78be1e74c2b2f9c931f571f9e3599f437ca2166cdb27a76b8462`.

```sh
GST_PLUGIN_PATH=/path/to/plugin python3 verify_last_age.py /path/to/freshness_probe
```

This verifies observation correctness, not camera capture time, consistent
500 ms presentation, smooth playback, or the experimental decoded pacing policy.
The next live work-isolation attempt produced no valid comparison: one startup
overlapped bridge feed interruption, and a later run experienced genuine RTSP
ParseError in both children before the planned work stop. Those failures are not
proof that this diagnostic caused them. The Pi was restored OFF, its service
inactive/disabled, and original two-camera configuration preserved.
