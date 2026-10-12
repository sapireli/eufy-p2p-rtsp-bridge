# Live baseline after RTSP writer rollback — 2026-10-12

## Why this check

The user reported new Fire TV disconnects during the RTSP writer trial. The
original go2rtc 1.9.14 binary was restored at 22:00:47 UTC; further bridge
deployments were stopped. Its SHA-256 is
`32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6`.
The Node bridge PID remained 26350. No camera quality, stream type, composition,
TV preferences, or APK was changed by this baseline.

Four go2rtc swaps occurred at 21:43:53, 21:54:00, 21:59:40, and 22:00:47 UTC.
Those operations disrupt RTSP connections and are a confound when interpreting
the user's report. The simultaneous Garage read timeouts at approximately
21:47:20 UTC were a separate event, not one of these swaps. Neither a quiet
baseline nor the rollback establishes the cause of that event.

## Fire TV check

Read-only collection ran from 22:09:39.470 to 22:10:09.647 UTC on both TVs.
Both apps remained foreground with unchanged PIDs (.85: 18063; .58: 10531).
Every feed used `OMX.amlogic.avc.decoder.awesome`.

| TV | Camera | Counter sample span | Decoded frame increase |
| --- | --- | --- | --- |
| .85 | Front Door | 27.004 s | 390 |
| .85 | Garage | 26.003 s | 327 |
| .58 | Front Door | 30.003 s | 448 |
| .58 | Garage | 25.005 s | 292 |

The collected interval contained no errors, reconnects, counter resets, or new
definite freshness misses. Reported queue maximum was zero. This proves only
that these feeds advanced without a logged failure during this short interval;
it does not establish sustained smoothness or capture-to-display latency.

Private metadata artifacts (no images or media payloads):

- `tv-original-writer-baseline30/summary.json`: SHA-256
  `c4f9c19e32047d1e838db44e0e8c7e8bdd513440d567c125583effe301228c69`.
- `tv-original-writer-baseline30/manifest.json`: SHA-256
  `f9a2c79c1ebf207fd4ef67a3428e61dd502abe491450fa01d915ddee2dae0038`.

## Bridge counters

A separate read-only `/api/cameras` check sampled every two seconds for 30.323 s
at bridge epoch 1791843044447–1791843074701 ms. Both streams remained live and
their stall counts stayed at two.

| Camera | Input frame increase | Output frame increase | New stale frames | New unmapped frames |
| --- | --- | --- | --- | --- |
| Front Door | 474 | 471 | 0 | 0 |
| Garage | 453 | 450 | 0 | 0 |

Input and output snapshots are asynchronous; the three-frame delta difference
does not by itself prove a dropped frame. Historical maximum egress ages stayed
at 9746.9 ms (Front Door) and 5303.196 ms (Garage); those are cumulative maxima,
not current latency measurements. Capture clocks remain unverified.

Private `bridge-steady-counters-30.jsonl` SHA-256:
`a2958332ab8e951f9ed87703e37d87736faf8df96072f81c9bd9198b1f624a16`.
Artifacts are under `.evidence/frame-freshness-2026-10-12/`.

## Remaining work

GaragePi still has delayed output and clock-sampling failures. These short TV
and bridge checks do not resolve the Linux backlog or the independent source
pauses. Keep the RTSP writer trial opt-in until its live behavior has been
verified without deployment churn.

## Subsequent source interruption under the original binary

The later bounded bridge journal (22:13:20–22:14:40 UTC) contradicts any claim
that rollback resolved all interruptions. Node PID 26350 and original go2rtc
PID 29270 remained unchanged:

- 22:14:08.099891: Front Door had no bytes for 31 seconds, stall #3.
- 22:14:10.100287: Garage had no bytes for 31 seconds, stall #3.
- 22:14:29.935386 and 22:14:31.133609: respectively, SDK errors reported
  `warm-timeout at awaiting-first-frame after 10 attempts; 20000ms deadline`.

`stream-manager.mjs` updates `lastBytesAt` before dispatching a received SDK
frame to HTTP/FFmpeg consumers. These counters therefore describe absence of
SDK Readable callbacks, not merely delayed encoded output. They do not locate
the cause between physical delivery, SDK reassembly/decryption/gating,
Readable state, or event-loop scheduling.

The SDK can prime a reopened consumer with a retained keyframe. Consequently,
the early `streaming` messages at 22:14:09.92 and 22:14:11.13 are insufficient
proof that newly arriving camera frames had resumed: the warm watch still
failed awaiting new source media. Recovery must be verified with advancing
frame identities and receipt times, not only the streaming flag.
