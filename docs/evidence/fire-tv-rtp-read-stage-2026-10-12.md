# Fire TV read-stage and exact-frame transport evidence — 2026-10-12

## What ran

Both TVs retained hardware AVC, original full preferences and APK SHA
`c5611f04a53e0444045d9ed9281a35495d9d90a096dc3883ee0665d11278b63a`
(code5de3cb5;53 unit tests passed). No buffer, camera quality, rendition policy,
thread priority or transport policy changed. The read observer adds bounded
metadata only, without per-packet logs or payload copies.

Owned INFO capture ran19:51:34.914–20:01:35.004 UTC (600.090 seconds), plus a
bounded concurrent Mac RTSP receipt observer on the same compatibility producers.
Local RX/thread observation ran19:52:19.587–20:01:31.432 UTC. All owned observers
closed. A duplicate server TCP probe was stopped when the parent started its
continuous collector; only the parent's socket data is used below.

The bridge cloud watchdog disconnected all feeds at19:57:17.039–17.065 UTC and
reopened them at18–19. Both TVs recreated players at19:57:19 with the same app
PIDs25471/24932. The parent independently identified a watchdog health-contract
bug; this boundary is **not** a natural media peer-END or an Android decoder fix.
Do not subtract cumulative player counters across that reset. Network thread IDs
also changed: the fixed-ID CPU sampler is valid only before the boundary; local
socket RX samples remained valid after it. The bridge epoch stayed
`5a9847d5d5de3c4f`.

## Exact-frame findings

One `-T1` historical outlier44 ms before capture start was excluded. Within the
window .85 recorded20 Door and50 Garage outliers; .58 recorded one Door outlier.
All71 matched the Mac observer by epoch, frame ID and ingress timestamp. The Mac
received8937 Door and8934 Garage stable stamped frames, with no invalid transport,
missing stamps, duplicates, regressions or upper-bound500 ms misses. All-frame
maximum upper receipt ages were490.696/420.980 ms respectively. These are bridge
ingress ages under clock bounds, not camera sensor age or physical display latency.

The following statistics select only the outliers; they are not all-frame latency
percentiles.

| .85 late frames | Door20 | Garage50 |
| --- | ---: | ---: |
| Mac receipt age upper median / max ms | 154.966 / 490.696 | 133.595 / 211.164 |
| TV completed-AU receipt age lower median / max ms | 534.283 / 734.034 | 573.919 / 830.267 |
| TV receipt lower minus Mac receipt upper median / max ms | 391.611 / 563.956 | 444.165 / 700.390 |
| Parser elapsed before AU callback median / max ms | 0.082 / 8.027 | 0.039 / 10.539 |
| Listener elapsed median / max ms | 0.058 / 0.144 | 0.050 / 1.478 |
| Receive → input-submit median / max ms | 2.395 / 6.693 | 5.059 / 18.256 |
| Input-submit → codec-output median / max ms | 36.072 / 105.414 | 39.017 / 158.685 |

TV receipt lower is decode lower minus measured receive-to-output duration; its
comparison with Mac receipt upper conservatively bounds extra delay on the TV's
path, conditional on clock models. All70 .85 outliers have positive extra receipt
delay;12 Door/37 Garage frames were already lower-age >500 ms when their complete
AU reached the callback. Some frames subsequently spend additional time in the
codec stage; this cannot explain the large receipt-delay examples below.

The single .58 Door outlier was close to a shared limit: frame88072 Mac receipt
upper490.696 ms, TV AU receipt lower494.452 ms, input→output21.531 ms. It does not
show the same several-hundred-ms extra path delay as the .85 witnesses.

## Actual long-read witnesses

At19:56:57.381 UTC .85 Door frame97458 had Mac upper receipt170.078 ms versus
TV lower completed-AU receipt734.034 ms. Its51 RTP packets accumulated60.420 ms
header-read elapsed and587.142 ms payload-read elapsed; parser8.027 ms,
listener0.129 ms, input→output16.589 ms. First header-read start mapped to a lower
age78.390 ms, so substantial delay occurred while this AU's read calls ran.

At19:56:57.427 .85 Garage frame97464 had Mac upper77.084 ms versus TV receipt
lower665.423 ms. Its69 packets accumulated194.909 ms header/471.401 ms payload,
parser10.539 ms, listener0.126 ms, input→output23.276 ms. Header start preceded the
stamp's ingress by about11 ms in the lower mapping.

At19:58:17.244, after the watchdog reconnection, Garage frame101764 had Mac upper
83.599 ms versus TV receipt lower706.000 ms. Its47 packets accumulated641.789 ms
header/471.228 ms payload, parser3.620 ms, listener0.555 ms, input→output22.225 ms.
**Its header read began before that frame's bridge ingress**, so the entire1113 ms
read sum is not post-ingress frame delay. Exact-frame receipt comparison still
establishes at least622.401 ms extra TV receipt delay under the interval models.
The following Garage frame101766 was already lower-age828.389 ms at header-read
start, then completed its own read+parser in about0.31 ms. A later small frame can
arrive stale behind an earlier transport wait without expensive parsing itself.

Elapsed read time includes OS descheduling, allocation, and blocking socket reads;
it is not pure wait or CPU time and does not identify Wi-Fi RF as the cause.

## Socket corroboration and limits

The parent's passive server TCP collector sampled about500 ms. Around the latter
event, .85 connection45534 showed:

- 19:58:16.254: Send-Q922915 bytes, last ACK604 ms earlier, cumulative retransmitted
  bytes269803, advertised send window522880 bytes.
- 19:58:16.756: retransmitted bytes291523 (+21720), Send-Q586091.
- 19:58:17.258: Send-Q0.

The other .85 connection45538 retransmitted bytes rose169291→191246 (+21955)
over those same samples. This is an observed TCP delivery/recovery episode that
coincides with exact late-frame receipt and long reads; static sender queue
capacity alone was not used as proof. At the .85 local RX samples16.433,17.195 and
17.962 both socket queues were zero. Sampling can miss brief occupancy, and these
records cannot isolate radio loss, access-point behavior, kernel scheduling, or
receiver thread descheduling as the underlying cause.

Before the watchdog boundary, at the19:56:57 episode, local RX queues were sampled
zero at56.722 and57.492. IO threads25546/25547 changed sampled state S→R and their
CPU counters increased2/3 ticks over approximately0.77 seconds. Parser/listener
elapsed was small in the exact AU records. These observations argue against
hundreds of milliseconds of parser CPU for those frames; they do not measure
runqueue delay. `schedstat` was unavailable and run-as `/proc/uptime` denied;
wall-nanosecond markers were recorded. The snapshot spacing was approximately
0.75 seconds due to command overhead, not an exact0.5-second guarantee.

## Conclusion and artifacts

The read-stage extension was exercised on real late frames. For the documented
large-delay witnesses, most delay is before AU callback, with long header/payload
read intervals and little parser/listener work. The19:58 episode has matching TCP
send-queue/retransmission evidence. This is a stronger localization than a generic
player-buffer theory; it is not yet a completed network or smoothness fix.
No new buffer or priority policy was applied.

Private metadata files live in
`.evidence/bridge-network-audit-2026-10-12/tv-rtp-stage-600s/`: INFO logs and manifest,
Mac receipt samples, sparse outliers, exact joins, RX/thread samples and manifest.
Parent TCP data is
`.evidence/frame-freshness-2026-10-12/tcp-receiver-joint-600s.jsonl`.
Encoded camera payloads are not committed.
