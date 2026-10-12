# Eufy Wall TV

One APK supports Android TV/Google TV and Fire TV devices that run Fire OS 6 or newer (Android API 24 or newer). The wall displays up to four cameras. It reads each live stream's width and height from the bridge: two portrait views share 70% of the width, with landscape views stacked on the right. PiP produces landscape video, so the wall adapts to a mixed portrait/landscape arrangement or a 2×2 grid. Playback was tested on Fire TV; Google TV hardware still needs device testing.

Build with JDK 17, Android SDK platform 36, build tools 35, Gradle 8.13, and Kotlin 2.2.21 (pinned in the project):

```sh
gradle assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Open **Eufy Wall** from the TV launcher. The app discovers `_eufy-wall._tcp` (HTTP port 3000, TXT `rtsp=<RTSP port>`) or accepts a bridge hostname or IP address. Enter `homebridge.lan` to save `http://homebridge.lan:3000` and use that hostname for camera URLs advertised by the bridge. The name must resolve through the device's network resolver; `.lan` is normally supplied by LAN DNS, while mDNS hostnames normally end in `.local`. It ignores a service that resolves to `127.0.0.1` or another loopback address. Select one to four cameras and choose **Start live wall**. Press Back to return to setup. The selection and bridge are saved locally. Each dual-lens camera has its own **View: Split/PiP** button in setup. Changing it sends a command to the camera through the bridge, restarts that camera's stream, and persists on the bridge. PiP uses the lower-right inset.

Launcher artwork lives in `assets/icon.svg` and `assets/banner.svg`. The manifest
supplies both a square icon and a 16:9 banner, with an adaptive square icon on
API 26+. The density-specific PNGs are checked in; to regenerate them after
editing the SVGs, run `python3 assets/render.py` with ImageMagick installed.
The launcher activity points both its icon and banner to the wide artwork so
Fire TV can use the 16:9 image for its app tile; the application icon remains
square for system surfaces that require it.

The bridge installer publishes the Avahi record. On a Linux client, `rtsp_base: auto` resolves the same record at startup; explicit RTSP URLs continue to work. The TV uses `/api/cameras`, `/ws`, and bounded `/hold/<serial>` requests. Video uses the bridge's advertised RTSP URL over TCP. It tries hardware decoding first and can fall back to software when hardware decoder instances are exhausted. H.265 passthrough requires device decoder support. When the bridge advertises an optional `nativeRendition`, the TV selects HEVC only if a named hardware decoder advertises the native dimensions at 30 fps and the source HEVC profile, level, and tier. Unknown dimensions/profile/level/tier, unsupported sizes or codec levels, and software implementations retain the compatibility URL. The selected native decoder is configured with the advertised native geometry. A native startup failure, decoder error, or playback-progress timeout switches that camera to the compatibility URL for the rest of the app session; native playback never falls back to software. Compatibility playback retains hardware-first decoding with software fallback. This capability check is a selection rule, not proof of smooth simultaneous playback on a particular device.

Live output uses timestamp-based surface pacing with at most 67 ms of future scheduling headroom. Newest late pictures display immediately; duplicate/out-of-order decoded pictures can be discarded without dropping compressed references. Hardware decoding and software fallback are retained. See [the pacing implementation and measurements](../docs/evidence/client-frame-pacing-2026-10-11.md); the scheduling allowance is not a camera-to-screen latency guarantee.

Source dimensions are preserved by default. If a camera exceeds a display's
working decoder limit, enable bridge transcoding and set `go2rtc.max_height`
to the highest shared ceiling verified with all displays and concurrent tiles.
The cap preserves aspect ratio and does not enlarge smaller native streams.
This requires FFmpeg with a suitable encoder; hardware is tried before software.

## Live playback

The TV uses [rtsp-client-android 5.6.3](https://github.com/alexeyvasilyev/rtsp-client-android/tree/5.6.3)
for RTSP/RTP reception and direct MediaCodec surface playback. Media3 remains a transitive codec
utility dependency, but ExoPlayer no longer plays or buffers the video. Each tile has its own TCP
connection and decoder. Hardware is tried first; software remains available if hardware cannot
start, including when decoder instances are exhausted. Decoder failures reopen RTSP so the new
decoder receives SDP initialization data again.

For H.264, the decoder reads the SDP SPS before configuration and allocates for the coded
picture size. The tile viewport only controls display fitting; it does not set
the decoder's picture allocation or resize the stream. This avoids incorrect
crop rectangles when a portrait dual-camera stream is larger than its tile.
Declared codec size limits and actual decoded geometry are logged. Some Fire TV
drivers successfully decode dimensions beyond their advertised range, so a
capability declaration alone does not prove playback at a chosen resolution.
Direct HEVC retains its existing initialization path; this H.264 repair does not
claim a newly verified HEVC allocation change.

`DrainingVideoDecoder` polls input for at most 5 ms and drains all available output before waiting
for more input. This avoids the pinned library's one-second input wait stranding already decoded
output. Decoded pictures use the bounded 67 ms surface scheduling described above; there is no
playback speed controller or periodic catch-up restart. The library still has a 60-unit compressed input queue and the
hardware decoder has its own pipeline; this is not a claim of literally zero buffering. Upstream
camera, bridge and network delivery can still pause or arrive in bursts.

Codec render notifications drive the established playback freeze watchdog. A live tile reconnects after five
seconds without a rendered frame (plus the one-second retry delay); startup allows fifteen seconds
for the first frame. Idle cameras release their players. A connection error also schedules a
one-second retry. `adb logcat -s EufyWallTV` reports decoder name, received/rendered counters,
compressed input queue depth, media timestamp distance ahead of rendering, and longest render gap.
`pendingMediaMs` is **not** camera-to-screen latency.

The optional [frame freshness protocol](../docs/frame-freshness.md) tracks each picture
from its receipt at the bridge. Capability revision 2 requires an explicit mode:

- `observe` keeps established hardware MediaCodec/SurfaceView playback and pacing. It records
  bridge-ingress age and 500 ms deadline misses without dropping or blanking pictures.
  Codec render timestamps are informational: notifications can be delayed, batched, or omitted.
  Camera labels keep their names; capture-clock uncertainty remains in diagnostics.
  Observation does not guarantee freshness.
- `enforce` uses an owned EGL renderer. SurfaceTexture timestamps identify the exact acquired
  buffer; its own stamp and clock bound gate drawing and scheduled presentation. Expired output
  is suppressed and expired retained pixels become black. Compressed references keep decoding,
  and the watchdog follows decoded progress. Codec notifications never control visibility.
  GPU submission timing is distinct from physical display presentation.

Revision 1 without a mode preserves its legacy enforcement meaning. Old revision-1 clients
ignore revision 2, which prevents them from treating observation as enforcement. The binary
frame stamp remains version 1. Unsupported capability revisions retain established playback.

Strict enforcement **failed the live playback gate** because it caused frequent blanking;
it remains disabled in production. Observation is the diagnostic path for separating upstream
pauses, transport delay, clock uncertainty, and decoder backlog. Capture age remains unverified
in both modes. See the evidence document before making a freshness guarantee.

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

### RTSP TCP packet capability

When `/api/cameras` advertises `rtspTcpPacketSize` between 256 and 65535, the
TV app requests that size using go2rtc's `pkt_size` URL option. Existing query
parameters and an explicit `pkt_size` are preserved. Without this optional
capability, the URL is unchanged. Larger interleaved TCP packets reduce RTP
fragmentation and packet-processing work; they do not change the encoded
picture, frame rate, decoder buffering, or render scheduling. This option is
for TCP and is not a UDP MTU recommendation.

### Saved wall during bridge startup

Version 0.5 retries camera discovery every five seconds when a saved wall is
opened while the bridge is unreachable or returns an empty startup camera list.
Retries stop when the wall opens, when setup is changed manually, and while the
activity is in the background. Returning resumes the saved-wall attempt. Old
HTTP responses cannot overwrite a newer setup choice. This is setup recovery;
it does not add playback buffering or periodically rebuild active players.

### Wi-Fi power investigation

A bounded off/on/off test of a high-performance Wi-Fi lock did not show a consistent
latency or cadence improvement relative to the second TV. The temporary debug hook
and its permission were removed. The direct player and former Media3 player both lacked
that lock, so its absence was not a demonstrated migration regression. See
[the live test evidence](../docs/evidence/fire-tv-wifi-power-2026-10-12.md).

### Bounded native decoder diagnostic

Debug APKs accept `nativeProfileProbeSeconds` (1–120 seconds) on a fresh activity
launch. This explicit diagnostic temporarily bypasses only the advertised HEVC
profile/level check; hardware, dimensions, and rate requirements remain. It never
rewrites source headers or changes saved camera selection. At expiry, conservative
selection is restored and active players reconnect. Release builds ignore it.
Use only for a bounded hardware experiment, with original preferences restored
after a temporary third-camera selection. A successful probe does not change
production capability policy.
