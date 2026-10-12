# Sustained-playback goal: deployment boundary

This supersedes the earlier deployment checkpoint's artifact list, not its
historical tests. The overall smooth, current playback goal is **not complete**.

## SDK and bridge

SDK branch `eufy-wall`, commit
`55dacd367068b0d90460561e20c9410a2ec03724`, was pushed with its companion evidence
file. The bridge lock resolves that exact commit. On `192.168.23.158`, all 156
built SDK package files match the local build, including the source map:

- Runtime bundle: `4ddbf934ccab2ebee30832fdacb7a1874e308278f3db2f41aa98503b6e938cf4`.
- Source map: `f06b26d56070ef4a25ca9946ab55ed0ed3d73ae25358ddcb4df58f16a1e8ae72`.

The candidate bundle was already running during the live comparison. Completing
the package and lock metadata did not change that bundle or restart the service.
The preceding package is retained at
`/opt/eufy-wall-bridge/sdk-linear-package-backup-20261012`; the original pre-change
runtime is separately retained at `sdk-linear-backup-20261012/index.js`.
Service and health checks passed afterward, with all three configured sources.

The proven benefit is lower copying/reassembly cost, **not a source-pause cure**.
See [upstream handoff](../sdk-linear-reassembly-pr-handoff.md).

## Android and Linux

Both Fire TVs (`192.168.23.85` and `.58`) now have diagnostic APK
`bb486e0e3cb44abffc1fe09872b870fe447b0589895d2f08e7ebcbe474195f82`.
Install used replacement without clearing data. Original preferences match
exactly, the native probe remains off, and AVC hardware decoder counters advance.
This diagnostic update distinguishes decoder backlog from callback lag; it adds
no playback buffer. Its new ten-minute capture began at 18:36:53.858 UTC and was
still in progress when this checkpoint was written.

GaragePi's observation build and complete ten-minute result are recorded in
[Linux observation evidence](linux-observe-playback-2026-10-12.md). The run found
substantial Garage ingress-to-KMS-return delay despite exact frame matching and
validated clock-rate bounds. Its service was restored inactive/disabled and TV
OFF afterward. Do not call this a latency repair or physical scanout proof.

Production remains in observation mode. Camera capture time is uncalibrated;
measured bridge ingress age does not prove sensor-to-screen age. Source pauses
and the Linux accumulation boundary still require root-cause investigation.
