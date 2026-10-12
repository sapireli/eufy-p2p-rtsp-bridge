# Android watchdog deployment — 2026-10-12

## Exact artifact and preserved state

Source fix: `1beb63f` (`fix(tv): detect stalled decoder output without render callbacks`).
APK SHA-256, independently pulled from both installed packages after update:
`fe0b8fd35a94d6b10fd45af7d11d5eb809cc905e5dc54d112401ea4a789b7e96`.
Build verification: 47 app and 14 library tests, zero failures/errors.

Both existing installations were updated with `adb install -r`, sequentially
on `.85`, then `.58`. Neither package was uninstalled. Before updating, each
installed prior APK was pulled and its rollback hash verified as
`77ca7494ca1506176838854dc5f32301e8222ce542a7fcc83ec29bea8a9fb30a`.
`shared_prefs/wall.xml` was byte identical to the original saved preferences
both before and after each update. Camera selection and layout were preserved.
No bridge, camera quality, transport, or buffering policy changed in this step.

## Short sequential deployment gates

Each update intentionally restarted its app, then explicitly launched the
existing MainActivity. These are deployment boundaries, not natural disconnects.
The other TV remained running throughout the sibling update.

| TV | UTC observation window | PID | Front Door decoded outputs | Garage decoded outputs |
| --- | --- | --- | --- | --- |
| `.85` | 22:15:05.933–22:15:35.939 | 20526 | 0 → 362 | 0 → 389 |
| `.58` | 22:15:55.256–22:16:25.283 | 19463 | 6 → 388 | 0 → 360 |

All four players reported `OMX.amlogic.avc.decoder.awesome`, rising decoded
output, queue depth zero at INFO samples, and zero definite ingress-age misses
over 500 ms. No RTSP errors, read timeouts, or watchdog reconnects appeared in
these windows. Both activities were still resumed at the final check. All
owned collectors completed and closed.

Raw metadata and rollback artifacts remain private in
`.evidence/frame-freshness-2026-10-12/watchdog-deployment/`; retained files include
per-device deployment manifests, INFO logs, original preference snapshots,
installed artifact hashes, and the summarized counters.

## Limits

These short checks verify deployment and decoder progress; they do not prove
physical smoothness or sustained uninterrupted playback. Informational render
callbacks remain distinct from actual panel presentation. No live omission of
callbacks was intentionally induced, and this correction is not attributed to
the earlier paired Garage `Read timed out` event. The broader playback goal
remains active.
