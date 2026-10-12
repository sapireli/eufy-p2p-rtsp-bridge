# Android decoder-progress watchdog contract

## Change and reason

The Android freeze watchdog now uses actual decoder output (`decodedFrames` and
`lastDecodedFrameAt`) in every playback mode. Previously only enforce mode did
this; observe and legacy modes used `OnFrameRendered` callback progress. After
one initial callback, five seconds without another callback could reconnect a
player even while its decoder continued producing output.

The [official MediaCodec callback contract](https://developer.android.com/reference/android/media/MediaCodec.OnFrameRenderedListener#onFrameRendered(android.media.MediaCodec,%20long,%20long))
says render callbacks are informational, can be delayed and batched, and before
Android 14 may be absent for frames that were rendered. Both tested Fire TVs run
API 28. Callback absence alone is therefore insufficient proof of a freeze.

The startup deadline remains 15 seconds without the first decoded output. The
live deadline remains five seconds without decoded output, followed by the same
one-second reconnect delay. Socket read/connect timeouts remain five seconds.
Frame pacing, decoder choice, freshness observation, buffers, and transport are
unchanged. Render callbacks remain diagnostics; decoder progress does not prove
physical screen presentation or sensor freshness.

## Verification and attribution limits

Deterministic tests cover decoder output every 500 ms for six seconds while
render callbacks remain silent, a genuine decoder/output stall reaching exactly
five seconds, and incoming packets without decoded output failing to extend the
15-second startup deadline. The MainActivity watchdog calls the tested helper in
all modes; it no longer chooses a render-callback progress source.

The change is a correction of an unsupported callback assumption, **not proof
that missing callbacks caused the observed live disconnects**. The paired Garage
event at 21:47:20 UTC reported `Read timed out`, which comes from the RTSP socket
read path and is unaffected by this watchdog correction. The original-go2rtc
baseline at 22:09:39–22:10:09 UTC was quiet on both TVs before this change; it
does not establish a post-change physical playback result.

The candidate was built and unit tested without installing it, restarting an
app, or changing a service. Live deployment verification remains pending.

`testDebugUnitTest assembleDebug` passed: 47 app tests and 14 library tests,
zero failures/errors. Candidate APK SHA-256:
`fe0b8fd35a94d6b10fd45af7d11d5eb809cc905e5dc54d112401ea4a789b7e96`.
