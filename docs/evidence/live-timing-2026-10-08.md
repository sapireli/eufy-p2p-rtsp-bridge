# Live stream timing evidence — 2026-10-08

## Report and scope

The user reported Garage CLE sometimes approximately 15 seconds behind real motion, on the Pi
and Fire TVs. Front Door was initially faster. Target bridge: DietPi on the Intel 2012 Mac Mini,
FFmpeg 7.1, go2rtc 1.9.14. Garage input was HEVC 1280×1440; Front Door input was H.264
1600×2200. Both use the resized VA-API H.264 Main output, without B frames.

This change concerns the bridge's FFmpeg timestamps and startup analysis, TV playback thresholds,
and a Linux reconnect regression encountered during deployment. It does not change the SDK.

## Clock fault measured before the change

A raw RTSP-over-TCP receiver counted video RTP marker packets and compared their 90 kHz media
clock with monotonic arrival time, excluding the first three seconds. It had no decoder or
player jitter buffer. The old FFmpeg output advanced by exactly 6000 ticks per frame (15 fps),
even when live frames arrived faster.

| Sample | Camera | Frames after settling | Arrival interval (s) | Media interval (s) | Media / arrival |
| --- | --- | ---: | ---: | ---: | ---: |
| Before, first | Front Door | 173 | 11.986 | 11.467 | 0.9567 |
| Before, first | Garage | 274 | 11.879 | 18.200 | 1.5321 |
| Before, second | Front Door | 212 | 11.818 | 14.067 | 1.1903 |
| Before, second | Garage | 207 | 11.855 | 13.733 | 1.1585 |

Garage's media clock gained 6.321 seconds during 11.879 seconds of delivery in the first sample.
A player scheduling those timestamps can accumulate delay. The mismatch was intermittent:
a subsequent simultaneous prototype A/B did **not** reproduce the large difference (old ratio
1.0271, candidate 1.0499). That A/B alone does not establish an improvement or a universal
camera frame rate. The two earlier production samples establish a real outgoing clock fault.

The bridge exposes raw Annex-B HTTP video, which contains no container packet PTS. FFmpeg had
synthesized timestamps from the nominal frame rate in the stream. The correction timestamps
input with arrival wall time, preserves frame timestamps through the encoder with
`-fps_mode:v passthrough`, and uses `-enc_time_base:v 1:90000` to avoid quantizing them back to
1/15-second units. It applies to the resized Linux VA-API preset used on this installation.

## Production clock results

The first timing correction was deployed at 21:42:50 UTC. A later production sample returned:

| Camera | Frames after settling | Arrival interval (s) | Media interval (s) | Media / arrival |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 184 | 11.949 | 11.813 | 0.9887 |
| Garage | 180 | 11.893 | 11.858 | 0.9970 |

The user then reported that Garage's long delay seemed gone, while Front Door remained a few
seconds behind. This is subjective motion-test feedback, not a measured end-to-end latency.
A constant delay can remain even when the media clock has the correct rate.

## Startup analysis and remaining delay

An isolated FFmpeg producer consumed each **real live** raw HTTP camera stream and encoded with
the same VA-API scale/Main/GOP/no-B-frame settings, sending RTP to a local UDP listener. Its
first-packet delay includes attachment to the next live keyframe, input analysis, decode and encode.
The raw bridge consumer waits for a fresh live keyframe; it does not replay the last cached one.

| Input | Input analysis | Time from process launch to first RTP packet |
| --- | --- | ---: |
| Front Door | Default | 2113 ms |
| Front Door | 100000 µs analysis / 262144-byte probe | 826 ms |
| Garage | 100000 µs analysis / 262144-byte probe | 1764 ms |

These are individual sequential runs, not a distribution or a controlled motion-to-screen
measurement. They support reducing a measured startup cost; keyframe arrival also varies.
Both candidates produced video for the 18-second collection window. FFmpeg logged the existing
pixel-range/default-quality warnings, with no decode errors in these samples.
The smaller probe retains its packets for decoding: it does not use `-fflags nobuffer` or drop
encoded reference frames. The configured byte/time limits are analysis limits, not a promised
bound on time to the first frame.

This input preset was deployed at 21:52:49 UTC. The actual FFmpeg processes included:

```text
-reinit_filter 0 -use_wallclock_as_timestamps 1 -analyzeduration 100000 -probesize 262144
-i http://127.0.0.1:3000/stream/<camera>
-vaapi_device /dev/dri/renderD128 -vf scale=-2:720:eval=frame,format=nv12,hwupload
-codec:v h264_vaapi -profile:v main -g:v 30 -bf:v 0
-fps_mode:v passthrough -enc_time_base:v 1:90000
```

The final production RTSP sample measured:

| Camera | Frames after settling | Arrival interval (s) | Media interval (s) | Media / arrival | Timestamp regressions |
| --- | ---: | ---: | ---: | ---: | ---: |
| Front Door | 180 | 11.922 | 11.950 | 1.0023 | 0 |
| Garage | 172 | 11.937 | 12.034 | 1.0081 | 0 |

Both Pi planes imported V4L2-decoded buffers and advanced in samples taken 170 ms apart after
this restart. Startup V4L2 timestamp/dequeue warnings still occurred; the earlier Pi deployment
also had startup warnings. These observations do not establish long-term stability.

## TV playback policy

The app used Media3 1.11.1's default load control. That version requires 1000 ms to start and
2000 ms to resume after starvation. The app now requests 200 ms to start and 500 ms to resume,
with min/max loading targets of 500/1500 ms. It logs the actual buffered duration at playback
state transitions and the first rendered frame.

These thresholds are not a hard maximum on RTSP latency. Media3's RTSP loader continuously reads
RTP; its media period does not discard buffered samples when the maximum loading target is met.
No encoded-frame dropping, live-edge seeking or periodic forced restart was added.
Decoder selection and fallback remain as before.

Primary implementation references:
- [Media3 1.11.1 DefaultLoadControl](https://github.com/androidx/media/blob/1.11.1/libraries/exoplayer/src/main/java/androidx/media3/exoplayer/DefaultLoadControl.java)
- [Media3 1.11.1 RTSP media period](https://github.com/androidx/media/blob/1.11.1/libraries/exoplayer_rtsp/src/main/java/androidx/media3/exoplayer/rtsp/RtspMediaPeriod.java)
- [FFmpeg input analysis options](https://ffmpeg.org/ffmpeg-formats.html)
- [FFmpeg encoder time base and frame timestamp options](https://ffmpeg.org/ffmpeg.html)

### Fire TV deployment and observed buffering

The APK built successfully and was installed with `adb install -r` on both Fire TVs
(`192.168.23.58:5555` and `192.168.23.85:5555`), then launched with the saved two-camera selection.
SHA-256: `bf987315bff6382e7d8c03e24b89bdbb8bace12da3f0c6643889f08a46cb7f84`.
Screenshots showed both Split camera views. Each process logged `HW.video.avc` decoder activity.

| TV | Camera | First rendered frame after player creation | Buffered duration at first frame |
| --- | --- | ---: | ---: |
| .58 | Front Door | 767 ms | 278 ms |
| .58 | Garage | 1384 ms | 1001 ms |
| .85 | Front Door | 2774 ms | 161 ms |
| .85 | Garage | 1438 ms | 931 ms |

The buffer policy did not eliminate starvation. On .58, Front Door initially rebuffered for
2120 ms and then 316 ms; Garage had short pauses on both TVs. Around 53–57 seconds into the .58
run, Garage resumed with 1220 ms and then 2525 ms buffered after 949 ms and 288 ms pauses.
This directly demonstrates why the configured maximum is not a hard RTSP latency cap.
The two TVs paused at similar times, consistent with common input delivery gaps, but this
observation alone does not identify where those gaps originated.

A 45-second production clock probe after deployment found no timestamp regressions:
Front Door 619 settled marker frames, 41.866 s arrival / 41.191 s RTP (ratio 0.9839);
Garage 669 settled marker frames, 41.996 s arrival / 42.645 s RTP (ratio 1.0155).
These windows still include delivery jitter; they do not establish zero drift indefinitely.

### Follow-up: distinguish camera timestamps from real latency

The user then identified the remaining 3–4-second difference by comparing camera timestamps
on the wall. A simultaneous raw/RTSP Doorbell comparison decoded each input with one thread,
cropped its embedded timestamp, and recorded local wall time when each JPEG was received.
Five clearly OCR-readable RTSP samples and thirty raw samples had overlapping apparent clock
ages (roughly 7–8 seconds); OCR failed on most resized RTSP samples, so this was not used as
an exact latency estimate. A single raw/TV capture showed raw Doorbell 6:00:15 PM at
22:00:22.487 UTC, while a TV capture taken between 22:00:22 and 22:00:24 showed Doorbell
6:00:13 PM and Garage 6:00:16 PM. A single pair cannot separate jitter from a constant offset.

To test whether the cross-camera timestamp gap already existed upstream of FFmpeg transcoding,
two raw HTTP inputs were decoded concurrently with `-threads 1`, a 100000 µs analysis window,
and a 262144-byte probe. Frames were cropped to the timestamp region and sampled every fifteen
frames. The following four strips were read visually (not inferred from unreadable OCR):

| Raw input | Local JPEG arrival (UTC) | Embedded camera time (EDT) |
| --- | --- | --- |
| Garage | 22:04:46.185 | 18:04:42 |
| Front Door | 22:04:46.795 | 18:04:39 |
| Garage | 22:05:01.949 | 18:04:58 |
| Front Door | 22:05:02.538 | 18:04:55 |

The approximately three-second **embedded timestamp difference** is present before transcoding,
RTSP and TV playback. It does not establish three seconds of physical motion delay. Different
camera clocks, camera/station delivery delay, and SDK delivery delay remain distinct possible
explanations for that incoming difference. No clock correction or SDK alteration was made on
the basis of this comparison. A physical movement or a common visible reference is required to
measure actual latency independently of each camera's clock.

## Linux reconnect fix

The initial bridge restart exposed a separate client bug: an early WebSocket hello, before
camera stream keys were available, overwrote explicitly configured named URLs with serial-number
paths. These returned 404. `plansFor` now preserves each configured tile URL across event-driven
reconnects. Its regression test covers the fallback serial and a later stream-key change.
The ARMv6 candidate deployed at 21:47:32 UTC has SHA-256
`79d6f308089049c10de76f02188d205e332fdd8426915d38af586ecc5eada94f`.
The subsequent bridge restart reconnected to the named Front Door/Garage paths without a manual
Pi service restart.

## Reproduce the clock measurement

```sh
python3 server/scripts/measure-rtsp-clock.py \
  rtsp://<bridge>:8554/front_door_cle rtsp://<bridge>:8554/garage_cle
```

The probe discovers the video track and RTP clock rate from SDP. It samples 15 seconds and
excludes the first three by default; use `--duration` and `--settle` to change that window.
It reports packet arrival/media progression and signed timestamp regressions. A ratio near one
cannot prove zero camera-to-screen delay, nor distinguish camera buffering from a constant
player offset. A simultaneous physical motion/display observation is still required for that.

Checks so far: all 102 server tests and all eight client packages with `go test -race ./...`
passed; `go vet ./...` passed.
