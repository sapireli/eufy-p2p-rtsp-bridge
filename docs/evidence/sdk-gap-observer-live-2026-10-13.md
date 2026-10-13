# Live shared-station video pause: SDK gate excluded for this event

## Capture and scope

One bridge-only bounded observation ran **2026-10-13 02:19:57.726–02:24:57.738
UTC** (300.012 seconds). Private artifact:
`.evidence/frame-freshness-2026-10-12/sdk-gap-deb08e9-300.jsonl`, SHA-256
`0702507aa6ce563bc65e5a200863836f3b53e45a247c7e56996c73b48d61caf8`.
It contains 302 JSONL rows and ends with `reason=duration`, not a cap or write
failure. The parent verified automatic cleanup, removed the diagnostic environment
drop-in and confirmed the rollback guard inactive. No SDK behavior was changed.
The running SDK remained D5, lock commit `d711f98`, bundle SHA-256
`d5db321bbd4ce03397dc0d2b8bd92de3603b5edc3f71bfe0be63b5a464835c92`.

This capture exercised a shared-source cadence pause. It did **not** capture the
previous paired Garage five-second socket timeout. That older event cannot be
assigned this cause by similarity. The observation mechanism and its predicted
versus independently observed counters are described in the
[observer plan](sdk-gap-observer-plan-2026-10-13.md).

## Actual route inventory

Garage was on station-1's base session-1, channel 1. Front Door was on the same
station's **separate** dedicated media session-3, `#live:2`, channel 2. Balcony
was on station-2's session-4, channel 0. These are observed object identities,
not an assumption that all HomeBase streams share one P2PSession.

Balcony's mediaChannel stayed unset: its acceptedData=0 means channel acceptance
was **unknown**, not that no packets arrived. Its session video-command counts
and complete/delivered AU counts nevertheless advanced continuously.

## Matched pause and recovery

The same epoch (`c88dfdc72b434ad0`) and source identities persisted across this
window. Below, A/D means accepted video commands / complete AUs / delivered SDK
video: all three had the same listed count for the two station-1 streams.

| Sample UTC | Garage A/D | Garage HTTP output | Door A/D | Door HTTP output | Balcony complete/delivered |
| --- | ---: | ---: | ---: | ---: | ---: |
| 02:22:36.061 | 2302 | 2242 | 2230 | 2229 | 773 |
| 02:22:37.062 | 2304 | 2245 | 2231 | 2230 | 798 |
| 02:22:38.062 | 2304 | 2245 | 2231 | 2230 | 823 |
| 02:22:39.061 | 2313 | 2253 | 2241 | 2239 | 848 |
| 02:22:40.069 | 2337 | 2276 | 2265 | 2262 | 873 |

Both station-1 sessions' reassembled video-command counters also stayed unchanged
between 37.062 and 38.062 (Garage 2310, Door 2231). Thus the stall was already
present **before complete video commands reached the access-unit assembler**.
No completed units accumulated for the SDK to gate, and encoded output followed
the input silence. Balcony delivered 25 AUs each interval, arguing against a
whole-process event-loop freeze for this particular interval.

Across the entire capture there were **zero `videoGap` events**, zero predicted
gated complete units, and no sampled waiting-keyframe flags. During the matched
pause Garage's keyframe count stayed 82, Door's 77; recovery delivered nine and ten
AUs without a new keyframe (counts still 82/77 at 39.061). That is a direct
falsification of waiting-for-keyframe as the reason for this observed pause.

## Monotonic quiet bounds and source-word progression

At the 38.062 sample (`57952592508` monotonic microseconds), last delivered video
was Garage `57951416168` and Door `57951143055`. Therefore their ongoing quiet
periods were at least **1176.340 ms** and **1449.453 ms**, respectively.

The 39.061 sample's latest delivered timestamps were Garage `57953551028` and
Door `57953569283`, after nine/ten newly delivered AUs. The first resumed AU's
timestamp is not stored, so exact longest frame gaps cannot be reconstructed.
The last-before to latest-after intervals give conservative **upper bounds** of
2134.860 ms and 2426.228 ms for those pauses, not exact measured freeze durations.

Garage's last source word advanced `856789563 → 856790163` (+600); Door's
`856793476 → 856794148` (+672). The next 1.007246-second sample interval delivered
24 AUs each, with source-word advances +1600/+1595. This is catch-up delivery of
media-time progression before encoding, rather than an encoder inventing constant
frame rate. The source word's absolute origin/capture-clock relationship remains
unvalidated; do not convert these words into guaranteed real capture ages.

## Decision and remaining boundary

The SDK's session-wide gap gate exists, but it was **not responsible for this
captured cadence pause**. No SDK gate change is justified by this run. The two
separate sessions to one station went quiet before complete video-command/AU
assembly, while the other station continued.

The next specific decision is to distinguish missing UDP ingress from packets
arriving but waiting in local socket/reassembly work. That requires one matched,
metadata-only kernel-arrival and SDK-event window around a natural pause; this
artifact does not contain physical UDP arrival timing. The parent owns any such
next capture. No new observation or control action was performed for this analysis.
The original five-second paired socket timeout remains unattributed.
