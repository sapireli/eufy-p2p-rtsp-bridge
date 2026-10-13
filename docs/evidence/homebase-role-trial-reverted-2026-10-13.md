# HomeBase session-role intervention: failed and reverted

## Hypothesis and intervention

Garage sharing the HomeBase base/control session might explain its incoming-media pauses. The retained baseline had Garage on the base session and Front Door on a dedicated session. A temporary server entry was built and syntax-checked on the Mac, then deployed with a ten-minute independent rollback timer. It ordered Front Door's warm first, blocked external warm requests until boot completed, and opened Garage second using the existing SDK routing. No SDK patch was made.

Candidate entry SHA256: `35eae136a49c802ceefc85f3a258e64cd1c55c5ccc70c2fc4d29d9382c9a9a22`. Actual bounded SDK inventory verified Front Door base/channel 2 and Garage dedicated/channel 1, with stable source/session identities. Stream selector 2, camera quality, geometry, LAN policy and hardware-first/software fallback were preserved. Codec discovery preceded starting go2rtc. The diagnostic writer stopped normally after 90 seconds.

## Short comparison and decision

The preceding matched baseline recorded a 2601.519 ms incoming Garage media pause and a shared 2683 ms TV callback maximum. Its TV decoded rate was already approximately 15 fps. The intervention's existing API counter capture ran 03:08:20.476–03:09:49.489 UTC, 90 samples, zero errors, approximately 0.62% CPU of one core. TV INFO ran 03:07:45.093–03:09:15.095 UTC; planned startup recovery was excluded from rate comparisons.

- Garage decoded at 14.997 fps on both TVs. Front Door was 14.953/14.755 fps. Hardware AVC decoding continued with no observed control failure, RTSP error or player release.
- New shared Garage callback maxima were 1016/999 ms; Front Door reached 1616/1599 ms. These callbacks are informational and do not independently verify physical scanout.
- At API sample 28, Front Door's SDK receipt age reached 1197.696 ms with receipt, input and output counters unchanged across one sampled interval. Garage advanced four input pictures but its latest receipt was already 610.837 ms old. Both recovered at sample 29. One-second sampling bounds these pauses without identifying their exact lengths.
- The overlapping SDK inventory had no session replacement, `videoGap` or gated complete unit. Its 90-second window ended before the API window; it is not a full-window gate counter claim.
- GaragePi's existing journal produced 18 reports per camera. Garage advanced 1229 pictures over 86.505 s (14.207 fps), Front Door 1179 over 86.833 s (13.578 fps), with three clock-request timeouts. Final decoded receipt upper ages were 397/217 ms. Neither better sustained Pi motion nor elimination of its intermittent backlog was established.

**Fail: no qualifying playback improvement.** The rate was already about 15 fps on Android, source pauses persisted under the swapped roles, and the Pi did not meet the 14 fps comparison target on both cameras. No sustained candidate extension was run. The trial does not justify a session-policy SDK change or a permanent startup-order change.

## Verified restoration

Rollback exited zero at Unix time `1791861037.0796468`. The original entry SHA256 `a990bcbee21f9b99985f0d095c47c602b9fc55eec0cf97284d12f8eeebac4505` was restored. SDK SHA256 stayed `d5db321bbd4ce03397dc0d2b8bd92de3603b5edc3f71bfe0be63b5a464835c92`; configuration SHA256 stayed `7ed30f307834ebfc5ead93a7ceb15fbf3d160449338769e7894928cfa45ba6c0`. The trial drop-in was removed and the rollback timer was inactive. Health returned `ok`, go2rtc running and all three intended live sources streaming. This verifies restoration, not resolution of the original playback problems.

Private artifacts: `.evidence/frame-freshness-2026-10-12/role-trial90/`, `tv-session-placement-b90-20261013/`, `linux-role-trial-90.txt` and its collection status, plus `role-trial-deployment.txt`. The 65/90-second inventory is a bounded existing observer, not a new media consumer. No code was compiled on GaragePi. The temporary candidate remains outside the committed source tree and is not an opt-in production feature.
