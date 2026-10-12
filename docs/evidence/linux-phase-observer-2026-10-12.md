# Linux compressed-to-KMS phase observation

## Contract and observer scope

The opt-in `-timing-diagnostics /absolute/path` records metadata at depayload
completion, parsed AU, hardware decoder output, downstream raw queue output, and
KMS sink return. It changes no queue sizes, quality, pacing or expiry policy.
It preserves caps negotiation and accepts both byte-stream and length-prefixed
encoded formats. NAL-aligned depayload completion uses the actual RTP marker;
it does not wait for a subsequent picture. Metadata uses exact bridge epoch,
frame ID and original PTS. The shared file is bounded to 64 MiB and each element
to 40,000 rows; failures disable observation while playback continues.

Raw callbacks never map pixels. The final observer uses true BaseTransform
passthrough, with inspection enabled in passthrough. The native integration test
retains a non-writable input buffer containing NO_SHARE memory and requires the
same GstBuffer and GstMemory identities at the sink. This prevents the observer
framework from copying decoder memory merely to make it writable. GStreamer
[documents this distinction](https://gstreamer.freedesktop.org/documentation/base/gstbasetransform.html).
The live run below predates that final passthrough improvement; consequently
its observer overhead cannot be claimed zero. An earlier run without per-stage
instrumentation already measured a Garage age lower bound above 11.679 seconds.

## Actual 600-second run

Pi-reported wall interval: 19:06:34.188–19:16:34.266 on 2026-10-12. Wall time was
unsynchronized; duration was measured with CLOCK_MONOTONIC: **600.078 seconds**.
The metadata file contained 73,009 rows / 22,708,842 bytes. It was exported and
analyzed after playback stopped. SDK runtime SHA remained
`4ddbf934ccab2ebee30832fdacb7a1874e308278f3db2f41aa98503b6e938cf4`.
Bridge epoch was `aa064658bab11fe0`; no source-quality change occurred.

| Boundary | Front Door count | Garage count |
| --- | ---: | ---: |
| Depayload completion | 9,188 | 9,138 |
| Parsed picture | 9,188 | 9,138 |
| Hardware decoded output | 9,180 | 9,136 |
| Existing raw queue output | 4,327 | 4,694 |
| KMS sink return | 4,327 | 4,693 |

All records had exact stamps. Clock bounds were temporarily unavailable for
some records; those are explicitly unknown, not counted as fresh. Decoder
output was approximately 15 fps per feed while the existing downstream queue
dropped roughly half. This is not evidence of an 8-fps hardware decoder limit.

| Paired boundary elapsed time | Door median / p95 | Garage median / p95 |
| --- | ---: | ---: |
| Depayload → parsed | 1.310 / 38.736 ms | 1.331 / 56.033 ms |
| Parsed → decoded output | 69.223 / 232.692 ms | 88.993 / 250.013 ms |
| Decoded output → post-queue | 21.491 / 89.468 ms | 18.324 / 88.445 ms |
| Post-queue → KMS return | 84.858 / 150.277 ms | 76.588 / 142.836 ms |

KMS return includes sink clock waiting and scheduling; it does not isolate
render duration or prove physical scanout. Reused DRM framebuffer IDs are not
freeze counters. No decoded pixels were read back.

## Root-cause boundaries

Garage frame **152774**, source word **830656767**, ingress **31818102885 µs**,
was already **12,335.566–12,381.200 ms** old at Pi depayload completion.
It subsequently took 1.400 ms to parse, 118.114 ms to decoded output,
7.318 ms to post-queue and 100.897 ms to KMS return. The concurrent Mac received
this identical stamped picture at **44.641–65.323 ms** ingress age. Door frame
**180861** likewise arrived fresh at Mac (70.080–105.885 ms) but already old at
Pi depayload (8,155.781–9,045.961 ms). These exact joins exclude a shared producer
hold for these pictures and locate the large Pi-specific delay before complete
depayload output. They do not yet distinguish Wi-Fi from RTSP read-chain or
hardware input blocking. See [server backpressure evidence](linux-shared-frame-backpressure-2026-10-12.md).

Presentation cadence is a separate issue: decoded output survives at ~15 fps
but roughly half is dropped before the sink. The sink-return measurement does
not by itself establish why. No queue policy was altered to conceal either issue.
Low CMA free pages were observed but do not prove DMA exhaustion; movable and
reclaimable allocations require separate allocation/blocking evidence.

## Provenance and restoration

Live binary SHA:
`b0068774383f3eac063cbffa82ca18cf552546e26cbb794258f24286e1dbd0ad`.
Live plugin SHA:
`20e4ecd6901084dda8f98db7f6fd3a42c974330343c969344ce88198b6f5330c`.
These identify the pre-passthrough-improvement diagnostic run, not the final
observer source. Backup: `/var/backups/eufy-wall/phase-linux-20261012/`.
Pi was restored OFF, client inactive/disabled and diagnostic service override
removed at Pi-reported 19:17:00.620. The Wi-Fi monitor was left unchanged.
Private artifacts: `.evidence/frame-freshness-2026-10-12/linux-phase-frames.jsonl`,
`linux-phase-health.jsonl`, `linux-phase-journal.txt`; exact Mac joins are in
`compat-mac-joint-600s.jsonl` and `compat-mac-pi-continuation-600s.jsonl`.
Capture-time calibration and physical scanout remain unverified.
