# Fire TV decoded progress and age intervals — 2026-10-12

## Diagnostic baseline

Both TVs were updated in place to APK SHA256
`bb486e0e3cb44abffc1fe09872b870fe447b0589895d2f08e7ebcbe474195f82`.
Exact original preferences were retained, native profile probe wasoff, and the
actual decoder remained `OMX.amlogic.avc.decoder.awesome`. No playback buffers,
camera settings, or admission rules changed.

The600-second INFO observer ran18:36:53.858–18:46:53.852UTC, with stablePIDs
21488/20905. It began only after the Pi measurement and clock sampler completed.
Root's separate Mac observer was present through18:39:50; go2rtc's actual API
showed one shared timed-stream producer per camera and three consumers (Mac plus
both TVs), not separate encoders. First-time encoder warmup/startup and that
additional-consumer window are marked. The following416–420seconds are TV-only.

| TV / camera | TV-only mean fps | Min sampled interval fps | Max compressed queue | Max decode-pending media | Max callback-pending media |
|---|---:|---:|---:|---:|---:|
| .85 Front Door |15.033|9.400|1|134ms|401ms|
| .58 Front Door |15.047|8.998|0|135ms|398ms|
| .85 Garage |14.992|9.198|1|134ms|404ms|
| .58 Garage |14.997|10.596|0|74ms|401ms|

There were no warnings/errors, playback retries, missing stamps, pacing drops,
freshness blanking, or sustained compressed-queue growth. Some INFO intervals
span6seconds rather than5; interval rate uses actual logged elapsed time.
The approximately15fps mean is not proof of even frame delivery or smooth display.

The old callback-pending metric was often~267ms while decoder-pending was0.
Thus a delayed informational callback can manufacture the appearance of decoder
backlog. API28 callback counts/timestamps remain informational, not physical
panel measurements or a count of dropped displayed pictures.

## Upper age is not actual age

The .85 Door/Garage upper-bound misses accumulated52/162 times; .58 accumulated0
for both. **An upper age crossing500ms means freshness cannot be certified by that
clock interval; it does not prove the actual frame was more than500ms old.**
These results alone cannot attribute the difference to Wi-Fi, decoder, or server.

A15-second streamed DEBUG tail ran18:43:43.307–18:43:58.674UTC inside root's passive
TCP sample. INFO streaming continued and logging was restored toINFO in `finally`.
Within that particular tail, sampled clockRTT was7–13ms; ingress→decode upper-age
medians91–96ms and maxima224–234ms. No upper500ms miss occurred in the tail.
Those samples do not explain the separate miss intervals. DEBUG overhead is not
asserted to be zero.

The diagnostic correction now reports both sides of the same valid clock interval:

- Earliest mapped ingress: requestStart + delta −1000ppm drift allowance.
- Latest mapped ingress: responseTime + delta +1000ppm drift allowance.
- Age upper/lower: now minus earliest/latest; lower is clamped at0.
- Interval width: pollRTT plus twice the stamp-relative drift allowance.

The same epoch, sample lease, RTT limit and future-stamp checks apply. Neither
admission/deadline computation nor clock-sample selection changed. A new INFO
counter records decode frames whose **lower** age exceeds the allowed age; the
existing upper-bound counter retains its conservative meaning.

Tests show a500ms clockRTT producing upper600ms/lower100ms: freshness is uncertified,
but the measurement does not prove a late frame. Additional tests cover lease,
epoch, and both signs of relative clock drift.

## Scope and artifacts

Private append-only files are in
`.evidence/bridge-network-audit-2026-10-12/tv-decoder-progress-10min/`: manifest,
INFO logs, summary, and the bounded timing tail. The tests and diagnostic fields
are a measurement improvement, not a smoothness or latency fix. A subsequent
INFO-only run is required to distinguish definite late frames from uncertain
clock bounds without extra per-frame DEBUG logging.
