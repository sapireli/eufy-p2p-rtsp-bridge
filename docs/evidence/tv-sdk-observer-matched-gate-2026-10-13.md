# Fire TV gate matched to bounded SDK observation — 2026-10-13

## Decision and scope

No natural paired RTSP timeout occurred in this window. The previous shared Garage timeout remains unresolved; this run cannot identify or exonerate its source stage. No additional quiet repeat is warranted from this result alone.

The parent intentionally restarted the bridge at02:19:55–56UTC to activate a bounded SDK metadata observer. This read-only TV INFO window ran **02:20:56.943343–02:24:55.995878UTC**,239.058261s, ending at that observer's deadline. Initial restart/recovery preceded the current capture and is not classified as a natural disconnect. No app action, DEBUG, preference edit, new media consumer, source/session/config change, or additional restart was performed by this observer. Both logcat readers terminated, exec handle28966 exited0, and final device clock/PID brackets were saved.

Installed APK SHA256 remained `1da7cdcb4a75751fc4831db85a6e8708c2a8b12130df0476299286bd08273830`. PIDs remained780(.85) and31852(.58). All aggregate records reported hardware `OMX.amlogic.avc.decoder.awesome`. No RTSP error, player release, or control-loss event appeared in either captured stream.

## Measured progress

Exact camera mapping: Garage CLE=T8425T1124130A0C; Front Door CLE=T8214510242321E6. Each span below uses first/last emitted aggregate rows, not assumed full-window endpoints.

| Camera / TV | Aggregate span | Decoded delta | Max input queue | Max decode pending | Callback longest gap | Pacing resets |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| Garage .85 |233.038s|3496|0|70ms|283→1850ms|0→1|
| Front Door .85 |238.043s|3583|0|75ms|1033→1653ms|0→1|
| Garage .58 |235.042s|3523|0|68ms|283→1850ms|0→1|
| Front Door .58 |235.036s|3547|0|74ms|1033→1635ms|0→1|

All decode500ms deadline-miss and definitely-late counter deltas were0. Cumulative observed decode upper-age maxima ended208/258ms(Garage/Door .85),179/251ms(.58). These cover decoded frames only and cannot characterize a period with no frame arrival.

Garage1850ms callback maxima were first notified at devicewall02:22:39.736(.85) and02:22:41.706(.58). Door1653/1635ms maxima were first notified at02:22:43.357/.356, with the same latest decoded PTS154672998. These5s notification rows do not provide exact gap start/end or prove physical display pauses. API28 codec callbacks are informational and may be delayed, batched or omitted. Device clock brackets are retained; event wall times are not asserted to equal bridge wall times.

## Provenance

Metadata-only private artifacts: `.evidence/frame-freshness-2026-10-12/tv-sdk-observer-matched-20261013/`. The manifest contains actual local wall/monotonic endpoints, initial/final device clock brackets, deadline and terminal-reader state; API clock/health endpoint snapshots and baseline log excerpts are retained alongside the two INFO logs. No camera pixels or media payload were captured.

| File | SHA256 |
| --- | --- |
| `manifest.json` | `e01fbbc3cab8ae3d8361c7aa9c618d1f3f1d45b46bb1a18109f92d2b8c40dc80` |
| `tv85.txt` | `74a523bcc1413dfb40c908a3d588fb65536f95ab026185257f29f0bfda97b616` |
| `tv58.txt` | `cace431bd7b5321cd192240af813064f88a877e94a57d38ab357d97baa071d82` |
