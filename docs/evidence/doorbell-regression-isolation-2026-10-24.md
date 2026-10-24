# Doorbell regression isolation, 2026-10-24

The root cause remains unresolved. The production observation locates the first pause at SDK raw reception; the matched direct-reader comparisons have not established an SDK version regression. No repair or claim against our SDK changes, or unrelated upstream changes, follows from these results yet.

## Production boundary

The observer used the running classes and matched source/readable identity: **296 raw VIDEO datagrams → 30 assembled units → 30 valid frames → 30 matching readable deliveries**. Last-stage timestamps were within 2 ms. All paused together; the decision recorded 5.021 s without raw VIDEO while the source remained LIVE with one consumer. No loss between those 30 assembled/valid/readable frames was shown. This does not prove every downstream bridge/client stage healthy, or exclude SDK outbound behavior initiating the ingress pause. The raw capture and derived summary have separate hashes below.

## Direct SDK comparison

Production was stopped during the independent readers. Both arms used the same public `login → getDevice → camera.openReadable({streamType:2,powered:"wired"})` path, an independent private copy of the same stopped session-store seed, LAN-only peer policy, unchanged camera quality/view, `autoRealtime:false`, `pollMs:0` and empty prewarm events. Explicit station connection was not required by that public path. Each completed 180 s after first delivery, with 1600×2200 H.264, zero recorded errors and no feed end.

| SDK | Raw datagrams | Assembled / valid / readable frames | Max raw / readable gap |
|---|---:|---:|---:|
|Pre-beta `df535e27`|22,365|2,678 /2,678 /2,678|3.103s /3.195s|
|Current `48e4f2f`|22,065|2,671 /2,671 /2,671|1.556s /1.969s|

These successful exposures differ from production workload and do not exonerate every current path. The earlier current startup attempt had 60 s without first output and one error counter whose message was not retained; login, device lookup and readable creation resolved. Its paired pre-beta run reached a first frame in 3.141 s but was interrupted before matched exposure. That initial attempt cannot establish a version regression or diagnose initialization failure.

The historical first 0.5 pin pair is bridge `855743676e6ab87b6ae1102cf3e8c7573379bb64` → `2d27c97218695df20022834356f1e11691331b5e`, with identical bridge code. Its 0.5 SDK `d35abffa` was built but **not run** in this direct comparison; current 48 is a later0.5 fork.

## Workload follow-up

A current 48 arm changed only `autoRealtime:true`, retaining polling 0, one requested live camera and the same public initialization. It completed 180 s with zero recorded errors/feed ends. Its two observed P2P sessions were HomeBase base plus Balcony control, not an extra Balcony live stream. Returning to FALSE then completed another matched 180 s:

| Current 48 arm | Raw datagrams | Assembled / valid / readable frames | Max raw / readable gap |
|---|---:|---:|---:|
|FALSE baseline|22,065|2,671 / 2,671 / 2,671|1.556 s / 1.969 s|
|TRUE|21,091|2,580 / 2,580 / 2,580|5.453 s / 5.473 s|
|FALSE reversal|22,226|2,728 / 2,728 / 2,728|1.654 s / 2.433 s|

All three retained 1600×2200 H.264 and recorded zero errors/feed ends. The reversal had only the HomeBase base session and one Garage live source. Baseline session roles and one explicit open were measured, but its live-source array was not collected; missing data does not mean zero sources. This sequence brackets one TRUE exposure with two FALSE exposures. It shows an association, not a causal diagnosis or a proven SDK regression. Production SDK/settings restoration is recorded; no experimental pin or client change is retained. A further old-TRUE/current-TRUE replication is pending and has no result in this record.

## Build provenance and private evidence

Only portable JavaScript dist artifacts were staged; macOS dependency/native directories were not copied. Linux reused installed server dependencies and verified imported classes. Pre-beta portable bundle SHA256: `5ae12418fb1a7048bdff04c55d44d647009dcd57274897f990bf63453023f193`; current 48 bundle: `dd649e91323a3b2b5159d68b99c369cfa1cbffc64cfa302392830e482ff4da47`. The unused first 0.5 bundle was `8d97e4ff7daa442a3c052b6208941bfe3a066f295b5d7a306e3abf7fa9e8b9ad`. Private credentials, account values and session-cache content remain excluded.

Files below reside in `/tmp/eufy-doorbell-runtime-20261023/`; hashes were read from actual bytes.

| Artifact | SHA256 |
|---|---|
| `capture-stage-boundary.json` | `a677955dbf35adbb730837fa8aa15227d7a3034b62f579217ed8683b3e6c9e17` |
| `summary-stage-boundary.json` | `2dc3138c4b985d103ca0c2f6208c50864307817121c3872dec58f4a47d705313` |
| `direct-aba2/summary.json` | `162302253e1291c7dfa5c03cffd4e40799e1844e1b5d80d43009dfc263557b83` |
| `direct-aba2/prebeta-df535-direct.json` | `ad3835666615f0f0142b28a23c7b31ff77f9f323b5629ff39bb16ad76f688c86` |
| `direct-aba2/current48-direct.json` | `fc6702d8c8d834f1ed622d89012560cece747393eceb0cd8435ae927b492af67` |
| `direct-aba2/restoration-proof.json` | `2654f2b24f616f3380a70c415e3df3335d719bdb80debcd25daac121a2486a61` |
| `current48-direct.json` | `5401c4fe520c78f6c7751c27a75fde2a07b325c77034907a563315ed962ee951` |
| `direct-autort1/current48-autort-true-direct.json` | `90066da7a06167c826d272aec62f11f4472b207f4e0d6b79ef5676397a96deb7` |
| `direct-autort1/restoration-proof.json` | `c1ab93a48cffb65d28a7e910bf62d485387f38342123dae07d24163f063ec3f3` |
| `direct-autort0-reversal/current48-autort-false-reversal-direct.json` | `17352170b43f89c0dde270bd0356ff2fcaeac5311bfd526f05c9413b063bfa4b` |
| `direct-autort-comparison.json` | `688dd870bfd4761fb3f0bc52df19c060d2f28a471f7adb4c00b7465901247906` |
