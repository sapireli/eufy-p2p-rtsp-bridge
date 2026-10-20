# Garage Doorbell reconnect investigation — October 20, 2026

## Observed failure path

Garage Doorbell (T8213, channel 5) is configured `power_override: always-on`,
`mode: always`, `dual_view: split`. The adapter passes `powered: wired`.
Its native sample was HEVC 1600×2300. The connected TVs used the hardware
Amlogic AVC decoder for the bridge compatibility rendition. A battery streaming
budget or a TV software decoder was not the observed reconnect trigger.

At 22:38:07 UTC the bridge logged six seconds without Garage Doorbell media,
stopped that feed, and scheduled its one-second reopen. All three Fire TVs
logged a Garage Doorbell socket read timeout at that time. Their application
processes remained running. This establishes the bridge's source watchdog as
the trigger for that particular reconnect.

There were also output-side closures. At 22:27:58 UTC the timed producer closed
on a -1386 ms source timestamp discontinuity while the bridge readable remained
open and its stall count was zero. At 22:38:52 Garage Doorbell and Balcony hit
encoder pending-frame bounds; Front Door followed at 22:38:54. These are
separate triggers. A pending-frame guard does not establish GPU capacity or
prove how many frames are physically queued in the encoder.

## Packet and SDK observations

A 90-second metadata capture recorded a 6106.480 ms gap between incoming
Garage Doorbell video datagrams whose transport sequence numbers were
consecutive. That broader observer reported 92 dropped capture records;
therefore it is not a lossless accounting of every packet. SDK counters in
the same observation stopped at raw command-1300 input, assembly and delivery
together. The observation locates an incoming-media pause before player
processing; it does not identify the camera, HomeBase or network as its cause.

A separate 45-second kernel-filtered capture had zero observer drops. All
5277 incoming video packet records matched an outgoing ACK. ACK delay median
was 11.900 ms, P95 39.521 ms and maximum 59.551 ms. The longest incoming gap
was 1445.004 ms; its preceding packet was acknowledged in 3.388 ms. All 18
gaps over 500 ms had consecutive transport sequences. ACK withholding for
the duration of those gaps is excluded in this window. Outgoing ACK capture
does not prove receipt at the peer.

Another incoming-only 90-second capture had 10611 records and zero observer
drops. One raw timestamp reversal of 64 ms accompanied an older/retransmitted
transport packet (sequence step -2). No matching SDK-delivered timestamp
anomaly was recorded in that window. This is not evidence that the SDK
forwarded timestamps out of order, and it does not explain the separate
-1386 ms producer error.

A bounded native-codec sample decoded 60 packets on the Mac: 3 I and 57 P
pictures, no B pictures and no ffprobe errors. B-frame reordering is not
supported as the explanation for that sample; other camera modes were not
exhaustively tested. No camera video was retained in the evidence directory.

## Reproduced bridge defects

1. Feed retirement retained old consumers and their decoder/timestamp state.
   A production-module reproduction joined a replacement delta picture to an
   old reference chain. EOF and watchdog regression tests fail before the fix
   and pass after it. See `camera-feed-consumer-retirement-2026-10-20.md`.
2. SDK keyframe priming retains the original receipt. The bridge formerly
   counted this replay as newly arriving media and cleared recovery failures.
   An actual-module reproduction repeated the same receipt through ten stalls:
   eleven readable opens, zero configured SDK source resets, and reported live
   media although the original receipt was over fifty seconds old.
   See `cached-prime-liveness-2026-10-20.md` for the focused repair and tests.

These reproductions establish software defects. They do not prove either
caused the first incoming packet pause in the observed live run. The first
consumer-retirement deployment still had upstream stalls, a same-readable
source timestamp reversal and encoder pending-frame failures afterward.

## Rejected interventions

The pending map prunes all older timestamps whenever a newer encoded packet
joins. A production-module fake-encoder test survived 360 inputs with only
every tenth frame emitted. Discarded older frames do not leak indefinitely
while output advances; raising/removing the bound is not justified.

The deployed FFmpeg version is 7.1.5. Its stats writer flushes every record;
the join reader drains on either stats or encoded-data arrival. Five reader
tests, including real FFmpeg output, passed. No stats-buffering intervention
was supported by this audit.

No SDK code, quality setting, codec policy, hardware fallback or station
session count was changed by these bridge repairs.

## Remaining limit and evidence location

The incoming source pauses and same-readable timestamp anomaly are unresolved.
No claim is made that HomeBase forwarding, signal strength, bridge GPU capacity
or a client decoder caused them. The reconnect issue is not declared fixed.

Private metadata/summary artifacts are under ignored
`.evidence/garage-doorbell-2026-10-20/`. They contain sequence numbers, timings,
geometry and counters, not media payloads or authentication material. Local
reproductions include `/tmp/repro-cached-prime-liveness.mjs` and
`/tmp/fresh-pending-drop-accounting-repro.mjs`.

## Follow-up: keyframe-boundary delivery, not an established session cutoff

Reanalysis of the same incoming-only capture (no additional camera session or
restart) found 26 distinct picture-header gaps over 500 ms; 24 immediately
preceded keyframes. Of those 24, 22 contained at least 500 ms without any
incoming VIDEO-data-type datagram, including interleaved audio. The largest
picture-header gap was 3075.438 ms, including 2994.000 ms without such packets.
The adjacent transport sequences were consecutive at all 24 boundaries.

The next keyframe segment then transferred roughly 162595 UDP payload bytes
in 26.844 ms in the worst gap. Across 44 keyframe segments, median payload was
approximately 161397 bytes over 21.520 ms. Ordinary picture segments had
approximately 1336 bytes median. Segment boundaries can include interleaved
audio and retransmissions; these are not exact coded-picture byte sizes.
The long gaps therefore are not merely a large picture taking several seconds
to transfer over the observed bridge LAN segment. After gaps, about one second
of source timestamp progression arrived in 0.60–0.68 seconds. The labels are
camera presentation timestamps, not validated sensor capture times.

The paired ACK capture also contains control traffic. During three incoming
media gaps of 690.197, 1093.139 and 1293.557 ms, the peer answered PING with
PONG in 0.909, 0.758 and 0.670 ms respectively. This rules out a complete
bridge-to-peer connection outage for those specific gaps. It does not clear
the camera-to-HomeBase link or establish why the peer withheld media.

The repeated GOP-boundary pattern is stronger evidence than a fixed-duration
firmware cutoff hypothesis, but it does not establish the originating
component. No camera-issued stop or fixed session-age timeout has been proven.
The current model's factory power limits remain relevant: Eufy's Video
Doorbell Dual battery FAQ says existing doorbell wiring extends battery life
and does not convert it into the wired model:
https://us.eufy.com/pages/video-doorbell-dual-2k-battery-powered . Eufy also says
this model does not support continuous recording with HomeBase 3:
https://service.eufy.com/article-description/FAQs-About-Video-Doorbell-Dual-Battery-Powered-Joining-Edge-Ecosystem .
Neither statement establishes a live-view cutoff. The bridge's wired override
changes SDK lifecycle/budget policy, not firmware capabilities or media-start
request fields. Existing archived iPhone captures contain no matching
channel-5/1600×2300 T8213 reference, so a different vendor request has not been
established and no request-field change was made.

## Control telemetry cross-check

The independent eufy-security-client implementation decodes command 1032 as
an `int32LE` RSSI notification in `src/p2p/session.ts` (master, lines 2536–2543):
https://github.com/bropat/eufy-security-client/blob/master/src/p2p/session.ts .
This supports interpreting the notification as signal telemetry, but does not
establish the units or the meaning of the -100 sentinel for this device.

Deduplicating repeated station reports across sessions within 100 ms gave eight
channel-5 reports over the complete SDK observation: -70 once, -75 three times,
-74 once and -100 three times. Seven reports overlapped the packet capture.
Only one of the three -100 reports fell inside a long media gap (4286.683 ms).
The other two arrived while video delivery advanced normally. The largest
6106.480 ms media gap overlapped a -70 report. Two other gaps over two seconds
had no -100 report. Channel 1 reported -61 through -63 without a media gap over
500 ms in that window. This association does not establish RF failure as the
cause of the target camera's pauses.

A later bounded inspector read used only the retained Device-state weak
reference, without a new SDK session or network property refresh. The target
Device object was no longer retained, so battery and charging state remained
unknown. The inspector was closed; port 9229 had no listener and the bridge
PID remained 135966. An absent cached Device is not a zero battery reading.

No new configuration or SDK intervention was supported by this control audit.
A same-model first-party reference, or a controlled camera-to-HomeBase link
change, is still needed to separate the remaining upstream explanations.
