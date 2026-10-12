# Production gate for the default RTSP framing repair

## Controlled promotion

At **23:11:11.596568 UTC on 12 October 2026**, the production owner atomically
replaced only the go2rtc child binary with the verified TCP write repair:
`7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585`.
The new child is PID **31070**. Node PID **30219**, SDK/source-receipt code,
camera policies, client code/preferences, buffers, and decoder selection were
preserved. This single planned RTSP child restart is a deployment boundary, not
a naturally occurring disconnect. No app was launched, installed, or restarted
for this verification.

The temporary 120-second rollback guard was stopped and confirmed inactive
after the following health gates passed. The verified framing repair remains
installed as the default. The temporary experimental enable flag was removed
in source commit `11a4cf9`.

## Both Fire TVs after recovery

Read-only INFO observation: **23:11:27.550–23:11:57.742 UTC**, after the planned
restart. App PIDs remained **20526** (`.85`) and **19463** (`.58`).

| TV / camera | Decoder outputs at first → last sample | Informational render-callback increase | Last ingress→decode age bounds |
| --- | --- | --- | --- |
| `.85` Front Door | 208 → 577 | +366 | 112–122 ms |
| `.85` Garage | 192 → 522 | +327 | 66–73 ms |
| `.58` Front Door | 204 → 572 | +366 | 55–63 ms |
| `.58` Garage | 189 → 562 | +370 | 90–99 ms |

All four players used **`OMX.amlogic.avc.decoder.awesome`**. Sampled input queues
were zero. There were no new errors, read timeouts, retries, counter resets,
or definite ingress-age misses above 500 ms. Render callbacks are informational
on these API 28 devices; their counts do not prove physical panel presentation.
The final age bounds are sampled bridge-ingress→decoder measurements, not sensor
capture age or whole-window maximum latency.

Both logcat collectors completed and closed. Private metadata is retained under
`.evidence/frame-freshness-2026-10-12/tv-writer-default-post30/`.

| Artifact | SHA-256 |
| --- | --- |
| `manifest.json` | `bc6d08bc1e1fe4ecdb5ad73ef1748c27222de1e176c153c7aeca4a92d65eea5d` |
| `summary.json` | `1ee42540cff834401ce3285dc00c49a4c388bf2fddc3c6dd12c2acd280b24959` |

## Parallel production-owner source/API gate

The owner observed the source API for **30.281 seconds**. Front Door input/output
increased **427/427**, and Garage **453/453**, with no new stale or unmapped units;
both source UUIDs stayed stable. Health reported all three cameras streaming,
authentication OK, zero SDK stalls, and three successful completed polls.
Previously accumulated maximum-age values **832/579 ms** predate this window;
they are not current latency measurements or new failures in this gate.

## Result and limits

The production promotion passed its short recovery/health gate and was retained.
The [isolated writer decision](go2rtc-writer-decision-2026-10-12.md) separately
proves the failed-write framing defect, healthy sibling delivery, and redial.
This production window exercised ordinary playback, not a deliberate failed
write on a household TV. It does not establish the cause of past Fire TV
disconnect reports or guarantee sustained physical smoothness. Independent
source pauses and Linux timing work remain separate active issues.
