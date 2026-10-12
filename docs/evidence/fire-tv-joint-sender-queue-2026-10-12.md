# Fire TV ingress-age and sender-queue observation — 2026-10-12

## Scope

This is diagnostic evidence, not a smoothness fix. Both Fire TVs ran the same
hardware AVC player, freshness capability v2 `observe`, and original two-camera
preferences. Observation never blanks or drops video. No buffering, camera quality,
layout, or stream policy changed. Sensor capture time and physical panel presentation
are not established by these measurements.

Installed APK SHA-256:
`c958fa6f981e53f9fcf1b129c2162bb44a797f624580ac85b6bd368998a22778`.
Both cameras on both devices used `OMX.amlogic.avc.decoder.awesome`.
The INFO-only collector ran 18:58:52.278–19:08:52.378 UTC (600.100 seconds),
while the root diagnostic go2rtc build measured sender queue occupancy and socket
writes. That build did not change queue or error-handling behavior. SDK sources
were unchanged during the window. A parent-owned RTSP observer shared the same
producers; this was a diagnostic load, not a TV-only capacity acceptance test.

## Results

Counter differences span approximately 595–596 seconds between first/last INFO
samples; INFO cadence is about five seconds, so sampled queue/clock maxima do not
bound every intermediate value.

| TV / camera | Decoded mean fps | Lowest sampled window fps | Definite ingress-age >500 ms frames | Upper-bound uncertified frames | Maximum decoder pending ms | Maximum sampled compressed queue |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| .85 Front Door | 15.006 | 8.998 | 82 | 87 | 72 | 0 |
| .85 Garage | 14.950 | 7.798 | 62 | 65 | 401 | 1 |
| .58 Front Door | 15.011 | 9.202 | 0 | 0 | 71 | 1 |
| .58 Garage | 14.986 | 5.801 | 15 | 15 | 130 | 0 |

Decoder-pending median was zero for all four streams. The .85 Garage 401 ms
sample occurred at 19:01:42.192 UTC; it is a real decoded-progress backlog sample,
not the render-callback metric. Callback-pending medians were about267–268 ms;
API28 `OnFrameRendered` can batch or omit callbacks, so this metric does not
establish decoder backlog, dropped displayed pictures, or physical display latency.

Sampled clock interval maxima were14/14/17/19 ms in table order. The late-frame
counter uses the **lower** ingress-age bound; an upper bound alone exceeding500 ms
only means uncertified freshness. Lower bounds remain conditional on the bridge
clock model and identify time since bridge ingress, not camera sensor capture.
There were no warning/error messages, reconnects, missing stamps, unknown clocks,
observation blanks, stale-output drops, or pacing drops. PIDs remained22552/22028;
exact full preferences still matched their pre-trial backups after collection.
Uneven sampled delivery rates remain; a15 fps mean does not prove smooth playback.

## Sender-write correlation and its limits

The root API sampled queues and cumulative write statistics at about500 ms.
Relevant .85 Garage observations:

- 18:59:19.252 UTC: queue6; write maximum increased to353.465 ms at19.752.
  The client lower-late count increased6 in18:59:17.162–22.164.
- 19:00:51.338 UTC: queue5; the lower-late count increased2 in
  19:00:47.181–52.182.
- 19:03:05.984 UTC: queue10 and cumulative maximum write394.233 ms;
  19:03:06.485: queue15 and maximum408.835 ms. The .85 lower-late count
  increased21 in19:03:03.204–08.207.

The latter event also added15 definite-late Garage frames on .58 in
19:03:05.124–10.126, with its sender queue sampled at zero and write maximum
75.25 ms. This establishes a coincident late-client event, not a common producer delay.
The concurrent Mac observer's maximum Garage ingress age was358 ms (Door437 ms),
so no Mac frame exceeded500 ms in that run. Shared Wi-Fi or client-side effects
remain possible; exact same-frame identity joins are needed to identify where
those TV frames were delayed.

Other .85 Front Door late bursts occurred with its sender queue sampled at zero,
write maximum at most61.88 ms, and decoder-pending sampled at zero. For example,
19:01:34.247–39.248 added15 late frames and19:01:44.249–49.249 added16.
Therefore server sender queue alone does not explain all late frames. No socket
write errors or short writes were observed. These observations do not distinguish
radio/TCP delivery delays from receiver read-thread scheduling or parser work.

Next isolation needs monotonic blocking header/payload read time versus parser and
listener CPU time, correlated with exact access-unit stamps, rather than a larger
player buffer or a queue-capacity change inferred from static source code.

## Earlier lower-bound baseline

The same APK ran an INFO-only180-second baseline at
18:52:50.369–18:55:50.376 UTC before the go2rtc diagnostic child restart.
Lower-late increments were .85 Door22/Garage35, .58Door1/Garage0;
upper increments23/37/1/0. Decoder-pending maxima were69/67/70/78 ms,
compressed queues zero, and sampled clock-width maxima18/17/27/19 ms.
This independently showed mostly .85 bridge-ingress lateness without a large
sampled codec backlog or per-frame DEBUG logging.

## Private artifacts

`.evidence/bridge-network-audit-2026-10-12/tv-go2rtc-joint-600s/`
contains collector manifest, append-only INFO logs, and computed summary with
late-count increment windows. The lower180-second run is in
`tv-age-interval-180s/`. Parent sender metadata is
`.evidence/frame-freshness-2026-10-12/go2rtc-queues-writes-600s.jsonl`.
No encoded camera image payload is included in this document.


## Sparse exact-frame follow-up instrumentation

A separate diagnostic-only Android change emits an INFO record only when decode
age **lower >500 ms**. It includes bridge epoch/frameId/ingress timestamp, RTP PTS,
complete parsed AU callback-entry monotonic time (before input queue insertion),
input-submit-start time, codec-output time, receive-to-output elapsed time, current
input queue depth, and exact lower/upper/clock-width nanoseconds. Receive time is
after RTP assembly, not the first socket byte. Input time is immediately before
`queueInputBuffer`, not an assertion of codec execution start. Missing times are
explicitly-1 and never borrowed from another frame. Render callbacks are not used
for this record. Frame-identity maps remain bounded256 entries. No per-packet logs,
buffer changes, admission changes, pacing changes, or display gates were added.

Tests cover upper-only uncertainty, the500 ms boundary, exact identity/times, and
invalid/missing stage timestamps. These records enable joining the same frame
with the parent Mac observer; they are not themselves a latency fix.
