# Current Fire TV 60-second operational gate — 2026-10-13

Read-only INFO collection ran **01:31:54.390–01:32:54.392 UTC**, 60.003s, with exact wall/monotonic brackets retained in the private manifest. No app/source/debug/Pi actions were performed. Both collectors terminated normally, with no ADB reconnection. App PIDs20526/.85 and19463/.58 remained unchanged; all sampled decoders were hardware `OMX.amlogic.avc.decoder.awesome`.

Camera names are verified from the saved `/api/cameras` response: **T8425T1124130A0C = Garage CLE; T8214510242321E6 = Front Door CLE**. Some earlier human-readable gate tables inverted these names; raw per-SN records remain valid. Those documentation labels are corrected separately.

| TV | Camera and exact SN | Decoded count increase | Actual stats span | Maximum queue | Maximum pending decode | Maximum sampled latest decode upper age | Informational callback deadline misses |
| --- | --- | --- | --- | --- | --- | --- | --- |
| .85 | Garage CLE (`T8425T1124130A0C`) | +930 | 62.011s | 0 | 70ms | 100ms | +2 |
| .85 | Front Door CLE (`T8214510242321E6`) | +893 | 60.014s | 0 | 69ms | 118ms | +12 |
| .58 | Garage CLE (`T8425T1124130A0C`) | +900 | 60.015s | 0 | 64ms | 79ms | +2 |
| .58 | Front Door CLE (`T8214510242321E6`) | +878 | 60.011s | 0 | 0ms | 98ms | +11 |

All four players had zero new decode upper-bound500ms misses, definitely-late counts, missing stamps, unknown clocks, pacing drops, or pacing resets. No warning, error, sparse age outlier, retry, first-frame/recreation event, or counter regression occurred **inside this60s gate**. Current operational playback passed; this does not establish physical-panel smoothness.

Cumulative maxima were unchanged in this window: Garage callback gap2666ms and pacing resets1 on both TVs; Front Door callback gaps2433/.85 and2416/.58ms and pacing resets8 on both. These prior gaps/resets are not newly witnessed stalls. New callback-only misses and callback backlog cannot be treated as physical drops because API28 render notifications may be batched/delayed/omitted.

## Lifecycle boundary since the earlier three-minute gate

Every current decoded-count baseline11310–11342 is lower than its per-SN final count in the earlier00:35:57–00:38:57 gate. Front Door's definitely-late diagnostic count was27 then and is0 now. Therefore diagnostic/player state was reset or replaced between gates, despite unchanged app PIDs. The current60s is not continuation of the same counters, and cumulative maxima/counter differences across that boundary cannot be interpreted as new-event deltas.

Bounded retained tagged history had only48–60 recent entries and no lifecycle event explaining that older reset. The exact reset time/cause was not recovered; same PID alone does not mean the same player instance. No camera clock or burned-in time comparison was used. No new observer or behavioral change is justified solely by the counter reset.

Private metadata is retained in `.evidence/frame-freshness-2026-10-12/tv-current-info60-20261013/`, including manifest, current/baseline/historical filtered logs, summary, clock/PID brackets, and existing bridge health/epoch reads. No media pixels or payload were captured. No code was changed.
