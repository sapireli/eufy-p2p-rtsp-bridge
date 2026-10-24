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


## External-only natural failure boundary

An untouched-production observation ran from 09:55:52.225 to 10:25:52.255 UTC on October 24. It used no inspector, SDK hooks, added holds or playback consumers. It collected 1,799 socket and 1,798 API samples; no initial sample was excluded. Both primary streams advanced approximately 27,000 frames with unchanged stall counts. Primary sockets remained stable, socket/global receive-drop deltas were zero, and wall-minus-monotonic clock span was 1.434 ms. The capture reported 826,667 packets and zero kernel drops. Its bounded ring retained only the final 355.853 seconds.

This was **not** uninterrupted playback. In the retained ring, Doorbell received no VIDEO or AUDIO datagrams for 2.960571 seconds at 10:25:06.273683–09.234254 UTC. Front Door received 449 media packets during that interval. API ages sampled approximately 996 ms, 2,002 ms and then 1.5 ms, so the two-sample age-above-2,500-ms trigger did not fire. The absence of a trigger must not be presented as the absence of pauses. Balcony separately recycled its socket once during the observation.

Across the Doorbell boundary, transport sequence advanced 45666→45667, video frame number 28286→28287, and audio frame number 34931→34932. The resumed video was a 1600×2200 keyframe. The ±10-second transport span contained 2,421 unique sequences with no missing sequence and 35 duplicate packets. All 2,456 received media datagrams had outgoing ACKs; the boundary ACK delays were 0.907 and 1.555 ms. There were no outgoing DATA commands on any captured HomeBase tuple within that window. Doorbell continued exchanging ordinary empty PING/PONG packets. Thus the pause was already present at NIC ingress and is not explained by a missing received sequence, a host socket drop, a contemporaneous media-control command, or a decoder queue. This locates the silence; it does not identify its cause or prove station receipt of every ACK.

The source timestamp advanced only 65 ms across a 2.981-second VIDEO-header gap. That field is unvalidated, and post-gap pacing differs; it cannot establish camera-clock correctness, sensor capture age, or that the camera's physical capture process stopped.

Heartbeat timing/payload review found no anomaly: 71 empty SDK PING/PONG exchanges on the usual five-second cadence, and eight empty station PING/SDK PONG exchanges. This gap began 2.189 seconds after the previous SDK PING and recovered 149.637 ms after the next. Earlier long gaps recovered at different phases. No heartbeat correction is justified by those observations.

A bounded journal retrieval found Balcony's adaptive H.265 resolution changes 1.580 seconds before Doorbell silence and during the pause, but earlier primary stalls did not share a tight timing relationship with such changes. No motion/push/recording/power/hold/settings event was logged in this window; logging absence does not establish event absence. The earlier unobserved stall increments were located at 09:29:11.804 UTC for Doorbell and 09:37:20.202 UTC for Front Door. These occurred before the external observer and must not be attributed to its collection or to the old bulk export without further evidence.

All owned capture processes were removed. Production PID, installed SDK bundle and effective settings remained unchanged. No speculative protocol fix was deployed.

| Natural observation artifact | SHA-256 |
|---|---|
| `external-firstfault/analysis.json` | `ae97c17d87108405366350584da835df233562fa49810d1dfa085448bc3ef858` |
| `external-firstfault/gap-boundary-analysis.json` | `2572ec9f6df68d249a5f0943e9ce9cc4e54b2f9ba614d4cadeb3f055bb98151a` |
| `external-firstfault/journal-window-analysis.json` | `dd72f7a971c0bdc98ea39392d162b4959f1825eefdf5512dcddffcd8dff8120d` |


## Short iteration: connection-role reversal

A fresh independent packet review distinguished 31 VIDEO-header gaps over one second from only six complete D1/01 media silences in the retained ring. Doorbell repeatedly delivered frames faster between pauses, preserving approximately 15 frames/s over the full window. These observations establish burst scheduling before NIC ingress, not a validated sensor capture clock. All 31 preceding VIDEO units completed by declared-length accounting within 23.760 ms; eight were IDRs and 23 were non-keyframes. No missing first-arrival UDP sequence was repaired late on either primary flow, so neither long-unit assembly nor the 700 ms reorder wait explains those header gaps.

A short matched A/B/A tested whether pauses followed the SDK connection role. Both primary sources were reset before each arm with the same mode gating, 15-second settle, native 1600×2200 profiles, three VA-API pipelines and existing consumer demand. The inspector was idle during measured arms; no prototype hooks were used. Roles were verified from the actual source/session association and packet tuples.

| 60-second arm | Front connection | Doorbell connection | Doorbell VIDEO gaps >1 s | Front VIDEO gaps >1 s | Doorbell complete media gaps >1 s |
|---|---|---|---:|---:|---:|
| Fresh A1 | Base | Private | 5 | 0 | 3 |
| B | Private | Base | 6 | 0 | 0 |
| A2 | Base | Private | 6 | 0 | 0 |

The pauses stayed with Doorbell, rather than moving with private-session ownership. The simple connection-role hypothesis was rejected. Complete-media-gap counts remain a separate endpoint; zero in a short arm cannot establish their elimination. Native geometry, encoder policies and desired settings were preserved. Original roles and semantic settings were restored; administrative settings revisions advanced. No SDK change was installed for this trial.

An additional original-SDK full-service-restart control then reproduced four Doorbell VIDEO-header gaps and zero Front gaps in 60 seconds. This controls for the restart needed by the next compiled SDK canary. Its cached parent station model was positively verified as T8030; its bundle still matched pristine source revision `48e4f2f` byte-for-byte. Querying the cached instance took 1,053 ms during labeled setup, before primary warmup and the measured arm. The measured arm had zero socket/global receive-error deltas, zero capture-kernel drops, stable 1600×2200 profiles and three unchanged VA-API process identities.

A narrow off-device candidate tests the remaining channel-zero START-slot discrepancy, scoped to T8030. Its full verification passed 253 files and 4,446 tests. Its pristine base rebuild is exactly the production bundle; the candidate changes only station-model plumbing, the START condition and wire tests. The candidate is an experiment, not an established pause correction. Deployment/acceptance must be decided from matched live results.

| Short-trial artifact | SHA-256 |
|---|---|
| `role-aba/summary.json` | `ce4e15a8e90d79213b12ad46ae739dcab4c9afc263c0e39e0c68beeec793ebf3` |
| `cold-original-control/summary.json` | `9cb6f9fc8c18e47eb4bd7ee6727314933cbb7c9faad86300e066d9ec3ba0020f` |
| Off-device gated candidate bundle | `ec08734af884a2adf0c17addf21a24a03d8a66b2df22a1c434b702d15f4192d4` |
| Off-device gated candidate package | `fa66cb0031a8b8a2fd9024dc9c2e5292bfc8b9d1b0bbb520c6e4facb38b20b1d` |


## Command-only START-slot comparison: negative

Two whole-package canary attempts ended before a measured arm. The first helper lost its initial setup exception when rollback encountered a startup-readiness 503. The second loaded the candidate and emitted a generated channel-zero slot, but failed an immediate encoder-cohort assertion before its 15-second settle. That helper failed to retain the actual FFmpeg count. Front and Balcony had already stalled before the changed channel-zero START. Neither attempt establishes candidate benefit or regression. The original whole package and desired settings were restored after each attempt.

A subsequent command-only comparison removed those restart/setup confounds. The same original SDK, source, stream, P2P session/socket, RSA/control keys and three encoder process identities remained active. An off-device compatibility fixture proved that the compiled candidate method could run on the original instance, retaining identical decrypted JSON and changing only the request slot beyond normal counters/crypto. During B, the candidate method and model metadata were bound only for one synchronous public nudge, then their exact descriptors were immediately restored. No candidate hook remained during measurement.

Fresh original A1 delivered eight Doorbell VIDEO-header gaps over one second and zero on Front. B delivered twelve on Doorbell and zero on Front; complete media gaps were one and two respectively. The outgoing slot changed 0→13. Incoming Doorbell VIDEO and AUDIO changed entirely to type 13, proving this was an active binding, not merely an ignored command. The final original command changed both back to type zero. No concurrent type mix appeared in either arm. Native 1600×2200 geometry, source identity, keys, encoder identities and settings remained unchanged; socket and capture-drop deltas were zero.

The generated slot did not improve steady playback. No A2 measurement or SDK promotion followed. This live reassertion test does not fully test initial-connection behavior, but it provides no basis for deploying the correction as a steady-pause fix. The alternate package files, inspector and guards were removed. Its inactive ESM code remains cached until process exit; no candidate binding or active instance remains.

## ACKed warm-up retries: exposure and counterexample

An original-SDK fresh Doorbell source sent four acknowledged STARTs before any media, at approximately 0, 2, 3 and 4 seconds. First video/keyframe arrived 5.455 seconds after the initial START, and the SDK accepted one frame. That source then went silent and was retired. This establishes exposure to redundant acknowledged pre-media requests, not their causality. Its intended 60-second baseline never qualified; an instantaneous freshness assertion failed before the measured arm and did not retain its actual age values.

A narrowly scoped guard was subsequently observed on one new source. It would suppress additional STARTs only after the initial matching ACK and before first delivered video or recognized audio. It changed no retransmission, timeout, profile or steady recovery policy, and removed both prototype hooks on first delivered media. In that run there was only one START: its ACK took 8.449 ms and first keyframe arrived 933.564 ms later. No retry opportunity or suppression occurred. The source survived with seven steady video-header gaps in the final 60 seconds, versus zero on Front.

This is not a suppression-efficacy test. It is a counterexample to initial pre-media START retries being necessary for steady Doorbell pauses. No A2 or further retry was run. Original package, settings and process remained active; all guard descriptors, listeners, timers and inspector/capture processes were removed.

| Additional experiment | SHA-256 |
|---|---|
| `command-slot-aba/summary.json` | `aca198866f7ef5c7e1b63c0d69d482a7ca6e956be8e19597ecfe3898968d60ec` |
| `command-slot-aba/incoming-type-analysis.json` | `c159faba11cf392d08fdfef5295c848ba3f6cb5af2dee4fe234375d01a23404d` |
| Original fresh-source `exposure-analysis.json` | `0bc3e88add4b4e6844e30c26484c83eaceda5af4359f74a55bffefe3c8908ea1` |
| `door-acked-pre-media-B/summary.json` | `11ba2429a246f3688d61cf18b830a1694af9d0556f9fd11cefe5fdccc4de68cf` |


## Full native media profile: negative

A matched comparison tested the native request envelope, IOS profile, P256 public key and authenticated media decoder together, on the existing session. Offline fixtures first verified authenticated H.264 decoding and return to the original RSA/AES path on the same compiled session. This was an experimental profile comparison, not an assertion that native encryption causes the pauses.

| 60-second arm | Doorbell VIDEO gaps >1 s | Front VIDEO gaps >1 s | Doorbell complete-media gaps >1 s | Doorbell bridge-egress input frames |
|---|---:|---:|---:|---:|
| Original A1 | 14 | 2 | 4 | 869 |
| Native profile B | 11 | 1 | 6 | 880 |

Incoming B video/audio used the requested type 10, and authenticated Annex-B/SPS decoding proved the new profile was active at the original 1600×2200 geometry. The same sources, sessions, socket tuples and three VA-API process identities survived both measured arms. Socket/global receive-error deltas and capture-kernel drops were zero. B retained a maximum video-header gap of 3.093 seconds, compared with 2.693 seconds in A1. Delivered cadence remained approximately 15 frames/s overall; this did not eliminate the pauses. No A2 followed the failed screen.

The original type-zero RSA request and real legacy H.264 decoding were verified before restoring the original method and decoder descriptors. Hot rollback required no source reset or service restart. Installed SDK, process and settings bytes remained unchanged; temporary hooks, inspector, capture and staged files were removed. Inactive imported module code remains cached until process exit. A Front stall increment occurred after the measured windows; this comparison does not establish sustained wall playback.

Earlier original-profile setup runs stopped before B: one source was already recovering, and another A1 had one Front gap above the original zero-gap screen. The acceptance policy was revised before native exposure to require zero Doorbell gaps and no worsening of Front relative to its measured A1 baseline. These aborted runs are not native-profile results.

| Artifact | SHA-256 |
|---|---|
| `native-profile-aba/summary.json` | `a00867e118a9111c096a4c7219d93fdc9f88f83915014e5dfa2253149c212f9f` |
| `native-profile-aba/native-binding-and-cadence.json` | `aa5694a1f8f604dadfa407d13c157306e4c8e425df3c04ad1bd5bbf7fcde36a1` |

The full native media profile is rejected as a correction for these steady pauses. No production SDK change is justified by this test.


## One-time native connection initialization: negative

A fresh Doorbell-only source received one empty native-shaped command 1139 immediately after connection and before its first START. The packet capture verifies exactly one injection, channel 255/sign zero/type zero/magic 0100, 261.138 ms before START. The temporary connection hook restored its original descriptor immediately, before steady measurement. The original SDK package, settings, process and sibling sources remained unchanged.

After a uniform 15-second exclusion, the 60-second window retained eight Doorbell VIDEO-header gaps over one second and one Front gap. Doorbell delivered 972 unique headers and advanced its bridge-egress input counter by 959; Front delivered 908 headers and advanced by 892. These counters cover different boundaries and are not interchangeable. Doorbell's initial measured-edge receipt age was 4,309.587 ms, versus 12.546 ms at the end; the source, socket and peer survived. The freshness acceptance check also failed. There was no A2, retry or promotion.

Four original STARTs preceded first video, 4.127 seconds after the initial START. Neither this startup nor the added initialization eliminated the steady pauses. Receive-overflow/error deltas and capture-kernel drops were zero. The closed-port counter advanced by 48; that is not a receive-buffer-overflow count. All temporary hooks and owned observer processes were removed.

Private artifact `door-init1139-B/summary.json` SHA-256: `883381b3e79e9af9159d4a20b809c7071ddb1065113c982c184362a1896cb162`.

## Cadence histogram correction

An independent review found omitted non-first histogram bins in the original cadence JSON. Its saved producer was unavailable, so the producer's exact defect is not established. A new offline parser re-read the same SHA-verified PCAPs and retained the originals. Corrected interval counts sum to 5,329 for Doorbell and 5,332 for Front. Exact gap records and the independent volume audit still agree on 31 Doorbell VIDEO-header gaps above one second and six complete media gaps, versus zero on Front. Use `fresh-cadence-audit-corrected.json`, SHA-256 `74ce93660eff31c2c8de855317c1412ea5a3af1b8c3ae56a59706f8db8e7fff5`, for histogram claims.


## Full native-frame content check

A 45.006-second off-device fetch used the existing native timed endpoint without an encoder, reset, SDK hook or settings change. Its FLV contains 670 complete stamped H.264 frames, all 670 decodable, and eight receipt gaps over one second. The largest receipt gap was 2,152.706 ms; source PTS advanced 69 ms. The end API read was delayed by about 35 seconds, so its counter deltas must not be divided by the capture duration.

The burned-in clock is absent in the first picture and in four decoded pictures around the largest gap. All 670 SEI payloads in this artifact are the bridge's own receipt stamp; no independent camera SEI clock is present. Consequently this capture cannot distinguish delayed historical pictures from a source clock that advances synthetically across a production pause. Its metadata remains receipt timing, not validated sensor capture time. No capture-time claim or code correction follows.

Private full-frame artifact SHA-256: `ebe60ef4ae64368f5284b397da6d7dd1b3d25282ceb5cf6792b029ea779f0dab`. Images, source identities and raw frame data remain in the private evidence archive.


## Fresh initial-request slots: no improvement established

The earlier generated-slot test changed an existing source; two whole-package attempts ended during setup. A new targeted comparison therefore bound the exact compiled generated-slot method before the first START on one fresh original session. First START used type 11, followed by retries 12, 13 and 14. All four were acknowledged within 8.486 ms, but that first adopted tuple received zero media packets. The source closed after 5.671 seconds. Its replacement's later output cannot be counted as success on the tested source. The helper restored exact descriptors and the controller reset Doorbell to a fresh original connection. An earlier original fresh-source run also failed, so this does not establish that the candidate caused the startup failure.

A further one-field test matched the native capture's initial type 10, preserving original encrypted body bytes, sequence and all other header fields. Its fresh original A1 retained nine VIDEO-header gaps above one second and 805 unique video headers. The SDK source and socket stayed stable, while bridge egress counters reset and the encoder cohort changed from three to two; A1 is valid raw-ingress evidence but not a fully matched egress/pipeline control.

Type-10 B sent one START, acknowledged in 5.695 ms. The peer returned type-10 media: 19 unique native video headers and 329 D1/01 datagrams. Delivery then stopped. The host sent STOP-associated control and END before the peer's END, so there is no evidence of a peer-initiated close. The first source ended after 7.031 seconds. Zero packets on its old socket in the subsequent steady window is a failed output check, not smooth playback. The candidate was removed and Doorbell reset to a fresh original connection. No A2 or promotion followed either failed screen.

Two fully retained sign-one notifications decoded offline as JSON commands 6246 (`payload.num=0`) and 6203 (`payload.dstZoom=1`). They are not START rejection results. The former is interpreted as RTSP livestream status by a separate client implementation; this does not establish a P2P stop. ECB decoding provides structural JSON validation, not authentication.

The native archive also contains authenticated START types 10, 11, 12 and 14 on one conversation, with STOP type zero. Thus native START is not universally type 10; neither the SDK's formula nor the meaning of the header byte is established merely by those examples.

The API's `freshnessStatus.inputFrames` belongs to a bridge egress consumer. Its reset does not by itself establish an SDK source reset. Earlier table labels have been corrected accordingly. Future source-continuity decisions must use actual source/session/socket identities alongside packet-level counts.

| Private artifact | SHA-256 |
|---|---|
| `initial-slot-fresh-B/initial-exposure-analysis.json` | `6791bec970e4baff013308183d201d4e8dde59e525cc9e054c502f108c36e472` |
| `initial-type10-ABA/prefix-analysis.json` | `68dc866c65110c7366856f78ba1b3c57e6bda2708fc0ab5c393c63d0ce7eaf83` |
| `initial-type10-ABA/1351-payload-structure.json` | `b471cefc881eab820d96c996dddee8783b75ccc8b76da941776dc1b35efc179c` |


## Full native profile before the first request: failed playback, successful crypto

A separate real-object offline fixture verified installing the authenticated decoder before the original `LiveStream.start()`, transforming every startup retry, preserving the original session's crypto and restoring exact descriptors/key buffers after close. The live canary then applied that coherent native IOS/P256 profile to one fresh Doorbell source.

Five type-10 STARTs were acknowledged. The first native keyframe arrived 4.258 seconds after connection; authenticated GCM and valid 1600×2200 Annex-B decoding were verified. The source received only 11 unique video headers before media stopped, and local stale recovery later sent STOP/END. The peer's END followed the host's. Another sibling changed during the observation; Balcony also changed resolution and reset its egress counters. This is failed playback and an invalid sustained comparison, not failure to authenticate or a demonstrated profile regression.

The initial verdict incorrectly looked only at the already-closed profile for crypto proof. The saved cleanup state contains the earlier successful proof. Acceptance still fails because the tested source did not survive and deliver the required frames. The correction is to the report, not the playback result.

The original connection was recreated after cleanup. Global/session/decoder descriptors were restored, owned temporary key buffers zeroed, and inactive staged package files removed. The original service PID, installed SDK bundle and desired settings remained unchanged. Inactive imported code remains cached until process exit. No SDK/profile correction was retained.

Private prefix analysis SHA-256: `bf59c10883f5f854e36ff23635872f0f521108bfa0d227ef213c1b172cfc504d`.

## Clock field and control limitations

A separate offline packet review found meaningful high timestamp bytes beyond the SDK's four-byte live VIDEO read. Reading six bytes produces contemporary Unix-millisecond values and agrees with the SDK's recording parser and an independent protocol implementation. This supports preserving the complete field for future investigation. It does not validate sensor capture time: some camera/header clocks were 9.3–9.56 seconds ahead of packet arrival. The original relative queue-debt observations remain conditional on timestamp semantics. No timestamp change is presented as a pause correction.

The official web portal explicitly selects its WebRTC player for T8030, but uses legacy signaling plus a separate portal live-view access PIN. The existing SDK implements NVR signaling, not that contract, and its session does not establish possession of the portal PIN. No speculative cloud bypass, authentication change or replacement RTC client was attempted. Healthy archived native captures remain insufficient as a control for the current failing physical camera.

A cache-only check found both T8214 watermark settings at value one, last updated roughly 35 hours earlier. Reasserting the same desired value through the public SDK returned, but a subsequent 12-second native capture still had no visible clock. The setting's cached value and successful setter return do not establish that an overlay was applied. No alternate overlay value was tried.


## Independent Mac receiver and recovery ordering

The earlier direct version and autoRealtime comparisons ran on the Linux production receiver. A
separate original48 sole-camera reader now completed on macOS x64, Node 26.8.1, using the same
original bundle hash `dd649e91323a3b2b5159d68b99c369cfa1cbffc64cfa302392830e482ff4da47`.
After a 15-second exclusion, its 60-second measurement contains 654 unique VIDEO headers and 11
header-arrival gaps over one second. Eight gap boundaries advance the video sequence by one; the
largest measured gap is 7.414 seconds. Event-loop maximum was 26.051 ms. This reproduces pauses
without the Linux receiver, encoder, or concurrent camera workload. It does not isolate a specific
host-network difference: these are Node receive callbacks, not Mac NIC captures, and the trials differ
in time, platform and Node patch version. Some Mac boundaries also skip video sequence values.

The host's five-second stale-feed policy can act before the SDK's default six-second attached-stream
reassertion. An intervention moved that SDK watch to two seconds on the existing source. The first
60-second exposure still retained 12 VIDEO-header gaps above one second, up to 4.016 seconds. A
repeat began on a live source but that source ceased to be current during B, the stall count advanced,
and recovery continued on replacements. The two-second change therefore did not establish sustained
recovery or eliminate the pauses. The separate nine-second host-grace proposal was not exposed; it
has no live result and is not a deployed correction. Temporary watch hooks were removed.

Private records: `mac-original.json` and `mac-linux-pause-clock-comparison.json` under the receiver
host comparison archive; `soft-recovery-probe-offline-analysis.json` and
`soft-recovery-repeat-result.json` under the runtime archive.

## Confirmed Readable EOF recovery defect and deployed repair

A separate SDK lifecycle defect was reproduced offline and then tested with actual cameras in two
baseline/candidate comparisons. Front opened first and held the shared station connection; Doorbell
opened second on a dedicated media connection. Once both delivered frames, the test normally stopped
Doorbell's actual SDK stream and let its public Readable reach EOF and destroy itself automatically.
These were deliberately induced stops, not spontaneous camera failures.

Both original48 arms changed from `stopped` immediately after teardown to `lingering` after the final
Readable consumer detached, despite having no underlying stream or session claim. Public reacquisition
returned the same source and old session identity without restoring that claim. Both arms produced
zero reopened frames during the 20-second wait and reported a typed startup timeout.

Both repaired arms kept the source `stopped` after EOF. Reacquisition created a new source and session
with a dedicated claim and returned real frames. The second candidate then delivered another 848
Doorbell frames and 906 Front frames during a 60-second observation after its initial three reopened
frames. All 60 samples retained the same new source/session, live state, underlying stream and
session claim. Final counts were 851 reopened Doorbell frames and 1,054 Front frames. Both comparison
pairs destroyed all readers and disconnected. Visual playback was not inspected.

The one-line repair preserves session-retention release and arms linger only if an underlying stream
exists. Explicitly destroying a live Readable still enters linger. Real Node EOF regressions cover
shared and dedicated reacquisition; all three fail without the repair. Full SDK verification passed
253 test files and 4,443 tests, along with builds, typechecks and guards.

The signed fork commit is [`21576c56eca5a6e057b938491c5e744293e1ba47`](https://github.com/sapireli/eufy-sdk/commit/21576c56eca5a6e057b938491c5e744293e1ba47).
Its complete tree was checked against the validated candidate as
`b5694b7e444e5810421d2a3325b9d8a97a685d82`. The installed SDK bundle under
`/opt/eufy-wall-bridge/server` has SHA-256
`d4d749def0558f8efc4c18048218a9974a41cb44a0365142e9744732af832e9e`; deployment started service
PID 104231 and updated the package lock. The original package backup is
`/var/lib/eufy-wall-bridge/sdk-before-eof-20261024`. The subsequent 90-second production observation retained the Doorbell source, stream, session and
Readable, with zero stalls or failures and advancing original-arrival receipt sequences. Front had
separate startup failures during this window and later began delivering; this was not uninterrupted
all-camera playback.

This repair addresses source/session reacquisition after EOF. It does not establish the cause of
initial ingress pauses or demonstrate that those pauses are fixed. The SDK evidence document
`docs/evidence/readable-eof-source-recovery-2026-10-24.md` records generic lifecycle proof and private
artifact fingerprints. No new upstream PR is included while review on PR #506 remains unresolved.


## Production follow-up and rejected policy change

A 45-second original-policy arm and 45-second `recreateClientAfter=2` arm kept the live deadline at
five seconds. The target entered B healthy, but neither arm exposed a watchdog stall. No reattachment,
reuse nudge, or reset occurred, so the trial is inconclusive. The original value one was restored.
Front's independent startup failures also prevent treating this as a controlled all-camera workload
comparison. No new retirement policy is retained.

After Front recovered, all three Fire TVs reported both primary feeds on the pinned Amlogic hardware
AVC decoder. Queue depths were zero or one; the sampled current SDK-receipt-to-decode age intervals
were 34–124 ms, with no primary-feed decode deadline misses. Longest frame gaps remained about
2.6–4.9 seconds. These logs confirm current hardware playback and continued pauses; they do not
prove sensor capture latency or smooth playback. Lower-priority Balcony software decoding on two
TVs independently showed approximately 0.8–1.1 seconds of current decode age.

The bridge suite passed 345/345 tests with the exact candidate distribution installed locally.
Initial test attempts lacked local dependencies and then the Git dependency's generated distribution;
those setup failures were corrected before the passing run. The service remains active at PID 104231,
with no service restarts after deployment at the check.

An independent archive audit found no mature official-app control for the current physical Doorbell.
The only archived channel-zero native run lasts 2.615 seconds and predates the reported replacement;
its START does not identify the physical child camera. The healthy 160-second native run is channel
two. SDK native-profile interventions must not be represented as official-app controls. A matched
native control remains missing; the existing data does not justify attributing pauses to HomeBase
forwarding, camera recording policy, or a particular untested protocol field.
