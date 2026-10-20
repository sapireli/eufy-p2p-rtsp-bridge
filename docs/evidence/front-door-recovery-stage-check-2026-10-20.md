# Front Door recovery after source-delivery pauses

## Hypothesis and decision criterion

Test whether the bridge encoder or Android player prolongs a source-delivery
pause after media resumes. A growing encoder queue or matched receive-to-decode
hold would justify investigating that stage. An informational codec render
callback alone does not prove a display hold. No playback code was changed.

## Bridge observations

Two 45-second observations read existing `/api/cameras` state four times per
second, without adding a media consumer, restarting a service, or changing camera
settings. Each window has 180 samples per camera and no observed counter reset.
The status describes the most recently registered compatible consumer for each
camera; it is not a distinct per-TV output trace. Source age is receipt age at
sampling, not validated camera capture age. Sampled maxima are lower bounds.

| Window start UTC | Camera | Maximum sampled source receipt age | Input/output frame deltas | Input minus output range | New expired-reference output frames |
| --- | --- | ---: | ---: | ---: | ---: |
| 18:45:44.986 | Front Door | 1009.702 ms | 695 / 695 | 0–5 | 0 |
| 18:45:44.986 | Garage | 3712.582 ms | 699 / 699 | 0–3 | 0 |
| 18:45:44.986 | Balcony | 65.994 ms | 1120 / 1119 | 1–5 | 0 |
| 18:47:22.992 | Front Door | 2943.268 ms | 627 / 626 | 0–5 | 0 |
| 18:47:22.992 | Garage | 2901.530 ms | 722 / 718 | 0–4 | 0 |
| 18:47:22.992 | Balcony | 53.346 ms | 1121 / 1117 | 1–5 | 0 |

The cumulative maximum egress age did not increase for any camera in either
window. Zero new expired-reference outputs means no newly emitted monitored
output reached the configured 500 ms source-receipt-to-write expiry threshold.
These windows do not demonstrate a sustained encoder backlog, despite source
receipt pauses. They do not measure HTTP delivery to a particular TV.

## Android receive and decode

On Fire TV `.85`, the existing `EufyFrameTiming` DEBUG tag was enabled for one
45-second window starting 18:47:26.794 UTC and restored to INFO afterward. The
app, selections, and decoder pins were unchanged. DEBUG logging adds observation
overhead; this is not an uninstrumented playback benchmark.

Front Door had 670 matched received/decoded pictures. Receive-to-decode median
was 9.558 ms, p95 21.012 ms, maximum 72.412 ms. Maximum consecutive-event gaps
were 1730.664 ms at receipt and 1729.227 ms at decode. This does not show a growing
Front Door decoder backlog in this window.

The codec-provided render timestamps had a 3053.926 ms maximum interval. One
matched picture was assigned immediate presentation after decoding but its
reported render time followed decode by 1788.915 ms, near its successor's target.
This uses the callback's timestamp argument, not callback receipt wall time.
It still cannot distinguish actual presentation holding from vendor timestamp
behavior. The independent SurfaceFlinger comparison below resolves that ambiguity for
matched frames in its observation window.

## Independent presentation comparison

A second bounded capture ran 18:50:19.709–18:51:04.961 UTC. It enabled the existing
DEBUG timing tag and read only Front Door's existing SurfaceFlinger `--latency`
ring every two seconds. No statistics were cleared, app restarted, or playback
settings changed. The tag was restored to INFO in guaranteed cleanup.

The Front Door surface was identified by its leftmost bounds and 800×1080 buffer.
654 output presentation targets matched exactly to SurfaceFlinger desired-present
timestamps. Of 695 decoded records, 41 were unmatched; capture boundaries or
unpresented frames must not be interpreted as proof of their timing.

| Exact-matched timing | Median | P95 | Maximum |
| --- | ---: | ---: | ---: |
| Target to SurfaceFlinger actual-present | 14.386 ms | 32.514 ms | 48.845 ms |
| Output release to actual-present, including planned pacing | 68.020 ms | 82.286 ms | 86.357 ms |

There were 15 incoming gaps over 500 ms whose first resumed picture had an exact
presentation match. Those gaps ranged from 500.383 to 968.917 ms. First-resumed
receive-to-decode time was at most 14.052 ms; receive-to-actual-present was at most
41.339 ms. Thus no extended Front Door recovery was shown in these matched events.

Sixteen matched pictures had codec callback-provided render timestamps more than
500 ms after release even though SurfaceFlinger reported actual presentation
18–37 ms after release. For PTS 468068995, output was released immediately at
21764051686918 ns. Its exact-matched actual-present time was 21764079425960 ns,
27.739 ms later; the codec callback reported 21765031536919 ns, 979.850 ms after
release. Callback receipt batching alone does not explain this discrepancy: the
callback's supplied timestamp itself differs from the independent presentation
record. It must not be treated as actual display latency on this device.

[AOSP FrameTracker](https://android.googlesource.com/platform/frameworks/native/+/refs/heads/main/services/surfaceflinger/FrameTracker.h)
defines actual-present separately from desired-present and frame-ready timing.
This device supplied finite positive triplets; exact target matching avoids using
unrelated presentation records. These are platform timing observations, not an
external optical scanout measurement. They support prompt presentation in this
window without guaranteeing all future playback or every device's implementation.

**Decision:** no encoder, decoder or surface patch is justified by these tests.
The first observation's long codec-render intervals do not demonstrate a physical
hold. The remaining media-delivery pauses still require a source/transport cause;
these results do not identify that cause or fix the original periodic-freeze goal.

## Evidence retention

Private logs and sanitized API samples are retained under ignored
`.evidence/front-door-pause-2026-10-20/recovery/`. No live code or media settings
were modified. Current telemetry included Wyze alongside the three Eufy cameras;
this test preserved that actual selection.

Sanitized result fingerprints:

| Artifact | SHA-256 |
| --- | --- |
| `front-surface-analysis.json` | `5ee61bffd5aabdff1e7a30166b155253077201b41cb188737404f63c3e1bcae9` |
| `front-surface-recoveries.json` | `5c2e05d3b9244638092db5b7f26b8bef40c667c85e4d55ce3b65c016e5155a3f` |
