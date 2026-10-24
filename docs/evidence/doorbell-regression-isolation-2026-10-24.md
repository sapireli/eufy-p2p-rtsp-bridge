# Doorbell regression isolation, 2026-10-24

The root cause remains unresolved. Matched direct-reader comparisons have not established an SDK regression or justified a version bisect. No causal claim against our changes or unrelated upstream changes follows yet.

## Production boundary

The running-class observer matched source/readable identity: **296 raw D1/01 media datagrams → 30 assembled units → 30 valid frames → 30 matching readable deliveries**. Timestamps agreed within 2 ms; all paused together before a 5.021 s raw gap, with the source LIVE/one consumer. No intervening delivery loss was shown for those 30 frames. Downstream health and SDK outbound causation remain unproved.

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

The existing NIC capture also contains one wire ACK for each of 20,148 target D1/01 media datagrams, including repeats. Final pre-gap packets were ACKed within 0.903/2.491 ms. All captured payload offsets in 3,121 HomeBase packets inside the two gaps contain no visible camera-0 VIDEO/AUDIO headers on another tuple. Thus these captures show neither missing wire ACK coverage nor visible camera-0 destination migration. Headerless continuations remain unattributed.

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

## Channel-zero START correction and remaining cutoff

The SDK START serializer used request slot zero for camera channel zero. Native authenticated requests select camera zero independently from their generated request slot. A narrow correction preserves STOP and station-sentinel behavior while assigning generated slots to START zero.

Direct three-HomeBase-reader B/A/B: corrected B1 delivered 2,692 target frames over 180 s, original A stopped after 64.575 s with a 5.021 s raw gap, and corrected B2 delivered 2,681 frames over 180 s. Sibling video continued in both corrected trials. The original SDK also sustained 180 s when camera zero held the base connection. These are bounded results, not uninterrupted-delivery guarantees.

The complete production canary with the signed corrected package failed approximately 80 s after service start. Its target private session received 7,560 raw media datagrams and delivered 868 matching assembled/valid/readable frames before silence. All three Fire TVs used hardware decoding with advancing output and empty queues until the incoming gap; their timeouts aligned with the raw gap. Original SDK, settings, ownership and service were restored. The correction was not promoted as a playback fix.

A separate concurrent direct-reader run with the corrected SDK failed after 85.072 s. Floodlight video had already stalled and its recovery STARTs preceded the target cutoff. That timing motivated two controlled tests:

| Test | Actual exposure | Result |
|---|---|---|
| Two existing Floodlight recovery nudges while healthy | Two serialized STARTs on the same source/session | Target kept streaming; maximum raw gap 1.026 s; all 1,340 frames delivered. Rejects simple healthy-START coupling. |
| Suppress Floodlight START after a natural LIVE stall | No eligible stall or suppressed command during 180 s | Inconclusive. Target delivered 2,735 frames; all 2,704 Floodlight assembled video frames reached valid output. |

The latter run showed zero drops on the selected sockets and no increase in UDP receive-buffer errors during the measured 86-second middle-to-late interval. It does not retrospectively establish kernel health during earlier failures.

Upstream [PR #506](https://github.com/mega-yfue/eufy-sdk/pull/506) carries only the START-slot correction and one regression test. Its description includes the negative production canary. Local full verification passed on Node 24.5 and 26.8; upstream template check passed, while the main CI workflows require maintainer approval. The PR is open, not merged.

## Independent protocol audits

The existing NIC capture contains 20,488 packets inbound to the target local port, all from the selected peer. Every received DATA packet has an outgoing ACK with the same data type and sequence, including repeats: D1/01 20,148; D1/02 190; D1/00 39. No missing, extra or malformed ACK was found. The longest interval without incoming D1/01 media was 17.077 s. This proves local ACK departure, not station receipt, and does not distinguish station output from network delivery.

A possible concurrent same-camera source-acquisition race was reproduced in isolation, but the actual host serializes opens before awaiting them. No path from the running host to that race was found; no unrelated race correction was deployed.

Connection setup differs from the recovered native reference: the SDK uses eight local CHECK_CAM source ports per open, while the native capture uses one. Across seven SDK opens, 49 losing probe ports sent 833 CHECK_CAM packets and received no CAM_ID, CAM_ADDR, DATA, PING or ACK. They close locally without END. This establishes extra handshake traffic, **not** leaked completed station sessions. The one-socket intervention did not reproduce the baseline cutoff under matched demand; no probe omission was promoted.

Completed private artifacts were copied outside temporary storage into a permission-restricted evidence archive. The verified snapshot contains 378 files (51,577,187 bytes), with source paths, sizes and SHA-256 checksums; originals remain unchanged. Later trial artifacts require an incremental archive refresh.

## Corrected production demand and held-pipeline challenge

The current effective policy has Doorbell, Front Door and Balcony always on; Floodlight is on demand. Enabled does not mean held. The no-extra-probe production arm incorrectly required a third HomeBase source to become fresh without requesting it, and ended after 104.101 s. Doorbell held BASE0 in that arm, unlike the prior failing PRIVATE0 canary. Thus neither its clean target output nor its missing Floodlight input is a matched pass/fail comparison. Original SDK/settings were restored; no probe omission was promoted.

An unchanged-process observation then confirmed PRIVATE0 healthy under actual demand: 1,803 matching assembled/valid/readable frames over 120.010 s, no HomeBase START/STOP, source replacement or target stall, and no Floodlight source. The observer made no holds. The earlier canary observer also made no holds; earlier Floodlight demand came from the live operating context.

A controlled challenge added only a bounded Floodlight hold, retaining the running Doorbell source and without creating a downstream playback subscription. The first 90 s hold and 20 s release interval were healthy. On its second introduction, HomeBase media on all three flows stopped within approximately 100 ms: Doorbell last raw at 1792826733782; Front Door last raw bucket at 1792826733700; Floodlight last assembled/valid video at 1792826733869. This was approximately 4.052 s after the new Floodlight START and 1.955 s after its warm-up retry. Incoming control packets continued. The boundary was observed in Node, without a simultaneous NIC capture.

Releasing/resetting only Floodlight at Doorbell raw age 2.550 s did not restore the held Doorbell source before automatic recovery replaced it. Both primary cameras recorded a stall. This establishes an observed trigger during a controlled workload addition, **not** a completed causal reversal or a blanket three-stream limit. All trial holds were removed; original SDK/settings and service PID remained unchanged.

Authenticated native Floodlight STARTs contain a camera-selection list omitted by the SDK legacy request. The one-field intervention and original-shape reversal both completed two clean 90-second holds. No benefit was established, and the field was not promoted. Native simple requests for the doorbell cameras omit the list, so the evidence does not justify adding it universally.


## Later controlled reversals and measurement interference

Six original-protocol Floodlight open/close cycles completed without a primary-camera stall. Five cycles repeated START after an already acknowledged initial request; first video arrived in 0.519–2.945 s. Suppressing only acknowledged, pre-first-media retries also completed six cycles, with seven retries actually suppressed. First video took 2.704–3.064 s. Neither arm reproduced a primary stall or established a playback benefit. The suppression remains an unshipped candidate.

A separate six-cycle intervention added only the native outer request identifier to Floodlight START. First video took 2.776–2.989 s, with the same two STARTs per cycle. Primary streams remained healthy. It was removed; no improvement was demonstrated. Additional command notifications appeared, but their meaning is unverified.

An unchanged-production, 600-second observation delivered 9,022 Doorbell and 9,031 Front Door matching assembled/valid/readable frames. Neither primary source changed, and stall counts did not advance. Paired wall/monotonic clocks differed by at most 1.051 ms; event-loop delay peaked at 130.154 ms. A repaired external socket watcher covered only the final 512 seconds, with no drop increase. This clean interval does not establish a fix for intermittent failures.

After that observer had stopped its event-loop monitor, its private helper returned 9,531,967 bytes of capture JSON through one inspector evaluation. External UDP counters then rose by at least 174 target and 141 sibling drops. The drops began after the measured playback interval. Their alignment with bulk serialization strongly implicates measurement interference; the missing teardown event-loop measurement prevents quantifying the original stall. These losses must not be presented as natural failures during the preceding 600 seconds.

The private helpers were corrected to use the repository's existing paged exporter, with 32 rows per batch and serialization outside the application. A live synthetic metadata replay reconstructed all 32,106 rows with identical SHA-256, exported in 18.219 s and limited replies to 69,894 bytes. Maximum event-loop delay during export was 47.120 ms. Across 77 external samples covering export and 30 seconds afterward, socket drops and global receive-buffer errors did not increase; all three active streams advanced with unchanged stall counts. This verifies exporter overhead for that workload, not resolution of the original media silence. No production source or service settings changed.

One separately controlled 1,051 ms main-thread pause did not reproduce prolonged sender silence. The NIC continued receiving primary media during the pause; valid output resumed within 84 ms afterward, with no new socket drops, source replacements or stalls. A generic one-second application pause alone therefore did not reproduce the observed multi-second incoming gaps.

| Later private summary | SHA-256 |
|---|---|
| `field-original-reversal/summary.json` | `309856e23d87f20fe79c25899445f329bce22bb82737d7a81e62d3f3f95932dc` |
| `original-lifecycle-stress/summary.json` | `901d58b520da91f99d69da5ccdc13b2acc32128c800549398c499474280c41f3` |
| `acked-warmup-suppression/summary.json` | `62b4980e9c819a192f459246378e35ead45f6cc0cc8eb92a00efe1f159b0ce14` |
| `outer-request-identity/summary.json` | `58d469d1cd11a31aa5bd950f79a32a7f75d7c60e47fa613a759d426018b3ce1c` |
| `single-1050ms-pause/summary.json` | `1af8bdb1c55a2ad8ae6260be40efd51fae8071d200d83aea28a41aaca3fa54d2` |
| `export-replay/summary.json` | `11afc2e7b23ff989abf6997a35d3b78b8b7572b984514c0dcbb9ff22fb9292c7` |

The original production package remains installed. None of these unsuccessful protocol interventions was deployed as a fix. A bounded, external-only first-fault capture is collecting the next natural primary failure without inspector hooks or added camera demand.


## Native upkeep and stream-option checks

The most recent supplied native phone reference contains 160.540 seconds of channel-2 video. It sends no periodic DATA commands in the middle of the run, excluding the initial and final three seconds. An initial 1139 precedes video; there is no repeated START or ongoing 1139 during playback. Maximum raw media gap is 1.038 s, and maximum visible video-header gap is 1.130 s. This does not establish indefinite streaming or current camera identity, but supplies no basis for adding a periodic keepalive as a fix.

Static call-path review found one stored stream-type option passed through initial START, warm retry and silence recovery. No retry-dependent option flip was found.

A cross-session slot audit found three simultaneous attached STARTs using slot 11 in both clean and failing trials. One clean 180-second run delivered 2,728 target and 2,705 Floodlight frames with zero actual command suppressions. Thus shared slot values alone do not establish global overwrite. The production startup capture has disjoint target/sibling slot sets but ends approximately 41 seconds before failure; it cannot exclude later overlap. The slot namespace remains unverified, and no global allocator was introduced.

Completed evidence was refreshed into a second private incremental archive: 84 files, 363,872,933 bytes, all source hashes/sizes/stability and restrictive permissions verified, with zero errors. The active external first-fault capture was excluded.


A read-only 60-second check of all three existing Fire TV processes found advancing Front Door and Garage Doorbell output, unchanged hardware pins/PIDs, and the same hardware AVC decoder on all six primary players. Rendered-counter deltas were 775–833 over approximately 55 seconds of sampled counters; hardware queues peaked at zero or one frame. No reconnect, reopen, reclaim, error, pacing-drop or decode-deadline increments appeared. Doorbell pacing resets increased by one or two per TV; this needs interpretation separately from reconnects. Balcony's software decoder advanced but accumulated late frames and queues of 8–21, with pending decode peaks of 0.749–1.278 s. These are distinct current client observations, not proof of the cause of incoming primary silence. No app/device state changed.


The TV artifact retained first/last samples and maxima, not intermediate sample times. It cannot align those pacing resets across devices. A reset can indicate a timestamp regression or a mismatch between timestamp and decoder-arrival progression, not necessarily an upstream silence. Receive/decode/render counters are not interchangeable: six primary players had a 23–49-frame decode/render counter deficit in the sampled interval despite no explicit pacing-drop counter increments. Existing metrics cannot distinguish platform discards from informational callback timing. These observations must not be reported as a physical smoothness or sensor-to-screen measurement.

| Additional private audit | SHA-256 |
|---|---|
| `native-fresh-ongoing-control-gap-audit.json` | `e94c865d570643e34a7348819c7f88b41e56d2a2855d228a29b24d18f58da79d` |
| `cross-session-start-slot-exposure-audit.json` | `c1b9745733b60aa947ddeeb37af08a45b1f980eb0822cbfeb0dd501bffdd6973` |
| `/tmp/eufy-tv-readonly-20261024.json` | `4e0ac934680badf16c06f489e9fa8cdf0d65c3dc8d3ee9bd318524d80ceb596d` |
