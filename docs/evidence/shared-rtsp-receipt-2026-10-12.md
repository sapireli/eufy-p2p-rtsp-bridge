# Shared RTSP resource receipt — 2026-10-12

A read-only 180-second observer ran on the Mac at approximately
18:36:50–18:39:50 UTC, excluding its first ten seconds from steady summaries.
It consumed `front_door_cle` and `garage_cle`, the same compatible H.264 resources
used by both Fire TVs. The live go2rtc API showed one producer per camera shared
by the Mac and both TVs, rather than additional encoder instances.

Two hardware egress starts at 18:36:51 coincided with TV replacement/relaunch and
observer connection. The resulting measurements describe that reopened producer
window; they do not reproduce the preceding Pi event or exclude an intermittent
producer problem at other times. The Mac sockets closed normally at completion.

| Bridge ingress → complete AU receipt, conservative upper bound | Front Door | Garage |
| --- | ---: | ---: |
| Matched steady frames | 2,528 | 2,585 |
| Median | 89.677 ms | 99.696 ms |
| p95 | 204.745 ms | 178.136 ms |
| Maximum | 290.211 ms | 303.392 ms |
| Maximum clock-bound width | 44.893 ms | 48.357 ms |

Both streams had zero missing/invalid stamps, invalid transport frames,
abandoned access units, duplicate/regressing frame IDs and age-upper values over
500 ms. A 1000 ppm rate allowance was included. Their common bridge epoch was
`aa064658bab11fe0`.

This establishes low ingress-to-receipt age in this window. It measures neither
camera capture time nor decoder/display latency. In particular, it does not
invalidate the preceding Linux run's >11.679-second lower-bound delay, which
occurred in another window. Exact per-stage identity tracing on the Pi is the
next isolation step.

Private metadata artifact (two JSON lines, no image payloads):
`.evidence/frame-freshness-2026-10-12/compat-mac-current-180s.json`, SHA-256
`944d7b1cfc533cd047f6bdbf470b6e1c955e787ea75900982e77f49455c31ef3`.
