# Off-device admission build: live trial rejected and reverted

## Hypothesis and change

The held decoded-admission prototype has a reproduced recovery race: a
pre-flush downstream return can overwrite the resumed generation's flow.
The generation correction passed the native GstPad fixture. This is not proof
that the race causes ordinary playback jitter or backlog.

The candidate was crosscompiled off-device using ARM1176/VFP hardfloat flags and
the Pi's exact userspace headers, libraries and startup objects. The final ELF
has ARMv6KZ/Thumb1/VFPv2 attributes; an earlier ARMv7 startup-object artifact was
rejected. No compiler ran on the Pi during the live comparison.

Candidate SHA-256:
`fdfdc5749b42660538c9fe468456e8988d2f59c5c03e20a353c614c40f4ab3c0`.
It was built from the five current held source files. The exact installed
`0b54` source was unavailable, so this is **not proven to be a generation-only
binary replacement**. The complete prototype remains unreleased.

The native fixture ran on the Pi and passed: earlyReopen=0,
flowAfterFlushStop=0, eosEvents=1, stopEvents=2, resumedBuffers=2. Fixture SHA:
`4f859e42258055b0696c1eedfcec23dd51c22e3dc35b36dc8e131f8420a7b509`.
Private plugin inspection succeeded before the playback observation. Both live
GStreamer processes mapped the installed candidate and used v4l2h264dec,
eufyadmission and kmssink on planes 98/109. Only the client was restarted.
Configuration, quality, layout, hardware/fallback policy and boot enablement
were unchanged. Original binary backup was retained before atomic installation.

## Short live result and decision

Measurements below use each camera's actual first-to-last aggregate monotonic
subwindow, rather than assuming identical wall-clock intervals. Deadline misses
are decoded-frame upper-age counter deltas; age means SDK ingress to KMS return,
not verified capture-to-screen latency. Latest samples are sparse, not all-frame
window maxima. Physical scanout is unverified.

| Camera | Stock baseline fps | Candidate fps | Baseline decoded deadline misses | Candidate misses | Baseline latest-sample upper max | Candidate latest-sample upper max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Front Door CLE, T8214510242321E6 | 13.27 | 13.18 | 29 | 39 | 555 ms | 1,730 ms |
| Garage CLE, T8425T1124130A0C | 14.74 | 14.27 | 11 | 125 | 677 ms | 4,838 ms |

Baseline observed deltas were 744/817 pictures over 56.047733/55.415670 s;
candidate deltas were 815/872 over 61.851291/61.090931 s. Baseline had no clock
errors or unknown-clock increments. Candidate had two HTTP clock timeouts and
unknown-clock increments 2/3. The Garage 4,838 ms sample was valid, with lower
bound 4,817 ms: clock uncertainty does not explain that backlog. No warnings or
counter regression occurred in the candidate observation. This sequential,
short comparison does not isolate source/network changes or prove the candidate
caused the regression. It does show no usable playback improvement.

**Decision: reject this playback candidate and revert**, as requested for changes
that do not help. Rollback completed with exit 0 and restored SHA-256
`0b54bb70ac0c78be1e74c2b2f9c931f571f9e3599f437ca2166cdb27a76b8462`.
The generation race remains documented/tested independently; it is not claimed
as the live playback fix. No SDK or kernel change was deployed.

Next bounded decision: determine whether the Pi RTSP reader has unread socket
backlog while blocked downstream, or is waiting on an empty socket. Use the
existing short metadata-only reader probe, compiled off-device, with its measured
observer-cost gate. Do not expand diagnostics without that decision.

Private artifacts under `.evidence/frame-freshness-2026-10-12/`:
`linux-postcompile-info65{.txt,.status.json}`,
`linux-cross-fixed-info65{.txt,.status.json}`,
`linux-cross-fixed-deployment.json`, `linux-cross-fixed-reverted.json`.
Native cross-build and fixture records are preserved under `/tmp/ewb-gst-cross/`.
Sustained smooth playback remains unverified; this trial did not achieve it.
