# SDK adopted-probe receive-buffer PR handoff

## Commit and evidence

The signed SDK commit is [63148e745da6793e58757ee1fb74ddd466cf5293](https://github.com/sapireli/eufy-sdk/commit/63148e745da6793e58757ee1fb74ddd466cf5293), on `fix/punch-probe-receive-buffer`. GitHub reports its signature as verified/valid. Its parent is `773b7481d657c49e5bac986c9f8324f840b87a5b`.

The same commit contains the complete [evidence Markdown](https://github.com/sapireli/eufy-sdk/blob/63148e745da6793e58757ee1fb74ddd466cf5293/docs/evidence/punch-probe-receive-buffer.md) and [sanitized measurement JSON](https://github.com/sapireli/eufy-sdk/blob/63148e745da6793e58757ee1fb74ddd466cf5293/docs/evidence/punch-probe-receive-buffer-observation.json). Use those artifacts when preparing the upstream PR. They include source/bundle hashes, actual socket-adoption evidence, timing windows, counter samples, delivery measurements, reproduction commands, excluded diagnostic windows, workload changes, and private artifact fingerprints.

## Verified change

The SDK already requests a larger receive buffer for its primary UDP socket. A connected punch-probe socket could replace it without receiving that request. The fix calls the existing buffer helper when adopting the replacement socket. The regression exercises both primary and probe selection; the unchanged source fails the probe assertion. Full SDK verification passed: 219 files, 3,886 tests.

The live baseline adopted probe had a 212,992-byte effective buffer and recorded 697 additional socket drops over 544.506 seconds. The candidate's proven adopted probe had a 425,984-byte effective buffer; cumulative host receive-buffer errors and all sampled SDK socket-drop counters stayed unchanged during the 548.693-second candidate window.

## Required PR boundaries

The candidate camera delivered 12,723 compressed first-slice NALs, including 269 IDRs, but also had a 26.217-second delivery gap and reconnected. These counts are compressed delivery, not decoded/displayed frames. The retained baseline delivery interval differs from the baseline counter interval, so there is no matched delivery-rate or freeze-cost comparison. An additional downstream consumer went offline during the candidate interval. Do not claim an identical total workload, uninterrupted playback, or resolution of every pause.

The runtime A/B used deployed source `df535e2` plus this buffer-only change; the evidence explains its difference from the commit parent. The live candidate bundle remains deployed. The fork's `eufy-wall` branch and bridge dependency were not advanced by this commit.

Separate investigations are documented in [source-pause evidence](bridge-source-pause-evidence-2026-10-09.md), [Cherryview GPU evidence](evidence/cherryview-gpu-hang-2026-10-09.md), and [Pi network teardown evidence](evidence/pi-wifi-reset-2026-10-09.md). This SDK commit does not establish a fix for those failures.
