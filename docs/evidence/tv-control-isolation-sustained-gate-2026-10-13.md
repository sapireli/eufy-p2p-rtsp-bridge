# Fire TV sustained gate after control-channel isolation — 2026-10-13

## Decision

**The reset-free, consistently smooth playback gate failed.** Keep the independently verified control-channel isolation fix; the remaining Garage media interruption needs its own diagnosis. This capture does not justify another player buffering change or attribute the interruption to a camera, SDK, encoder, RTSP server, or shared network.

## Scope and retained artifacts

A read-only INFO capture ran **01:54:10.999637–01:59:10.998400 UTC**, measured300.005952s. Both owned logcat readers terminated normally. No app action, debug logging, camera/session change, new media consumer, or service restart was performed by this observer. Installed control-isolation APK SHA256 is `1da7cdcb4a75751fc4831db85a6e8708c2a8b12130df0476299286bd08273830`. Application PIDs stayed780 (.85) and31852 (.58). Metadata-only private artifacts are under `.evidence/frame-freshness-2026-10-12/tv-control-loss-deploy/sustained300/`.

Device wall clocks were bracketed by local wall/monotonic reads at both endpoints. Their event timestamps below are **device wall**, approximately0.8–1.3s ahead of the Mac bracket; they are not precise bridge event times. Bridge epoch `7795771fb7475382`, authOK/all3streaming, and cumulative stall counters Garage1/FrontDoor1/Balcony0 were unchanged at endpoints. Endpoint health cannot exclude an interruption inside the interval.

## Actual shared Garage timeout and recovery

Garage CLE is **T8425T1124130A0C**; Front Door CLE is **T8214510242321E6**.

| Event (device wall UTC) | TV .85 | TV .58 |
| --- | --- | --- |
| Garage `Read timed out` |01:57:41.696|01:57:41.840|
| Player release, reason `rtsp-retry` |01:57:42.697|01:57:42.841|
| New hardware first-frame callback |01:57:43.459|01:57:43.602|
| New-player first-frame elapsed |753ms|751ms|

Both error records have **the same receivedUnits7199, decodedFrames7196, latestDecodedPtsUs2352065976**. Both recovered with PTS2352731976 and `OMX.amlogic.avc.decoder.awesome`. At timeout each queue was0 and pendingDecodeMediaMs0. There was no control-loss or metadata-change release in this capture. Front Door continued without replacement.

Matching last received/decode counts and PTS on independent TVs supports a shared delivery interruption before decoder processing; it does not identify its upstream stage. The last decode-age sample99–107ms in the error record is the age of the last decoded frame, **not current age at timeout**. Informational callback maxima2933/2916ms preceded recovery; they cannot be read as physical panel freeze durations.

## Aggregate progress, split at the actual Garage replacement

Each delta uses its first/last emitted5s aggregate records; these spans differ from the full capture. A reset counter is not subtracted across replacement.

| Camera / TV | Statistics span | Decoded delta | Pacing resets | Informational callback longest gap |
| --- | ---: | ---: | --- | --- |
| Garage .85, before retry |205.026s|3014|0→7|650→2933ms|
| Garage .58, before retry |205.042s|3011|0→7|700→2916ms|
| Garage .85, after retry |84.010s|1418|0→0|50→316ms|
| Garage .58, after retry |85.015s|1431|0→0|50→316ms|
| Front Door .85 |292.057s|4350|1→3|1133→1416ms|
| Front Door .58 |295.042s|4402|1→3|1133→1400ms|

All sampled input queue depths were0. Decode pending media maxima were137ms for Front Door .85 and67–71ms elsewhere. Decode deadline-miss and definitely-late counters stayed0 in all player segments; cumulative observed decode upper-age maxima were238–249ms. These counters only cover frames actually decoded, so they do not exonerate a period with no arrivals. Android API28 render callbacks are informational and can be delayed/batched/omitted; none of these measurements verifies physical smoothness.

## Other boundaries

Garage had already replaced its player between the earlier controlled window end01:49:37 and sustained start01:54:11: counters and maxima changed while application PID stayed the same. One authorized retained tagged log dump did not retain that earlier event, so its cause remains unknown. The new in-window event above has an explicit `rtsp-retry` reason; no metadata-equality patch is supported by the missing earlier record.

The independently collected API counter window **01:55:39.343455–01:56:39.349837 UTC** ended before this timeout. It showed Garage SDK input+893/encoded output+894, stable identity, max sampled ingress age402ms and no request errors; it does not cover the01:57:41 interruption. Within that API interval Garage callback maxima stayed2933/2916ms, while pacing resets4→5 were first notified at devicewall01:56:00.704/03.871. Do not use that quiet window to explain the later timeout.

## Artifact hashes

| File | SHA256 |
| --- | --- |
| `manifest.json` | `bff0ad49a19f06bffa6c728c6f518673024f02d2a383de2d2e8c8e691f965677` |
| `tv85.txt` | `291395fedfb54373069f309b4432e0ce8fd029371e9e53786d0e1428c248f4c5` |
| `tv58.txt` | `4cba179fefabb94855999e95f12e5175712ea09040c7de525ebf3c7509c43707` |
| `bridge-start.json` | `bbcf5a13c66acd89b6b8be0ed7d773b9382c64afd0f376448b39ad529809eec4` |
| `bridge-end.json` | `4976c2b45721a01abb2a712812e08653c2af61f2639bec1aba70d97ad6f8233b` |
