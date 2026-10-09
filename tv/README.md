# Eufy Wall TV

One APK supports Android TV/Google TV and Fire TV devices that run Fire OS 6 or newer (Android API 24 or newer). The wall displays up to four cameras. It reads each live stream's width and height from the bridge: two portrait views share 70% of the width, with landscape views stacked on the right. PiP produces landscape video, so the wall adapts to a mixed portrait/landscape arrangement or a 2×2 grid. Playback was tested on Fire TV; Google TV hardware still needs device testing.

Build with JDK 17, Android SDK platform 36, build tools 35, Gradle 8.13, and Kotlin 2.2.21 (pinned in the project):

```sh
gradle assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Open **Eufy Wall** from the TV launcher. The app discovers `_eufy-wall._tcp` (HTTP port 3000, TXT `rtsp=<RTSP port>`) or accepts a bridge IP address. It ignores a service that resolves to `127.0.0.1` or another loopback address. Select one to four cameras and choose **Start live wall**. Press Back to return to setup. The selection and bridge are saved locally. Each dual-lens camera has its own **View: Split/PiP** button in setup. Changing it sends a command to the camera through the bridge, restarts that camera's stream, and persists on the bridge. PiP uses the lower-right inset.

The bridge installer publishes the Avahi record. On a Linux client, `rtsp_base: auto` resolves the same record at startup; explicit RTSP URLs continue to work. The TV uses `/api/cameras`, `/ws`, and bounded `/hold/<serial>` requests. Video uses the bridge's advertised RTSP URL over TCP. It tries hardware decoding first and can fall back to software when hardware decoder instances are exhausted. For high-resolution cameras that exceed a TV's decoder limit, configure the bridge with `go2rtc.transcode: always` and `go2rtc.max_height: 720` (or set `BRIDGE_GO2RTC_TRANSCODE=always` and `BRIDGE_GO2RTC_MAX_HEIGHT=720`). This requires an FFmpeg build with a suitable hardware encoder. H.265 passthrough requires device decoder support.

## Live playback

The TV uses [rtsp-client-android 5.6.3](https://github.com/alexeyvasilyev/rtsp-client-android/tree/5.6.3)
for RTSP/RTP reception and direct MediaCodec surface playback. Media3 remains a transitive codec
utility dependency, but ExoPlayer no longer plays or buffers the video. Each tile has its own TCP
connection and decoder. Hardware is tried first; software remains available if hardware cannot
start, including when decoder instances are exhausted. Decoder failures reopen RTSP so the new
decoder receives SDP initialization data again.

`DrainingVideoDecoder` polls input for at most 5 ms and drains all available output before waiting
for more input. This avoids the pinned library's one-second input wait stranding already decoded
output. Frames render immediately: there is no playback speed controller, PTS presentation buffer,
or periodic catch-up restart. The library still has a 60-unit compressed input queue and the
hardware decoder has its own pipeline; this is not a claim of literally zero buffering. Upstream
camera, bridge and network delivery can still pause or arrive in bursts.

Actual surface-render callbacks drive the freeze watchdog. A live tile reconnects after five
seconds without a rendered frame (plus the one-second retry delay); startup allows fifteen seconds
for the first frame. Idle cameras release their players. A connection error also schedules a
one-second retry. `adb logcat -s EufyWallTV` reports decoder name, received/rendered counters,
compressed input queue depth, media timestamp distance ahead of rendering, and longest render gap.
`pendingMediaMs` is **not** camera-to-screen latency.

The library's codec helper requests vendor low-latency options, including Fire OS's older Amlogic
option. A request being applied is not proof of Android's standardized low-latency capability;
that API requires Android 11 and decoder support.

For frame timing diagnostics (normally disabled):

```sh
adb shell setprop log.tag.EufyFrameTiming DEBUG
adb logcat -v threadtime -s EufyFrameTiming EufyWallTV
# Restore normal logging after capture:
adb shell setprop log.tag.EufyFrameTiming INFO
```

The app has one activity instance and releases players, WebSocket connections and camera holds
when it goes into the background. Returning rebuilds the saved wall. See the
[direct-player migration and choppiness investigation](../docs/evidence/tv-low-latency-rtsp-2026-10-08.md)
for live-device measurements and verification limits. The previous
[Media3 backlog investigation](../docs/evidence/tv-rtsp-backlog-2026-10-08.md) and
[bridge clock investigation](../docs/evidence/live-timing-2026-10-08.md) remain historical evidence.
