# Doorbell regression isolation, 2026-10-24

The root cause remains unresolved. Matched direct-reader comparisons have not established an SDK regression or justified a version bisect. No causal claim against our changes or unrelated upstream changes follows yet.

## Production boundary

The running-class observer matched source/readable identity: **296 raw VIDEO datagrams → 30 assembled units → 30 valid frames → 30 matching readable deliveries**. Timestamps agreed within 2 ms; all paused together before a 5.021 s raw gap, with the source LIVE/one consumer. No intervening delivery loss was shown for those 30 frames. Downstream health and SDK outbound causation remain unproved.

## Direct SDK comparison

Production stopped during independent readers. Both used public `login → getDevice → camera.openReadable({streamType:2,powered:"wired"})`, private copies of the same stopped cache seed, LAN-only peers, unchanged quality/view, `autoRealtime:false`, `pollMs:0` and no prewarm events. Each completed 180 s after first delivery, 1600×2200 H.264, zero errors/feed ends.

| SDK | Raw datagrams | Assembled / valid / readable frames | Max raw / readable gap |
|---|---:|---:|---:|
|Pre-beta `df535e27`|22,365|2,678 / 2,678 / 2,678|3.103 s / 3.195 s|
|Current `48e4f2f`|22,065|2,671 / 2,671 / 2,671|1.556 s / 1.969 s|

The initial current attempt resolved login/device/readable but produced no frame within 60 s and retained only an error counter; its paired old run reached output in 3.141 s before interruption. Neither establishes regression. Bridge `855743676e6ab87b6ae1102cf3e8c7573379bb64` → `2d27c97218695df20022834356f1e11691331b5e` is the same-code first 0.5 pin pair; its SDK `d35abffa` was built but not tested here.

## Workload follow-up

Changing only `autoRealtime:true` retained polling 0 and one requested live source. Sessions were HomeBase base plus Balcony control, not extra Balcony live video. Returning to FALSE completed the reversal:

| Current48 arm | Raw datagrams | Readable frames | Max raw / readable gap |
|---|---:|---:|---:|
|FALSE baseline|22,065|2,671|1.556 s / 1.969 s|
|TRUE|21,091|2,580|5.453 s / 5.473 s|
|FALSE reversal|22,226|2,728|1.654 s / 2.433 s|

All completed 180 s with matching assembled/valid/readable counts, 1600×2200 H.264 and zero errors/feed ends. Reversal measured one live source; baseline did not collect its source array, so missing means unknown.

The matched TRUE replication then completed 180 s on both versions:

| SDK, TRUE | Raw datagrams / readable frames | Max raw / readable gap | Max event-loop delay |
|---|---:|---:|---:|
|Old `df535e27`|23,791 / 2,700|1.969 s / 2.997 s|182.845 ms|
|Current `48e4f2f`|22,076 / 2,703|2.417 s / 3.173 s|198.705 ms|

Both had one live source plus Balcony control, matching assembled/valid/readable counts, 1600×2200 H.264 and zero errors/feed ends. The initial 5.453 s TRUE gap **did not reproduce**. Neither an autoRealtime cause nor SDK regression is established. An earlier TRUE comparison with an overlapping guard owner was discarded; only `direct-true-version-ab2` is used here.

Restored production inherits global `transcode:always` for Front/Door/Floodlight; Balcony explicitly uses ALWAYS. Two actual FFmpeg processes used VA-API decode and `h264_vaapi` encoding. This differs from direct readers. Original SDK/settings were restored between tests.

## Concurrent sources and production output contract

The direct four-source attempt sustained all three HomeBase sources for 180 s. Garage Doorbell delivered 2,760 valid/readable frames; its longest raw gap was 2.444 s and event-loop delay peaked at 194.773 ms. Balcony stopped after approximately 20 s, so this did not establish sustained four-source equivalence.

Repeating with production's `objectMode:true, arrivalTime:true` contract sustained three HomeBase sources for 180 s. All 2,716 Doorbell frames matched their Readable envelope by object identity, with zero shape/timestamp errors and at most 1 ms arrival-to-delivery age. Maximum raw/readable gaps were 4.058/4.548 s; event-loop maximum was 193.987 ms. Balcony separately failed startup. These results show no delivery loss under the drain workload, without proving the full production path healthy.

The existing NIC capture also contains one wire ACK for each of 20,148 target VIDEO datagrams, including repeats. Final pre-gap packets were ACKed within 0.903/2.491 ms. All captured payload offsets in 3,121 HomeBase packets inside the two gaps contain no visible camera-0 VIDEO/AUDIO headers on another tuple. Thus these captures show neither missing wire ACK coverage nor visible camera-0 destination migration. Headerless continuations remain unattributed.

## Build provenance and private evidence

Only portable JavaScript was staged; macOS dependencies/native directories were not copied. Linux reused installed dependencies and verified imported classes. Pre-beta bundle SHA256: `5ae12418fb1a7048bdff04c55d44d647009dcd57274897f990bf63453023f193`; current48: `dd649e91323a3b2b5159d68b99c369cfa1cbffc64cfa302392830e482ff4da47`; unused first0.5: `8d97e4ff7daa442a3c052b6208941bfe3a066f295b5d7a306e3abf7fa9e8b9ad`. No account/cache values are included.

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
| `direct-true-version-ab2/prebeta-df535-autort-true-direct.json` | `982587f93897d9b160f7deed03df02e7fd972e76e1445d111382bd130a0ef7f1` |
| `direct-true-version-ab2/current48-autort-true-repeat-direct.json` | `f051cd217ddd2cd7649d8bf31664663b1c6a53355fbf015bbd621afc8b5929b4` |
| `direct-true-version-ab2/production-transcode-profile.json` | `46622e5c013ddfb32d109d28454d59675096cfcfcea74ff4c7064aebb20f698f` |
| `direct-true-version-ab2/summary.json` | `6dc00989180af8a2f2223b0bd15b2cc37526fff08c9ff73aee038d7c84c4a9c4` |
| `direct-four-live/summary.json` | `b1a67992ebd5a2e2c9b532ea373d2f39841c79f1c0fae9efe6447502b22f2bb7` |
| `direct-four-object/summary.json` | `510ab2821a295966a7d45f75ee607a6b04a5baf3dadccf0ec9bb3cf8153eafd0` |
| `/tmp/eufy-kernel-wire-ack-coverage-20261024.json` | `9710c43091a77b8e0d3a61b256044dcc5174470aa42d014dbc67ebb21948bee8` |
| `/tmp/eufy-kernel-cross-tuple-camera0-20261024.json` | `59ca90393f39cfbc3c15a267304962a9c41b2fabc88ef67fac218aa0c9e8baf1` |
