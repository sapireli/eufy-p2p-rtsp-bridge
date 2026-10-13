# Original Linux client over Ethernet, 2026-10-13

## Intervention and exact scope

The user rebooted GaragePi and connected Ethernet. The parent verified address `192.168.23.20`, `eth0` at 100 Mb/s full duplex, no wireless interface, and the original hardware `v4l2h264dec` / KMS child command lines. The parent verified the original Go binary (SHA prefix `5420`), installed plugin (SHA-256 `0b54bb70ac0c78be1e74c2b2f9c931f571f9e3599f437ca2166cdb27a76b8462`) and unchanged configuration (SHA prefix `54faa`). This evidence file uses the parent's binary/configuration verification; this journal collector did not reread those files or poll the decoder processes.

No client timing, queue, decoder, plugin, resolution or source-quality code change was deployed for this measurement. The failed generation, encoded-queue and TCP-clock wrappers were reverted. Kernel/driver experimentation was stopped; no candidate module was loaded. The parent started the existing client. The intervention combines Ethernet, reboot and client startup; it does not isolate Ethernet from restart/network recovery or establish the cause of every prior wireless stall.

The collector followed only existing service INFO logs with `journalctl -f -n 0`, bounded by `timeout`. No per-picture logging, new camera viewer, packet probe or extra process sampler was added. Exit 124 means the collection reached its planned time limit.

## Short original-client measurement

UTC window: **04:32:06.239953–04:33:07.090879**. Twelve reports per camera:

| Camera | Counter interval | Output rate | Receipt upper age over 500 ms | Last KMS-return upper age |
| --- | --- | ---: | ---: | ---: |
| Front Door | 775 pictures / 55.653 s | 13.926 fps | 1 / 775 (0.13%) | 101.131 ms |
| Garage | 812 / 55.706 s | 14.577 fps | 53 / 812 (6.53%) | 132.002 ms |

There were no clock timeouts, unknown-clock increments, stamp misses, reorder increments, pacer drops, counter resets or recorded decoder errors. Front Door's short rate is below 14 fps and Garage's short late incidence is above 5%; this interval is not a full pass of those thresholds. The parent authorized the longer measurement as an external wired baseline, without changing code or treating it as candidate retention.

## Completed five-minute measurement

UTC window: **2026-10-13T04:33:30.138155+00:00–2026-10-13T04:38:30.835253+00:00**. The bounded collection completed **300.697 seconds**, exit 124, empty stderr. There are 59 reports per camera. Per-picture timestamps bound the counter intervals below; those intervals are shorter than collector wall time because reports occur about every five seconds.

| Metric | Front Door | Garage |
| --- | ---: | ---: |
| Observed, matched and successful sink-return counter delta | 4,226 | 4,283 |
| Counter interval | 293.378 s | 292.932 s |
| Output rate | **14.405 fps** | **14.621 fps** |
| Receipt upper age over 500 ms | **46 / 4,226 (1.09%)** | **63 / 4,283 (1.47%)** |
| Median sampled KMS-return upper age | 169.868 ms | 159.897 ms |
| Maximum sampled KMS-return upper age | 541.530 ms | 759.321 ms |
| Samples above 500 ms | 1 / 59 | 2 / 59 |
| Final sampled KMS-return upper age | 171.241 ms | **609.455 ms** |
| Final sampled decoded receipt upper age | 154.575 ms | **583.766 ms** |
| Cumulative longest decoded gap, first → last | 830.202 → 830.202 ms | **612.420 → 1,662.065 ms** |

Both cameras retain epoch `ef8d6ac33b3483d9`. There are **zero** HTTP clock timeouts, unknown-clock increments, missing stamps, reordered-picture increments, pacer drops, counter resets, unplanned restart entries or recorded decoder errors in this completed interval.

The original wired client passes the agreed five-minute throughput (at least 14 fps per camera), late incidence (at most 5% per matched picture), and no-clock-timeout/no-restart gates. It does **not** meet an all-pictures-below-500-ms guarantee. Garage's newly increased 1.662-second decoded gap and 609-ms final sink-return age remain unresolved. The collector alone cannot locate that gap before or after the bridge, or separate source quiet from client processing.

A subsequent read of existing logs narrowed the new Garage gap to the reports at 04:38:21–26 UTC. The bridge journal contained no entries for 04:38:15–31 UTC. Both TVs' INFO rings had already overwritten that interval (earliest retained device-wall entries were 04:38:51.847 and 04:38:51.355). Later TV progress and unchanged historical callback maxima cannot locate this particular Pi gap. No buffering, SDK, or restart intervention was justified by this retrospective check; no new timed capture was started. Metadata artifacts are in `.evidence/frame-freshness-2026-10-12/tv-pi-gap-retrospective-20261013/`.

`observed` is after raw admission; it does not count any earlier discarded decoded pictures. `accepted` and `submitMatched` measure successful return from the KMS sink chain, not physical scanout. Sample-age medians describe the 59 low-rate reports, not every picture. Reported ages are bounded SDK-source receipt/bridge-ingress ages, not validated sensor-capture ages. Physical smoothness was not observed by this collector. This result is a bounded baseline improvement and **does not complete the overall playback goal**.

## Private artifacts

Files are under `.evidence/frame-freshness-2026-10-13/ethernet-original-client/`:

- `info-60.txt`, SHA-256 `7ee396c78f233602b94b167ef107b4c68b3d2576b65409a1b81bbf93e152fab3`; matching status JSON.
- `info-300.txt`, SHA-256 `c089d740ed1f02e872d521a4efaa49ed4b92d74544dd0eef010ed9cb294175fc`; matching status JSON.
- `info-300.summary.json`, derived from the exact report counters and sample fields.

UTC labels are programmatically converted from the local collection status epoch values. An earlier chat timestamp for the short collection was wrong and corrected from these artifacts. Both bounded SSH collectors have exited; no extra observer was retained.

## Simultaneous follow-up: no reproduced gap

One bounded comparison used existing Pi INFO logs (04:43:00.004127–04:45:00.817921 UTC), both TVs' INFO logs (04:42:21.257208–04:44:21.257336), and bridge API counters at 1 Hz (04:42:30.072929–04:44:30.086529). No service, player, quality, queue or timing configuration changed. The common window is approximately 04:43:00–04:44:21; individual counter brackets are shorter.

In the common Pi window, Front Door delivered 1,124 pictures over 75.795 seconds (14.830 fps) and Garage 1,120 over 75.605 seconds (14.814 fps). There were no new over-500-ms receipt increments, clock timeouts, missing stamps, unknown clocks, resets, errors or longest-gap increases. Sampled KMS-return maxima were 255.608 and 323.048 ms. The complete Pi follow-up averaged 14.771/14.791 fps, with no new late-picture increments or recorded errors. Its original processes and source epoch remained unchanged.

The bridge's common endpoint samples (04:43:00.073228–04:44:20.073170, 81 rows) advanced input by 1,200 pictures per camera; output increased by 1,201/1,199. No one-second counter stasis or new stale/expired/unmapped output appeared. Maximum sampled receipt quiet ages were 275.292/183.299 ms. All 121 API requests succeeded; maximum request time was 91.781 ms and observer CPU was 0.609% of one core. TV Garage five-second received/decoded increments were 74–76 on both devices, with hardware decoding, empty input queues and no recorded errors or releases. Historical TV callback maxima were unchanged, which cannot exclude a shorter pause followed by a burst.

**Decision:** this interval did not reproduce the remaining Pi gap. It supports current progress without accumulating backlog, but cannot attribute the earlier event, establish physical smoothness, or justify a new intervention. Collection ended at its fixed limits with no repeat. Private artifacts are in `frame-freshness-2026-10-13/ethernet-original-client/matched-info-120.*` (Pi), `frame-freshness-2026-10-12/tv-pi-source-matched120-20261013/`, and `frame-freshness-2026-10-12/wired-pi-api120-20261013T044230Z/`, beneath `.evidence/`. API metadata SHA-256 is `e812ce05a5f8cd6c19cdb718cb713cb210103b76984c694ef96148c5bd4e0fbd`.
