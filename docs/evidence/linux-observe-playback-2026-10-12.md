# GaragePi hardware playback and observe-only timing — 2026-10-12

## Scope and saved state

Target `192.168.23.73` is a Raspberry Pi Model B Rev 2 (`armv6l`), running
GStreamer 1.26.2 with the hardware `v4l2h264dec` factory and `/dev/video10`.
Hardware HEVC factories are absent; both existing compatible H.264 resources
remain selected. Camera quality and client configuration were not changed.
The initial TV state was OFF, HDMI connected, client service inactive and
**disabled**. Temporary playback uses only the existing `/home/dietpi/tv-power.sh`
for power and starts the service without enabling it. A finally block and a
separate 12-minute systemd cleanup timer restore OFF/stopped/disabled.

Wall values below are **Pi-reported UTC**; NTP is disabled and its wall clock
has an approximately twenty-second offset from the Mac. Relative durations use
CLOCK_MONOTONIC. No video-age calculation uses these wall timestamps.

## Existing-code baseline

Sample: **18:05:53.119–18:15:53.132 UTC**, 600.012 seconds. Binary SHA-256:
`eba236cba7382d3b1767936b6331814711bd1aef443294e6c155762bad9ddf61`.
Native pacing plugin SHA-256:
`c5b0e3cb7086697aec3bc258e9eedf7797510ad2455aad8d65365eb991214e8f`.

Metadata-only sampling every 200 ms read DRM plane framebuffer IDs. Every five
seconds it checked service status, actual process command lines, hardware device
FDs and the HVS underrun counter. All 117 sampled service states were active,
with actual V4L2 `/dev/video10` and DRM descriptors in the camera processes; HVS
underruns remained zero. Plane 98 (Front Door) showed 991 sampled framebuffer-ID
changes; plane 109 (Garage) showed 1104. Repeated IDs are buffer-pool reuse:
maximum sampled same-ID intervals of 3.434 and 3.317 seconds are **not measured
freezes, latency, or evidence that pixel contents did not change**.

This is not an uninterrupted-playback pass. At 18:15:06 UTC Garage's GStreamer
RTSP source reported `Could not receive message. (Parse error)`. Its supervisor
restarted the process at 18:15:08 and hardware decoding resumed. Front Door kept
its original process. The restarted Garage decoder also reported decreasing PTS
and `24 initial frames were not dequeued`. The cause of that RTSP parse error is
not established by the metadata observer.

No DRM CRC interface was exposed. A WAIT_VBLANK ioctl query returned EINVAL,
so this run does not establish actual physical scanout or glass-to-glass age.
The only earlier user-visible physical confirmation belonged to older sessions.
A growing JSON artifact was accidentally analyzed on the single-core Pi around
sample seconds 254–265; this observer-induced CPU interval is a measurement
limitation. Subsequent analysis is performed on the Mac after capture.

Restoration was verified at 18:16:12.597 UTC: TV OFF, service inactive, disabled.
Private artifacts: `.evidence/frame-freshness-2026-10-12/linux-physical-baseline.jsonl`
and `linux-baseline-journal.txt`; these contain metadata, not video pixels.

## Observe implementation and regression proof

The updated plugin adds `observe-only=true`. It associates the custom encoded
stamp's original PTS with the decoded buffer, measures conservative clock-mapped
bridge-ingress age after the existing one-buffer decoded queue and after the
synchronous KMS sink call returns. It retains the existing pacing and KMS sync
policy. No expiry thread is created and no plane is cleared in this mode.
Expired, missing or unknown-clock frames still display. Bulk cumulative counters
are emitted every five seconds, rather than logging every frame.

The native Pi build passes `-Wall -Wextra -Werror`, pure C pacing/freshness tests,
and both GStreamer integration probes. The observe probe delivered all four
raw buffers, including expired/missing/invalid-clock examples, with no expiry
message (`matched=2 missing=1 unknownClock=1 expiredUpper=1 accepted=4
pacingDropped=0`). The separate strict probe still verified suppression,
no-input expiry, and fresh recovery without pipeline restart. All Go packages
pass their tests, including exact selected-URL observation binding and graceful
legacy fallback when the installed plugin lacks the new property.

The age scope remains **bridge ingress**, not calibrated camera capture. The
clock bound includes request RTT plus a conservative 1000 ppm projection
allowance. KMS sink return is a software boundary, not verified scanout;
reports explicitly say `scanoutVerified=false`.

Installed while the Pi was OFF/inactive, with backups under
`/var/backups/eufy-wall/observe-linux-20261012/`:

- ARMv6 binary: `3ba96a199f268c3b79d7fb432bb9e2dcec4f8a5b8b0daf75e4eca9a627fb23eb`.
- Native plugin: `d80ce034039c7388b572b4a482973441ed80e842974a717191fa2117a4f32e10`.

A premeasurement startup attempt was interrupted to allow the coordinated SDK
restart. It never reached the sample-start gate and is excluded from playback
proof; OFF/inactive/disabled restoration was checked.

## Updated-code sustained run

Sample: Pi-reported 18:25:38.446–18:35:38.658 UTC, **600.212 seconds**.
The live bridge SDK runtime SHA was
`4ddbf934ccab2ebee30832fdacb7a1874e308278f3db2f41aa98503b6e938cf4`.
The new SDK had already restarted and warmed before this run; there were no
SDK/server/camera changes within it. This is not a controlled plugin-only A/B.

Both compatible H.264 processes kept the same PIDs (31200 and 31201) in all
117 health samples, used V4L2 `/dev/video10` and DRM FDs, and produced progressing
plane framebuffer IDs. HVS underrun stayed zero. The full client journal contains
no RTSP fatal error, process exit or supervisor restart in this run. Exact stamp
matching worked for every observed raw buffer; missing metadata, unknown clock,
reordered frame IDs and pacing drops were all zero. The counts include the
pre-sample startup because the existing power helper starts the client while
CEC verification proceeds.

| Observed software boundary | Front Door | Garage |
|---|---:|---:|
| Decoded-after-queue buffers, all matched | 5,203 | 4,907 |
| Cumulative age upper P50 bound | 425 ms | 650 ms |
| Cumulative age upper P95 bound | 1,325 ms | >2,500 ms overflow bucket |
| Maximum age upper at decoded-after-queue | 3,531.437 ms | **12,527.801 ms** |
| Maximum age upper at KMS sink return | 3,737.226 ms | **12,598.098 ms** |
| Observed age upper above 500 ms | 2,097 | 3,113 |
| Longest raw-buffer arrival gap after queue | 2,754.365 ms | 3,356.570 ms |
| Maximum synchronous sink-call duration | 307.963 ms | 331.708 ms |
| Maximum clock interval uncertainty | 851.675 ms | 848.158 ms |

Histogram buckets are 25 ms wide through 2,500 ms; the overflow bucket returns
the maximum as a conservative percentile bound. Garage's printed P95
12,527.801 ms therefore **does not mean the actual 95th percentile equals the
maximum**. This run proves a substantial age tail, not a precise tail quantile.
The clock uncertainty cannot explain that tail: subtracting its global maximum
from Garage's maximum upper age still gives a lower age bound **above 11,679 ms**
under the clock model.

The first cumulative report showing the Garage maximum was Pi wall
18:28:26 UTC, with 1,471 raw buffers observed. Its last frame ID was 30,435;
that is the report's last buffer, **not an identified maximum-age frame**.
Next-stage instrumentation must retain exact per-frame phase identities. During
later approximately five-second report windows, raw count deltas ranged
24–65 for Front Door and 18–58 for Garage; some windows had every observed
upper age exceed 500 ms. These are decoded-after-queue counts, not source FPS or
an encoded packet-loss metric. The current cumulative histograms do not provide
exact per-window age quantiles.

### Clock-rate validation

A separate persistent process on the Pi made metadata-only HTTP `/api/clock`
requests to the same bridge every five seconds for 180 seconds, recording local
request/response CLOCK_MONOTONIC and server monotonic microseconds. There were
34 valid same-epoch samples, minimum RTT 10.146 ms. The tightest pair spanning
140.053129 seconds, with endpoint RTTs 13.396 and 10.146 ms, bounded the Pi's
relative monotonic rate versus the bridge to **[-554.745, -386.621] ppm**. Bounds
include request/response intervals and an additional two-microsecond allowance
for server timestamp quantization. This falls within the model's conservative
1000 ppm projection allowance for the observed workload. It does not prove
all future clocks, power states or workloads satisfy that bound. Two independent
SSH samples were too uncertain (multi-second RTT) to assess that rate; wall-clock
offset and disabled NTP alone were not evidence of monotonic drift.

### What passed, what remains

Hardware decoder use, sustained driver-plane progression, exact metadata
association and clean process continuity were verified. **Low latency was not
fixed**: Garage's measured bridge-ingress-to-software-output tail exceeded eleven
seconds even after clock uncertainty. Its origin is still upstream of this
post-decoded-queue point; independent RTSP/depay/parser/decoder/KMS timestamps
are needed to distinguish producer delay from compressed decode backlog.
Physical pixel smoothness and confirmed scanout remain unverified without a
physical observer or usable DRM CRC/vblank evidence.

Restoration at Pi-reported 18:35:58.619 UTC was verified OFF, inactive and
**disabled**, with the cleanup timer stopped. Both finite capture processes and
the clock sampling connection closed. Camera quality/configuration remained
unchanged. A later CEC status poll could return busy while the existing power
helper completed its request; busy was never treated as an OFF confirmation.

Private artifacts:

- `linux-physical-observe.jsonl`: bounded hardware/plane metadata.
- `linux-observe-journal.txt`: full native logs and cumulative stamp statistics.
- `linux-observe-stats.json`: parsed cumulative statistics.
- `linux-bridge-clock-rate.jsonl`: 34 clock request/response samples.
- `linux-observe-clock-rate.json`: broad-RTT SSH samples, not drift proof.

Normal matched stamps precede the first VCL NAL. The native stamp reader scans
encoded headers and returns at that prefix SEI, without scanning/copying decoded
pixels or the VCL body. Missing/nonmatching metadata can cause a full encoded-AU
scan; missing=0 throughout this run means that fallback overhead was not tested
on the Pi workload.


### Private artifact digests

- `linux-physical-baseline.jsonl` SHA-256 `8909134afb072fee878e006d8383ff2a21f44fde1f222b2cbeca66b58145a152`.
- `linux-baseline-journal.txt` SHA-256 `9b103126129782e62e56ee18a9f9ef1ebb08c1b12b53d00e6cd11d103217d395`.
- `linux-physical-observe.jsonl` SHA-256 `44b50e0f16f1cce7e65a4484d9624d8eff69b7af20f297da500b5d088adbeb04`.
- `linux-observe-journal.txt` SHA-256 `118cdc927285d7ce1e4956b6e75a34392c8cbe4b38b1139c8c03c9111288b515`.
- `linux-observe-stats.json` SHA-256 `bd5778b739a326209233e8cfe95ff6cc8bf4031a34fd5b62032dda0882584c58`.
- `linux-bridge-clock-rate.jsonl` SHA-256 `3e82e995f47c94a145f19a8ca121868a55e17106f42a9d60a5784029063ae7bd`.
- `linux-observe-clock-rate.json` SHA-256 `311273a0fe250dbbdc7bab81f0c33a226d37fe5f329c41444ea963051e2b5678`.
