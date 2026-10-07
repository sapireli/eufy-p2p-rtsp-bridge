# Motion-time dual-view switching

## Cause and fix

The bridge's managed live pull used the SDK's HomeBase-attached default (outer `1350`, inner `1003`),
`payload.streamtype: 1`. A decrypted first-party app capture requests `2`. Controlled testing on a
T8214 showed that changing this field from `2` to `1` brings back motion-time PiP, while `2` keeps
Split. The bridge explicitly passes `streamType: 2` to `openReadable()` for every camera, alongside
any power claim. The SDK forwards that option into the managed start and subsequent retries. Its
existing defaults remain `1` for HomeBase-attached cameras and `2` for own-session cameras; the stop
command and view-mode commands retain their payloads.

The low-level SDK command API accepts arbitrary payload fields, but a raw control command does not
supply the managed stream's media-key handshake or retry state. Passing the option through the managed
path keeps those existing responsibilities in the SDK. The earlier global-default change in commit
`7dba7e8` is superseded by this explicit client selection.

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

The regression tests decrypt synthetic emitted SDK packets and assert that the public managed
option reaches attached and own-session starts, retries, forced starts, and silence reassertions.
They also preserve the original defaults and empty attached stop payload. Full SDK verification
passed 3,886 tests, including 95 documented snippets. The explicit selector is implemented in
[SDK commit df535e2](https://github.com/sapireli/eufy-sdk/commit/df535e27f013dea610d2a5b2f775105a71407293).

The bridge passed all 102 tests with that locked SDK commit. DietPi installation verified the locked
revision and compiled attached default `1`. Runtime inspection found `streamType: 2` on all three
active managed pulls (two attached, one own-session). A managed forced reassertion produced two
attached starts, both decrypted with selector `2`; the own-session start was not newly emitted in that
inspection window. The synthetic wire tests cover its selected starts and retries.

All three feeds were active with zero reported stalls, authentication OK, push connected, and go2rtc
running. RTSP probes reported the T8214 output as H.264 524×720 and the T8425 output as H.264
640×720. A fresh connected Fire TV screenshot showed both cameras in Split. Diagnostic wrappers
were restored and the temporary inspector was closed. Additional post-deployment motion has not been
measured; the controlled single-field motion comparison above is the prevention evidence.

The geometry suppression/reassert workaround was removed. This fix does not hide frames, crop
PiP, or send a correction after motion. Existing explicit per-camera PiP controls remain available.
