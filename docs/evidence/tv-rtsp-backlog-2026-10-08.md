# Front Door TV backlog — 2026-10-08

## Report and the comparison that located the delay

After the bridge RTP clock correction, the user reported Front Door in the Eufy phone app
approximately four seconds ahead of Eufy Wall. That is a user comparison, not a recorded
physical motion-to-screen measurement. Earlier cross-camera OSD comparisons had found Front
Door behind Garage even in direct SDK frames; those comparisons cannot locate player delay
because they compare different camera clocks.

The follow-up compared **the same camera** at successive pipeline stages:

- With Garage stopped, a Front Door SDK keyframe arrived at **22:51:55.375 UTC**, displaying
  encoded OSD time **18:51:48 EDT**. It was saved directly in `LiveStream.video` and decoded
  offline as one finite H.264 IDR with SPS/PPS, one decoder thread and EOF.
- A Fire TV .85 screenshot taken around **22:51:56–57 UTC** displayed Front Door
  **18:51:44 EDT** and the Garage idle label. The newer SDK image was available before the
  screenshot. This establishes several seconds of additional downstream delay, independent
  of the camera's OSD clock offset.
- In concurrent one-thread raw HTTP and production RTSP decodes, raw JPEG receipt at
  **22:54:10.207** and RTSP receipt at **22:54:10.641** both showed **18:54:03**. Later receipts
  at **22:54:26.509** and **22:54:26.762** both showed **18:54:19**. These sampled same-camera
  pictures do not show a multi-second transcoder delay; their one-second OSD resolution does
  not establish precise sub-second latency.
- At **18:51:09.037**, the old TV app logged Front Door resuming with **4127 ms** buffered;
  another simultaneous activity logged **4112 ms**. Neither activity then caught up while
  ordinary playback continued. The screenshot above was approximately 47 seconds later.
- In a later socket snapshot, the two Front Door TCP connections to .85 each had only
  **494 bytes** in the bridge's Send-Q. This snapshot is not a maximum over the whole run,
  but it does not indicate a multi-second server-side TCP backlog at that instant.

## Why the previous buffer setting was insufficient

Version-specific primary sources:

- [Media3 1.11.1 RtspMediaPeriod](https://github.com/androidx/media/blob/1.11.1/libraries/exoplayer_rtsp/src/main/java/androidx/media3/exoplayer/rtsp/RtspMediaPeriod.java):
  RTP loaders keep appending samples; `continueLoading` only reports their state, and
  `reevaluateBuffer` does not discard samples. The configured maximum loading target is not
  a hard RTSP queue limit.
- [Media3 1.11.1 RtspMediaSource](https://github.com/androidx/media/blob/1.11.1/libraries/exoplayer_rtsp/src/main/java/androidx/media3/exoplayer/rtsp/RtspMediaSource.java):
  its timeline is not dynamic and has no window-start wall time.
- [Media3 1.11.1 ExoPlayerImplInternal](https://github.com/androidx/media/blob/1.11.1/libraries/exoplayer/src/main/java/androidx/media3/exoplayer/ExoPlayerImplInternal.java):
  automatic live-offset speed control requires a suitable dynamic live timeline. This RTSP
  source does not supply one.

After starvation, the playback position pauses while incoming RTP timestamps continue advancing.
Once queued video arrives, normal 1× playback can retain the new delay indefinitely. Lowering
startup/rebuffer thresholds helps initial waits but does not drain an existing queue. The
live traces and the observed old-app 4.1-second buffer support this mechanism.

## Changes

`LiveRtspPlaybackControl` provides one feedback controller per camera:

- Sample buffered duration every 500 ms while the current player is visible and playing.
- Enter catch-up at 1500 ms buffered; leave at 700 ms or below.
- Use 1.5× at 2500 ms or above, 1.25× at 1500 ms or above, and 1.1× while finishing catch-up.
- Return to normal speed during starvation or pause; reevaluate after playback resumes.
- Decode the existing reference chain continuously. No encoded-reference dropping, unseekable
  RTSP seek or periodic player restart was introduced for latency control.

The controller targets accumulated **player** delay. It cannot bound camera/station latency or
repair an input that cannot arrive or decode fast enough. Input delivery gaps can still cause
visible pauses. Catch-up temporarily speeds up visible motion.

The live audit also found two Front Door RTSP consumers from .85 and duplicated playback-state
logs in one process. On .58, the old app still streamed while Amazon Settings was foreground.
The activity lacked `onStop` cleanup and had standard launch mode. The app now uses `singleTask`,
releases its players/events/holds in the background, and rebuilds the saved wall on return.
This fixes observed hidden/duplicate resource use. It does not establish that duplicates alone
caused the measured latency.

## Live recovery test

APK SHA-256: `656eac7df6d02dd9c380963a7383ce1c167e8bd370478c3344436d88acd32bff`.
`adb install -r` succeeded on .58 and .85. The candidate was launched on .85 with Front Door and
Garage selected. The bridge remained the same production preset and SDK; neither was modified
for this client fix. Hardware AVC decoder activity was logged throughout the sample.

A temporary `tc` egress filter paused only bridge TCP port 8554 traffic destined for .85 from
**23:00:21.789 through 23:00:24.801 UTC**, approximately three seconds. Other destinations and
SDK ingress remained connected. TCP retransmission preserves the video data; the test did not
inject missing RTP reference frames. The exact filter and temporary `clsact` qdisc were removed
with a cleanup trap. The original `mq`/`fq_codel` configuration was verified afterward.

| TV local time (EDT) | Camera | Buffer (ms) | Playback speed / observation |
| --- | --- | ---: | --- |
| 19:00:25.277 | Front Door | 3075 | 1.5×, catch-up started |
| 19:00:25.415 | Garage | 3482 | 1.5×, catch-up started |
| 19:00:30.811 | Front Door | 1460 | 1.1× |
| 19:00:30.945 | Garage | 1231 | 1.1× |
| 19:00:35.845 | Front Door | 167 | returned to 1× |
| 19:00:36.970 | Garage | 364 | returned to 1× |
| 19:00:37.358 | Front Door | 1907 | a subsequent rebuffer caused another catch-up, 1.25× |
| 19:00:41.878 | Front Door | 411 | returned to 1× again |

The first excess queue drained in approximately 10–12 seconds after resumption. Front Door
then starved from 19:00:35.964 to 19:00:36.796 (832 ms), so the test does **not** establish
pause-free playback. Player creation elapsed times continued; no stall-triggered player restart
or decoder error was observed in the recovery sample. The same mechanism recovered the next
backlog rather than leaving it as a permanent offset.

## Isolation experiment and its verification limit

The first solo-camera capture failed when a temporary listener accessed `source.stream` after
Garage source disposal. That uncaught diagnostic error restarted the bridge at 22:50:25; the
service recovered and all three feeds resumed. It was a capture-hook bug, not an SDK failure.
The hook was corrected to retain stable stream/session references and catch callback errors.

The successful rerun stopped Garage at 22:51:26.308, restarted only the Front Door media request
at approximately 22:51:47, and restored Garage at 22:52:08.368. It captured 1379 completed units
across baseline, solo, solo-restart and restored phases with no callback errors. The TV screenshot
and direct SDK frame above came from the solo-restart phase, proving that the downstream gap
was still present when Garage was absent. No user catch-up result from that interval was received.

## Linux applicability and checks

This failure mechanism concerns Media3's RTSP timeline and sample loading. The Linux client
uses GStreamer with its configured 200 ms RTP jitter allowance and unsynchronized video sinks;
it has no Media3 queue or automatic live-offset control to backport. It benefits from the already
deployed bridge timestamp correction. This turn did not measure physical Pi latency or alter its
decoder/fallback behavior.

Four JVM tests cover ordinary jitter, burst drainage/hysteresis, starvation recovery and camera
independence. `testDebugUnitTest` and `assembleDebug` passed. Further device verification is
recorded below; no physical Google TV was available.

## Final device verification

After the recovery test, raw HTTP and production RTSP were decoded concurrently again. Screenshots
were collected automatically at scheduled times, so these samples did not depend on manually
starting a capture and later taking a screenshot after it had ended.

| Sample | Receipt or capture interval (UTC) | Encoded Front Door OSD (EDT) |
| --- | --- | --- |
| Raw HTTP, first | 23:01:21.775 | 19:01:15 |
| RTSP, first | 23:01:21.592 | 19:01:14 |
| TV .85, first | 23:01:20.617–23:01:22.747 | 19:01:13 |
| Raw HTTP, second | 23:01:30.560 | 19:01:24 |
| RTSP, second | 23:01:30.698 | 19:01:23 |
| TV .85, second | 23:01:29.616–23:01:31.678 | 19:01:22 |

The TV was one **displayed clock second** behind the nearest sampled RTSP picture in both cases.
Screenshot commands took about two seconds and OSD granularity is one second; these samples
cannot establish an exact one-second latency, or total sensor-to-screen latency. They show a
smaller same-camera discrepancy than the earlier four-second SDK/TV difference. The fresh
decoder samples themselves include parsing and decode time. Physical phone/TV confirmation
after the fix remains pending at this checkpoint.

Repeated `am start` delivered an intent to the existing activity rather than creating another.
Sending Home left only the Pi's RTSP consumers on both camera paths after teardown completed.
Returning to the wall produced one .85 consumer per camera and resumed playback. The .58 TV
was left in its existing foreground Settings screen; its APK was updated, but foreground
playback on .58 was not retested in this turn.

Installed `base.apk` SHA-256 was read on both TVs and matched the build digest above. Bridge
health showed all three configured live cameras, zero recorded stalls and go2rtc running.
The inspector was closed and its diagnostic handles removed. No temporary camera disable,
request override or network filter remained.

Private captures, SDK keyframes, logs, scripts and per-file SHA-256 manifest are retained locally
in `.evidence/tv-rtsp-backlog-2026-10-08/`, which is excluded from git. The published report retains
the measured values and these identifiers for an authorized local recheck:

| Artifact | SHA-256 |
| --- | --- |
| Recovery TV log | `ac5dd20a75ef917f2bdf4cc88be9bbfada5a251db642a21f1487894a90292edb` |
| Direct SDK OSD crop used in the solo comparison | `f4c8edf08d5bc771e5d82047d6972cc78f656a270122438a83d2540e952bb7d1` |
| Old-app solo screenshot | `41d77422ee9b2f8f73f0edd3c5745f6c797481046ec02f8353922032fab5f48a` |
| Updated-app screenshot, first | `7ff6927f2a6dce467a9c012db0ff3e5a3359c171e871bf6be571169a4647ded8` |
| Updated-app screenshot, second | `2d131a9da0ac40d8cdd21f92d74633a18a9e8ac3c3d3e97d8ccdbb7bb5d3fc1d` |
