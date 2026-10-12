# Pacing and the latest-picture queue: falsification

This is an isolated native Pi GStreamer test, not a production fix or physical
camera playback result. It uses a genuine live 15 fps `videotestsrc`, the existing
one-slot downstream-leaky queue, the actual pacing plugin with its unchanged
67 ms allowance, and a `sync=true` fake sink. Synthetic sink work is explicitly
bounded. No encoded picture, SDK, source-quality or production policy changed.

## Tested hypothesis

A downstream queue sets DISCONT following leakage. Our pacing plugin treats any
raw DISCONT as a new timeline, resetting its rolling clock. A candidate resets
only on real SEGMENT/FLUSH_STOP, preserving the PTS regression guard. Its native
lifecycle test passes, but the cadence hypothesis fails these experiments:

| Sink work / disturbance | Existing plugin | Candidate |
| --- | ---: | ---: |
| 25 ms + one 220 ms sink disturbance | 174/180, 14.463 fps | Not measured in same final matrix |
| 70 ms + one 220 ms sink disturbance | 87/180, 7.174 fps | 90/180, 7.440 fps |
| 25 ms + 150 ms upstream jitter each second | 155/180, 12.816 fps | 155/180, 12.816 fps |

The sustained-work run measured 5,859.487 ms queue-to-handoff clock wait and
6,254.715 ms actual sink work for the existing plugin (12,160.254 ms total).
The candidate still waited 5,617.145 ms and worked 6,472.165 ms
(12,136.780 ms total). Ignoring raw DISCONT therefore does not by itself repair
this loss of cadence. No live deployment or smoothness claim follows from it.

## Exact duty-cycle mechanism

Candidate debug trace began at raw PTS 27.594 ms, local running time 28.895 ms,
with target 95.895 ms: a 67 ms wait. The next selected raw PTS was 160.928 ms,
local time 166.554 ms, target 229.228 ms: another 62.674 ms wait. Subsequent
selected PTS values advanced 133.333 ms and repeated this wait. A correctly
clocked source and one-slot latest-picture queue replaced the already-due
picture while the sink was working, retaining a newer picture whose target was
again in the future. The candidate retained the initial min-offset estimate;
anchoring that same estimate before the queue alone would not change this
fixture. Sink work plus wait occupied almost the entire elapsed interval.

This proves an algorithm/queue interaction under controlled load. It does not
prove the Pi's actual KMS render-only duration: production sink return includes
clock waiting and scheduling. The large pre-depay backlog has separate
[exact frame and backpressure evidence](linux-shared-frame-backpressure-2026-10-12.md).

## Reproduction

Compile `client/gst/queue_pacing_probe.c` against GStreamer. Run with the desired
plugin directory through GST_PLUGIN_PATH. Options are `--work-us=70000`,
`--jitter-us=0` and `--slots=1` (only one or two slots permitted).
It reports whole-picture counts, local discontinuities, actual sink work,
queue-to-handoff wait, and source-PTS age at handoff. The optional
`--min-frames=N` supports a candidate regression gate. Synthetic pixels are 1×1
RGB, so this does not benchmark decoder or image-processing capacity.
The lifecycle candidate is intentionally not deployed or committed as a cadence
fix. Two slots with the SAME allowance are the next evidence gate; no latency
benefit is assumed before measuring actual handoff age and drops.
