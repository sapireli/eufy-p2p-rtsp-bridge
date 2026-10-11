# Bridge hostname deployment — 2026-10-11

`homebridge.lan` resolved to the current bridge on all three client devices.
The name is supplied by the LAN resolver; `_eufy-wall._tcp` discovery remains
available separately. No site-specific hostname is hardcoded into either client.

## GaragePi

Changed `/etc/eufy-wall.yaml` from the numeric bridge URL to
`rtsp_base: rtsp://homebridge.lan:8554` and restarted the running client.
Backup: `/etc/eufy-wall.yaml.before-hostname-20261011`.

Service PID **20388** was active. Both live GStreamer pipeline arguments contained
`rtsp://homebridge.lan:8554/<camera-key>`, with V4L2 decoding, zero RTSP jitter
latency and the existing bounded pacer. Both physical KMS video planes changed
framebuffers across repeated snapshots. The binary and plugin were unchanged.

## Fire TVs

The existing app already accepts hostnames; changed the setup wording to make that
support explicit and rebuilt successfully with `assembleDebug`. Installed that
APK on both TVs, retaining camera selections. Changed the saved bridge URL on
each to **`http://homebridge.lan:3000`** and restarted the apps.

Preference backups are in each app's private
`files/wall-before-hostname-20261011.xml`. No camera selection, owner ID, layout
or decoder preference was changed.

The new APK SHA-256, verified against both installed APKs, is
`63eaa8c73af3c92c51bf3ac0e7983a9da1e6ec448cdfdc7b7b249b146be86ead`.
Version remains **0.5.2 / code 7**; this debug update changes setup wording only.
TV `.85` ran as PID **18470**, `.58` as **14013**. Both camera surfaces on each
TV showed advancing SurfaceFlinger actual-present timestamps after reconnecting.

Requests to the bridge with `Host: homebridge.lan:3000` advertise
`rtsp://homebridge.lan:8554/<camera-key>` for every camera. API, WebSocket and
RTSP clients therefore retain the configured name instead of a persisted IP.
The bridge service and SDK were unchanged by this hostname configuration update.

Private verification summary:
`.evidence/frame-pacing-2026-10-11/hostname-deployment.json`, SHA-256
`e76ab9baeb7e0a777539a4836864b37fd742304ad566ad947535751b11628c74`.

These checks verify name resolution, saved settings, installed artifact identity
and ongoing presentation. A future address change was not simulated.
