# Current Fire TV three-minute operational gate — 2026-10-13

Camera-label correction: the saved `/api/cameras` response maps `T8425T1124130A0C` to **Garage CLE** and `T8214510242321E6` to **Front Door CLE**. Earlier human-readable labels in this document were inverted and are corrected here. Raw per-SN counters, timestamps, and retained artifacts are unchanged.

## Exact scope

Both existing applications were observed at low-frequency INFO from **00:35:57.332 to 00:38:57.332 UTC**, 180.005 seconds. The local collector monotonic interval was `406304012080453` → `406484016750646` ns. Both owned logcat readers terminated normally. No app launch/restart/install, UI input, preference change, DEBUG logging, Inspector action, new camera viewer/producer, or source/configuration mutation was made.

Root concurrently owned a Raspberry Pi driver trial. The parent reported bridge Node 32681, SDK D5, and go2rtc 7f unchanged; this collector did not independently query process binaries. Existing `/api/clock` and `/healthz` reads at start/end showed the same epoch `7795771fb7475382`, auth OK, all three sources streaming, go2rtc running, and stalls 0.

Application PIDs were 20526/.85 and 19463/.58 before and after. Every sample reported hardware `OMX.amlogic.avc.decoder.awesome`. There were 36 aggregate samples per camera, with no decoded/received/callback counter regression, warning, error, sparse age outlier, retry/recreation event, or ADB reconnection.

## Decoder and freshness counter differences

The first baseline statistics precede the logcat capture according to each camera's five-second logging phase. Counts below therefore use the **actual displayed statistics subwindow**, not an assumed identical 180-second interval or cross-camera frame-rate comparison.

| TV | Camera | Decoded count increase | Actual statistics span | Maximum input queue | Maximum pending decode media | Maximum sampled latest decode upper age | Final decode lower–upper age |
| --- | --- | --- | --- | --- | --- | --- | --- |
| .85 | Garage | 2760 | 184.022 s | 0 | 67 ms | 113 ms | 56–64 ms |
| .85 | Front Door | 2644 | 176.028 s | 0 | 68 ms | 116 ms | 56–63 ms |
| .58 | Garage | 2700 | 180.034 s | 0 | 70 ms | 106 ms | 53–59 ms |
| .58 | Front Door | 2701 | 180.042 s | 0 | 70 ms | 113 ms | 44–52 ms |

All four cameras had **zero increase** in upper-bound 500-ms decode deadline misses, lower-bound definitely-late counts, missing stamps, unknown clocks, pacing drops, and pacing resets. Garage's baseline definitely-late count was 0; Front Door's was 27 on each device and remained 27. The prior cumulative Front Door maxima 1261/.85 and1260/.58 ms are not this window's latency measurements.

Media presentation timestamps advanced by 176.266–183.997 seconds, consistent with those different statistics spans. These are media timeline values, not sensor timestamps or absolute monotonic-clock readings. Input queue samples remained0; the67–70 ms pending-decode maxima represent roughly one frame at this sampling resolution, not proof of a deep accumulated queue.

## Informational callback limits

Informational render-callback deadline miss counts increased by 1 for Garage on each TV, and by 5/.85 and 6/.58 for Front Door. Callback pending media maxima were337–401 ms across the four players. These counters describe delayed callback accounting; Android API28 may batch or omit frame-rendered notifications. They do not prove physical drops, buffering at the panel, or a visible freeze.

The cumulative longest callback gaps did not increase: Garage 916/.85 and 899/.58 ms; Front Door 1583/.85 and 1566/.58 ms. Those prior maxima are not newly witnessed pauses in this gate. A quiet sampled interval and rising decoder counters do not establish perfectly smooth physical playback or sensor-to-display freshness.

## Identity and clock boundaries

Observe-mode aggregate `frameId=0` is not an actual SEI identity, so this gate does not claim exact rendered frame-ID progress. No new sparse outlier supplied frame-specific clocks/identities. True bridge source receipt identity was read separately:

| Camera | Stable source UUID | Receipt sequence | Global bridge frame ID |
| --- | --- | --- | --- |
| Garage | 3845b397-dbfc-47d2-8550-f843fbac7350 | 34092→39361 | 102418→111887 |
| Front Door | f63d9fcf-13e0-41fb-b437-1b5ed58d1bb7 | 34065→39385 | 102420→111889 |

Those identity reads occurred at 00:36:54.547 and00:39:47.068 UTC; the latter is after the TV window. They establish bridge-side identity progress over that explicit interval, not exact client presentation of those same frames.

Device wall-clock and `/proc/uptime`/PID snapshots were bracketed by local wall/monotonic samples before/after observation. Device wall clocks were roughly 0.8–1 second ahead of the Mac. `/proc/uptime` is a boot/uptime sample and cannot substitute for the codec's CLOCK_MONOTONIC. Therefore source clocks, burned-in clocks, and raw device/Mac wall timestamps were not compared as latency evidence.

## Result and retained evidence

**Operational gate passed:** current hardware decoding continued with no reset/retry, growing decoder counts, shallow sampled pending decode, and no new ingress-scoped 500-ms age violations. This does not close the separate source-pause investigation or prove physical-panel smoothness.

Private metadata artifacts remain under `.evidence/frame-freshness-2026-10-12/tv-current-info180/`: manifest, raw filtered INFO logs, baseline logs, summary, bracketed device clocks, and bridge clock/health/identity snapshots. No image/video payload was recorded. No application or server code was changed for this gate.
