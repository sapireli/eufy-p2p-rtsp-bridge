# Eufy Wall TV

One APK supports Android TV/Google TV and Fire TV devices that run Fire OS 6 or newer (Android API 23 or newer). The wall displays up to four cameras. It reads each live stream's width and height from the bridge: two portrait views share 70% of the width, with landscape views stacked on the right. PiP produces landscape video, so the wall adapts to a mixed portrait/landscape arrangement or a 2×2 grid. Playback was tested on Fire TV; Google TV hardware still needs device testing.

Build with JDK 17, Android SDK platform 36, build tools 35, and Gradle 8.13:

```sh
gradle assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Open **Eufy Wall** from the TV launcher. The app discovers `_eufy-wall._tcp` (HTTP port 3000, TXT `rtsp=<RTSP port>`) or accepts a bridge IP address. It ignores a service that resolves to `127.0.0.1` or another loopback address. Select one to four cameras and choose **Start live wall**. Press Back to return to setup. The selection and bridge are saved locally. Each dual-lens camera has its own **View: Split/PiP** button in setup. Changing it sends a command to the camera through the bridge, restarts that camera's stream, and persists on the bridge. PiP uses the lower-right inset.

The bridge installer publishes the Avahi record. On a Linux client, `rtsp_base: auto` resolves the same record at startup; explicit RTSP URLs continue to work. The TV uses `/api/cameras`, `/ws`, and bounded `/hold/<serial>` requests. Video uses the bridge's advertised RTSP URL over TCP. It tries hardware decoding first and can fall back to software when hardware decoder instances are exhausted. For high-resolution cameras that exceed a TV's decoder limit, configure the bridge with `go2rtc.transcode: always` and `go2rtc.max_height: 720` (or set `BRIDGE_GO2RTC_TRANSCODE=always` and `BRIDGE_GO2RTC_MAX_HEIGHT=720`). This requires an FFmpeg build with a suitable hardware encoder. H.265 passthrough requires device decoder support.

Live playback waits for 200 ms of video to start and 500 ms to resume after starvation, instead
of Media3 1.11.1's 1000/2000 ms defaults. These are jitter allowances, not a hard maximum on
RTSP delay. `adb logcat -s EufyWallTV` reports playback state, actual buffered duration and
time to the first rendered frame. See [live timing evidence](../docs/evidence/live-timing-2026-10-08.md)
for the bridge clock correction and the limits of these measurements.

RTSP also needs explicit recovery from accumulated playback delay: Media3's RTSP source keeps
loading and does not use its automatic live-offset speed control. Each tile checks its buffered
video every 500 ms. At 1500 ms it begins catching up, using 1.1×, 1.25× or 1.5× playback according
to the remaining buffer, then returns to 1× at 700 ms or below. During catch-up motion can briefly
look faster. It decodes the existing stream continuously; there is no live-edge seek or periodic
player restart. Ordinary buffers below the catch-up threshold stay at normal speed. These values
control player backlog, not a guarantee of camera-to-screen latency.

The app has one activity instance and releases players, WebSocket connections and camera holds
when it goes into the background. Returning to the app rebuilds the saved wall. See the
[TV backlog investigation](../docs/evidence/tv-rtsp-backlog-2026-10-08.md) for live-device results.
