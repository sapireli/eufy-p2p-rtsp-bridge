# Bridge counters during wired GaragePi playback

## One bounded observation

Existing bridge `/api/cameras` fields were read on bridge loopback once per second
from **2026-10-13 04:32:10.792152285 through 04:33:10.804347645 UTC**.
The 61 endpoint samples span 60.012202 seconds. GaragePi was reported by the
controller as wired, using its original client at `.20`; this observer made no
Pi reads or changes. Fire TV measurements were owned by a separate agent.

No service restart, SDK hook, new media consumer, packet capture, camera setting,
network setting or client change was made by this observer. All jobs ended.

The controller recorded **zero request errors**, maximum request duration
**72.523 ms**, and **394.786 ms process CPU**, approximately **0.658% of one core**
over the window. This measures observer process overhead, not all kernel or
bridge work induced by the requests.

## Observed counters

| Existing bridge field | Front Door CLE | Garage CLE |
| --- | ---: | ---: |
| Consumer input frames, first → last | 18488 → 19384 | 18452 → 19351 |
| Encoded HTTP output frames, first → last | 18487 → 19383 | 18451 → 19350 |
| Input and output increase | 896 | 899 |
| Input and output average rate | 14.933 frames/s | 14.983 frames/s |
| Median latest SDK receipt age | 33.740 ms | 27.464 ms |
| Maximum sampled latest SDK receipt age | 637.845 ms | 213.478 ms |
| New stale / expired reference / unmapped output frames | 0 / 0 / 0 | 0 / 0 / 0 |

Both cameras retained one source ID throughout. Every sampled interval advanced
SDK receipt sequence and consumer input/output counters. No one-second sampled
counter stasis or source replacement was observed.

Front Door's latest SDK receipt age exceeded 500 ms at two samples:

- **04:32:27.792761349 UTC:** 637.845 ms; receipt sequence 38182, frame ID 267809,
  input and output counters both 18703.
- **04:32:55.792828768 UTC:** 574.285 ms; receipt sequence 39094, frame ID 269376,
  input and output counters both 19150.

These ages are elapsed time since the most recent SDK video receipt, giving a
lower bound on a receipt quiet period. They are not measurements of the age of a
currently displayed picture. One-second sampling can miss shorter pauses and
cannot determine exact pause onset, recovery or the largest inter-frame gap.

The cumulative `maximumEgressAgeMs` field already contained historical values
(including Front Door 57839.744 ms) and did not establish a new maximum during
this run. It must not be presented as current-window latency. No new stale output
was counted, but existing API aggregates do not provide a distribution or hard
bound for every output picture's age.

## Matched device scope and decision

This collection supplies the exact bridge window for comparison with the
controller's wired Pi and separate TV observations. It does **not** independently
verify the Pi's wiring, selected URI, decoder, presented frame IDs, clock bound,
physical output or the devices' complete overlap with this window. A matched
client result must use its own actual observation timestamps and source identity;
startup and non-overlapping samples are excluded from a causal comparison.

Garage's bridge input/output advanced at approximately 15 frames/s without a
sampled receipt age above 500 ms. Front Door retained brief receipt quiet periods.
This observation identifies no cause for those periods and **justifies no bridge
or SDK code change**. No repeated capture or intervention followed.

## Preserved artifacts

Private directory:
`.evidence/frame-freshness-2026-10-12/wired-pi-api60-20261013T043210Z/`.

- `api.jsonl`: 61 bounded metadata rows; SHA-256
  `5b700ed17878af0eeb51a6cad3418f037f7d4b9d67fa3d0a73fc8422510e590e`.
- `summary.json`: derived counter deltas, source IDs, sampled ages and request
  maximum, calculated from the preserved rows.

No frame bytes or camera credentials were stored in these artifacts.
