# Front Door periodic playback pause, 2026-10-20

## Status

The pause remains unresolved. These trials do not justify an SDK change or an
upstream fix claim. A temporary SDK serializer experiment was reverted; no SDK
change is being shipped from these trials. The supported
finding is that at least one pause was already visible in incoming UDP traffic;
the evidence does not establish why the sender paused or identify HomeBase
forwarding as the cause.

## Observation boundaries

- Both Fire TVs showed coincident Front Door receive gaps. This points away from
  a failure confined to one TV's decoder; it is not camera-capture timing proof.
- The bridge's `sourceReceipt` is observed after an SDK consumer receives media.
  A pause there cannot distinguish camera, radio, transport, or SDK delivery delay.
- The bounded SDK observer reported zero gap-gated units during the observed
  freezes. That does not prove the whole SDK or network path is lossless.
- The initial incoming-only UDP header capture contains a 2.703879-second gap
  on one sender/socket flow, with sequence 19719 followed by 19720. Thus the
  recorded pause is not a missing sequence number between those two packets.
  Capture loss statistics were not retained for this capture; consecutive
  recorded sequences are not a general capture-loss guarantee.
- The separate bidirectional ACK capture reported 29,903 dropped packets
  (`stats: [143114, 29903]`). Exact matched packets can still establish their
  own observed timing, but the incomplete capture cannot bound all ACK delays,
  establish missing ACKs, or demonstrate a sender-window defect.

The bridge source has no normal recurring ten-second camera settings write.
Quality and dual-view pinning run at boot, authentication recovery, station
connect (with a 30-second per-camera minimum), or explicit user requests. A
reconnect-triggered write would need corresponding pin/recovery logs to connect
it to these pauses.

## Garage concurrency comparison

The reversible A-B-A trial compared Garage enabled, disabled, then restored.
Each stage was sampled for approximately 45 seconds. The initial baseline used
the existing service; the service restarted before the Garage-disabled stage
and again before the restored stage. Each stage had a different Front Door
source ID: `651ebae3…`, `01a464e0…`, and `4456c9a1…`, respectively. This is not
a same-session comparison. Restoring both streams provides a second both-stream
condition, but does not isolate concurrency from session/restart effects.

| Stage | Sample span | Input / output / fresh-output frame deltas | Largest sampled receipt interval | Largest sampled receipt age |
| --- | ---: | ---: | ---: | ---: |
| Both before | 44.794 s | 656 / 656 / 656 | 0.831917 s | 627.088 ms |
| Front Door without Garage | 44.967 s | 599 / 598 / 579 | 2.058741 s | 1796.731 ms |
| Both restored | 44.957 s | 682 / 682 / 662 | 1.835123 s | 1597.801 ms |

Disabling Garage did not remove the pauses in this short comparison. It does
not support Garage concurrency as the demonstrated cause. These intervals are
differences between successive **sampled** receipt timestamps, not a complete
per-frame gap distribution. Counters come from available encoded-HTTP telemetry;
the source receipt remains observable even when that consumer counter is absent.
The bridge reports `captureClockValidated: false` throughout, so these ages do
not measure camera capture-to-display latency.

## Rejected native pass-through trial

Fire TV `.85` rejected the native 1600×2200 stream. Its decoder reported supported
widths 64–1920 and heights 64–1088. The original compatible/transcoded path was
restored at approximately 17:33 UTC before the trial finished.
`/tmp/front-native-trial.json` therefore mixes different paths and is invalid
as a performance comparison. Native pass-through was discarded; it is not a fix.

## Link telemetry follow-up

A test-only diagnostic module was installed for a bounded 100-second observation,
writing `/var/lib/eufy-wall-bridge/front-link-1792517709.jsonl`. It records
decrypted command 1032 metadata as a signed raw value, channel, opaque session
ID, and wall/monotonic timestamps. Units, sentinel meanings, and camera-link
attribution are unverified; values must not be presented as definitive dBm.
The observer adds no media consumer and changes no media behavior. The completed
local copy, `/tmp/front-link-diagnostic.jsonl`, contains 174 records including its
`end` record (`reason: duration`, 173 preceding rows). The observation stopped
after 100 seconds; the observer is no longer active and its temporary environment
drop-ins were removed.

Channel 2 had 30 command observations, ranging from -44 to -40. Channel 1 had
42, ranging from -64 to -60. Reports can appear on both base and dedicated-media
sessions, so these counts are observations rather than independent RF samples.
There was no -100 report or conspicuous value deterioration coincident with the
recorded Front Door pause. This does not exclude a radio/link cause.

One exact interval illustrates the boundary. Front Door's delivered count stayed
at 1297, with `lastDeliveredUs: 258203033003`, across samples at wall times
1792517805146 through 1792517808146. Their sampled delivery ages grew from
0.632730 to 3.632420 seconds, while `gatedCompleteUnits` remained zero.
At wall time 1792517806988, the base session reported channel 2 value -41;
the dedicated session reported the same value at 1792517806990. Both reports
arrived during the delivery pause. At wall time 1792517809147, the delivered
count had resumed at 1310 and sampled delivery age was 0.013661 seconds.

## Evidence provenance and upstream handoff

### Rejected captured-app start-request experiment

A second A-B-A comparison tested whether the app's captured start-request fields
changed the pause behavior. The temporary serializer changed six fields only for
start command 1003 on channel 2:

1. `ClientOS: "IOS"` instead of `"Android"`.
2. Added `msg_id: 1` to the inner payload.
3. Omitted inner `accountId`.
4. Omitted outer `mChannel`.
5. Omitted outer `mValue3`.
6. Used fixed binary-header stream ID 10 instead of `10 + (level2Seq & 127)`.

Encryption and the existing managed stream
remained unchanged. A stub test verified sibling start requests and stop command
1004 serialized identically to the original implementation.

Each condition started from a service restart and excluded five seconds of
warmup. The table summarizes the remaining approximately 44.7 seconds per stage.
These are sampled source-receipt ages, not direct camera-capture latency or a
count of distinct freezes.

| Stage | Samples | Median age | Maximum age | Samples over 500 ms | Samples over 1000 ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| Restarted original request | 198 | 33.5105 ms | 1299.486 ms | 17 | 2 |
| Captured-app request | 195 | 36.915 ms | 802.881 ms | 16 | 0 |
| Restored original request | 198 | 35.7905 ms | 691.539 ms | 8 | 0 |

The captured request still exceeded 500 ms in about 8% of samples, and restoring
the original request produced fewer such samples. This short comparison did not
show a repeatable improvement attributable to the captured fields. The candidate
was rejected. The original SDK bundle was restored in `finally`.

- Original/restored bundle SHA-256: `1dee01fc12a1cea1c208033c6b630ecbde42039c3279b1563025abbf35fb883c`.
- Temporary trial bundle SHA-256: `723c650c9f292b6829993eeaed5cceaf3a664eee1d36d6bcdb6d651af9ed41ed`.

### Completed kernel-filtered ACK capture

The separate kernel-filtered capture covered 30.000798 seconds, retained 28,354
rows, and reported observer statistics `[28355, 0]`: zero observer drops in this
window. Matching each incoming DATA sequence with its first subsequent outgoing
ACK on the reverse socket flow gives:

| Flow | DATA records | Matched ACKs | Median ACK delay | P95 ACK delay | Maximum ACK delay |
| --- | ---: | ---: | ---: | ---: | ---: |
| Front Door, channel 2 | 4071 | 4071 | 12.896 ms | 40.306 ms | 106.109 ms |
| Garage, channel 1 | 5070 | 5070 | 12.828 ms | 47.763 ms | 97.017 ms |

Front Door's longest recorded DATA interval was 0.626242 seconds between
sequences 11394 and 11395. The preceding packet's ACK was observed after
24.229 ms. Garage's longest interval was 0.727186 seconds between sequences
16512 and 16513; the preceding ACK delay was 1.148 ms. Balcony had one unmatched
DATA record at the capture edge; that alone does not establish a missing ACK.

These results do not show an ACK withheld for the length of either longest
interval. They cover this 30-second window only. An outgoing ACK observed on
the bridge does not prove its receipt by the peer, and zero observer drops do
not prove an absence of network loss or diagnose the earlier 2.7-second pause.

### Independent Mac receiver comparison

An independent Mac process received the three native camera streams directly
through the original SDK bundle (`1dee01fc…`). The remote bridge was stopped
while the Mac exclusively used a clone of the shared session identity. A guarded
180-second recovery and `finally` cleanup returned the updated session and
restarted the original bridge service. There were no startup settings pins, no
FFmpeg, no RTSP producer, and no TV player in this receiver path. The request
retained `streamType: 2` and Front Door's wired power claim.

| Camera | Native geometry | Frames | Measured span | Delivered rate | Largest arrival interval | Intervals over 750 ms / 1000 ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Front Door | 1600×2200 | 677 | 44.849634 s | 15.073 fps | 1541.376 ms | 4 / 2 |
| Garage | 1920×2160 | 671 | 44.953829 s | 14.904 fps | 460.242 ms | 0 / 0 |
| Balcony | 1280×720 | 1124 | 44.891420 s | 25.016 fps | 188.122 ms | 0 / 0 |

Across Front Door's longest arrival interval, its source timestamp advanced from
1517607699 to 1517607768, only 69 ms. That demonstrates uneven delivery of
successive source-timestamped pictures; the source timestamp is not a validated
absolute capture clock.

The observed Front Door pause therefore does not require the weak bridge host,
FFmpeg, RTSP, or a TV player to occur. This was a combined host and downstream
pipeline comparison: macOS/Node 26 and its NIC replaced Linux/Node and the bridge
pipeline. It does not isolate CPU or prove a camera, radio, HomeBase, or SDK root
cause. No fix is claimed. Only metadata `result.json` and `analysis.json` are
preserved under the private evidence directory; configuration, session material,
and `run.log` must not be copied into the repository.

### Rejected ACK batching experiment

A temporary SDK candidate batched outgoing acknowledgements. The detached SDK
worktree was `/tmp/eufy-ack-trial-sdk` at `afcbc3fe5a66d488120dd3d29bcadd1e0eba2c77`.
The manifest records 32 focused tests passing and `npm run build` passing.
No SDK commit or upstream push was made. The original bundle (`1dee01fc…`)
was restored after the trial.

| Stage | Source-age samples | Samples over 500 ms | Maximum sampled age | Observer capture statistics |
| --- | ---: | ---: | ---: | --- |
| Restarted original | 199 | 23 | 3155.585 ms | `[11450, 0]` |
| Batched ACK candidate | 199 | 11 | 829.220 ms | `[7974, 16]` |
| Restored original | 200 | 13 | 2664.981 ms | `[12055, 0]` |

The candidate failed the stated playback criterion of zero samples over 500 ms.
The restored baseline also improved relative to the initial baseline, so this
short comparison does not establish a robust playback improvement beyond natural
variation. It is not a root-cause fix.

The wire measurement does demonstrate reduced ACK packet overhead: Front Door
had 444 ACK packets carrying 1796 sequence entries during the candidate, versus
1792 single-entry ACK packets initially and 1866 after restoration. About four
entries per ACK corresponds to approximately 75% fewer ACK packets per sequence
entry. This transport result does not establish unchanged latency or prove that
ACK packet volume caused the playback pauses. Candidate capture statistics
include 16 observer drops, so unmatched candidate packets cannot be interpreted
as missing protocol acknowledgements without further evidence.

### Historical first-party keepalive audit

A metadata-only audit of a historical first-party capture found four complete
outbound command frames on the target flow, with consecutive DATA sequences
21–24. Existing decrypted metadata identifies start, 6237, 1427, and stop.
No additional periodic 1139/1152 commands appeared in the approximately
10.75-second media window. This short historical firmware/network observation
does not establish whether a keepalive is required today or justify adding one.
The capture extends beyond that window and eventually ends in a truncated packet;
reported holes across all control flows must not be attributed to this target
outbound flow. A fresh first-party comparison on the current path remains needed.

### Shutdown drain fixed a separate defect, not this freeze

A real-class UDP regression exposed queued STOP/END loss on immediate session
close: the original delivered neither packet in 20 trials; the candidate delivered
both in order in all 20. The live candidate also emitted three END packets and
had no incoming DATA after its last END in the capture, whereas the original
emitted no END and HomeBase DATA continued 7.266929 seconds after its last ACK.
Those durations use the wire clock only; Mac API event times have clock offset.

The playback comparison still failed: original-before had 4/201 sampled source
ages over 500 ms (maximum 1296.039 ms), after drained shutdown had 5/199
(1153.383 ms), and restored original shutdown had 9/201 (1105.734 ms).
The original SDK was restored. Shutdown reliability is a demonstrated independent
fix candidate, not an explanation or fix for these freezes. The candidate is not
deployed on the live bridge. See the
[SDK PR handoff](../sdk-udp-close-drain-pr-handoff.md) for proof and scope.

### Longer historical first-party sample

The additional archive survey found an October 9 first-party iPhone capture
with a 103.099-second Front Door-like stream: channel 2, geometry 1600×2200,
HomeBase peer ending `.233` and phone ending `.68`. Its largest recorded packet
gap was 1.046183 seconds between consecutive sequences 21827 and 21828.
A similar pause therefore occurred in at least one historical first-party path
without this SDK. This is not a current equal-condition comparison or proof of
the present root cause. Capture-filter provenance is unknown; absence of a
heartbeat in this archive cannot establish that no heartbeat was sent.

### Picture-header cadence comparison from Downloads

A fresh `tshark` extraction of both iPhone PCAPs reproduced the previously
retained private TSVs byte for byte. Both captures end in a truncated final
packet; extraction reports that error and retains preceding complete records.
The archives are historical October 9 observations, not a current controlled
app/SDK comparison.

For incoming VIDEO (`d101`) datagrams, this census selects visible XZYH command
1300 headers on channel 2 with geometry 1600×2200. It deduplicates header starts
by source timestamp and video-header sequence, so repeated headers/chunks do not
inflate the picture count. These are visible picture-header starts, not proof
of complete, decodable pictures or actual display cadence. The SDK comparison
uses its retained complete native-picture delivery records on the independent Mac.

| Recording and observation stage | Distinct picture starts/deliveries | Span | Largest arrival gap | Source timestamp advance across that gap | Gaps >500 / >750 / >1000 ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| `eufy-iphone.pcap`, first header packet | 395 | 26.278 s | 484.146 ms | 67 ms | 0 / 0 / 0 |
| `eufy-iphone2.pcap`, first header packet | 1516 | 103.099 s | 1065.235 ms | 66 ms | 11 / 2 / 1 |
| Independent SDK Mac, complete delivery | 677 | 44.850 s | 1541.376 ms | 69 ms | 6 / 4 / 2 |

The long iPhone recording contains the same type of uneven arrival: approximately
one second elapses between pictures whose source timestamps advance one nominal
frame. It is therefore useful as a first-party transport reference, rather than
a proven smooth reference. Different dates, conditions and observation stages
prevent interpreting the gap counts as an SDK regression or equivalence result.
The PCAP alone does not show whether app buffering concealed its arrival gaps.

The two additional Downloads archives do not provide another media comparison:
`br0.pcap` covers 119.351 seconds on July 23, 2025 with Eufy discovery/handshake
packets but no identified DATA/video; `br0 2.pcap` covers 28.483 seconds with no
identified Eufy packets. Both have 128-byte snapshots and truncated final packets.

The sanitized comparison is retained under ignored
`.evidence/front-door-pause-2026-10-20/picture-cadence-comparison.json` (SHA-256
`c3c68fa4685ec17b10cb0cfa140580f110ab23fd8169ef765926cd252722bb07`). The source `eufy-iphone2.pcap` SHA-256 is
`7936bc93c2290f858faad805d111d59e8ee8f385d3136101942b61b1b2c48b15`.

Raw artifacts are private local files, not committed packet captures:

| Artifact | SHA-256 |
| --- | --- |
| `/tmp/front-concurrency-trial.json` | `962fbf3339fc39f85fb7cfcd681ff05d754ae3205bb724535a95cfc752ead2f6` |
| `/tmp/front-freeze-udp-headers.json` | `b3a2de755363d98c1ce40a521112eb3404f2c139d3bdcc29ef0cc7b3f5351ec5` |
| `/tmp/front-freeze-ack-all.json` | `73dd19a3dd2afe9d795e16ff69847c5c8076e03b9dbec331775ac336b55305cd` |
| `/tmp/front-link-diagnostic.jsonl` | `986fcd86f3014a6b08821a0726e6ccfe44794fa33e92389a24fb8cda9c2b54b2` |
| `/tmp/front-protocol-trial.json` | `54cf3caab03fe7e6d2a5d5e83a31198e75c89656598aa4731835a3b000948ae1` |
| `/tmp/front-filtered-acks.json` | `1191b61e00ca9d92979269d12edd44a367cd90bfbe6c4d4556d22909042e55c6` |
| `/tmp/front-host-trial/result.json` | `f08cb936027f3f5eefe38445509f81d245c80c1b169d56ae3e3b1e8ad1719482` |
| `/tmp/front-host-trial/analysis.json` | `9e851cd8ae3328d48c71e5fbd8ada1cd32f119ad735c4efd3076bba5b304a8a0` |
| `/tmp/eufy-ack-trial-artifacts/live-trial.json` | `4e86d6ebfa629728ceee18ff835ead45a2f7d2de294a5ef66ce89c9b0ca769b3` |
| `/tmp/eufy-ack-trial-artifacts/wire-analysis.json` | `60a6c79a5aafc4a6733a0916b4507aaa055979f28bdc5467c9d635353f2a6423` |
| `/tmp/eufy-ack-trial-artifacts/manifest.json` | `f9f971f71c909edc11d6e7960defaacd63492d7ac1b64993d185a142712cdb2b` |
| `/tmp/eufy-ack-trial-artifacts/ack-trial.patch` | `800f05df909a246e5de530b7c27c69cdef4c7f3a051cbe2e4306a0bae3dd9004` |
| `/tmp/firstparty-keepalive-audit-20261020/summary.json` | `ad5ae7eb4941ca8fd5ed47a28242a6a41fe684b9c7b8542f901135c02015db54` |
| `/tmp/eufy-close-trial-artifacts/live-trial.json` | `1dd2509e682c47095d4df94716fbe4f0d0d4e65ede86e921e5770d97bc2d7f39` |
| `/tmp/firstparty-keepalive-audit-20261020/additional-archive-survey.json` | `0d0f4fffd592794cc23753d0e2780ddc8c74d6d9b8d4d553c1512819e75672c1` |

The UDP header file has 10,567 rows over 21.990653 seconds. The ACK artifact has
21,322 retained rows plus its drop statistics. Copies are preserved under ignored
`.evidence/front-door-pause-2026-10-20/`. Preserve those limitations in any
upstream report. A future SDK PR needs a demonstrated SDK defect, a focused
change, and a controlled live comparison showing its effect. This record supplies
observations and rejected hypotheses, not that justification.
