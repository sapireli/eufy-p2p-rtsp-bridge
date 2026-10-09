# Direct RTSP TV playback and choppiness correction — 2026-10-08

## Scope and deployed artifact

Replace the TV's ExoPlayer RTSP playback/catch-up controller with independent direct MediaCodec
surface players using rtsp-client-android **5.6.3**, upstream tag commit
`4abac4b09d3f5441c255bb7a0217ddeebb36d368`. Preserve setup, discovery, saved selection, dual-lens
controls, layouts, idle release, camera holds and activity lifecycle. This change does not modify
the bridge, Eufy SDK, transcoding configuration or Linux client.

Both Fire TVs (LAN addresses ending .58 and .85) are AFTALMO devices running API 28. The final
0.2/versionCode 2 APK builds with JDK 17, Gradle 8.13, Kotlin 2.2.21, compileSdk 36 and targetSdk 35.
The pinned RTSP dependency raises minSdk to 24. Version 6.1.1 was examined but requires a newer
SDK/Kotlin toolchain; it is not the version tested or shipped here. JitPack is restricted to the
RTSP library's Maven group. Media3 1.11.1 stays explicitly pinned for the library's codec utilities;
there is no ExoPlayer playback instance, Media3 RTSP source, or speed controller in the TV app.

The installed `base.apk` SHA-256 was read on **both** devices and matched the built artifact:
`f0f251cf2eec7629fad2b4357e8db547a6bb7bfbef35568d0f3821cf5e3b88b9`.

`assembleDebug lintDebug` passed (zero lint errors, existing warnings remain). Both final
installed players resumed rendering. The interruption-tested APK was `e1e4f588b37842c386cd75ae32a755f3eb34cafcfa8dedf3b3517212286e1d25`;
the final rebuild only factors the diagnostic logging tag into a constant to satisfy lint,
with no change to the playback loop. Both APKs are preserved below.

## Regression and root cause

The user confirmed both cameras were visible and moving in the initial direct-player candidate,
then reported choppiness. ADB screenshots showed black hardware video planes, so screenshots were
not used as proof that the picture was absent or smooth.

The pinned upstream decoder loop obtains input before reading output. Its `VideoFrameQueue.pop()`
uses a **1,000 ms** default wait. It releases at most one actual decoded output per outer loop;
the inner loop repeats only for format/buffer changes. Consequently a decoded frame can wait
for another input frame, and arrival bursts lead to bursts of output release. The adapter had
preserved that loop. Merely calling `releaseOutputBuffer(..., true)` immediately did not make
output draining independent of input.

`DrainingVideoDecoder` now drains **all available output** before polling for another input,
uses a **5 ms input poll**, retains pending compressed input when no codec input slot is free,
and uses zero-timeout codec dequeue calls. It continues decoding compressed reference frames;
it does not drop them for catch-up. It keeps the library's hardware/software codec discovery and
vendor low-latency helper. Hardware startup failure can fall back to software. A non-transient
runtime decoder failure reconnects the full RTSP source, which reissues SDP initialization;
recreating a bare codec could lose SPS/PPS/VPS on cameras that do not repeat them.

The optional upstream timestamp stabilization was **not** enabled. It shifts its presentation
baseline forward for lateness and does not cap the resulting offset. The deployed path has no
presentation buffer or playback speed changes. The user confirmed **“Smoother now”** after the
independent-drain candidate was installed. The final artifact preserves that loop and adds
supported viewport sizing, safe codec-error reconnection and opt-in timing diagnostics.

## Matched-frame measurements on .85

Trace each received RTP video unit and each released decoded output by the same media timestamp.
Use `System.nanoTime()` at reception/release and the actual `systemNano` value supplied by
`MediaCodec.OnFrameRenderedListener` for display intervals. Callback arrival on the main thread
is not used to compute render spacing. The library's `timestampMs` name is misleading: video RTP
ticks are converted to **microseconds**, then passed unchanged to MediaCodec.

These are sequential production windows, approximately 10–11 seconds before and 27 seconds after;
they are not identical-input lab trials. Statistics below match reception to decoded output,
not sensor capture to display. Startup units without a matching receive record are excluded.

| Camera | Candidate | Matched units | Median receive→decoded release | p95 | Maximum |
|---|---|---:|---:|---:|---:|
| Front Door | Original upstream loop | 158 | 51.9 ms | 227.5 ms | 599.0 ms |
| Front Door | Independent drain | 412 | 6.0 ms | 8.1 ms | 295.6 ms |
| Garage | Original upstream loop | 168 | 44.6 ms | 216.3 ms | 756.5 ms |
| Garage | Independent drain | 384 | 6.1 ms | 7.2 ms | 14.4 ms |

Input still arrived irregularly. In the corrected trace, Front Door's longest receive gap was
710.9 ms and Garage's was 914.8 ms. Render gaps remained about 700 and 900 ms respectively.
The decoder correction does not invent missing incoming frames or prove that all upstream
stuttering is gone. It removes the additional wait for the next compressed input.

`pendingMediaMs` is received media timestamp minus last rendered media timestamp, divided by
1,000. It describes video ahead of display inside this path, **not glass-to-glass latency**.
Both restored two-camera walls later reported empty compressed queues and 225–346 ms timestamp
distance in the last samples collected around 22:33:54–22:34:02 EDT.

## Playback-path interruption tests

A temporary bridge egress `tc` filter dropped only TCP traffic from RTSP port 8554 to .85. It did
not stop the bridge or interfere with the Pi/.58. The filter was removed by a cleanup trap;
a final `tc filter show ... pref 49152` check returned no filter.

- **Three-second interruption:** 02:32:43.971–02:32:46.977 UTC (22:32:43–46 EDT).
  Both players remained in the same process/decoder session. By 22:32:52.9, Front Door had 220
  rendered frames and Garage 225, queues were empty, and timestamp distances were 163/303 ms.
  Their longest actual render gaps were both 3,317 ms. Recovery is demonstrated by that sample;
  this does not claim an exact sub-second recovery time.
- **Six-second interruption:** 02:32:57.115–02:33:03.122 UTC.
  Read timeouts were logged at 22:33:03.678/.963. Both reopened with hardware decoders.
  Garage rendered at 22:33:05.953 (2.83 s after filter removal), Front Door at 22:33:06.276
  (3.15 s after removal). At 22:33:10.7/.97 both queues were empty and timestamp distances
  were 284/409 ms. The app process stayed alive.

The five-second rendered-frame watchdog and fifteen-second startup deadline remain. Errors and
watchdog timeouts retry after one second. These are recovery thresholds, not video buffering.

## Multi-tile and lifecycle checks

On .58, temporarily select Front Door, Garage and Balcony. Front Door/Garage start with
`OMX.amlogic.avc.decoder.awesome`. Balcony's first RTSP attempts time out while its stream becomes
available; it then exceeds the two H.264 hardware instances and starts `OMX.google.h264.decoder`.
At 22:32:58.292 it reports **five actually rendered frames**, with queue depth zero and 158 ms
received/rendered timestamp distance, while both hardware tiles continue. This proves basic
three-tile playback and software fallback, not a long software-decoding soak test. The original
two-camera selection and owner identifier were restored. Four simultaneously live tiles were
not retested during this migration.

The initial direct-player lifecycle test sent .85 Home and queried go2rtc consumers. There were
no .85 consumers; the Pi/.58 continued consuming. Returning .85 added exactly one consumer per
selected camera. Repeated launcher intents reused the same activity. Final code retains that
lifecycle and the decoder drain exits on the existing stop flag/interrupt.

Google TV compatibility is manifest/build compatibility only; no physical Google TV was available.
There is no new H.265 passthrough result: the tested bridge endpoints supplied H.264, 524×720
Front Door and 640×720 Garage. Android's standardized low-latency decoder feature requires API 30;
these API 28 Fire TVs instead receive the library's vendor options, including `vdec-lowlatency`.
Applying those options is not proof of a standardized decoder capability or sub-second total delay.

## Reproduction and preserved evidence

Build/install as described in [TV README](../../tv/README.md). Enable `EufyFrameTiming` with
`adb shell setprop log.tag.EufyFrameTiming DEBUG`, stream logcat to a file during playback, then
restore the tag to `INFO`. Match events by camera and `ptsUs`, excluding reused timestamps across
reconnections. Render events log the codec-provided `atNs`; received/decoded events use the local
monotonic clock. The summary above uses the original-loop and independent-drain logs separately.

Private raw logs/APKs are retained under `.evidence/low-latency-rtsp-2026-10-08/` (gitignored).
The previously deployed APK is separately preserved for rollback. Artifact hashes:

```text
6ed66a6398c7c852dce9ae33ee29e525140b03b3fb368804e3bf61906f98660b  immediate-timing.log
812557f0620741512c7d9d030f728271cdfabe00b9ad8716fdbc8c38cdb72bf3  drained-timing.log
72708291ae7d2a8bde85376b4a74c96a34ee41def1304660ff3a2092b5c110e0  final-recovery.log
2dc8fd6a7157f1e27f5905e816d65e92269110d642b5c8482940f62f95844c4d  final-pause-3s.txt
fe5f0ad7c7ece458bc4d76cc36928c7c556dd45ad113b7701183c3693fe99f79  final-pause-6s.txt
290f93d8c0949825ecdb47fdb45ebfef318168642a2614dbda68d8493907bd13  final-three-camera.log
8b635745a179d89876d88e2fd01814a3cb84f76850a752ef64297118a43e34c4  home-consumers.txt
27b304637620b7051181f6d9154d3e354c01e54a7c77dbfd15a7bc46eaf6cb09  returned-consumers.txt
f0f251cf2eec7629fad2b4357e8db547a6bb7bfbef35568d0f3821cf5e3b88b9  candidate-app-debug.apk
e1e4f588b37842c386cd75ae32a755f3eb34cafcfa8dedf3b3517212286e1d25  interruption-tested-app-debug.apk
```

Source references:

- [Pinned upstream decoder loop](https://github.com/alexeyvasilyev/rtsp-client-android/blob/5.6.3/library-client-rtsp/src/main/java/com/alexvas/rtsp/codec/VideoDecodeThread.kt)
- [Pinned queue default wait](https://github.com/alexeyvasilyev/rtsp-client-android/blob/5.6.3/library-client-rtsp/src/main/java/com/alexvas/rtsp/codec/FrameQueue.kt)
- [Pinned optional surface pacing](https://github.com/alexeyvasilyev/rtsp-client-android/blob/5.6.3/library-client-rtsp/src/main/java/com/alexvas/rtsp/codec/VideoDecoderSurfaceThread.kt)
- [MediaCodec API and rendered-frame callback](https://developer.android.com/reference/android/media/MediaCodec)
- [Android low-latency decoder support](https://source.android.com/docs/core/media/low-latency-media)
