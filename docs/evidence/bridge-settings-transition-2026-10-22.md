# Bridge settings transition: October22,2026

## Implementation and deployment

Branch: `fix/balcony-decoder-recovery`.

- `6cf6c70`: atomic revisioned settings store and one-time legacy import.
- `17b2005`: authorized settings API, camera-scoped policy application and migration documentation.
- `a08e9f1`: Linux settings commands, private backups and authenticated legacy view changes.
- `a53e25c`: Android shared settings controls, per-bridge pairing and backup/restore.
- `693f758`: send only changed camera properties; distinguish pre-login/failed application from applied bridge policy.

The live bridge uses these changes. Its final process is PID229367. Nine deployed server files matched the committed source after the final forward deployment; see [source manifest](data/settings-transition-20261022/source-manifest.json). SDK48e4f2f1d0dd5d255d928766cdae585bec6076d0 and its installed package were preserved. No SDK source was changed.

Android0.7.0/code19 was installed on all three Fire TVs. Installed APK SHA256 on each matched `7f444e7789cad4cd8ec0088c3e8d9b38c6075c04d85ebff5b15574e7d6d13cc7`. Wall preferences remained byte-identical. Actual D-pad navigation to the new settings screen, authorized settings reads and return to playback were verified on one TV.

Both Linux devices received the off-device ARMv6 build, SHA256 `ede2d6fdbc2cda4a074eb8aa6aa60b8f787977215b329ffa4cc1c73daf2cc4b1`. Their display configurations remained SHA256 `1bc970ab3bad4b33ca3c48b238e125b04881e7971d6cddec46541c4c9a25327d`. DietPi's active client restarted once for the binary replacement, then remained PID5670 with no service restarts during comparisons. GaragePi was inactive and was not woken. Its updated executable and settings commands were verified, not active playback.

All five clients were paired. Administrator token files and Linux backup files have mode0600. The temporary local pairing token was removed after provisioning. No token or external-source credential is included in this evidence.

## Settings verification

- Server:296 tests pass, including migration precedence, corrupt-store handling, complete backup/restore, request limits, authentication, stale revision conflicts, pre-save camera policy validation, isolated application and pending restart state.
- Android:111 tests pass and debug APK builds.
- Linux:full Go suite, command race tests, ARM crossbuild and real HTTP/store/runtime integration with mocked vendor transport pass.
- Pre-deployment migration of the live configuration produced no effective operating-setting differences. Existing saved dual views were preserved; external sources were embedded. New store mode0600 was verified.
- On the live service at22:42:10UTC, changing default hold60→61 succeeded at revision2. A stale revision1 edit returned409. Importing the original backup restored the complete settings document exactly at revision3. The bridge process remained PID228516 throughout these edits.
- The authorized supervised restart returned202. Revision3 and its exact settings survived process replacement. Unauthorized settings reads returned401.
- Legacy YAML operating-field warnings appeared at startup. Settings reads identified bridge-policy scope and explicitly reported that camera video had not been verified.

Private operational backups and rollback source remain under `/var/lib/eufy-wall-bridge/settings-transition-20261022/`. Local verification logs include `/tmp/eufy-wall-settings-server-tests-final.log`, `/tmp/eufy-settings-linux-deployment-20261022.json` and `/tmp/eufy-settings-android-20261022/`.

## Playback gate and code comparison

Settings success is not a successful playback gate. Before deployment, Garage Doorbell's selected serial was absent from the bridge inventory. It reappeared after restart, but media delivery remained intermittent.

After an initial settings deployment/restart, both doorbells received no frames on the TVs, while Balcony and Wyze rendered normally. DietPi stalled waiting for RTSP DESCRIBE/SDP before decoding began. Bridge logs showed warm-timeout/EOF on HomeBase sources. These observations do not identify the underlying cause.

A bounded code comparison preserved SDK, camera configuration, client binaries, app processes and wall preferences. Only bridge code and the required bridge restart changed between windows:

| Window UTC | Bridge code | Front Door | Garage Doorbell |
|---|---|---|---|
|22:46:55–22:47:56|New settings|TVs0 received/rendered; DietPi0|TVs0 received/rendered; DietPi0|
|22:49:54–22:50:59|Previous code|TVs0; DietPi0|TV hardware rendered691/733/733; DietPi49→142 accepted over11s|
|22:52:06–22:53:09|New settings again|TV hardware rendered809/796/788; DietPi48→606 accepted over56s|TVs0; DietPi0|

In the final forward window Balcony rendered1314/1282/1264 frames and Wyze821/823/822. Front's latest TV queues were0; DietPi's latest KMS age upper bound was72ms. Those figures describe decoder/output callback progress, not sensor capture-to-screen latency or physical scanout.

The comparison does not establish a settings-code regression: Front failed on previous code and played on new code. Garage Doorbell's recovery after rollback is confounded by restarting its upstream session. The final deployed code is the new settings implementation. Garage Doorbell media delivery remains unresolved, and all-camera sustained playback is not verified.

## Remaining migration work

The persistent store is authoritative now, and normal operating changes use the clients. Legacy first-run import remains during the documented transition period. Its eventual removal is a separate release gate, not completed by this deployment. Preserve bootstrap options and backups. See [migration policy](../bridge-settings-migration.md).

## Installer and legacy-file retirement follow-up

The installer now uses a startup-only bootstrap example for new installations and preserves existing files on upgrades. Its credential template directs operating changes to client controls. An offline retirement tool removes imported operating fields from an existing YAML file after validating the persistent store. It preserves bootstrap values and access permissions and writes an exact0600 backup. It never creates a missing store or starts/stops a service.

The complete server suite now passes305 tests, including eight retirement tests and a fresh-bootstrap/reload test. `bash -n deploy/install-server.sh` passes.

The retirement tool was installed on the bridge and exercised against private copies of the real configuration and revision3 store. It removed `cameras`, `lan`, `go2rtc`, `freshness` and `stall`; the resulting bootstrap loaded an identical effective configuration, the store bytes stayed unchanged, the backup mode was0600, original config mode0600 was preserved and the second run made no change. The live YAML still matches its pre-migration backup byte-for-byte and the live store remains revision3. No service restart or live configuration cleanup was performed for this follow-up.

Legacy importer removal remains gated on the documented release period. This tool prepares per-installation cleanup; it does not remove that compatibility code early. The previously recorded playback failure is not claimed resolved by these installer/tool changes.
