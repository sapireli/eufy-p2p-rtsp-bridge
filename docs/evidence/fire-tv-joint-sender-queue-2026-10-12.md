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


## Live exact-frame join after sparse instrumentation

APK `c411aa914167a1f6fae368f1ac7d742c9102a3f01e6d250752d1d66343d59b13`
was installed sequentially with full original preferences preserved. Both cameras
on both TVs still used the hardware AVC decoder. SDK candidate deployment at
19:27:11 UTC preceded this separate window; original release go2rtc was restored.
This evidence does not assign changes between different runs to that SDK candidate.

INFO collection ran19:28:21.044–19:31:21.049 UTC (180.005 seconds), bridge epoch
`5a9847d5d5de3c4f`, PIDs24296/23786. There were28 Door and67 Garage sparse
outlier records on .85; .58 had none. All95 records matched the concurrent Mac
observer by **epoch, frameId, and ingressMonoUs**, not camera clock or RTP clock.
The first Garage miss counter already contained18 warmup misses, excluded from
this run's delta. No warning/error, reconnect, missing stamp, unknown clock,
observation blank, or pacing drop appeared in the recorded interval.

Statistics below apply only to these selected late frames, not all frames.

| .85 late frames | Door (28) | Garage (67) |
| --- | ---: | ---: |
| Mac receipt ingress-age upper median / maximum ms | 110.99 / 148.95 | 134.05 / 189.95 |
| TV assembled-AU receipt ingress-age lower median / maximum ms | 586.64 / 1464.99 | 837.46 / 1365.84 |
| AU receipt → input submit median / maximum ms | 4.68 / 7.87 | 0.98 / 16.05 |
| Input submit → codec output median / maximum ms | 61.15 / 245.38 | 55.65 / 476.44 |
| Frames already lower-age >500 ms at TV AU receipt | 22 | 51 |
| TV AU receipt lower minus Mac receipt upper positive | 28 | 61 |

TV receipt lower is decode lower minus measured receive-to-output duration.
Comparing it with Mac receipt upper conservatively bounds additional delay on
that TV's path under the respective valid clock intervals. Clock/model allowances
remain conditional; this is bridge-ingress age, not sensor-to-screen latency.

Worst Door record: frame11425, ingress33241174356 µs. Mac receipt upper104.746 ms;
TV assembled-AU receipt lower1464.989 ms; receipt→input0.486 ms and input→output
12.125 ms. Worst Garage decode-age record: frame11420, ingress33241115824 µs;
Mac receipt upper151.966 ms; TV AU receipt lower1365.839 ms; receipt→input0.752 ms,
input→output16.225 ms. These exact-frame examples localize most delay **before
complete AU receipt on .85**, rather than shared producer delivery or that frame's
decoder input/output stage. They do not distinguish socket/radio/TCP delay from
read-thread scheduling or RTP assembly CPU. Other Garage records separately show
input→output holds up to476 ms; one explanation is not sufficient for all frames.

The mean decoded rates were about15 fps for Door and16.2 fps for Garage in this
window; uneven five-second delivery remained. No smoothness acceptance claim is
made. Private `tv-exact-outliers-180s/` contains manifest, INFO logs, summaries,
sparse records, and exact Mac joins. The parent observer is
`compat-mac-sdk-end-exact-180s.jsonl`. No camera image payload is committed.


## Read-stage diagnostic extension

The next candidate adds one bounded accumulator per RTSP connection and one small
snapshot per delivered NAL/AU. For each video RTP timestamp it records header-read
start, last payload-read completion, complete AU callback time, packet count,
network thread ID, summed header/payload elapsed times, parser elapsed before the
AU callback, and listener elapsed/completion. Snapshots attach to the existing
VideoFrame and exact sparse outlier; there are no per-packet logs or payload copies.
Elapsed header/payload time includes blocking, allocation, and thread descheduling;
it does **not** identify RF delay or pure CPU time. Callback/listener duration is
completed after the callback returns; a zero completion timestamp explicitly means
it was not yet complete when sampled. No bytes, parser branches, buffers, or
input/output scheduling policies change.

Timestamp changes reset aggregate counters; earlier snapshots retain their own
identity. Tests cover same-timestamp aggregation, listener exclusion from parser
elapsed, timestamp reset, sparse-field attachment, and byte-equivalent single-NAL
and fragmented AVC output against the unobserved parser with exact AnnexB fixtures.
CPU/schedstat sampling is left to a separate bounded passive observer rather than
adding an expensive per-packet system call.
