# Keep tile surfaces alive across playback retries

## Observed failure and code boundary

At 23:57:19 UTC on October 20, Fire TV .85 crashed in RenderThread with
SIGSEGV/null dereference. The native stack runs through RefBase::decStrong,
SurfaceControl.destroy, SurfaceView$SurfaceControlWithBackground.destroy and
surfacePositionLost_uiRtSync. This happened during the cluster of retries
caused by a bridge restart. It is a separate client failure from the bridge's
Intel video-engine hangs.

Each LowLatencyRtspPlayer previously created and owned its SurfaceView.
Releasing a transport/decoder player removed that view from the tile; retrying
created another view. A network interruption therefore exercised the native
surface-destruction path repeatedly. The stack establishes that failure
boundary, not the precise vendor-framework race. Amazon's Android 9 method
path differs from the stock AOSP implementation; no unverified double-free
claim is made.

## Change

Each wall tile now owns one attached TileVideoSurface across player lifetimes.
Retry release stops the old decoder and removes its listeners, then the next
player attaches to the same valid surface. Wall teardown removes the view.
Stale callbacks are guarded; a replacement starts explicitly when the inherited
surface is already valid. A black cover masks retained output until the new
player renders its first frame. Standalone player callers retain ownership and
removal behavior. Transport, decoder priority, fallback, quality and pacing
policies are unchanged.

## Verification

- 139 Android/RTSP module unit tests passed; app and test APK builds passed.
- Live .85 test: two decoder-failure callbacks in the same UI turn; failed
  players replaced on identical attached views/surfaces, zero destruction
  callbacks, healthy players retained and all decoding resumed. Passed in
  10.321 seconds.
- Live .85 test: four simultaneous transport-failure callbacks; all four
  replacement players retained identical attached views/surfaces, zero
  destruction callbacks and all resumed decoding. Passed in 9.371 seconds.
- TV app 0.6.9/code 18 installed on .58, .85 and .100 with preferences retained.
- APK SHA-256: `655ed6350b69556e9df6703cbab969ce845cc871e0dd1947ab82799103bcdcda`.

The tests verify surface lifetime and recovery through the previously harmful
application path. They do not establish that every future vendor-framework
crash is impossible or repair the separate shared Intel GPU failure.

Private evidence: `.evidence/three-camera-gpu-fault-2026-10-20/fire85-crash.log`
and `four-camera-surface-instrumentation.log` in the same directory.
