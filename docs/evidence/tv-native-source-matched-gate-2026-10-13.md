# TV gate matched to native source observation — 2026-10-13

## Decision and scope

The previously observed paired Garage timeout did not recur. A new shared Garage informational callback-gap maximum was observed; source-side metadata from the independently owned native/API collector is needed to interpret it. No player, buffer or camera policy change follows from the TV records alone.

Read-only INFO capture: **02:42:17.073785–02:46:24.999479UTC**,247.931634s, ending at the coordinated source observer's hard deadline. Exec54597 exited0, both readers closed. No app action, source/session change, DEBUG, preference edit, new viewer or service restart was performed. The native/API collector started earlier at02:41:24.859UTC; this TV capture does not cover that first52s. Application PIDs stayed780(.85)/31852(.58), hardware decoder `OMX.amlogic.avc.decoder.awesome`. Installed APK remains SHA256 `1da7cdcb4a75751fc4831db85a6e8708c2a8b12130df0476299286bd08273830`.

## Output and lifecycle

Garage CLE=T8425T1124130A0C; Front Door CLE=T8214510242321E6. There was no RTSP timeout/error, player release, control loss or counter reset in either captured log.

| Camera / TV | Actual aggregate span | Decoded delta | Max input queue | Max pending decode | Callback maximum | Pacing resets |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| Garage .85 |244.048s|3660|0|70ms|2099→2683ms|2→3|
| Front Door .85 |247.049s|3706|0|76ms|2133→2133ms|6→9|
| Garage .58 |240.035s|3601|0|68ms|2116→2683ms|2→3|
| Front Door .58 |245.044s|3676|0|70ms|2133→2133ms|6→9|

Decode500ms miss/definitely-late counter deltas were0 in all four rows. Cumulative observed decode upper-age maxima ended261/265ms(.85 Garage/Door),258/267ms(.58). These measure decoded frames; no-arrival intervals have no decoded-age sample.

Garage2683ms callback maximum was first notified at devicewall **02:43:03.964(.85)**, latest decoded PTS1374734986, and **02:43:06.918(.58)**, PTS1379467986. These are5s aggregate notification timestamps, not exact start/end of the underlying gap. Clock brackets are retained; device wall is not assumed identical to bridge wall. On API28 render callbacks may be delayed/batched/omitted; this metric does not independently prove physical panel freeze duration or sensor freshness. Front Door's2133ms maximum predated this window and is not a newly witnessed gap.

## Provenance

Metadata-only private artifacts `.evidence/frame-freshness-2026-10-12/tv-native-source-matched-20261013/` retain baseline/tagged INFO logs, local wall/monotonic endpoints, initial/final device clock/PID brackets, API clock/health endpoints and normal terminal-reader state. No media payload/pixels were captured. Source-agent native/API analysis is separately owned; TV records do not infer sender behavior or kernel delivery from SDK counters.

| File | SHA256 |
| --- | --- |
| `manifest.json` | `ca89dfda5135da728d6e4fcd0cb8c11b4776c976222aa7863da8b07c41597086` |
| `tv85.txt` | `f8bd128a439ea201ab3671cb97f6bb651c65f3bf7c1edbb755cb5630beb1f64b` |
| `tv58.txt` | `e454ccb379cc5e5b1c063ad7810b7439c6ac68e122f40196e5c9903b5dda72ab` |
