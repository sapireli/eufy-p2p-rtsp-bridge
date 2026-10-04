# Eufy Wall TV

One APK supports Android TV/Google TV and Fire TV devices that run Fire OS 6 or newer (Android API 23 or newer). The first release displays one camera or a fixed 2×2 grid of up to four cameras. Playback was tested on a Fire OS 7 device; older and Google TV devices still need device testing.

Build with JDK 17, Android SDK platform 36, build tools 35, and Gradle 8.13:

```sh
gradle assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Open **Eufy Wall** from the TV launcher. The app discovers `_eufy-wall._tcp` (HTTP port 3000, TXT `rtsp=<RTSP port>`) or accepts a bridge IP address. It ignores a service that resolves to `127.0.0.1` or another loopback address. Select one to four cameras and choose **Start live wall**. Press Back to return to setup. The selection and bridge are saved locally.

The bridge installer publishes the Avahi record. On a Linux client, `rtsp_base: auto` resolves the same record at startup; explicit RTSP URLs continue to work. The TV uses `/api/cameras`, `/ws`, and bounded `/hold/<serial>` requests. Video uses the bridge's advertised RTSP URL over TCP and requires a hardware video decoder. For high-resolution cameras that exceed a TV's decoder limit, configure the bridge with `go2rtc.transcode: always` and `go2rtc.max_height: 720` (or set `BRIDGE_GO2RTC_TRANSCODE=always` and `BRIDGE_GO2RTC_MAX_HEIGHT=720`). This requires an FFmpeg build with a suitable hardware encoder. H.265 passthrough requires device decoder support.
