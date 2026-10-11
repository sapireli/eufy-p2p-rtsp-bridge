# Bounded client frame pacing — 2026-10-11

## What this fixes

Both clients previously displayed decoded pictures as soon as they became available.
An arrival burst therefore became a display burst. Neither path deliberately scheduled
each picture against its presentation timestamp. This change addresses that presentation
behavior; it does not establish the cause of Front Door's reported brightness flashing.

Hardware decoding remains the first choice, with the existing software fallback. There
is no change to bridge encoding, camera resolution, camera settings, or SDK source.
Compressed reference pictures continue through the decoder. Duplicate/out-of-order
decoded pictures can be rejected; newest pictures are presented immediately when late.
The superseded lateness-only policy and its measured failure are retained below.

## Android implementation

`LiveFrameClock` maps media PTS to `System.nanoTime()` using the minimum decoded-arrival
offset observed in a rolling two-second window. Each output gets at most 67 ms of future
presentation headroom. A newest picture whose target is already past renders immediately.
Timestamp discontinuities reset the clock. The decoder does not sleep for pacing: it
continues independently draining input/output while the surface presents scheduled frames.

The 67 ms budget is approximately one frame at the cameras' nominal 15 fps. It is a
deliberate jitter allowance, not zero extra delay and not a bound on all camera/network/
hardware delay. Expiring offset samples allow clock drift to be tracked without repeatedly
adding lateness to a persistent playback epoch. Requested future timestamps are always
clamped to `now + 67 ms`.

The custom decoder calls [MediaCodec's timed release API](https://developer.android.com/reference/android/media/MediaCodec#releaseOutputBuffer(int,long)),
which uses the monotonic clock and SurfaceView VSYNC. Device presentation can occur later
than the requested timestamp; the measurement below uses reported frame-render callbacks.
[Android documents](https://developer.android.com/reference/android/media/MediaCodec.OnFrameRenderedListener)
that releases before Android 14 can omit callbacks for rendered frames. These callback
counts and reported times therefore do not independently prove physical display cadence.

We inspected the pinned library's [5.6.3 surface decoder](https://github.com/alexeyvasilyev/rtsp-client-android/blob/5.6.3/library-client-rtsp/src/main/java/com/alexvas/rtsp/codec/VideoDecoderSurfaceThread.kt).
Its stabilization implementation moves its playback epoch forward on mild lateness. Our
custom independently draining decoder does not invoke that surface decoder, so toggling
`videoFrameRateStabilization` alone would not implement pacing here. We retain the custom
drainer and use the explicitly bounded clock instead.

Four JVM tests cover arrival jitter, late decoded bursts, timestamp resets/long source
pauses, and a synthetic hour of clock drift and variable arrival timing. All passed.

## Initial Fire TV candidate observations (superseded lateness policy)

On TV `192.168.23.85`, the unchanged player was traced for about 59 seconds, then the
candidate for over ten minutes, using both live bridge feeds and the Amlogic AVC hardware
decoder. Startup's first ten seconds are omitted from candidate statistics. These are
sequential live windows, not an identical recorded-input benchmark; source variability
limits causal comparison of the longest gaps.

| Metric | Front Door before | Front Door paced | Garage before | Garage paced |
| --- | ---: | ---: | ---: | ---: |
| Measured seconds | 58.266 | 618.817 | 58.616 | 620.934 |
| Reported render callbacks | 876 | 8,789 | 880 | 9,107 |
| Reported callback rate, fps | 15.017 | 14.201 | 14.996 | 14.665 |
| Median render interval, ms | 50.027 | 66.633 | 66.544 | 66.672 |
| 95th percentile render interval, ms | 166.702 | 116.648 | 133.366 | 116.493 |
| Median receive-to-render, ms | 74.759 | 104.930 | 81.533 | 107.123 |
| 95th percentile receive-to-render, ms | 185.623 | 149.226 | 150.836 | 149.717 |
| Longest render interval, ms | 383.867 | 1,016.656 | 433.206 | 676.692 |

Requested pacing wait never exceeded 67 ms. Median wait was 37.221 ms (Door) / 35.948 ms
(Garage). Actual median receive-to-render increased by approximately 30 / 26 ms. Pacing
is therefore not free in latency; it trades a small bounded allowance and skipped overdue
pictures for more even typical presentation.

Across complete 30-second windows, receive-to-render medians stayed in 103.192–106.531 ms
(Door) and 105.645–109.015 ms (Garage), without growing through the run. Final diagnostics
showed zero compressed queued units, no clock resets, and 391 / 115 decoded lateness
drops. This does not count every skipped picture: SurfaceView can also coalesce frames
on the same VSYNC. The lower callback rates and longer occasional reported gaps are retained
in the table rather than treating better typical spacing as proof of perfect playback.

Both TVs received the final APK. `.85` was relaunched with both hardware-decoded feeds;
`.58` was running another application, so its installation did not interrupt that app
and physical playback of this candidate on `.58` was not exercised. Diagnostic DEBUG
logging was restored to INFO after capture.

Initial APK SHA-256:
`9ed5c16bd2822ae764b152d5b8bd340c0f376fa6e6a2b4fb26314863bb7a79d8`.
Measured candidate APK (before diagnostic-counter volatile visibility annotations):
`4fff19a9cb1edae25bb8ce005978a8db0e501da85e59e283c23a02be97135c87`.
The initial scheduling policy is identical between them. It was subsequently corrected
after independent SurfaceFlinger measurements identified an artificial freeze; see
[the current display evidence](android-surface-pacing-2026-10-11.md).

Private raw evidence is retained under `.evidence/frame-pacing-2026-10-11/`:

| File | SHA-256 |
| --- | --- |
| `baseline-85.log` | `53fb483e7279afb41755653da8ccffd03a9af8fb6bb21f1233f2b68c3b9ca040` |
| `candidate-85.log` | `82349187b78cbc0c7cb4d95ca6641b305421281b8a5fbb854919350f8df051b4` |
| `baseline-summary.json` | `35205cc504f7ccb63ecdd6fbc5fd1a4dcae58122ab48d6589ab8aab499eed57a` |
| `candidate-summary.json` | `c20a2061afad58eaa78f6acbc7d90b0828dcaa188c7f541372bced7b08d89558` |

## Two-frame allowance comparison

A temporary 133 ms allowance was tested on `.85` with both cameras for about 264
seconds, then the 67 ms APK was restored. At nominal 15 fps these allowances are
approximately two frames and one frame respectively. They describe scheduling
headroom, not the number of compressed reference pictures retained by the decoder.

| Metric | Front Door, 133 ms | Garage, 133 ms |
| --- | ---: | ---: |
| Reported render callbacks | 3,870 | 3,902 |
| Reported callback rate, fps | 14.688 | 14.754 |
| Median render interval, ms | 66.630 | 66.642 |
| 95th percentile render interval, ms | 133.508 | 133.118 |
| Median receive-to-render, ms | 169.666 | 170.404 |
| 95th percentile receive-to-render, ms | 190.236 | 186.161 |
| Longest render interval, ms | 466.691 | 1,017.154 |
| Maximum requested future wait, ms | 133 | 133 |

The larger allowance retained a greater proportion of pictures, but added about
63–65 ms to median client receive-to-render delay. Typical cadence did not consistently
improve over the 67 ms run. These sequential live samples do not establish an optimal
budget or a causal comparison of long input gaps. The permanent default remains 67 ms.

Raw `two-frame-85.log` SHA-256:
`aff37e8a4ab1c0ac8d5a7d9e6ba0358c402e0384c8c5486b8fb605e3f88bdc38`.
Summary `two-frame-summary.json` SHA-256:
`01a07c98b760cefee2eb2763c57a64ee2c12622a952a4c67d7d9745f353f45c6`.

## Upstream timestamp correction

The bridge opens the SDK readable in byte mode and forwards Annex-B bytes without
the SDK's `sourceTimestampMs` metadata. Its FFmpeg input uses
`-use_wallclock_as_timestamps 1`, so outgoing presentation timestamps reflect arrival
timing. In the ten-minute Android trace, after omitting startup, incoming PTS increments
were already irregular: median 60.612 / 61.133 ms and 95th percentile
121.989 / 122.456 ms (Door / Garage). Media-to-arrival clock ratios remained
0.999858 / 0.999876. Client scheduling cannot recover source cadence that was
discarded before encoding.

A passive live SDK source-clock measurement confirmed regular source timing; see
[the source capture and transport correction](source-timestamp-transport-2026-10-11.md). Do not substitute a
synthetic nominal frame clock: the previous Garage clock-rate correction prevented
18.200 seconds of media accumulating during 11.879 seconds of delivery. Source clocks
also need validation across pauses and resets before a timestamp-preserving bridge
transport can be adopted. The bridge now preserves source timestamps in MPEG-TS;
its live verification is recorded separately. No SDK source change was required.

## Reproduce Android measurements

The tested installed application is `com.eufywall.viewer` / 0.5.2, from the workspace's
existing launcher/package changes. Those unrelated edits were preserved. The pacing
implementation is independent of the application ID and launcher resources.

```sh
adb -s 192.168.23.85:5555 shell setprop log.tag.EufyFrameTiming DEBUG
adb -s 192.168.23.85:5555 logcat -v epoch -s EufyFrameTiming:D EufyWallTV:I > timing.log
# Stop the collector after a bounded run. Select the application's current PID:
python3 tv/tools/frame_timing.py timing.log --pid PID --skip-seconds 10
adb -s 192.168.23.85:5555 shell setprop log.tag.EufyFrameTiming INFO
```

The analyzer matches received, decoded, and reported render-callback PTS on the same device.
It reports incoming PTS/arrival spacing and their clock ratio, render cadence,
requested wait, receive-to-render delay, and successive
30-second delay windows. Device monotonic clocks must not be compared with each other.
Receive-to-render excludes camera capture, P2P delivery, bridge processing, and the
network before this client's RTSP receive callback; it is not glass-to-glass latency.

## Linux

See [the native plugin and hardware verification record](linux-frame-pacing-2026-10-11.md).
The same principle applies after decoding, through a small GStreamer raw-video element.
Ordinary `sync=true` plus a lateness limit was rejected after it waited 277 ms: lateness
limits do not limit future timestamps. The custom element explicitly rebases/clamps those
timestamps. A one-picture leaky queue prevents raw frames accumulating behind a sink.
The installer builds the plugin for the destination CPU and GStreamer ABI.

## Boundaries

- A short bounded allowance smooths small timing variations. Longer missing-input periods
  remain visible; hiding them would require additional delay or invented frames.
- A scheduler bound does not bound firmware, socket, codec, compositor, or physical-display
  delay. Actual receive/render measurements must accompany the scheduling unit tests.
- Brightness flashing remains unverified by these timing tests. The cameras' displayed
  date/time also cannot substitute for a motion/latency measurement.
- Linux display-service enablement and TV power must be preserved during unattended tests.

## Final client correction and deployment

The initial late-frame rejection policy could manufacture a freeze after an upstream
pause: it rejected 21 consecutive current pictures after a 518 ms incoming gap.
Both clients now render the newest monotonic overdue picture immediately. Future
scheduling remains capped at 67 ms; duplicate/out-of-order pictures are rejected.
Compressed references remain intact.

The corrected APK SHA-256 is
`73028d782a68d61bc05788a37bb81c05de4bebc35e4e191ad0bd8f8a192f08c9`.
It was installed on both Fire TVs. On `.85`, 209 seconds of exact decoder-target
matches to SurfaceFlinger actual-present timestamps measured 14.79 FPS Front Door
and 14.89 FPS Garage. Receive-to-present medians were 49 ms and 58 ms, with maxima
95 ms and 99 ms. Zero pacing-policy drops occurred. A 606 ms incoming Front Door
gap produced a 567 ms actual-present gap and then the next consecutive picture,
rather than an additional run of rejected frames. These are client stage timings,
not glass-to-glass measurements. See the [SurfaceFlinger evidence](android-surface-pacing-2026-10-11.md).

Linux uses the same bounded-clock/latest-picture rule, preserves RTP timestamps,
and prevents sink QoS in the rewritten clock domain from reaching the decoder.
The native two-camera hardware test and installation checkpoint are documented
in [Linux evidence](linux-frame-pacing-2026-10-11.md). GaragePi was intentionally
off during installation; physical KMS presentation remains unverified.
