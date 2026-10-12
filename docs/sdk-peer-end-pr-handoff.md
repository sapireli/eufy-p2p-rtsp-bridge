# SDK peer-END recovery and session ownership handoff

## Two isolated fixes, two companion evidence files

The SDK eufy-wall branch contains these separate commits:

1. **55d3fea — scope station teardown to owned live sources.** A station-control
   session closing previously swept every camera-key prefix, including healthy
   sources on independent media sessions. The actual session ownership map now
   selects affected sources and reservations. Structural regression fails on
   the base, passes on the fix, and checks continued independent consumer delivery,
   in-flight media-key reservation survival and unchanged explicit closeAll.
   Evidence: `docs/evidence/p2p-session-owned-teardown-2026-10-12.md` in the SDK.
2. **a9424bf9f92adc0a388aba66157763b4b45f24b2 — honor well-framed END from the
   connected peer.** An incoming four-byte peer END used to be UNHANDLED, leaving
   keepalives and the stream on a dead connection until heartbeat silence.
   Current-peer host/port plus active-socket and framing checks now invoke normal
   close cleanup. LiveStream stops its own timers on session close. Real UDP
   regression fails on the base and passes on the fix; foreign/stale endpoints
   and malformed packets do not terminate the connection. Opaque nonzero payloads
   are allowed when framing is valid. Evidence:
   `docs/evidence/p2p-peer-end-recovery-2026-10-12.md` in the SDK.

Neither fix changes streamType 2, source quality, resolution, ACK scheduling,
reorder waits, keyframe gating, LAN policy or playout buffering. No upstream PR
has been opened. Keep commits isolated and validate on a clean upstream-main
branch; do not merge unrelated eufy-wall history. Upstream main 7d1f016 has both
underlying source patterns. Its clean-base test/build gate remains the PR
preparer's responsibility.

## Actual deployed proof

Original SDK 55dacd3 bundle:
`4ddbf934ccab2ebee30832fdacb7a1874e308278f3db2f41aa98503b6e938cf4`.
The original standalone peer END was followed by continued 1139/PING, a stop
16.009 seconds after VIDEO ceased and a 22.681-second physical cross-connection
VIDEO gap. This did not explain separate heartbeat-alive HomeBase pauses.

Combined fixed bundle, deployed unchanged as PID 23635 at 19:27:11 UTC:
`48372212a4e16a45fd2601febc5239e21f503339303ad975e79aca78583cddb5`.
All 4,103 SDK tests in 235 files, typecheck and build passed. A passive 600-second
NIC capture ran 19:26:25.065381–19:36:25.068802 UTC, retaining 502,375 metadata
rows with zero tap drops, missing timestamps or truncations. It saved integers
and timestamps only; no camera body, encrypted bytes, keys or Inspector hooks.

A **natural END on an active candidate connection** arrived at
19:30:50.149400431 UTC, actual length four bytes / declared payload length zero.
The bridge stopped journal entry followed at +19.890569 ms, feed-ended/reopening
at +25.665569 ms; a replacement CAM_ID appeared at +2452.702244 ms, outer1700
start at +2759.575170 ms and new VIDEO at **+2879.498653 ms**. Bridge streaming
resumed at +3013.432569 ms. There were no further outgoing DATA commands on the
ended connection. Journal intervals include host delivery and logging, not a
separately observed internal close-call timestamp. Transport packets are not
rendered-frame counts or sensor capture-age measurements.

Two independent HomeBase connections received 242/636 VIDEO datagrams during
that recovery. This is continuity evidence across the standalone event; the
ownership fix itself remains structurally tested, with no physical HomeBase
control-END claimed. Three deployment-time incoming ENDs were on **old SDK
active ports** and are excluded by identity; being after the new process's start
does not make them candidate recovery events.

The before/after natural END events are sequential, not identical-scene or
randomized trials. The measured benefit is prompt recognition of explicit peer
closure and removal of ignored-END waiting. Why the peer closed is still unknown.

## Remaining source pauses are distinct

The same 600-second capture contains a Garage HomeBase VIDEO quiet interval of
**9807.603913 ms**, consecutive sequences 5701→5702 at
19:28:03.929888497–19:28:13.737492410 UTC, while heartbeat/control traffic
continued. The existing attached recovery starts occurred 6024.378645 and
9024.938205 ms after silence began, so they do not explain its onset. Another
HomeBase session had **2797.339118 ms** between consecutive sequences 6221→6222,
with no outgoing parent-peer DATA within ±5 seconds. No END was observed on
those candidate HomeBase sessions. These need further source/session/sender
investigation; this handoff must not be rewritten as a general jitter fix.

## Private provenance

Under `.evidence/bridge-network-audit-2026-10-12/`:

| Artifact | SHA-256 |
| --- | --- |
| `command-nic-120.jsonl` (original END) | `e479c547e2e2131008597994f5fd81be1496c697d5b736629810aaea15463292` |
| `end-candidate-nic-600.jsonl` | `2ce9fb1504ce2b061181e69c496a4029b29bf71e07183cd04ffb14e63e490c43` |
| `end-candidate-journal.jsonl` | `fd866edb9526ecaaafc418c952a5a51983dd04a7b5c4deb09bda02fed28cc7fb` |

`end-candidate-analysis.json` retains exact packet/journal records privately.
Public SDK evidence is sanitized; do not upload raw journal device identifiers
or private packet endpoint mappings into an upstream PR. The candidate source
and bundle remained unchanged through deployment and the final hash check.
