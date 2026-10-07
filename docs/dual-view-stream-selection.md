# Motion-time dual-view switching

## Cause and fix

The SDK's HomeBase-attached live-start command (outer `1350`, inner `1003`) previously requested
`payload.streamtype: 1`. A decrypted first-party app capture requests `2`. Controlled testing on a
T8214 showed that changing this field from `2` to `1` brings back motion-time PiP, while `2` keeps
Split. [SDK commit `7dba7e8`](https://github.com/sapireli/eufy-sdk/commit/7dba7e8b5ac3b1a00dd36a11f690289191abe00a)
changes this start-only field to `2`; the stop command and view-mode commands keep their existing
payloads.

These measurements establish the selector's effect on this device. They do not establish a
universal protocol name for values `1` and `2`, or the camera firmware's internal encoder routing.

## Live evidence

Tests ran October 7, 2026 UTC, with the camera configured for Split. Raw media was inspected before
go2rtc, transcoding, or client rendering. Device identifiers, addresses, accounts, keys, and capture
files are intentionally omitted.

| UTC | Request or test | Result |
| --- | --- | --- |
| 00:49:13 | Motion with the SDK request and app-matching request active together | Both became real PiP at 00:49:18 and returned to Split at 00:49:47. |
| 00:52:26 | Motion with both feeds and the phone live view open | Both feeds changed again; the operator reported that the phone also switched. |
| 00:56:32–00:58:48 | Bridge stopped; motion with phone live view open | Operator reported that the phone stayed in Split. |
| 01:00:16 | Only the app-matching Front Door request active in the bridge, with selector `2` | Source stayed at 1600×2200 through the motion window; operator confirmed normal views. |
| 01:01:58 | Same request, changing only selector `2` to `1` | Source became PiP, 1920×1080, at 01:02:02 and returned to Split at 01:02:30. |
| 01:02:45 | Original SDK wrapper, overriding only selector to `2` | Normal 1600×2200 playback resumed. Additional motion under this wrapper was not measured in that interval. |

During the parallel tests, complete decoded keyframes from both sessions had identical SHA-256
hashes and visibly contained the same PiP image. Frame channel, transport data type, and each
session's media identifier remained unchanged during the switch. No bridge start or view command
occurred around those motion transitions.

The parallel test alone could not isolate the start request: both sessions received the same
camera encoder output. The bridge-off and single-request tests were needed to remove that
interference. The SDK's other request differences, including its media-identifier allocation,
are outside this fix.

## Verification boundaries

The regression test decrypts synthetic emitted SDK packets and asserts selector `2` in the
attached start, alongside the existing empty stop payload. It failed on the old selector and
passed after the fix. Full SDK verification passed 3,881 tests; the bridge passed all 101 tests.

DietPi deployment verified the locked SDK revision and the compiled selector `2`. All three active
camera feeds recovered with zero reported stalls. RTSP probes reported the T8214 output as H.264
524×720 and the T8425 output as H.264 640×720. The connected Fire TV displayed both cameras in Split.
No additional post-deployment motion event was measured before this checkpoint; the controlled
single-field motion comparison above is the prevention evidence.

The geometry suppression/reassert workaround was removed. This fix does not hide frames, crop
PiP, or send a correction after motion. Existing explicit per-camera PiP controls remain available.
