# DietPi Zero W client deployment and keep-up test

## Target and installation

`dietpi.lan` (`192.168.23.128`) is a Raspberry Pi Zero W Rev 1.1, ARMv6, Raspbian 13/DietPi, kernel `6.18.50+rpt-rpi-v6`. HDMI reports a connected 1920×1080 display. GPU split remains **64 MB**, CMA total 256 MB.

Installed the current Linux client with hardware-first/software fallback, per-camera hardware pins and ranks. The Go ARMv6 binary was built on the Mac from clean revision `7e6e164780cb0327ec17839f5c5227da66e49a02`; SHA256 `c5d8529badc240d03918942905a2bbf555b094f7ea163baf2513e41512767fe3`. The native ARMv6 plugin was cross-built off-device; SHA256 `1d465453643cbc31bf18ba1a6618659718dffa1790c670cda52d20f9a9a2207c`. No compilation ran on the Zero. Installed GStreamer 1.26.2 runtime packages and software fallback libraries. Temporary package-index decompression was restored after installation; no persistent APT policy change.

Copied GaragePi's Front Door CLE / Garage CLE side-by-side layout, pins and ranks, `rtsp://homebridge.lan:8554`, `decoder: auto`, `sink: planes`, plane IDs 98/120 and zero RTSP buffering. Both RTSP resources retain `pkt_size=8192`. The client's black DRM backdrop is enabled. No bridge/transcoding/quality settings changed, and GaragePi was not stopped or modified.

Installed/enabled `eufy-wall.service`, HDMI-connected startup wait and writable private cache/state directories. Registered `+ eufy-wall` with DietPi Services. The target exposed an existing service-file defect: `StartLimitIntervalSec` was placed in `[Service]` and ignored. Moved it to `[Unit]` and validated the deployed unit.

## Test method and scope

Keep both cameras running unchanged for five minutes. Sample existing CPU, process and socket counters every five seconds; use existing `eufy-observe` logs and bridge API counters. Warmup excluded for CPU/drift comparisons. Active `/dev/video10` descriptors plus `v4l2h264dec` factories and advancing output counters establish hardware decoding.

Frame age is measured from **bridge ingress to KMS sink return**, with clock uncertainty included. It does not establish camera capture age or physical scanout. Source/accepted cadence comparisons cover aligned time windows; the native observer's all-frame histogram is distinguished from occasional latest-picture samples.

## Result

The unchanged-playback run lasted **300.004 seconds**, with 61 five-second host/API samples.

- Both cameras used `v4l2h264dec`, active `/dev/video10` descriptors and DRM/DMA-BUF resources. No software fallback, decoder-allocation failure or throttling occurred (`throttled=0x0`). Leave GPU split at 64 MB based on this run.
- Median total CPU busy was **56.1%**, with p95 **93.8%** and a 100% peak. Available RAM stayed above **320 MiB**, and swap use did not grow. Maximum temperature was **51.9 C**. Peak load reaches full CPU capacity.
- No multi-second age accumulation was observed. Latest-picture bridge-to-KMS age upper-bound sample medians were **146 ms Front Door / 161 ms Garage**. Their sampled p95 values were 468/342 ms; these are five-second snapshots, not all-frame percentiles. Final native all-frame KMS age histogram p95 was **325 ms Front Door / 375 ms Garage** (Garage's final decoder lifetime). Maxima were 881 ms / 2051 ms, so the result is not an every-frame sub-500 ms guarantee.
- Front Door accepted **10.45 fps** versus bridge **15.04 fps**, about **69.5% retention** across the sustained window. Garage accepted about **11.85 fps** over active decoder-report intervals; whole-run retention is confounded by source counter resets. Matched short pre-interruption windows retained approximately 73% Front Door / 82% Garage. Full-cadence smoothness therefore **does not pass**. Source code/counters locate missing pictures before pacing admission; they do not distinguish decoder QoS from the bounded decoded queue. No new buffering or quality reduction was applied.
- Service main PID stayed **5179**, restart counter stayed **0**. Front Door decoder stayed **5239**. Garage decoder restarted once, from **5240** to **5732**, after its existing 15-second input watchdog.
- The Garage interruption was visible at the bridge: last-source-receipt age progressed through **3.711, 8.764, 14.011, 18.819, 23.886 and 28.829 seconds** while bridge output stopped/reset. Fresh receipt resumed by sample 215 seconds; Front Door continued throughout. This establishes an upstream input interruption rather than a Pi queue holding 30 seconds of video; it does not establish the upstream cause.
- Sampled Pi RTSP receive queue peaked at **242045 bytes**, then drained; a paired bridge snapshot had zero send backlog on both sockets. Bytes are corroboration, not duration.

**Decision:** installation and hardware decoding pass; no growing Pi backlog was observed. Full-cadence smoothness and uninterrupted Garage source delivery do not pass. Keep the current 64 MB setting: no measured allocation failure justifies increasing it. The service remains enabled and running, and DietPi Services reports it active. Physical viewing quality remains unverified.

Committed measurements: [summary JSON](data/dietpi-zero-w-20261021/result.json), [host/API samples](data/dietpi-zero-w-20261021/samples.csv), [native observer reports](data/dietpi-zero-w-20261021/observer.csv), [hardware/DRM state](data/dietpi-zero-w-20261021/hardware.txt) and [restart events](data/dietpi-zero-w-20261021/events.txt). Raw local artifacts: `/tmp/eufy-dietpi-test-20261021/`.
