# Cached keyframes do not prove live source progress

## Defect and reproduction

The SDK retains an immutable receipt when replaying a cached keyframe to a new
readable. The bridge preserved that receipt in output stamps, but `onChunk()`
also treated the replay as new source media: it refreshed `lastBytesAt`, marked
the reopened feed live, and reset failure and backoff counters.

A production-module reproduction uses `createStreamManager`, `createFreshness`,
real object-mode `PassThrough` feeds, the five-second stall, and the configured
eighth-failure reset policy. It repeatedly reconnects and receives the same
retained receipt. Before the fix, ten stalls caused eleven readable opens and
zero camera-source resets. The original receipt was 50,010 ms old, while the
bridge reported zero last-byte age, live media, and zero failures. No live
service was changed by this reproduction.

The committed regression exercises eight feed-end recoveries with a retained
keyframe. Against the previous implementation it fails: the replay marks the
feed live and erases the accumulated failure. With the fix, all eight failures
remain visible and the configured eighth recovery resets the camera source
once. A genuinely advancing receipt subsequently marks the feed live and
clears failures. A new source can start its sequence at one; a later replay of
the older source cannot move the observed receipt watermark backward.

## Semantics

An SDK receipt advances source liveness when its process-monotonic timestamp
has not moved backward and either its source identity changes or its sequence
increases. These are the SDK receipt's validated `bigint` fields, not the
camera's wrapping wire sequence or presentation timestamp. Equal monotonic
microseconds are allowed because multiple arrivals can share that resolution.
The first observed receipt establishes the watermark without an arbitrary age
cutoff. Legacy chunks without receipts retain their existing behavior.

An established SDK source retains its configured live-stall limit after a
reopen; an unchanged prime does not grant a new cold-start window. A separate
regression verifies the initial thirty-second startup window remains intact,
then two genuine five-second playback stalls retire two successive feeds
despite a cached prime on the replacement.

A retained picture can still initialize a consumer, using its original receipt
and freshness stamp. It cannot refresh last-source activity, mark a reopened
feed live, clear failure/backoff history, or clear the gap-disconnect latch.
The configured reset threshold and camera-scoped reset operation are unchanged.

## Verification and limits

- Before-change regression fails; fixed stream-manager suite: 22 passed.
- Full server suite: 250 passed on Node 26.8.1 using existing checkout dependencies.
- Local reproduction: `/tmp/repro-cached-prime-liveness.mjs` and its `.log`.
- Test logs: `/tmp/bridge-prime-liveness-baseline.log`,
  `/tmp/bridge-prime-liveness-focused.log`, `/tmp/bridge-prime-liveness-full.log`.

This establishes false liveness from cached replay. It does not prove cached
replay caused the currently observed camera's source silence or in-readable
presentation-timestamp regression. There is no SDK change in this fix.

## Deployment

Deployed to homebridge at 22:41:34 UTC, October 20. The service PID was 135966.
The tested and deployed stream-manager SHA-256 matched:
`069a69891c172706ff1b11d6f72a97903b4dd12632c4e673fafe52f04370b269`.
The original SDK bundle was retained. This deployment verifies the tested
module is active; a service restart alone does not establish improvement in
intermittent camera delivery.

The 60-second smoke check included startup: three API requests returned 503
while the SDK initialized, then 111 successful camera polls. All four always-on
Eufy cameras advanced receipt sequences with zero watchdog stalls. Garage
Doorbell advanced from sequence 5 to 810, with maximum sampled source-receipt
age 2595.690 ms. All three Fire TVs reconnected using the Amlogic hardware AVC
decoder and each reported 703 decoded Garage Doorbell frames by 22:42:43 UTC.
Their decoder queue depth was zero or one; observed longest rendered-frame
gaps remained approximately 2.8 seconds. No new discontinuity or pending-frame
bound occurred during the smoke window. These source pauses remain visible;
this is deployment health evidence, not a sustained cure or a comparison that
separates the recovery change from restarting the service.

Private live sample: `.evidence/garage-doorbell-2026-10-20/garage-doorbell-prime-fix-live60.json`.
