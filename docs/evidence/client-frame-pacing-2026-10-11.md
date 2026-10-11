# Bounded client frame pacing — 2026-10-11

## What this fixes

Both clients previously displayed decoded pictures as soon as they became available.
An arrival burst therefore became a display burst. Neither path deliberately scheduled
each picture against its presentation timestamp. This change addresses that presentation
behavior; it does not establish the cause of Front Door's reported brightness flashing.

Hardware decoding remains the first choice, with the existing software fallback. There
is no change to bridge encoding, camera resolution, camera settings, or SDK source.
Compressed reference pictures continue through the decoder. Only overdue decoded
pictures can be discarded.

## Android implementation

`LiveFrameClock` maps media PTS to `System.nanoTime()` using the minimum decoded-arrival
offset observed in a rolling two-second window. Each output gets at most 67 ms of future
presentation headroom. A decoded picture more than 67 ms past its deadline is discarded.
Timestamp discontinuities reset the clock. The decoder does not sleep for pacing: it
continues independently draining input/output while the surface presents scheduled frames.

The 67 ms budget is approximately one frame at the cameras' nominal 15 fps. It is a
deliberate jitter allowance, not zero extra delay and not a bound on all camera/network/
hardware delay. Expiring offset samples allow clock drift to be tracked without repeatedly
adding lateness to a persistent playback epoch. Requested future timestamps are always
clamped to `now + 67 ms`.

The custom decoder calls [MediaCodec's timed release API](https://developer.android.com/reference/android/media/MediaCodec#releaseOutputBuffer(int,long)),
which uses the monotonic clock and SurfaceView VSYNC. Device presentation can occur later
than the requested timestamp; the measurement below uses actual frame-render callbacks.

We inspected the pinned library's [5.6.3 surface decoder](https://github.com/alexeyvasilyev/rtsp-client-android/blob/5.6.3/library-client-rtsp/src/main/java/com/alexvas/rtsp/codec/VideoDecoderSurfaceThread.kt).
Its stabilization implementation moves its playback epoch forward on mild lateness. Our
custom independently draining decoder does not invoke that surface decoder, so toggling
`videoFrameRateStabilization` alone would not implement pacing here. We retain the custom
drainer and use the explicitly bounded clock instead.

Four JVM tests cover arrival jitter, late decoded bursts, timestamp resets/long source
pauses, and a synthetic hour of clock drift and variable arrival timing. All passed.

## Fire TV observations

On TV `192.168.23.85`, the unchanged player was traced for about 59 seconds, then the
candidate for over ten minutes, using both live bridge feeds and the Amlogic AVC hardware
decoder. Startup's first ten seconds are omitted from candidate statistics. These are
sequential live windows, not an identical recorded-input benchmark; source variability
limits causal comparison of the longest gaps.

| Metric | Front Door before | Front Door paced | Garage before | Garage paced |
| --- | ---: | ---: | ---: | ---: |
| Measured seconds | 58.266 | 618.817 | 58.616 | 620.934 |
| Rendered frames | 876 | 8,789 | 880 | 9,107 |
| Render rate, fps | 15.017 | 14.201 | 14.996 | 14.665 |
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
on the same VSYNC. The lower delivered frame rates and longer occasional gaps are retained
in the table rather than treating better typical spacing as proof of perfect playback.

Both TVs received the final APK. `.85` was relaunched with both hardware-decoded feeds;
`.58` was running another application, so its installation did not interrupt that app
and physical playback of this candidate on `.58` was not exercised. Diagnostic DEBUG
logging was restored to INFO after capture.

Final APK SHA-256:
`9ed5c16bd2822ae764b152d5b8bd340c0f376fa6e6a2b4fb26314863bb7a79d8`.
Measured candidate APK (before diagnostic-counter volatile visibility annotations):
`4fff19a9cb1edae25bb8ce005978a8db0e501da85e59e283c23a02be97135c87`.
The scheduling policy is identical between them.

Private raw evidence is retained under `.evidence/frame-pacing-2026-10-11/`:

| File | SHA-256 |
| --- | --- |
| `baseline-85.log` | `53fb483e7279afb41755653da8ccffd03a9af8fb6bb21f1233f2b68c3b9ca040` |
| `candidate-85.log` | `82349187b78cbc0c7cb4d95ca6641b305421281b8a5fbb854919350f8df051b4` |
| `baseline-summary.json` | `35205cc504f7ccb63ecdd6fbc5fd1a4dcae58122ab48d6589ab8aab499eed57a` |
| `candidate-summary.json` | `c20a2061afad58eaa78f6acbc7d90b0828dcaa188c7f541372bced7b08d89558` |

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

The analyzer matches received, decoded, and actual rendered PTS on the same device.
It reports render cadence, requested wait, receive-to-render delay, and successive
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
