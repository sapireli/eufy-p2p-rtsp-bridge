# Android decoder recovery isolated to the failed tiles

## Defect and fix

The activity counted decoder errors across all tiles. Two failures could invoke
`recoverDecoder`, release every player, and kill/relaunch the app after two
seconds. If recovery had run recently, it instead kept the whole wall blocked
for 30 seconds. This explicit cross-tile recovery path is a source defect. The
retained pre-change TV logs do not establish which historical user-visible
incident triggered it; expired logs are not proof of absence.

The fix removes that global escalation and reuses the existing one-second local
retry. Delayed retries verify that the tile still owns the failed player and
still wants live video. Other player instances, surfaces, sockets and decoder
leases remain intact. Hardware-first selection, software fallback, native-to-
compatible fallback, rebalancing, pins, image quality and frame rate are unchanged.

Commit `6a2f95d` contains the fix, regressions and APK version 0.6.8/code 17.
No SDK change was made for this repair.

## Verification

All 108 Android unit tests passed. Debug app and instrumentation APK builds
passed. New unit cases exercise three successive local failures while retaining
unaffected player identities, and reject retries for replaced or hidden players.

A connected-device instrumentation test on Fire TV `.85` launches the saved wall,
then invokes two actual registered decoder-failure callbacks in the same UI turn.
This reaches the production retry path; it does not mock MainActivity's players.
The test continuously checks unaffected player identity while the failing players
are replaced and resume decoding. It does not reset the bridge or alter preferences.

The test passed in 6.568 seconds. During local recovery, the retained Front Door
player decoded another 30 pictures and the retained Balcony player another 49.
Both failing players were replaced. This deliberately exercises the former global
threshold without attributing all historical shuffles to that path. The test
APK was then removed and the normal wall relaunched.

## Deployment and requested wall changes

The same APK was installed on Fire TVs `.58`, `.85` and `.100`. Each installed
base APK matched SHA-256
`55e05c0408c149f76fb98bf4336c11f951db6541261592f5291f6b6c56c63cc1`;
package metadata reports version 0.6.8/code 17. Existing wall geometry, tile
identity, other cameras, motion configuration and network settings were retained.

The second/center tile now shows Garage Doorbell on all three TVs. Its bridge
entry has `power_override: always-on`, `mode: always`, and stacked dual view.
The live API confirms powered true/always-on. Deployed adapter code maps this
explicit override to SDK `powered: wired`; the SDK gives the supplied option
precedence over inference and does not arm its battery budget in wired mode.
A private bridge-config backup was saved before editing.

At the user's subsequent request, hardware pins were changed to Front Door and
Garage Doorbell on all three TVs, replacing Balcony's priority. Live logs then
confirmed both cameras using `OMX.amlogic.avc.decoder.awesome` on every TV, with
decoded counts advancing. Balcony uses `OMX.google.h264.decoder` software fallback.
The earlier instrumentation test retained the two cameras pinned at test time
(Front Door and Balcony); pin changes were verified separately after deployment.
GaragePi selection was not changed.

Garage Doorbell also exhibits a separate source interruption: the bridge records
5–7-second no-media periods and camera-scoped reopens. The wired override is
applied, and retained logs show no battery-budget expiry, authentication error or
explicit SDK cause. These source pauses are unresolved and must not be described
as fixed by the Android recovery change.

## Evidence

Private build output, device proof logs, installed hashes and saved-setting
verification are under ignored `.evidence/android-isolated-recovery-2026-10-20/`.
No production debug broadcast or failure-injection endpoint was added. The
instrumentation test requires a saved wall with four active fixed players,
including two pinned and two automatic players; it is an opt-in device test.
