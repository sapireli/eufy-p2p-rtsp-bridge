# Atom bridge migration: live hardware preflight, 2026-10-08

## Scope

The prospective bridge host at `192.168.23.158` was tested using two already active raw
HTTP camera streams from the production bridge at `192.168.23.199`. These tests did not
start a second Eufy SDK session or stop the production bridge. The first part below proves
that this host can concurrently decode, resize, and encode these two actual camera feeds;
it does not by itself prove a migrated bridge, client playback, or glass-to-glass latency.

## Host and driver

- Intel Atom x5-Z8350, four logical CPUs, 1,886 MiB RAM.
- DietPi on Debian 13.7 (trixie), kernel `6.12.111+deb13-amd64`.
- FFmpeg `7.1.5-0+deb13u1`, libva `2.22.0-3`, render node `/dev/dri/renderD128`.
- `vainfo --display drm --device /dev/dri/renderD128`, using the Intel i965 CherryView
  driver, advertises HEVC Main decode and H.264 Main/High decode and encode.

The initially installed free `i965-va-driver` package advertised these codec profiles but
did not expose `VAProfileNone / VAEntrypointVideoProc`. An actual Garage HEVC decode and
`scale_vaapi` attempt produced zero encoded frames and failed with:

```text
Failed to create processing pipeline config: 12 (the requested VAProfile is not supported).
```

A second attempt to download decoded VA-API frames for CPU resizing also failed before
encoding, with `Failed to read image from surface ... requested function is not implemented`.

Installing `i965-va-driver-shaders` version `2.4.1-2` from the host's existing Debian
`trixie/non-free` repository replaced the free driver. No repository was added. `vainfo`
then exposed the VideoProc entry point, and the concurrent VA-API pipeline below succeeded.
Thus the advertised HEVC codec profile alone was insufficient evidence that the whole
transcode path worked on this installation.

## Actual source streams and command

| Camera | Raw input | Encoded output |
| --- | --- | --- |
| Front Door CLE | H.264 High, 1600 × 2200 | H.264 Main, 524 × 720 |
| Garage CLE | HEVC Main, 1280 × 1440 | H.264 Main, 640 × 720 |

Both sources were read through their existing `/stream/<serial>` endpoints. FFmpeg
identified nominal 25 fps in the headers; actual live delivery was approximately 15 fps.
The benchmark used one process per camera, simultaneously, with these input options:

```sh
-init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va
-hwaccel vaapi -hwaccel_output_format vaapi -hwaccel_device va
-reinit_filter 0 -use_wallclock_as_timestamps 1
-analyzeduration 100000 -probesize 262144 -i <raw-camera-HTTP-URL>
```

And these output options:

```sh
-an -vf scale_vaapi=w=-2:h=720:format=nv12
-c:v h264_vaapi -profile:v main -g:v 30 -bf:v 0
-fps_mode:v passthrough -enc_time_base:v 1:90000
-progress <camera-progress-file> -f null -
```

The hardware input and `scale_vaapi` keep decoded frames in VA-API surfaces. H.264
encoding also uses VA-API. Main profile, no B frames, wall-clock timestamps, and
passthrough timing match the production bridge's established transcode choices.

For comparison, a second concurrent run used the existing production-style CPU decode
and resize path followed by the same hardware H.264 encoder:

```sh
-vaapi_device /dev/dri/renderD128
-reinit_filter 0 -use_wallclock_as_timestamps 1
-analyzeduration 100000 -probesize 262144 -i <raw-camera-HTTP-URL>
-an -vf scale=-2:720:eval=frame,format=nv12,hwupload
-c:v h264_vaapi -profile:v main -g:v 30 -bf:v 0
-fps_mode:v passthrough -enc_time_base:v 1:90000
```

## Concurrent 40-second measurements

Hardware run started at `2026-10-09T02:49:53Z`; the comparison started at
`2026-10-09T02:50:36Z` (the evening of October 8 in America/New_York).

| Path | Camera | Encoded frames | FFmpeg output duration | Final fps | Final speed | Last process CPU | Last RSS |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| VA-API decode, resize, encode | Front Door | 599 | 39.741 s | 15.08 | 1.00× | 18.0% | 109.7 MiB |
| VA-API decode, resize, encode | Garage | 587 | 39.035 s | 15.19 | 1.01× | 19.4% | 91.5 MiB |
| CPU decode/resize, VA-API encode | Front Door | 569 | 38.390 s | 14.73 | 0.994× | 164% | 168.5 MiB |
| CPU decode/resize, VA-API encode | Garage | 583 | 39.365 s | 14.80 | 0.999× | 123% | 128.1 MiB |

CPU values are the last `ps -eo pid,pcpu,rss,comm` samples, expressing lifetime CPU
time as a percentage of one CPU. The pair therefore used approximately **9.4% of
four-CPU capacity** with the VA-API path, versus **71.8%** with CPU decode and resize.
These are short real-stream runs, not identical-frame offline benchmarks. Variations
in received frame count do not establish dropped frames or image quality differences.

All four final FFmpeg progress records reported `dup_frames=0` and `drop_frames=0`.
Both runs were intentionally stopped with SIGINT after 40 seconds; `timeout` returned
124, and FFmpeg logged a normal signal-driven exit. Neither successful hardware camera
run logged a decoder or encoder failure. Garage logged one nonmonotonic DTS warning
from the null muxer, consistent with a wall-clock packet timestamp backstep; this test
does not claim that input timestamps were perfectly monotonic.

At `2026-10-09T02:51:29Z`, `pgrep -a ffmpeg` on the candidate host returned no processes,
so the test left no benchmark processes competing with subsequent bridge validation.

## Boundaries and deployment requirement

The two-camera hardware path has materially more CPU headroom on this machine. The Linux launcher now tries hardware against the actual stream first, retaining
hardware encoding when CPU decoding is required, and finally falling back to software
encoding. The old Ivy Bridge server cannot decode HEVC through its VA-API path.

This preflight used fixed source resolutions. It does not establish behavior across
midstream resolution changes, a third simultaneous camera, a long soak, or a client
display. Null output also omits RTSP packaging and downstream playback. Those checks
belong to the actual migration validation.

## Private capture provenance

Private captures are in `.evidence/atom-bridge-migration-2026-10-08/`, which is ignored
by Git. Relevant SHA-256 values:

| File | SHA-256 |
| --- | --- |
| `hardware-benchmark.sh` | `b458e83f204f53fef63c57c5a9c0179d9bcbad9398b8d278c8b6c0d3f91f32b1` |
| `hardware-door.progress` | `d51a1b61229831b6430d1ad01ed9a17ca9485ffb8e961c34dc99bef79bbc7168` |
| `hardware-garage.progress` | `fc445727ba01b39b22930b4ce8a8e0225f209e21d8b2b66f9bdf5e235f2ffa0d` |
| `hardware-cpu.txt` | `72e0a309156405bb1b8a4492845e40190075d3bf0926ada41be9f292e8b22411` |
| `software-door.progress` | `2bf10e700d0403521596b4dfbd793fb401e41fdcf6421f77f91e827ed1ad3e59` |
| `software-garage.progress` | `a549575387cbaf62350fdafca9413896185a77e7f095f1958f604c69dc60b1f1` |
| `software-cpu.txt` | `733f2e2e91fa6c788044325fb867a09bd60bf81505c5a9c641cdfd9c955724a8` |
| `vainfo.log` (before) | `b53f16824d615b13a2015f1d24366aa79e3903726bc3e382670f05f0956fea6a` |
| `vainfo-shaders.log` (after) | `26a1cd36edffbefe10703d2a7f5aaa6f8d9fdf7279babae53901672edd999f50` |

## Production migration and full-service measurements

At `2026-10-09T02:55:59Z`, the old bridge was stopped before the new service was
started, and its latest saved SDK session was transferred privately. Credentials,
camera power overrides, saved dual-view preferences and per-camera policy were
preserved. The Atom resumed authentication without another verification code.
Front Door, Garage and Balcony raw feeds were streaming with zero stall counters;
Front Door and Garage had one shared hardware transcoder each. Battery cameras
remained on-motion. This is three active SDK feeds, **two encoded outputs**.

Both Fire TVs (`.58`, `.85`) and the Pi (`.73`) were changed to `.158`. go2rtc
reported all three consumers for both camera outputs. TV hardware-decoder rendered
frame counters advanced. The Pi's imported V4L2 framebuffers advanced in both planes,
with unchanged processes during its steady capture. The user confirmed: **Both look good**.
See [the Pi capture and its startup-warning boundaries](linux-playback-audit-2026-10-08.md).

The full systemd service cgroup was sampled 46 times from
`2026-10-09T02:58:25.185Z` to `02:59:56.462Z` (91.276 seconds):

- Average CPU: **25.81% of four-core capacity**, including SDK, go2rtc and FFmpeg.
- Cgroup memory: **313.9–325.4 MiB**, including accounted memory beyond process RSS.
- The same service PIDs and transcoder producer IDs remained throughout this window.
- Front Door's separate 30-second RTP check: 451 frames, clock ratio 1.0015.
- Garage's 60-second check: 915 frames, clock ratio 0.9988.

These clock ratios measure progression, not glass-to-glass delay. An initial
60-second Front Door probe ended with `RTSP connection closed`; the subsequent
30-second probe passed. The cause of that observer disconnect was not established,
so this is not a claim that every connection remained uninterrupted.

### Direct GPU evidence and CPU breakdown

A later 10-second `/proc` sample found both FFmpeg processes holding Intel i915
render-device file descriptors. Their `drm-engine-video` counters advanced by
1.263 and 1.401 seconds; their `drm-engine-render` counters advanced by 0.744 and
0.652 seconds. Their commands explicitly used VA-API hardware decode surfaces,
`scale_vaapi`, and `h264_vaapi`, with no CPU decode/encode fallback at that point.
These GPU activity counters corroborate the active command, rather than relying
only on advertised driver capabilities.

| Process | CPU as % of one core | CPU as % of whole four-core host | RSS |
| --- | ---: | ---: | ---: |
| SDK / bridge Node | 61.7% | 15.4% | 132.9 MiB |
| go2rtc | 6.4% | 1.6% | 27.3 MiB |
| Front Door FFmpeg | 18.3% | 4.6% | 92.9 MiB |
| Garage FFmpeg | 20.6% | 5.2% | 93.1 MiB |

This process sample is shorter than the cgroup sample. CPU includes packet handling,
bitstream parsing and submission even when decoding/encoding run on the GPU. It
is not evidence identifying a particular SDK function as inefficient.

### Old installation removal and DietPi maintenance

At the user's request, the old `.199` bridge code, systemd unit, credentials, session
and data, source staging directory, Avahi advertisement and DietPi service-list entry
were removed. Its dedicated service account was removed and power-off requested.
SSH was no longer reachable afterwards. The old installation is no longer a rollback
copy. The new host's updater target is `.158`; configuration and credentials remain
outside the deployed source so subsequent updates preserve them.

The Atom installer registered `+ eufy-wall-bridge` in DietPi's include list. During
verification, a separate `dietpi-software` Homebridge installation cleanly stopped
the service at `03:03:37Z`. This was a maintenance stop, not a decoder crash:
systemd reported `Result=success` and zero automatic restarts. DietPi's full
`dietpi-services status` output included the bridge entry.

## Final hardware-first update and fallback validation

The deployed Linux default now tries hardware decoding and encoding first, without
requiring `vaapi_decode: true`. The temporary explicit decode preference was removed
from the Atom's YAML. Unsupported decoding/VPP falls back to CPU decode/scale with
hardware encoding; an unavailable encoder finally falls back to libx264. Automatic
render-node discovery was tested against the real Garage feed. The launcher selected
`/dev/dri/renderD128` and encoded 12 frames successfully. This implementation covers
VA-API; it does not claim NVIDIA NVENC or V4L2 encoder selection.

After the separate Homebridge installation held the apt lock, the updated source was
synced without changing packages or dependencies. The service was started at
`03:07:48Z`; `dietpi-services status` reported **active (running)**. It remains enabled
at boot. Homebridge's package installation was left untouched.

Two controlled tests used the actual Garage raw stream without another SDK session:

1. A temporary FFmpeg shim replaced only the hardware decoder's device with a
   nonexistent render node. Real FFmpeg reported a device-creation / decoder-setup
   failure. The launcher retried the same HTTP URL with CPU decode/scale and VA-API
   encoding, produced 12 frames, and exited successfully.
2. An explicitly nonexistent render node caused both hardware stages to fail. The
   launcher then used libx264, produced 12 frames, and exited successfully.

The 12-frame tests establish failure handling and output availability, not sustained
software throughput. A separate go2rtc instance bound only to loopback ports
`18554` and `11984` also used the nonexistent render node against Garage. An RTSP
client decoded **48 H.264 Main frames at 640 × 720** through the software fallback.
The fixture was intentionally stopped by timeout and its temporary files removed.
Only the two production FFmpeg producers and their launchers remained afterwards.

The final production streams retained full VA-API decode/scale/encode. A 10-second
post-update cgroup sample measured **24.33% of four-core CPU capacity** and
**343.8 MiB** at its last sample. Both FFmpeg processes retained active i915 GPU
file descriptors; neither production producer took a software fallback.

The post-update 30-second concurrent RTSP checks reported:

| Camera | Frames | Arrival fps | RTP / wall-clock ratio | Timestamp regressions |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 438 | 14.51 | 1.0018 | 0 |
| Garage | 443 | 14.71 | 1.0052 | 0 |

Both Fire TVs' hardware rendered-frame counters advanced past 2,400 frames after
reconnection. The Pi recovered on its existing supervisor and both hardware planes
advanced in the final capture; see its linked audit. This verifies recovery and
clock progression, not a new numerical glass-to-glass measurement.

Local validation: all **111 server tests** passed. They include actual launcher-child
retry and cancellation tests, same-input preservation, bounded fallbacks, automatic
render-node selection, default hardware preference, and output/network failures that
do not cause a software downgrade. Linux `go test -race ./...` and `go vet ./...` also
passed during this session; no Linux playback implementation was changed.

### Migration capture hashes

| Private file | SHA-256 |
| --- | --- |
| `live-service-samples.jsonl` | `1a4ccde7659a94a46f57d6e1ef7004bde378fa309c926b674e32778302ffff99` |
| `gpu-process-proof.json` | `19712914189e7532bb6514902c3d641245a647fca342a96a17142f372068f20a` |
| `post-update-gpu-proof.json` | `9f878e50ff19810d4441f84a19d276074e1e5cbabe1972a64a7015e399f08422` |
| `post-update-rtp-clock.jsonl` | `365e59764a8916b2b0f16e144608a4285d17cfe68cbb9b80db2c0d7c2e8f435e` |
| `mixed-fallback.log` | `a523ad9bec6a5a2dc62cb926393f08a8b916ea61380c707dc306ce658ca198aa` |
| `software-fallback.log` | `ee3f911379f5b30f4230cf5bace23896597408c374b27a840516df41fbe868e8` |
| `go2rtc-fallback-probe.json` | `61891072811b61c1ca1653569a3ee2b4db23db7a3f9a9fcbd620da191fcbf2df` |
| `automatic-gpu.log` | `eff8a94b536e42cbb43a85b74ec8a66ccd2377a85d259d8058cd4a70b4a7e5b5` |

### Recovery after DietPi completed its maintenance

DietPi restarted the registered service again at `03:14:39Z` while completing the
separate Homebridge install. At `03:15:33Z`, health reported authenticated status,
all three active raw feeds, go2rtc running, zero stalls and `NRestarts=0`. Both encoded
streams again had the two Fire TVs and Pi as RTSP consumers, and the TVs' hardware
rendered-frame counters advanced after this restart. This later maintenance event
does not change the earlier measured GPU/CPU windows.
