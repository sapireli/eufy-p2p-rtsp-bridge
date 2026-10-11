# Android surface pacing evidence — 2026-10-11

## Scope

Read-only inspection of Fire TV `192.168.23.85:5555`, app `com.eufywall.viewer`, PID 12730, API 28. The bridge source-timestamp transport candidate was running; Android used `LiveFrameClock` with 67 ms headroom. Hardware decoders were `OMX.amlogic.avc.decoder.awesome`. The initial surface audit was read-only; the subsequent clock-policy correction below changes only the Android clock and its tests. No bridge mutations, logging clears, or decoder/library changes were performed by this audit agent.

The device reports an active 60 Hz display. SurfaceFlinger identifies layer `SurfaceView - com.eufywall.viewer/com.eufywall.tv.MainActivity#1` as Garage (1280×1440) and `#2` as Front Door (1222×1680). Both are composed by the hardware device.

## Callback measurements are not exhaustive display counts

[Android OnFrameRenderedListener documentation](https://developer.android.com/reference/android/media/MediaCodec.OnFrameRenderedListener) warns that callbacks may be delayed/batched and, before Android 14, frames may render without a callback. Therefore callback frequency alone is not a measured display frame rate. Callback `nanoTime` is documented as render time, but this device also disagrees with independent SurfaceFlinger actual-present timestamps; use the latter for the surface stage in this test.

The decoder already calls the pinned RTSP library’s `MediaCodecHelper.setDecoderLowLatencyOptions(format, codecInfo, 1)`. For Amazon/Amlogic this requests `vdec-lowlatency=1`, realtime priority/operating rate where supported, and `vendor.low-latency.enable=1` on API 26+. These requests are present; driver acceptance/effect cannot be inferred merely from setting them. There is no evidence here for adding another missing low-latency switch.

## Independent surface probe

Collected 16 overlapping snapshots, separated by about two seconds, without clearing SurfaceFlinger statistics:

```sh
rtk proxy adb -s 192.168.23.85:5555 shell 'dumpsys SurfaceFlinger --latency "SurfaceView - com.eufywall.viewer/com.eufywall.tv.MainActivity#1"'
rtk proxy adb -s 192.168.23.85:5555 shell 'dumpsys SurfaceFlinger --latency "SurfaceView - com.eufywall.viewer/com.eufywall.tv.MainActivity#2"'
```

Deduplicate `(desiredPresentTime, actualPresentTime, frameReadyTime)` triples across overlapping snapshots. Exclude zero and pending-fence/sentinel actual times (`actual >= 2^62`); otherwise pending fences create bogus billion-second delays. Join each desired time **exactly**, without fuzzy matching, to the Android timing log’s `decoded ... targetNs=...`, then use that record’s media PTS to join callback samples. Every retained surface triple matched an exact release target.

Private capture: `.evidence/frame-pacing-2026-10-11/surface-latency-85.json`, SHA-256 `c4ca2b98c54184390d7ac15dc3a09ea155e80c7edfa8fd246285b3f0cb4ef697`. Corresponding timing log: `.evidence/frame-pacing-2026-10-11/source-pts-fixed-85.log`. It was still collecting during this analysis; final log hash belongs in the parent transport evidence record. Local reproduction script `/tmp/eufy-surface-analyze.py` was used for this probe.

All durations below are milliseconds except the capture spans. Quantiles use the sorted sample at `floor((n-1)*q)`.

| Measurement | Garage | Front Door |
| --- | ---: | ---: |
| Unique actual presents / span | 627 / 42.317 s | 487 / 43.983 s |
| Actual presents per second | 14.793 | 11.049 |
| Actual present minus release target, p50 / p95 / p99 / max | 12.972 / 29.133 / 33.059 / 34.543 | 11.586 / 27.792 / 30.972 / 34.248 |
| Actual present minus decoded time, p50 / p95 / p99 / max | 46.013 / 75.322 / 80.133 / 82.972 | 50.351 / 76.933 / 82.295 / 83.916 |
| Callback reported render time minus actual present, p50 / p95 / p99 / max | 50.998 / 84.342 / 117.626 / 367.610 | 51.016 / 101.007 / 967.854 / 1534.319 |
| Actual present interval, p50 / p95 / p99 / max | 66.664 / 100.011 / 133.321 / 383.366 | 66.662 / 116.665 / 983.341 / 1550.042 |

These are observed SurfaceFlinger presents, not photographic glass-to-glass measurements. The surface presents accepted buffers within roughly two display periods of the requested target. Multi-second callback “lateness” does **not** establish multi-second presentation delay for that buffer. However, real Front Door presentation gaps remain in this run.

## A real Front Door freeze occurs before surface submission

For the largest sampled Front Door surface gap (1550.042 ms), there were **zero accepted timed releases between its endpoints**. The timing log explains why:

| PTS (microseconds) | RTSP receive time (nanoseconds) | Decoder output time (nanoseconds) | Target |
| ---: | ---: | ---: | ---: |
| 9259050618 | 4464355485168 | 4464366270793 | 4464368383084 |
| 9259117618 | 4464873887751 | 4464890913001 | dropped (`-1`) |
| 9260449618 | 4465855043418 | 4465866420627 | dropped (`-1`) |
| 9260516618 | 4465901673793 | 4465913054418 | 4465913054418 |

The first two frames advance camera media time by 67 ms, but RTSP arrival time advances **518.403 ms**. The hardware decoder outputs the next frame about 17 ms after its reception. Twenty-one consecutive pictures are then discarded by the bounded late-frame policy until incoming media catches up to its rolling clock offset. This is neither a multi-second decoder processing delay nor a Surface FIFO holding these accepted pictures; the next accepted frame is presented about 29 ms after its release target.

The gap begins after media PTS ending near the recurring 2-second epoch. That is consistent with GOP/keyframe-related upstream bursts, but no keyframe flag was logged in this capture, so it is **not proof of an encoder or keyframe root cause**. Many other sampled >300 ms Front Door surface gaps similarly contain zero accepted releases.

Doubling client headroom from 67 to 133 ms cannot absorb a 518 ms arrival gap. It may retain more moderately late pictures, while adding up to another frame of deliberate latency. A root fix must establish where the upstream delivery gap originates and prevent that backlog/burst; increasing the presentation budget alone is not supported as its fix.

## Clock-policy correction following the probe

The investigation identified a client bug independent of the upstream stall: lateness against the rolling historical offset was treated as proof that a decoded picture was stale, even when it was the newest picture available. The client dropped a continuous run of valid current pictures and prolonged the unavoidable upstream pause.

`LiveFrameClock` now rejects only duplicate/out-of-order media PTS. A newest decoded picture whose target is in the past is submitted immediately; a target in the future remains bounded to 67 ms. This does not move the clock epoch later on every overdue frame, sleep in the decoder thread, discard compressed reference pictures, or add a larger queue. SurfaceView can coalesce overdue pictures submitted for the same display interval; see [MediaCodec timed-release documentation](https://developer.android.com/reference/android/media/MediaCodec#releaseOutputBuffer(int,long)).

Tests require every frame in a continuous 20-picture, 500 ms delayed-arrival sequence to receive an immediate target (the old code failed this). Separate tests retain duplicate/out-of-order rejection, require non-null targets and the 67 ms future bound for an hour of clock drift/bursts, and exercise timestamp resets. Build/test and post-fix live measurements are recorded by the parent deployment evidence; this policy change must not be called physically verified on the strength of unit tests alone.

## Post-fix independent surface verification

APK SHA-256 `73028d782a68d61bc05788a37bb81c05de4bebc35e4e191ad0bd8f8a192f08c9` was installed by the parent agent; app PID 14123. After reinstall, the layer identities changed: **Front Door is now `#0`, Garage remains `#1`**. Verify `--list` and decoded geometry on every run instead of retaining earlier layer suffixes. An initial post-fix attempt used the former Door `#2` suffix and yielded no Door samples; its Garage samples were valid but are superseded below.

The final probe collected 91 snapshots per verified layer at approximately two-second intervals (more than 180 seconds of active sampling, with overlapping historical samples providing about 209 seconds of unique presents). All reported records join exact `targetNs` from the new process; pending actual fences were excluded as before.

| Measurement | Garage | Front Door |
| --- | ---: | ---: |
| Actual presents / span | 3120 / 209.467 s | 3102 / 209.633 s |
| Actual presents per second, `(n-1)/span` | 14.890 | 14.792 |
| Actual present minus target, p50 / p95 / p99 / max ms | 12.527 / 28.787 / 32.239 / 34.665 | 14.085 / 30.405 / 32.382 / 34.390 |
| RTSP receive to actual present, p50 / p95 / p99 / max ms | 58.145 / 86.555 / 92.037 / 98.578 | 49.342 / 84.853 / 90.998 / 95.354 |
| Actual present interval, p50 / p95 / p99 / max ms | 66.665 / 100.027 / 133.384 / 216.717 | 66.651 / 100.115 / 299.971 / 566.679 |
| Received / decoded samples in target window | 3142 / 3142 | 3148 / 3149 |
| Policy drops in target window | 0 | 0 |
| Largest RTSP arrival gap in target window | 268.163 ms | 606.509 ms |

Receive/decoded counts use timestamps within the same requested-target window, so one boundary decode can belong to a receive just outside that window. Actual present counts need not equal decoded counts: pictures released close together can be coalesced by the surface. These are sequential natural windows, not a controlled matched-content frame-rate benchmark.

### Natural delayed-arrival recovery

The largest sampled post-fix Door surface gap was **566.679 ms**, between PTS `14173115913` and `14173183913`. Those are consecutive source pictures separated by **68 ms** of media time; their RTSP reception is separated by **606.509 ms**. The next decoded picture is retained and presented immediately after it becomes available, rather than dropping another 21 pictures against the historical offset. The surface gap follows the upstream arrival gap; it no longer extends to a long missing-media run like the earlier 1550 ms case.

The other sampled Door surface gaps of at least 250 ms also advanced only 64–73 ms of media time between their endpoints and tracked an incoming reception pause. Therefore the fix is verified for the client’s manufactured freeze, **not** for eliminating upstream pauses. Rendering cannot produce source pictures that have not arrived. Door still has visible interruption risk when its input stalls; this is a separate delivery issue to trace in the bridge/camera path.

Private immutable artifacts:

- `.evidence/frame-pacing-2026-10-11/surface-latency-latest-frame-verified-85.json` — SHA-256 `8fa9ad30ffeec3b6be8e9211a33132a7dccaca0f5a7011af9bcc6ff5fd2cbbc7`.
- `.evidence/frame-pacing-2026-10-11/surface-latency-latest-frame-summary.json` — SHA-256 `ade0e012bbb7e297fc7101c355138d2542cc8d1829100bdaed4ffd3278f66441`.
- Source timing log `.evidence/frame-pacing-2026-10-11/latest-frame-fixed-85.log`; collector has stopped. Final SHA-256 `aac69e3e1dde30e16422dff5a57e8d2c4b01cab4b1e03411fee5235d6f88fd4a`.

Reproduce the table with the repository utility (all shared metrics exactly agree with the original analysis):

```sh
rtk proxy python3 tv/tools/surface_timing.py \
  .evidence/frame-pacing-2026-10-11/latest-frame-fixed-85.log \
  .evidence/frame-pacing-2026-10-11/surface-latency-latest-frame-verified-85.json \
  --pid 14123 \
  --layer-camera 0=T8214510242321E6 \
  --layer-camera 1=T8425T1124130A0C
```

For a new capture, enable `log.tag.EufyFrameTiming=DEBUG` and stream that tag to a separate log while taking overlapping latency snapshots for the current layer names. Restore the **same** tag to `INFO` afterward; `EufyWallRaw` is a different tag and does not disable these timing records. Preserve logs/snapshots before hashing them. No clock values were compared between different machines.

The original extended analysis `/tmp/eufy-surface-final-analyze.py LOG SURFACE_JSON` additionally enumerates each >=250 ms surface gap’s media advance and corresponding reception gap; these detailed records are retained in the hashed summary JSON.

## Final finite-PES transport deployment check

After the parent deployed the final timestamp-preserving, finite-PES MPEG-TS transport on bridge `192.168.23.158` (service PID 11104), the existing Android process PID 14123 reconnected without an app relaunch. Surfaces were recreated by recovery: geometry now identified Front Door as `#1`, Garage as `#2`. A 31-snapshot-per-layer probe sampled for over 60 seconds, alongside a new timing log. Its collector restored `log.tag.EufyFrameTiming=INFO`; `getprop` confirmed `INFO` after completion.

Exact-target matches exclude historical SurfaceFlinger entries from before this new log began (1118 Door surface rows yielded 990 matches; 1117 Garage rows yielded 991). All following metrics use matched samples only:

| Final deployment measurement | Front Door | Garage |
| --- | ---: | ---: |
| Actual presents / matched span | 990 / 67.183 s | 991 / 67.317 s |
| Actual presents per second | 14.721 | 14.707 |
| RTSP receive to actual present, p50 / p95 / p99 / max ms | 48.911 / 87.449 / 93.067 / 96.010 | 54.960 / 86.655 / 92.331 / 96.550 |
| Target to actual present, p50 / p95 / p99 / max ms | 13.802 / 30.531 / 32.389 / 34.019 | 13.068 / 30.041 / 32.520 / 34.474 |
| Actual present interval, p50 / p95 / p99 / max ms | 66.644 / 150.042 / 266.646 / 333.328 | 66.664 / 100.163 / 166.681 / 250.077 |
| Whole-log received / decoded pictures | 1012 / 1012 | 1012 / 1012 |
| Whole-log policy drops | 0 | 0 |
| Largest whole-log RTSP arrival gap | 357.922 ms | 292.811 ms |

This verifies both live paths after the final transport deployment. The client still presents the latest available pictures without the earlier artificial late-frame freeze. Arrival pauses remain observable; the final transport check does not establish that all source jitter disappeared, nor does it measure physical brightness flicker or glass-to-glass latency.

Reproduce final metrics:

```sh
rtk proxy python3 tv/tools/surface_timing.py \
  .evidence/frame-pacing-2026-10-11/finite-pes-final-85.log \
  .evidence/frame-pacing-2026-10-11/surface-latency-finite-pes-final-85.json \
  --pid 14123 \
  --layer-camera 1=T8214510242321E6 \
  --layer-camera 2=T8425T1124130A0C
```

Immutable private artifacts:

- `finite-pes-final-85.log` — SHA-256 `69f071c7de61d86143b330d466cacb90bac4216a3d91f95be0609d93fbca3ff6`.
- `surface-latency-finite-pes-final-85.json` — SHA-256 `d32bcbd9332b6a2332789c21334e58aaede8ad9d6980060ba2fccc8638b26c15`.
- `surface-latency-finite-pes-final-summary.json` — SHA-256 `66191dbc4cba95230ac2b43c896dd540a5c4cd15cc8e911f98e5cae580807dd3`.

All three reside under `.evidence/frame-pacing-2026-10-11/`.

## Remaining verification

Match source-frame arrival and bridge encoder output at the same recurring epochs to distinguish camera/P2P delivery, MPEG-TS HTTP transport, FFmpeg parse/decode/encode scheduling, and RTSP delivery. A later comparison must repeat the independent surface probe, rather than relying solely on callback count or reported callback render times. Physical brightness flicker and glass-to-glass motion have not been verified by this read-only probe.
