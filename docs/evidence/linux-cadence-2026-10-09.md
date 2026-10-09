# Linux playback cadence: actual Pi display path, 2026-10-09

## Scope and conclusion

The user confirmed the Pi's black background and both moving cameras, then reported that playback looked choppier than Android. This capture measures the two existing display pipelines on `192.168.23.73`; it does not infer playback latency from the cameras' different on-screen clocks. The user subsequently identified those clock differences as camera clock differences.

The longest measured display pauses were already present before the hardware decoder. Front Door's longest pause was 781 ms at compressed-frame input and 781 ms at rendering; Garage's was 1,032 ms at input and 1,036 ms at rendering. The decoder and DRM sink add shorter delays, including backpressure during bursts, but this capture does not establish a Linux-specific fault as the cause of those longest pauses.

No Linux playback configuration or source change was made. In particular, clock synchronization, packet dropping, queues, decoder selection and restart policy were not changed. Investigate source frame timing and burst delivery before changing presentation policy: the transmitted presentation timestamps are themselves irregular, so enabling sink synchronization alone cannot reconstruct a uniform camera cadence.

## Device, pipeline and capture boundaries

- Pi Model B Rev 2, ARMv6, DietPi/Raspbian Trixie, GStreamer 1.26.2.
- Installed client from commit `1104897`; two independent `v4l2h264dec` hardware pipelines, with software still available through the existing decoder selection.
- Front Door: H.264 524×720, DRM overlay 98. Garage: H.264 640×720, overlay 109.
- RTSP bridge `192.168.23.158:8554`; TCP transport, `rtspsrc latency=200`.
- Both sinks: `sync=false`, shared DRM descriptor `fd=3`, `skip-vsync=true`, connector 35. The persistent black primary plane remained active.
- Trace categories: `GST_DEBUG=v4l2videodec:6,basesink:6,kmssink:6`, with `GST_DEBUG_FILE=/tmp/eufy-cadence-%p.log` in each child. The temporary systemd environment used `%%p` to escape the systemd unit-prefix specifier.
- The bridge's SDK instrumentation was restored at **15:05:42 UTC**, disconnecting the initial Pi children. That initial trace and reconnection failures are excluded from the steady-state results below.
- The measured replacement children were Front Door PID **1522**, started **15:05:49 UTC**, and Garage PID **1523**, started **15:05:50 UTC**. Analysis excludes each process's first 12 seconds and stops at the last captured input/render time: 70.934 seconds elapsed for Front Door, 72.275 for Garage. These are separate approximately 59/60-second windows, not perfectly simultaneous endpoints.
- Debug logging increased CPU usage: observed diagnostic children were about 25–34% CPU each, versus approximately 15–31% after restoration. Measurements therefore include diagnostic overhead and are not a claim of uninstrumented decode performance.

## Measured cadence

All durations are milliseconds. Percentiles use sorted samples, indexing `floor(p × (n−1))`; input/output gaps are consecutive frame observations within the stated window.

| Metric | Front Door median / p95 / maximum | Garage median / p95 / maximum |
|---|---:|---:|
| Compressed input gap | 40.10 / 275.44 / 781.46 | 38.95 / 253.86 / 1,032.12 |
| Hardware output gap | 38.60 / 272.49 / 781.78 | 39.74 / 228.08 / 1,036.08 |
| Sink render-start gap | 38.32 / 270.02 / 781.35 | 40.67 / 220.59 / 1,036.09 |
| Matched input → output | 43.86 / 320.00 / 582.90 | 37.74 / 546.12 / 932.30 |
| Sink render-call duration | 20.41 / 39.82 / 157.62 | 19.07 / 43.09 / 139.35 |
| Output presentation timestamp increment | 41.72 / 206.84 / 779.79 | 59.99 / 142.84 / 1,019.78 |

Input rate was 14.78 fps for Front Door and 15.00 fps for Garage. Output rates were 14.81 and 14.90 fps respectively; small boundary differences are expected because an input submitted before a measurement window can finish inside it. This comparison is not a dropped-frame count.

Front Door had 309 render intervals shorter than 33 ms and 81 longer than 150 ms, out of 872 intervals. Garage had 313 shorter than 33 ms and 67 longer than 150 ms, out of 897. Both deliver bursts separated by holds, despite their approximately 15-fps average rate.

“Input → output” is the elapsed time from `Handling frame N` to `Got buffer for frame number N`. It includes decoder buffering and downstream backpressure; it is not isolated silicon decode time. “Render call” spans the BaseSink rendering message to its post-render unref message. It measures sink submission completion, not the physical screen's scanout time.

### Same-frame proof for the longest holds

| Stream and adjacent frame IDs | Input gap | Output gap | Render-start gap | Presentation timestamp increment |
|---|---:|---:|---:|---:|
| Front Door 276 → 277 | 781.46 | 781.78 | 781.35 | 62.16 |
| Garage 340 → 341 | 1,032.12 | 1,036.08 | 1,036.09 | 587.32 |

The source input already contains these holds. The Front Door example also demonstrates why arrival time is not interchangeable with presentation time: the next frame arrived 781 ms later but its presentation timestamp advanced only 62 ms.

Across the steady captures, presentation timestamp increments were not consistently 66.7 ms. Increments as short as approximately 0.2–7 ms occur during bursts; there were no decreasing output presentation timestamps in the analyzed traces. The bridge currently transports Annex-B video through HTTP without per-frame timestamps and supplies FFmpeg with `-use_wallclock_as_timestamps 1` plus passthrough output timing in `server/src/go2rtc.mjs`. This is a concrete path to inspect for loss of camera frame timing, not proof that it alone causes every burst. SDK/device frame timestamps and arrival times need a matched upstream capture to decide that.

## Decoder and sink source boundaries

GStreamer's [V4L2 decoder 1.26.2](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c) starts a separate output task in `gst_v4l2_video_dec_handle_frame`; `gst_v4l2_video_dec_loop` dequeues capture buffers and finishes frames independently. Android's former input-wait-before-output-drain bug does not apply to this implementation.

[BaseSink 1.26.2](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gstreamer/libs/gst/base/gstbasesink.c) bypasses clock waiting when `sync` is false. [kmssink 1.26.2](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-bad/sys/kms/gstkmssink.c), in `gst_kms_sink_show_frame`, imports the decoded DMA buffer and submits `drmModeSetPlane`; `skip-vsync` bypasses its separate vsync wait. Submission can still take time, as the measured render calls show. This capture does not establish whether each longer call is driver blocking or process scheduling.

The measured children logged 12 Front Door and 16 Garage initial frames not dequeued during startup, before the analyzed windows. The driver warning is retained as an observation; this test does not prove its cause. These counts are not recurring steady-state frame loss measurements.

## Restoration and evidence

At **15:07:16 UTC**, the temporary `/run/systemd/system/eufy-wall.service.d/90-cadence-audit.conf` was removed, systemd reloaded, and the Pi client restarted to stop diagnostic logging. The service returned active with main PID **1669** and normal children **1686/1687**, the same pipeline arguments, and the original environment `GST_DEBUG=2`. No bridge changes were made by this audit. Startup warnings after this restoration are outside the cadence capture.

Private raw evidence is retained locally under `.evidence/linux-cadence-2026-10-09/`:

- `eufy-cadence-1522.log`, `eufy-cadence-1523.log`: separate process logs for the measured window.
- `summary.json`: derived distributions using the boundaries stated above. Unmatched input counts at capture boundaries include frames still in flight and are **not evidence of dropped frames**.
- `runtime.txt`: restart boundaries, child mapping, restoration environment and service state.
- `eufy-cadence-1434.log`, `eufy-cadence-1435.log`: initial captures interrupted by the bridge restart, excluded from the steady results.

Analysis joins `Handling frame N` with `Got buffer for frame number N` within each PID, reads sink PTS from the chain-level `got times start` message, and measures rendering/unref messages in order. No periodic buffer purge or restart was introduced as a playback fix.

## Follow-up: bridge hardware decoder threading change

The bridge was subsequently redeployed with its GPU decoder input limited to `-threads:v 1`. This is a bridge decoding change; the Pi's buffering, clock policy and display pipeline were not changed. This follow-up is a brief recovery check, not a second per-frame cadence measurement.

At **15:46:38–15:46:53 UTC**, the existing Pi supervisor PID **1669** remained active. Garage child **4465** and Front Door child **4466** had reconnected, both still using `v4l2h264dec`, `latency=200`, `sync=false`, shared `fd=3` and `skip-vsync=true`. Their PIDs remained unchanged across the check. Normal debug environment remained `GST_DEBUG=2`; no diagnostic drop-in or new client restart was used for this follow-up.

Fifty read-only DRM observations covered **14.323 seconds**, from **15:46:39.209 to 15:46:53.532 UTC**. Front Door overlay 98 changed framebuffer ID on **40/49** adjacent observations; Garage overlay 109 changed on **41/49**. The app-owned RGB565 black primary plane 86 stayed on its existing framebuffer. Both video overlays still contained hardware-decoded imported YU12 buffers, sized 524×720 and 640×720 respectively. No service journal entries occurred from **15:46:10 UTC** through the end of the check. Observed child CPU usage was approximately 17–24% during this short check.

The framebuffer observations establish advancing display planes and successful recovery. Reused framebuffer pools and approximately 0.2-second polling mean they cannot establish exact frame rate, improved smoothness, end-to-end latency or elimination of camera ingress pauses. Startup immediately before this window still logged decreasing timestamps and **17 initial frames not dequeued for each camera**; the bridge change does not establish a fix for those startup warnings.

Private evidence: `.evidence/linux-cadence-2026-10-09/post-gpu-threads1.txt`, `post-gpu-threads1-summary.json`, and `post-gpu-threads1-startup.txt`.

## Follow-up: native resolution and exact Pi limits

At approximately **16:34 UTC**, the bridge's `max_height` was changed to zero to preserve native source resolution. This was followed by read-only Pi checks; no client scaling, decoder substitution, buffering or restart configuration was changed.

Front Door's H.264 **1600×2200**, High profile, level 5 stream repeatedly failed caps negotiation at `v4l2h264dec0:sink`, then exited with `streaming stopped, reason not-negotiated (-4)`. The existing supervisor retried it. At **16:34:50 UTC**, both overlay framebuffers were zero during reconnection; the black primary remained active. A rejected decoder sink cap alone does not isolate the failing hardware component because decoder caps also intersect downstream display constraints.

A small temporary CGO-free ARMv6 diagnostic queried **only** `DRM_IOCTL_MODE_GETRESOURCES`, `VIDIOC_QUERYCAP`, `VIDIOC_ENUM_FRAMESIZES` and `VIDIOC_ENUM_FRAMEINTERVALS`. It did not set formats, allocate stream buffers, start decoding or change DRM state, and was removed after use. On the actual Pi, kernel `6.18.50+rpt-rpi-v6`:

| Interface | Actual reported bounds |
|---|---|
| DRM framebuffer resources | Width 0–2048; height 0–2048 |
| `/dev/video10`, `bcm2835-codec-decode`, H.264 frame sizes | Stepwise (type 3): width 32–1920; height 32–1920; step 2 on both axes |
| Decoder's GStreamer profiles | Baseline, constrained baseline, Main, High |
| Decoder's GStreamer levels | 1 through 5.1, including 1b |
| V4L2 frame interval query at 1280×1440 and 1396×1920 | Unsupported ioctl (`ENOTTY`); no frame-rate bound obtained |

The [Linux V4L2 UAPI](https://github.com/torvalds/linux/blob/master/include/uapi/linux/videodev2.h) identifies frame-size type 3 as stepwise and defines the returned minimum, maximum and step fields. The native Front Door height exceeds **both** the driver's enumerated 1920-pixel height and the DRM resource limit of 2048. These are current device measurements, not a generic claim that all Raspberry Pis cannot decode portrait video. The driver frame-size enumeration is not a throughput guarantee.

Garage recovered after the bridge stabilized: child **7792** entered PLAYING at **16:35:09 UTC**, negotiated native **1280×1440**, and remained running through the subsequent check. Twenty-five DRM samples from **16:37:25.248 to 16:37:32.581 UTC** covered **7.333 seconds**; Garage overlay 109, using imported hardware-decoded YU12 buffers, changed framebuffer ID on **12/24** adjacent observations. Front Door overlay 98 remained framebuffer zero; the black primary remained unchanged. Garage child CPU usage was approximately **61%** at the check, while a Front Door retry process temporarily used approximately **54%**. These observations establish native Garage playback, not sustained smoothness with two supported native-sized streams.

For Front Door's 1600:2200 aspect, **1396×1920** is a candidate within the enumerated size bounds, preserving aspect to the nearest even width. It has **10,560 H.264 macroblocks per frame**; Garage 1280×1440 has **7,200**. At 15 fps each, that totals **266,400 macroblocks per second**. These are calculated workloads, not measured hardware throughput limits, and this candidate has **not** been live-verified as the highest working resolution. The user subsequently authorized using the highest supported resolution if native fails; a separate client rendition can preserve native resolution for more capable clients while respecting this Pi's measured bounds.

Private evidence: `.evidence/linux-cadence-2026-10-09/native-resolution.txt`, `native-resolution-followup.txt`, `native-resolution-limits.txt`, `native-resolution-summary.json`, and `native-resolution-intervals.txt`.

## Follow-up: 1920-height candidate on the Pi

The bridge next limited height to `min(input_height, 1920)`, producing Front Door **1396×1920** while retaining Garage's native **1280×1440**. At **16:42:46 UTC**, both Pi overlays contained imported YU12 buffers from `v4l2h264dec0`, with precisely those dimensions. Front Door child **8521** and Garage child **8553** were playing. This verifies that this particular Pi can negotiate and decode the 1920-height candidate; it does not establish that other clients support it or that two streams are consistently smooth.

A temporary read-only `DRM_IOCTL_MODE_GETPLANE` sampler observed overlay framebuffer IDs between **16:44:08.092 and 16:44:43.099 UTC**. It requested a 5-ms sampling interval but actual intervals were **6.23 ms median, 24.81 ms p95 and 62.05 ms maximum**. It observed **404** Front Door framebuffer changes and **325** Garage changes over 35.006 seconds, with no observed zero framebuffer. The longest observed intervals between changes were **1.147 seconds** and **2.235 seconds**, respectively. These are sampled plane-state changes, **not exact decoded FPS or physical scanout measurements**: reused framebuffer pools, sampling gaps and burst delivery can hide updates.

The sampler itself consumed approximately **18.85% of one CPU**, so it materially perturbed this single-core ARMv6 device. Child CPU usage during the sampler was approximately **35.51% / 34.34%**. After removing the sampler, a separate **10.120-second** `/proc` CPU sample measured **45.06% Front Door / 39.23% Garage**; both PIDs were unchanged and the service journal contained no new entries from 16:44 UTC through this check. CPU uses the device's reported `CLK_TCK=100`. This verifies continued operation but leaves limited CPU headroom and does not prove uninstrumented visual smoothness. The sampler was removed, no GStreamer debug logging or client restart was added, and immediate playback remained unchanged.

Private evidence: `.evidence/linux-cadence-2026-10-09/height1920-initial.txt`, `height1920-cadence.json`, `height1920-cadence-summary.json`, `height1920-post-probe.txt`, and the reproducible sampler `eufy-plane-cadence.go`.

## Follow-up: verified 1680-height common candidate

The bridge was next deployed with a **1680-pixel** height limit for clients that could not decode the 1920-height Front Door rendition. On the Pi, the actual decoded DMA buffers were **1222×1680 Front Door** and unchanged native **1280×1440 Garage**. Both remained hardware-decoded YU12 on the independent overlays. The black primary and immediate playback policy were unchanged.

A low-overhead check from **16:47:31 to 16:48:09 UTC** found supervisor **1669**, Front Door child **8893**, and Garage child **8894** active with unchanged PIDs and input/display dimensions. A **10.666-second** `/proc` CPU sample measured **34.50% Front Door / 47.25% Garage**, using `CLK_TCK=100`. There was no high-rate polling helper, extra decoder, increased GStreamer logging or client restart in this check.

Four additional one-second-spaced DRM observations at **16:48:48–16:48:51 UTC** showed Front Door framebuffer IDs **679 → 680 → 683 → 681**, and Garage **675 → 675 → 675 → 674**. The black primary remained framebuffer **670**. Reused framebuffer pools mean an unchanged sampled ID is not evidence that the camera stalled. The service journal had **no entries from 16:47:31 UTC through 16:48:51 UTC**. This passes the Pi's hardware negotiation, recovery and short stability checks at the common candidate resolution. It does not establish exact FPS, physical scanout cadence, absence of upstream delivery gaps, or user-perceived smoothness; those remain separate acceptance checks. The 1680 limit is a verified candidate, not proof of a mathematically maximal resolution.

Private evidence: `.evidence/linux-cadence-2026-10-09/height1680-final.txt` and `height1680-advancement.txt`.

### Final check after restoring the common 1680 limit

After the higher-resolution client boundary tests, the bridge was restored to the common **1680** height limit. A fresh Pi check from **16:54:47 to 16:55:14 UTC** found supervisor **1669**, Garage child **9495**, and Front Door child **9510** stable. Both decoded overlay dimensions remained **1222×1680 Front Door / 1280×1440 Garage**, imported hardware YU12. Front Door framebuffer changed **683 → 681**, Garage **674 → 676**, and the app-owned black primary stayed **670**. No journal entries occurred during this check.

Following 15 seconds of additional warm-up, a **10.596-second** CPU sample measured **33.88% Front Door / 39.82% Garage** (73.70% combined). The immediate pipeline arguments and original `GST_DEBUG=2` environment were unchanged. The temporary diagnostic drop-in and both diagnostic helpers were confirmed absent. This final check establishes hardware playback and stable recovery at the restored common limit; earlier approximately one-second source delivery holds remain a separate finding, and physical smoothness still requires user confirmation.

Private evidence: `.evidence/linux-cadence-2026-10-09/height1680-restored-final.txt` and `height1680-restored-summary.json`.
