# Current parallel Fire TV playback check — 2026-10-13

Read-only INFO capture **04:31:41.869613–04:32:41.872581 UTC**, lasting 60.005061 s. No app/source changes, restart, DEBUG, new viewer or preference action. Both owned readers terminated; exec handle 53426 exited with status 0. Final application PIDs: .85 = 780, .58 = 31852. All recorded decoders were hardware `OMX.amlogic.avc.decoder.awesome`.

| Camera / TV | Actual aggregate span | Decoded frames / fps | Maximum input queue | Max decode pending | Last decoded ingress-age bounds |
| --- | ---: | --- | ---: | ---: | --- |
| Garage CLE .85 | 55.010 s | 825 / 14.997 | 0 | 70 ms | 54–63 ms |
| Front Door CLE .85 | 55.005 s | 823 / 14.962 | 0 | 65 ms | 48–54 ms |
| Garage CLE .58 | 55.009 s | 825 / 14.998 | 0 | 68 ms | 53–60 ms |
| Front Door CLE .58 | 55.006 s | 836 / 15.198 | 0 | 74 ms | 57–65 ms |

Garage = T8425T1124130A0C; Front Door = T8214510242321E6. There were no recorded media/control errors, player releases, counter resets, or pacing-reset increases. Decode 500 ms deadline-miss and definitely-late counter deltas were 0 for all four camera players.

Known cumulative informational callback longest-gap maxima stayed unchanged: Garage 2633 ms (.85) / 2666 ms (.58), Front Door 1999 ms (.85) / 2016 ms (.58). These predate this window and are **not a newly exercised pause**. API 28 callback delivery can be delayed/batched/omitted and does not verify physical smoothness. Age bounds concern bridge/SDK receipt, not sensor capture. This quiet minute confirms current hardware progress without queue accumulation; it cannot resolve or exonerate earlier source versus client pause causes or establish sustained playback.

Metadata-only private artifacts `.evidence/frame-freshness-2026-10-12/tv-parallel-info60-20261013/` include initial/final local wall/monotonic and device clock/PID brackets, endpoint health/epoch snapshots and INFO logs. No media pixels/payload captured. No repeat is implied.

| File | SHA256 |
| --- | --- |
| `manifest.json` | `cd3355844d6533e47921061b6165403fb2b4878ddc3d84dee9b18006d233b6d2` |
| `tv85.txt` | `f1a870a04e6a2c4d4f6c7667d01edf5f6be6a5522bb257a089d95b19ed0bb843` |
| `tv58.txt` | `02a9b1d38b351035f27ac2c121913882ce7d02e856f724a4810ebcadf2f1f164` |
