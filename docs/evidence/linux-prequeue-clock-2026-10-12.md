# Decoded-arrival clock and bounded presentation queue

**Historical prototype evidence, not a released playback fix.** The associated
runtime and dependent fixtures are preserved on
`experiment/linux-arrival-admission-20261013`, separate from `main`. The tests
below did not establish sustained hardware playback or fix the whole-path
backlog. Failed live candidates were rolled back; these sections describe their
test windows, not the current deployment. The later original-client Ethernet
baseline is documented in [the wired test](linux-original-client-ethernet-2026-10-13.md).

## Root mechanism and scope

An isolated native Pi GStreamer fixture reproduced the presentation duty loss:
15 fps live input, a bounded decoded queue, actual eufypacing, a sync sink with
production timing (processing deadline zero, lateness ceiling 67 ms), 70 ms
synthetic sink work and one 220 ms disturbance. Raw source-PTS age is measured
at handoff, not inferred from output count. This is not camera capture age or
physical scanout. [Earlier falsification](linux-pacing-queue-falsification-2026-10-12.md)
explains why ignoring local DISCONT or adding a second slot alone is insufficient.

The old mapper samples its rolling offset after the raw queue, so downstream
waiting/work becomes part of its source-clock estimate. One latest-picture slot
also discards an already-due picture while retaining the next future picture.
When admitted, that picture receives another future target. Local queue DISCONT
then also resets the clock. These interactions manufacture waiting under load.

The candidate captures only PTS and local running-clock arrival **before** the
raw queue. A fixed 256-entry per-channel registry carries that observation to the
pacer without modifying or mapping GstBuffer memory. The pacer samples source
offset from this arrival and clamps due targets to current time. A general admission element retains at most two queued raw pictures: the latest
already-due picture and nearest future picture. If all are future it protects the
nearest and retains the newest; if all are due it retains the latest. A downstream
renderer can separately own one buffer. The policy has no 15 fps assumption.
The allowance remains 67 ms; no extra startup hold is added. Actual SEGMENT or
FLUSH_STOP restarts the timeline; local raw DISCONT cannot defeat PTS ordering.
All compressed references still reach the decoder. Decoder preference, resolution,
source quality and missing-plugin fallbacks are preserved.

## Native controlled comparisons

All rows use 180 synthetic 1×1 RGB pictures on the actual Pi, genuine Gst queue,
exact production sink timing, and the same 67 ms allowance. Fixture setup tests
software scheduling, not decoder capacity. Source-PTS handoff age is distinct
from the bridge-ingress age measured on real feeds.

| Configuration: 70 ms sink work | Output / fps | Aggregate clock wait | Handoff age median / maximum |
| --- | ---: | ---: | ---: |
| Existing clock, one slot | 87 / 7.174 | 5,858.736 ms | 101.166 / 134.248 ms |
| Existing clock, two slots | 95 / 7.822 | 5,325.112 ms | 159.626 / 200.307 ms |
| Lifecycle correction, one slot | 90 / 7.440 | 5,622.330 ms | 82.441 / 99.252 ms |
| Lifecycle correction, two slots | 103 / 8.475 | 4,780.994 ms | 147.921 / 169.992 ms |
| Actual prequeue anchor, two slots | 168 / 13.981 | 113.521 ms | 99.393 / 133.860 ms |

Actual sink work in the final row was 11,947.206 ms across 12,121.216 ms;
output approaches the work-limited 14.286 fps instead of halving capacity through
clock waiting. The final row uses the actual metadata-only eufyarrival element,
not the earlier test-only DTS transport experiment.

| 25 ms sink work with periodic source delay | Existing clock / two slots | Actual anchor / two slots |
| --- | --- | --- |
| 150 ms each second | 166 / 13.731 fps; age 152.269 / 173.374 ms | 167 / 13.827 fps; age 67.600 / 151.500 ms |
| 250 ms each second | 144 / 11.803 fps; age 145.163 / 252.659 ms | 155 / 12.710 fps; age 67.590 / 251.677 ms |

The actual-anchor tests reported zero sink latency, processing deadline and render
delay. A source pause still causes a corresponding output gap: measured maxima
were 220.613 and 251.655 ms, including deliberately injected disturbances.
No attempt was made to conceal that pause by replaying or relabeling old pixels.

## Verification and deployment boundaries

Full Go suite passes. Pure C tests cover sampled arrival excluding downstream
work after initial samples age beyond the two-second window. Native lifecycle
probe rejects a regressing timestamp even when raw DISCONT is set and accepts
restart after a genuine SEGMENT. Native metadata integration verifies exact
original picture identities, encoded decoder-chain return timing, pacing targets,
and the identical retained GstBuffer plus NO_SHARE GstMemory through both the
arrival anchor and observer. No raw pixel copy is permitted by these elements.
Old installed plugins retain the existing one-slot pipeline unless both new
factory and compatible pacer property are detected.

## General admission and hardware test failures

The generic two-slot queue was insufficient at 30 fps: with 70 ms sink work it
produced 60/180 pictures (9.828 fps), with 1,762.8 ms accumulated clock wait.
The bounded due/future admission instead produced 82/180 (13.538 fps), 23.114 ms
clock wait, and median/maximum source-PTS handoff age 93.012/133.661 ms. At 15 fps
it produced 160/180 (13.414 fps), 44.570 ms clock wait and age 99.167/132.995 ms.
With 25 ms work and periodic 150/250 ms source delays, the 30 fps admission rows
produced 94/72 pictures, with age medians 67.791/67.949 ms and maxima
151.435/251.456 ms. These are scheduling fixtures, not actual camera age proof.
Native tests passed finite lone-frame delivery without a following picture,
blocked downstream FLUSH, SEGMENT and CAPS ordering, EOS drain, stop/restart,
and nonlive asynchronous preroll. The allowance remains 67 ms.

A retained NO_SHARE fixture proves the generic parent transform can copy raw
memory when it needs a writable header. The candidate uses a fresh header with
identical GstMemory references and GstParentBufferMeta retaining pool ownership;
identity, independent timestamps and parent lifetime are verified. Actual Pi
hardware startup samples, however, had writable headers, dmabuf memory, input
reference count one and identical memory through the original transform. Thus
raw copying is **not established as the actual hardware cause**.

An initial diagnostic run failed because its private output directory was owned
by root while the service runs as wall. It emitted no hardware pictures and is
excluded. Diagnostic open failures now disable observation while preserving
playback; a native invalid-path test verifies this. The corrected original-clock
startup showed Garage decoded output at 7.52 s and Front Door at 9.57 s; the old
10 s KMS gate was too short and yielded no valid steady comparison.

The first candidate actual-frame-gated 45.206 s run failed: both pipelines reached
32 KMS returns, then the 15 s watchdog restarted them. Maximum observed ingress
age upper bounds reached Front Door 18.204 s and Garage 18.636 s (clock uncertainty
up to 0.891 s). The candidate also performed a synchronous upstream LATENCY query
from its asynchronous output thread every five seconds. This diagnostic could
invert decoder stream-lock/CAPS ordering, but there is no stack trace or
query-entry/return pair proving that explanation. The next A/B removes only that
query and preserves every pacing/admission setting; actual downstream sink
latency was already measured as zero. The query-free A/B **falsified this hypothesis**: both pipelines again stopped
at exactly 32 KMS returns. At the 15 s progress gate their last output was already
8.8/9.1 s old; the controller failed fast and restored OFF/inactive/disabled.

The actual newly introduced ownership error is in `prepare_output_buffer`: its
writable branch returned `gst_buffer_ref(input)`. GstBaseTransform does not unref
its input when output is the identical buffer. Its standard writable branch
assigns `*outbuf = inbuf` without increasing the reference. Consequently every
actual writable decoded picture retained one leaked reference, consistent with
32-picture pool exhaustion. A native regression with the failed plugin observed
64 writable pictures and **zero** finalizations after EOS and pipeline teardown.
The correction transfers the same pointer without the additional reference;
shared-header parent ownership remains unchanged. The identical native post-fix regression observed 64 writable pictures and
64 finalizations; retained NO_SHARE memory identity and parent-buffer lifetime
also passed. The corrected live candidate (plugin SHA-256
`bb897df6853083d9479941c37d68f0735fc689a4913635de4639a358164cd15a`;
client binary `5420f3e8f8d2ec2d66584f2f580f9bd404a07f0e52233ece5b98f796d25f6971`)
passed the progress gate beyond 32 pictures and completed 45.078 actual-frame-gated
seconds without a child restart. Exact KMS identities advanced throughout:
Front Door 508 returns (11.269 fps), Garage 377 (8.363 fps); decoder-output counts
were 688/466 (15.262/10.338 fps). This proves removal of the newly introduced
ownership stall, not resolution of the older client backlog.

The Garage ingress-age upper bound at depay increased from 2.624 s to 16.859 s
(maximum 17.326 s); KMS maximum was 17.507 s. Front Door depay median/maximum were
4.175/6.073 s and its final age was 1.939 s. Exact same-frame depay→KMS medians
were 183.568/181.374 ms for Door/Garage. Parsed-AU→decoder-chain-return medians
were 29.700/35.582 ms, p95 123.199/124.009 ms, maxima 281.618/339.170 ms.
The substantial Garage delay was already present before depay. Synchronous
per-frame observation has not been proved neutral on this single-core machine;
a diagnostic-disabled comparison is needed before attributing all throughput
cost to the production path.

The controller restored OFF, service inactive/disabled and removed its runtime
override. Physical panel power during playback was not queried because CEC was
busy; hardware decoder output and active DRM overlay planes were verified,
while actual pixel scanout remains unverified. Private metadata artifacts:
`linux-ownershipfixed-45-{frames,health}.jsonl` and journal. Actual-frame window
Pi CLOCK_MONOTONIC 183635172710852–183680250612626 ns; restored at
183706067196876 ns. Wall-clock labels are not substituted for this clock proof. See [GstBaseTransform source](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gstreamer/libs/gst/base/gstbasetransform.c).

## Diagnostic-disabled comparison

The same candidate completed another 45.092 actual-frame-gated seconds with
per-frame timing diagnostics disabled; automatic stamp/clock observation and
five-second aggregates remained enabled. No source, resolution, decoder or
pacing setting changed. Aggregate counts advanced 77→570 for Door and 77→550 for
Garage over approximately 41.2 s (about 11.96/11.47 fps). Cumulative KMS ingress-age
upper maxima were 2.864/3.237 s (Door/Garage), substantially below the previous
6.217/17.507 s. Final cumulative decoded-age histogram medians were 0.675/1.425 s;
these are coarse cumulative buckets, not each picture's latest age. In the last
aggregate interval, Door 37/52 and Garage 9/49 pictures had decoded upper age
within 500 ms. Neither feed is proven consistently within the requested budget.

This is suggestive of instrumentation cost, not a controlled producer exclusion:
the notification to align the independent Mac observer was missed, and there is
no simultaneous exact-frame join for this window. Controller CPU was measured:
5.286 CPU seconds over 45.092 elapsed seconds, approximately 11.7% of the Pi's
single core. Its 200 ms DRM-state parsing is therefore not neutral. The next
comparison reduces only diagnostic polling to five seconds; production playback
policy remains identical. Per-frame diagnostics also perform synchronous
line-buffered writes and per-record fstat. Their cost is not assumed harmless.
OFF/inactive/disabled and override removal were again verified after the sample.

## Reduced-monitor short result

With per-frame diagnostics still disabled and the same candidate, DRM/controller
polling was reduced to five seconds. The intended 45-second gate ran 49.460 actual
seconds because the final five-second sleep overshot the target. Setup is excluded.
Controller CPU was 0.912 seconds over that window (1.84% of one core), compared
with 11.7% in the previous run. It remains a measured observer, not zero overhead.
The last available aggregates span 43.23 seconds: Door 78→586 returns (11.75 fps),
Garage 71→549 (11.06 fps). Final cumulative decoded-age medians were 650/725 ms;
KMS upper-age maxima, including startup, were 3.371/1.813 seconds. In the final
aggregate interval only Door 25/71 and Garage 28/47 decoded frames had upper age
within 500 ms. Playback is not proven consistently smooth or within the deadline.

The simultaneous Mac observer received all 885/903 stamped Door/Garage frames
within its 60-second window with upper ages below 500 ms. The first Pi aggregate
picture IDs were outside that window; the next seven aggregate IDs per camera
match the exact same epoch/picture IDs at Mac. Their Mac receipt upper ages range
58.739–249.340 ms for Door and 56.199–144.162 ms for Garage. This validates producer
freshness for those pictures, not an exact Pi age for each one: the Pi log exposes
cumulative age statistics rather than the final picture's individual age. Full
49.460-second observer overlap is not claimed.

The controller completed normally and restored OFF/inactive/disabled, with its
runtime override removed. Private artifacts are `linux-lowobserver-45-health.jsonl`
and `mac-lowobserver-60s.jsonl`. The next bounded isolation compares Garage alone
with the two-camera run at identical native dimensions and hardware decode;
original selection and power policy must be restored afterward.

**Real candidate smoothness and whole-path latency remain unverified.** The separate Pi-specific
pre-depay backlog previously reached >12 seconds while the identical picture
arrived fresh at Mac. This pacing correction cannot claim that whole-path latency
is fixed. [Exact picture evidence](linux-phase-observer-2026-10-12.md) locates that
backlog; the next live run must measure receiver/decoder-chain blocking and actual
hardware output, memory allocation errors and ingress-age tails simultaneously.
The synthetic 30 fps scheduling gate passes; actual hardware capacity is not
inferred from it. Capture time and physical scanout remain unverified.
