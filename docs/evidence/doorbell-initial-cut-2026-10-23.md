# Doorbell interruptions and bridge restart isolation

The initiating Garage Doorbell video interruption remains unresolved. The ownership trial did not justify changing SDK allocation, and the hardware checks found both important cameras correctly pinned. A normal base-session source reset recovered playback twice; the attempted private-session comparison ended under the bridge's no-byte watchdog before the SDK's first-frame deadline.

## Hardware and power-policy verification

Garage Doorbell's persistent settings revision 4 contains `power_override: always-on` and `mode: always`; the live API reports `powered: true` and `powerOverride: always-on`. Observed SDK sources report `powered: wired`. This is the configured power policy, not an electrical measurement of the camera.

All three Fire TVs (.58, .85 and .100) already pin the current Front Door and Garage Doorbell device identities. Both feeds actively used `OMX.amlogic.avc.decoder.awesome`. During the 15:37:14–15:37:35 UTC check, their rendered-frame increases were Front Door 228/217/224 and Garage Doorbell 227/225/226, respectively. Queues were zero; the largest sampled pending-decode duration was 68 ms. No codec allocation failures, lease revocations or player releases were observed in this short window. Pins, layouts, preferences and installed APKs were unchanged. Android clients do not encode these feeds.

The bridge's actual FFmpeg processes for both door feeds requested VAAPI decode, hardware surfaces and scaling, and `h264_vaapi` encode, with an open `/dev/dri/renderD128` descriptor. Advancing ingress/output counters and the active processing arguments verify the hardware path; no software fallback was observed. Existing quality and scaling settings were preserved.

GaragePi already pins the same two current camera identities with hardware priorities 100/50 and `decoder: auto`, preserving hardware-first/software fallback. A brief normal TV-on check verified both actual `v4l2h264dec` → `kmssink` pipelines produced output. Front Door advanced from 175 to 472 matched/KMS frames over 31 seconds. Garage Doorbell produced seven hardware-decoded/KMS frames before its upstream interruption; sustained Doorbell playback was therefore not verified. The prior TV-off state was restored, client processes stopped, and the configuration hash was unchanged. The former DietPi address was unreachable and its hostname unresolved; a separate old host is not claimed verified.

These checks found no hardware-decoder allocation or bridge hardware-encoder failure explaining the captured initial cutoff. They do not rule out every possible hardware fault. In the measured initiating failure, incoming VIDEO stops before SDK assembly, bridge decode/encode and client decoding.

Private hardware artifacts: `/tmp/eufy-hardware-pin-audit-20261023/` and `/tmp/eufy-doorbell-hardware-verification-20261023.md`.

## Intervention

The live bridge process remained PID 7116. From 2026-10-23 15:29:29.148 to 15:32:29.786 UTC, three approximately 60-second arms used the same public `resetCameraStream()` operation for Garage Doorbell. Front Door and all other sources were left running. No codec, quality, power policy, decoder, encoder, client, or persistent configuration changed.

- A1: native allocation, using the station/control connection.
- B: a one-shot, exact-source-key override of `stationSessionInUse()` selected the SDK's existing dedicated-media allocation path. The original method was restored immediately when selected. This was a diagnostic shim, not a supported public setting or a proposed production fix.
- A2: native allocation restored, again using the station/control connection.

B necessarily added a connection while the original control connection remained open. It was therefore not a session-count-matched experiment. A preliminary baseline was interrupted before B to add explicit cleanup restoration; its samples are excluded from the table below.

## Results

| Arm | Doorbell completed video headers | Doorbell emitted frames | Longest emitted-frame gap | New bridge stalls | Actual ownership |
| --- | ---: | ---: | ---: | ---: | --- |
| A1 | 858 | 857 | 2.717 s | 0 | Base throughout |
| B | 372 | 372 | 3.237 s after playback resumed | 1 | Private start failed to deliver; later frames were on base |
| A2 | 869 | 869 | 2.693 s | 0 | Base throughout |

The B frame-gap value excludes its initial 35.739-second wait for any emitted Doorbell frame. It must not be read as the longest interruption of that arm.

B's decisive events, relative to the whole trial:

- At 61.549 s, the exact-key allocation override selected a private connection.
- At 62.162 s, that connection reported connected.
- At 62.354 s, the SDK issued its first media START on channel 0.
- The private connection delivered no observed VIDEO datagrams, completed video headers, or emitted frames.
- At 68.644 s, the bridge retired the source. Its journal reports `no bytes for 6 s` and a source reset. The SDK STOP and private-session close occurred at 68.644/68.657 s.
- At 69.970 s, a subsequent native retry issued START on the base connection. That retry reached its own 20-second first-frame timeout, then a later base retry began emitting at 96.023 s.

The trial called the SDK reset directly, bypassing the bridge’s replacement-grace tracking. This startup/watchdog interaction is a limitation of the intervention and is not evidence for the initiating production cutoff.

The private attempt was therefore removed after roughly 6.29 seconds following START, before the SDK's 20-second warm-up deadline. Its outcome cannot establish whether private ownership would sustain playback. Neither A1 nor A2 encountered the previously observed full cutoff during its short window; both still had multi-second frame gaps.

All three other SDK video sources continued emitting through every arm. Their per-arm frame counts were respectively 879/929/899, 902/904/903, and 1498/1488/1452. Their owner connections were unchanged. These counters establish continued progress, not perfectly smooth rendering or sensor-to-screen latency.

## Earlier boundary and source audit

The prior first-cutoff observation located media absence before SDK assembly or keyframe gating: raw VIDEO stopped while the selected control peer still answered PING/PONG. The first source STOP followed that absence by approximately 6.07 seconds. That observation rules out that STOP as the initiating event; it does not identify the camera-side or protocol reason for the absence.

Source inspection found that dedicated media connections do not emit the public `p2pConnect` event. The bridge's station-wide repinning loop is consequently triggered by control-session reconnects, not every dedicated-media connect. View commands carry the correct device channel. The normal snapshot route returns stored images before the later live-snapshot branch, and motion prewarming only holds/opens the station connection. No initiating unrelated START/STOP was established from these paths.

## Cleanup and evidence

The recorded cleanup restored all six prototype hooks and the allocation method. Final ownership was the original base connection, live, wired, stream type 2, with one consumer. No private Doorbell connection remained. The inspector was closed. No production source file was changed or committed.

Private raw evidence: `/tmp/eufy-doorbell-runtime-20261023/capture-ownership-trial.json`, mode 0600, SHA-256 `faa056835d9fb6ca1b385606efd5b2aacc971c4ee849c3d0218f23a2a57d2664`. The payload and runner are retained beside it. Raw evidence contains device identifiers and remains outside the repository.

Decision: keep the native allocation policy. The initial cutoff remains unresolved. Any next intervention needs a demonstrated initiating mechanism rather than treating restart recovery or this incomplete private-session comparison as a fix.

## Live ACK association at a natural cutoff

After the allocation trial had restored the original source ownership, a passive
observer followed Doorbell's base connection (`station-1`, channel 0) without
issuing camera commands or adding media consumers. The window began with the
existing stream live and ended at the first raw VIDEO gap of at least six
seconds after 100 emitted frames. Its maximum duration was 180 seconds, with a
separately verified 190-second cleanup guard.

The observation ran for 120.300 seconds and captured a natural cutoff after
1,625 emitted frames. Last raw VIDEO arrived at elapsed 113,509.550 ms. The final
snapshot was 6,729.889 ms later, with no further target VIDEO packets.

| Measurement | Result |
| --- | ---: |
| Incoming target VIDEO datagrams | 21,390 |
| Eligible datagrams from the connected peer | 21,390 |
| ACK invocations | 21,390 |
| Actual socket sends | 21,390 |
| Successful send callbacks | 21,390 |
| Missing or duplicate ACK invocations | 0 |
| Missing actual sends | 0 |
| Sequence, socket or peer mismatches | 0 |
| Send throws, callback errors or callback byte-count mismatches | 0 |
| Pending callbacks at capture end | 0 |
| Maximum receive-to-ACK invocation time | 4.292 ms |
| Maximum socket-send-to-callback time | 40.612 ms |
| Retained mismatch/error anomalies | 0 |

Each receive occurrence was associated synchronously with its ACK invocation,
then with the actual socket send and its callback. The association checked the
video data type, sequence, receiving socket and source peer. Duplicate incoming
sequences would still require their own ACK occurrence. The observer retained
aggregate counters and a bounded anomaly ring, not per-packet files or media
payloads.

This excludes local SDK ACK omission, wrong-socket/wrong-peer ACK dispatch and
reported socket-send failure in this observed cutoff. Successful UDP callbacks
establish local completion; they do **not** establish physical transmission,
network delivery, peer receipt or the peer's sender-window state. No ACK policy
change is justified by this result.

The observer also retained 32 decoded `commandResult` events. All had numeric
code 0 and channel 0. The closest sampled results surrounding the raw cutoff
were at 111,856.086 ms and 115,107.357 ms. Their command IDs were not retained, so
they cannot be assigned to a particular request. The event list was bounded at
32 entries. This observation did not retain numeric status from command 1351
notifications and does not exclude a separate device-reported condition.

All five prototype hooks were restored, the observer global was removed, and
the loopback inspector was verified closed. Bridge PID 7116 and systemd restart
count 0 remained unchanged. The allocation and ACK behavior were not changed.

Private evidence:

- `/tmp/eufy-doorbell-runtime-20261023/capture-acks.json`, mode 0600,
  SHA-256 `7524bfb643b12570e09d710e5c93bc4691c62558ea4b386576d233ad9315e095`.
- `/tmp/eufy-doorbell-runtime-20261023/summary-acks.json`, mode 0600.
- The observer payload and runner are retained beside those files.

Decision: keep ACK behavior unchanged. The initial VIDEO cutoff is reproduced,
but its initiating mechanism remains unresolved.


## Peer-status observation overlapped the user HomeBase reboot

A passive 180.010-second capture beginning at Unix time 1792770474092
retained 111 bounded, sanitized numeric/boolean status records with no ring
overflow. The privacy parser was tested offline before installation: it drops
strings, arrays, identity fields and getters, and bounds field count and depth.
An explicit allowlist retains scalar battery/recording/status parameter entries.
No media payloads, camera queries, resets or policy changes were added.

This run **does not reproduce the isolated natural Door cutoff**. Door emitted
77 frames, with its last raw VIDEO at +6149.853 ms. Floodlight and Front last raw
VIDEO arrived at +7304.315 ms and +7321.059 ms respectively. Their final inbound
packets also stopped together: Door +7270.098 ms, Floodlight +7304.294 ms and
Front +7321.034 ms. No continuing HomeBase PING/PONG traffic supports an isolated
camera conclusion in this window. The user's HomeBase reboot and the journal's
shared feed/control disruption confound this transition.

Initial ownership remained Door on base session `station-1`, channel 0, and
Front on private session `station-45`, channel 2. Before the shared disruption,
the only decoded 1351 status was `cmd:6203,payload:{dstZoom:1}`, broadcast on all
three sessions at approximately +1.53 seconds. There were no 1302 frames in the
capture. Later recovery retained 1351 `cmd:6246,payload:{num:1}` for channels 2
and 1 around +56.13 and +59.87 seconds. No lifecycle meaning is asserted for
these numeric reports without a verified device-specific schema.

Two later numeric results were -108 on new base session `station-60`: channel 0
at +57366.439 ms and channel 1 at +62896.511 ms. A synchronous `handleFrame`
context proves both results were enclosed by command 1350 frames (132-byte
incoming bodies). It does not associate them with a particular outgoing request
or establish a decoded rejection reason. Both occurred after the shared outage;
neither proves the trigger for the earlier isolated natural Door failure.

The capture reached its duration limit below the requested 100-frame stop gate.
All 15 hooks were restored, observer globals removed, and inspector port 9229
was verified closed. Bridge PID 7116 and restart count 0 were unchanged. No
production change follows from this confounded run.

Private artifact: `/tmp/eufy-doorbell-runtime-20261023/capture-status.json`;
SHA-256 `92998ee1c9af0da257d0c6b552bf8274a33035f97791620b1ab51c0ce6b8aa45`.
The raw artifact stays outside the repository. The 1302 hook records existence
and pre-decryption body length only; it does not interpret an unknown body.


## Original baseline after HomeBase recovery and bridge restart

The bridge's configured continuous-failure watchdog exited the old process;
this subsequent observation verified PID 23538 and restart count 1. A passive
180.011-second original-code baseline began at Unix time 1792771132962, after
a healthy Door receipt gate (40.972 ms, stalls 6). No source reset or header
patch was installed. The bridge PID/restart count remained unchanged through
cleanup; all 15 hooks were restored and inspector port 9229 was closed.

Ownership had changed naturally: Door was private `station-21` / `#live:0`,
while Front initially used base `station-2`. Door delivered 2694 accepted,
assembled and emitted video frames with zero gated units. Its largest observed
1 Hz-sampled raw/output silence was 1.776/2.051 seconds. These are sampled
silence observations, not exact maximum interframe gaps. There was no six-second
Door cutoff, so the prepared START-slot candidate was not applied.

Front and Floodlight still failed on multiple ownership paths. Relative times
below use the capture start; matching-peer PONGs followed every listed cutoff:

| Active source | Frames emitted | Last raw VIDEO (s) | Subsequent PONG (s) |
| --- | ---: | ---: | ---: |
| Front base station-2, first source | 785 | 67.632 | 68.276, 73.277 |
| Front base station-2, next source | 487 | 132.912 | 133.310, 138.317 |
| Front private station-37 / #live:2 | 60 | 145.759 | 150.096 |
| Floodlight private station-34, channel 1 | 38 | 93.370 | 95.455 |
| Floodlight private station-35, channel 1 | 2 | 105.663 | 107.377 |

Front also attempted private station-33 and station-38 with no delivered video.
Several Floodlight private attempts likewise delivered no video. The base
Front cut at 67.632 seconds preceded its recovery START at 73.613 seconds and
STOP at 74.420 seconds; recovery commands therefore do not initiate that cut.
Floodlight channel-1 START retries on private station-32 occurred before that
Front cutoff, including two at 66.350/66.352 seconds; this is correlation only.

While Front was private station-37, base station-2 also received channel-2
video. Their final raw arrivals were only 1.883 ms apart. A numeric 1351 report
`cmd:6246,payload:{num:2}` for channel 2 reached Door-private, Front-private
and base at approximately 146.705 seconds, about 946 ms after the final Front
raw datagram. The base also emitted code 0 enclosed by a 1350 frame then.
These facts do not establish the meaning of `num`, duplicate source requests,
or a cause of the cutoff; the report follows the media loss.

The 512-entry numeric-status ring overflowed 588 entries; its earliest retained
status is at 92.683 seconds. Therefore decoded status before the first Front
cut is unavailable. Ordinary command/lifecycle/PING/PONG events and 1 Hz
counters remain available. Neither channel 0 nor base ownership alone explains
all cuts in this run, since nonzero-channel private Front/Floodlight streams
also stopped while their peer heartbeat continued. No candidate production
change follows from this baseline alone.

Private capture: `/tmp/eufy-doorbell-runtime-20261023/capture-original-baseline.json`.
SHA-256 `d3778a600e4768a9ed9a2558ce2da016e269d9181846fa4c756739003ab831f4`.
Derived ownership/cut timeline:
`/tmp/eufy-doorbell-runtime-20261023/summary-original-baseline.json`.


### Base/private overlap after the base Front source stopped

The recorded base channel-2 `stopLiveMedia` call was at +139462.971 ms;
`sendMediaPayloadLevel2(1004)` followed at +139463.031 ms. The last base
channel-2 START had been at +120002.295 ms. Base then started channel 1 at
+139466.181 ms, while the private channel-2 START was later at +140179.369 ms.
No further base channel-2 START is recorded before the overlap.

The new base channel-1 LiveStream (owner 17) nevertheless received 90 command
1300 headers for channel 2, accepting zero as channel-1 media. Its first
snapshot already counted 15 headers, with last-header time +140140.005 ms:
677 ms after base STOP and 39 ms before private START. The exact first header
was not individually retained; it lies after the channel-1 owner's creation at
+139465.914 ms and no later than +140140.005 ms. The last base channel-2 header
was +145736.808 ms. The old base Front owner 13 did not resume its counters.

Base used incoming peer ID 4 / socket ID 4 throughout. The private session's
winning peer/socket were ID 19 / ID 70. These are distinct endpoint identities
and distinct socket objects, so the observed overlap is not one shared local
socket dispatch. However, the capture records STOP method/send-envelope
invocation, not its UDP callback or peer acknowledgement. It cannot yet tell
whether the station retained the old request, processed STOP late, or normally
fanned media out to the control session. Focused private extraction:
`/tmp/eufy-doorbell-runtime-20261023/summary-base-private-overlap.json`.

### API-only recurrence watcher

A subsequent 1 Hz API-only watcher on PID 23538 ended after 156.180 seconds at
Unix time 1792771532559. Door receipt age reached 6.380 seconds and stalls rose
from 6 to 7. Front was already stale for 64.888 seconds (stalls 22), while
Floodlight receipt age was 2.661 seconds (stalls 39). Thus Door's earlier private
session baseline was not sustained proof of a fix. Front could not provide the
requested fresh comparator, so no isolated-cause classification is made. This
also does not prove a simultaneous all-HomeBase outage: Front had failed much
earlier. No hooks, resets or other intervention ran during this watcher.
Private aggregate trace:
`/tmp/eufy-doorbell-runtime-20261023/watch-original-baseline.jsonl`.


## Authenticated native app START/STOP shapes

Later offline analysis authenticated all 48 unique archived sign-8 command-1350
bodies with AES-GCM. The archived session key and cipher private key stayed
inside the analysis process; only sanitized fields were exported. This
supersedes the earlier inference from command size and following media: the
channel-0 command in packet 22326 is now authenticated as START1003.

| App packet | Camera channel | Request-ID location | ID | Header type |
| --- | ---: | --- | ---: | ---: |
| 3098 | 2 | `payload.msg_id` | 1 | 10 |
| 8504 | 1 | outer `msg_id` | 2 | 11 |
| 11598 | 1 | outer `msg_id` | 3 | 12 |
| 22326 | 0 | `payload.msg_id` | 5 | 14 |

All four observed STARTs pair header type with ID plus nine. This establishes
four request identities, not a universal allocation or wrap rule. The two
channel-1 requests are 17.026 seconds apart without an authenticated STOP1
between them; that alone does not prove replacement rather than an additional
or recovery request. Simple channel-0/channel-2 forms have nested numeric
timestamps and key strings of length 128; the complex channel-1 form has an
outer numeric timestamp and key length 256. The sanitized result contains no
public-key hash, so equal lengths cannot establish key reuse.

All five authenticated attached STOP1004s, channels 2/1/3/0/4, use
`payload: {channel: <addressed channel>}`, header type 0 and encrypted bodies of
124 bytes. The pinned SDK instead supplies an empty payload and uses nonzero
STOP header types for nonzero camera channels. Native channel-2 and channel-0
last observed VIDEO packet starts followed STOP by 8.540 and 13.194 ms; the
other three last VIDEO starts preceded STOP. These packet observations do not
establish physical display timing or persistent teardown.

The authenticated structures prove wire differences. They do **not** prove that
the missing fields caused the current interruption, or that correcting STOP
will clear a retained subscription. In particular, the original empty-payload
STOP affected the camera in A1, and subsequent corrected STOP exposures below
did not deliver durable recovery. Earlier size-only classification was also
misleading: 176 of 184 candidate packets were image/database commands 1308/1306,
not STOPs.

Private sanitized handoff: `/tmp/eufy-authenticated-app-start-stop-20261023.json`
and its companion Markdown. The original capture SHA-256 is
`5290131e60869fa2950045854afd520cf6ac328a1941995648ee91448305c344`.
No keys, account values, RSA moduli or capture payloads are included here.

## START-slot B trial invalidated by the configured process watchdog

One authorized B intervention used the existing Door LiveStream's
`nudge(true)` on PID 23538, without consumer failure, source cycling, STOP or
allocation changes. The healthy gate at Unix time 1792772027185 measured Door
receipt age 14.978 ms and stalls 11. The controller asserted exactly one
channel-0 START slot rewrite, unchanged ciphertext and unchanged source/session
identity. STOP retained its original wire form.

The last returned progress was at capture +29.997 seconds / candidate +13.821
seconds: 310 Door video frames, raw silence 2.039 ms, exactly one patched START,
zero hook aborts and zero source cycles. These are the only exported live
candidate measurements; the final in-memory observer result was lost.

At 16:14:46 UTC, the existing bridge watchdog exited PID 23538 because Floodlight
had failed continuously for 301 seconds. The inspector evaluation ended with
`Execution context was destroyed`. Systemd started PID 24781 at 16:14:52 UTC
(restart count 2); inspector port 9229 was verified closed. The old in-memory
hooks ended with the old process, rather than returning a restoration report.
The fresh process uses the original unmodified SDK; no candidate package was
installed or committed, and the temporary wire rewrite was not reapplied.

This trial never reached its 180-second comparison and is invalid for judging
the candidate's effect on natural media cutoffs. In particular, the watchdog
exit is neither a candidate success nor a demonstrated candidate failure.
Private reported-progress artifact:
`/tmp/eufy-doorbell-runtime-20261023/summary-slot-B-aborted.json`.

## Two independently verified bridge-wide restart defects

These fixes address measured service-wide interruptions. They do not establish
the initiating reason for the Doorbell's individual upstream VIDEO silence.

### One failed camera terminated healthy services

The original `streamTick()` called `exit(1)` as soon as any wanted camera's
continuous failure exceeded 300 seconds. Its camera-scoped retry path did not
protect siblings from this final process-wide escalation. The 16:14:46
Floodlight-triggered exit above is a real execution of that branch.

Commit `a745d2ef23be100e7692deb9d5956a7f1fcdce73` requires every demanded,
unblocked SDK camera to exhaust its own failure budget before global escalation.
Healthy, starting and recently failed siblings prevent global exit. Idle,
blocked and imported RTSP cameras are excluded. Existing playback, startup,
retry and global recovery durations are unchanged.

Four new regressions failed against the old implementation. The corrected
implementation passed 44 stream-manager tests and all 342 server tests,
including a demanded camera missing its initial slot, cached receipt replay,
iteration order and retained all-source failure recovery.
[CI passed](https://github.com/sapireli/eufy-p2p-rtsp-bridge/actions/runs/39986223833).

Deployment at 16:23:12 UTC produced PID 25377, restart count 0. The installed
stream-manager SHA-256 is
`d6ffbf016247156bb4f547d43995cf6fc0fa26e69ba005f869a6c8884e5c980d`.

The API/proc observation from 16:25:36.996 to 16:32:37.463 UTC collected 420
samples over 420.468 seconds. PID and restart count remained unchanged.
Front Door and Garage Doorbell retained zero stalls and advancing receipts,
with final receipt ages 217.9 and 13.0 ms. Floodlight stalls rose from 2 to 24,
but intermittent genuine recoveries limited its longest observed continuous
failure to 96.099 seconds. Consequently the live 300-second failure premise
was **not reproduced**. This observation proves deployed stability during
intermittent failures; the threshold branch is covered by the regressions,
not by this live window.

Private results: `summary-failure-isolation.json` and
`watch-failure-isolation.jsonl` beside the other capture artifacts. The JSONL
SHA-256 is
`7801075748635e13eec94ea4a397da621ebf890530cd9c02181c518edbb742d9`.

### Late codec discovery restarted every RTSP connection

At 16:24:45 UTC, Floodlight's first learned HEVC geometry changed the generated
native-route plan. The bridge logged `go2rtc egress changed`, terminated go2rtc
at 16:24:46, and restarted it at 16:24:49. All three TVs logged connection
refusals at 16:24:47–48 and then recovered. This occurred while bridge PID
25377 remained running, independently of the process-wide failure watchdog.

Commit `0d73ac47e860c3bb6a2406ec71404b17e218bd6e` provisions lazy native aliases
before codec discovery, compares the actual stable timed HTTP routes, and
canonicalizes camera enumeration for plan comparison. Actual legacy producer
changes retain reconciliation. Native capability is still advertised only
after HEVC and geometry are known. The aliases neither preload nor wake
cameras; quality, hardware selection and buffering remain unchanged.

The metadata and ordering regressions failed before the correction. Afterward,
20 go2rtc tests and all 345 server tests passed.
[CI passed](https://github.com/sapireli/eufy-p2p-rtsp-bridge/actions/runs/39987700724).
Deployment at 16:34:17 UTC produced PID 26051, restart count 0. The installed
go2rtc-module SHA-256 is
`fe8c62122857a02a4046f91a8e38f5fe2138d473a00f1f7585d64f1324b62314`.

A later API/proc observation ended after 52.076 seconds on another individual
Doorbell interruption: receipt age 6.204 seconds and stalls 5 to 6. Front Door
and Balcony remained fresh at 35.4 and 26.8 ms. Bridge PID 26051, go2rtc PID
26066 and configuration SHA-256
`ce6a7ec9772cce54948618ead5a210de5567202422d1139c456a80c6b449b95b`
remained unchanged. Balcony changed resolution three times without a child
restart. All codecs were known before this observation started, so it does
**not** prove the late-codec-discovery regression live, nor constitute a
600-second pass. It does confirm that the individual Doorbell interruption
remains after both service-wide restart corrections.

All three TVs rendered Front Door and Garage Doorbell through Amlogic hardware
decoders in the separate 16:36:03–16:36:35 UTC window. No RTSP connection refusal,
stream error or counter reset was observed in that window. Doorbell's initial
decoder warm-up is included; this is a bounded recovery check.

Private results: `summary-late-codec.json`, `watch-late-codec.jsonl`, and
`/tmp/eufy-tv-late-codec-recovery-20261023/`. The watcher JSONL SHA-256 is
`f076cd450b914e4faac73bdd8b90d9b9a4b33af99bb8d14b8cea97d7398fbfd1`.

### Deployment provenance

Both deployments matched the complete runtime source, entry point and package
files in the checkout using a checksum-only rsync comparison with no remaining
differences. The installed pinned SDK bundle remained
`dd649e91323a3b2b5159d68b99c369cfa1cbffc64cfa302392830e482ff4da47`;
no SDK candidate was permanently deployed. Settings revision 4 remained
byte-identical, SHA-256
`bde723a5149c4b882c6d40bdc766044ab7d37209146e26e6df6d2dc9d498b7ff`.
Backups are under `/opt/eufy-wall-bridge/backups/` in the respective
`source-isolation-20261023T162312Z` and
`rtsp-metadata-isolation-20261023T163417Z` directories.

## Natural-retry candidate gate: no intervention

After the isolated post-deployment Doorbell cutoff, a 60.127-second observer
waited for a source that was warming, had one consumer, had delivered no video,
and had already attempted at least two original starts. It would modify only a
subsequent natural START, without adding a nudge, STOP, reset or ownership
override. No source met that condition, so no trial helper was installed and
zero packets were patched.

The original SDK naturally replaced the Door source from base station-2 to
private station-24. That replacement succeeded on its first START and delivered
420 frames during the observation. All 15 observer hooks were restored, the
inspector closed, and PID 26051/restart count 0 remained unchanged. This is an
unmodified recovery, not candidate benefit or sustained proof of a fix.
Private results: `capture-slot-natural.json` and `summary-slot-natural.json`.

The next useful evidence must distinguish the initiating individual cutoff
from subsequent recovery traffic. Channel zero, base ownership and hardware
selection alone have not explained all measured episodes. The off-device SDK
slot candidate remains uncommitted and is not a production fix.


## Controlled original STOP on a duplicate base channel-0 stream

One adaptive test observed the base session receiving channel-0 media while the
actual Door source was private. The 5.005-second baseline counted 121 base
channel-0 headers, with no SDK base channel-0 owner. Door and Front were fresh
on private sessions station-59 and station-64. Base's channel-1 START retries
were already failing to retarget the old channel-0 delivery during baseline.

Exactly one controlled command was sent: the unchanged SDK
`base.stopLiveMedia(0, accountId, true)` at relative +15872.912 ms. Its normal
media builder was observed at +15873.102 ms, and the actual send boundary at
+15875.992 ms. The outgoing header channel/type were both 0; encrypted body
length was 141 bytes; the base socket and key object were unchanged. The wire
hash and scalar results were persisted immediately to the service journal.
No candidate B command, START, reset or local LiveStream stop was issued.

There were zero further base channel-0 headers in the next 3 seconds. The last
header preceded the control-window start by 25.471 ms. Private Door output also
stopped: its emitted count stayed 255, and media age reached 3.004 seconds.
Matching-peer PINGs continued on both base and private sessions. Background
base channel-1 STARTs continued, first 250 ms after the command. Front advanced
from 240 to 270 frames over this short window.

However, both base and private channel-0 delivery resumed without any recorded
base/private START0. By the +6-second continuation, the base had 75 additional
channel-0 headers and private Door had advanced to 331 frames (age 2 ms).
Front was fresh again (age 11 ms). Thus the short A1 pause is not proof of durable
teardown of the duplicate base subscription, and cannot establish whether
`payload.channel` is required for persistent teardown. No conclusion about
nonzero-channel STOP behavior follows from this channel-0 test.

All 16 observer hooks were restored, inspector closed, and PID 26051/restart
count 0 remained unchanged. No candidate was installed or reapplied. Private
capture: `/tmp/eufy-doorbell-runtime-20261023/capture-orphan0-stop.json`;
SHA-256 `af5bdd5ef8bf7ded390ed38f8970a2174c31524dcdfef75aee6221964b1507d4`.
The focused interpretation is `summary-orphan0-stop.json` in that directory.
Its immutable control-window scalar timestamps/counters are the timing evidence;
ordinary snapshot `headerTimes` objects were shallow references and must not be
used for historical timestamps. First resumption time is bounded by snapshots,
not individually recorded. The controlled command's relation to the temporary
shared pause is suggestive, but continuing background controls and the immediate
resumption prevent a durable STOP-semantics claim from this test alone.


## Temporary correction of naturally issued STOPs: no demonstrated Door benefit

A monitored, same-process temporary wrapper corrected only naturally issued
attached STOP1004 commands for the target HomeBase: it added the actual channel
to the payload and copied the outgoing frame to set STOP header type to 0.
STARTs and own-session commands were unchanged. No commands, source resets,
leases, quality/power changes or extra media consumers were injected.

The observation ran 121.860 seconds and recorded five corrected STOPs, all
channel 0 on base station-2, at +34.533, +54.668, +75.816, +95.875 and
+116.765 seconds. Each sent body was 152 bytes with payload.channel 0 and
header type 0; copied-header integrity, unchanged ciphertext and identical
key/socket objects were verified and logged immediately to the service journal.
No nonzero-channel STOP occurred, so the nonzero STOP-header correction was
not exercised live.

Door initially had a fresh 37.330 ms receipt (stalls 135). Its last accepted
header was +28181.090 ms, last emitted video +28182.531 ms and last raw VIDEO
+28196.173 ms. This initial natural cutoff preceded the first corrected STOP
by 6.351 seconds. At the final sample Door was stale for 93.204 seconds with
stalls 142. Front stayed fresh at 202.146 ms/stalls 8 and Floodlight at
35.468 ms/stalls 159. There was no observed binding clearance or Door recovery.
The temporary correction was restored as a negative result; it was not deployed
or committed as a Door-cutoff fix.

This capture also retained the previously missing peer-status boundary under
original command traffic, before any STOP was corrected. A base code-0 result
enclosed in command1350 arrived at +27097.960 ms. After the final Door video,
command1351/cmd6246/num1 arrived at +28679.664 ms, then num0 at +28718.140 ms;
these reports were broadcast to the other HomeBase sessions. They followed the
loss by about half a second, not preceded it. Their device-specific lifecycle
meaning remains unverified; the code-0 result is not request-associated by the
SDK. Matching-peer base PONGs continued at +28216.397 and +33215.851 ms.
No local HomeBase media START/STOP appears immediately before the cutoff;
the standalone Balcony session did close separately at +27614.276 ms.

All 16 hooks were restored; inspector port 9229 was verified closed. Node PID
26051/restart count 0 and go2rtc child 26066 remained unchanged. The final
result has no automatic endReason because cleanup was invoked explicitly on
the negative result. The raw capture remains private:
`/tmp/eufy-doorbell-runtime-20261023/capture-natural-stop-correction.json`;
SHA-256 `1ce1081bc4186a4169ec4a5a2377cb4ef4a76420927446ea5fe8db993b164ac2`.
Focused summary: `summary-natural-stop-correction.json` in that directory.


## Natural channel-0 START identity trial: negative late-retry result

A second same-node temporary observation changed only the paired request
identity for naturally issued HomeBase channel-0 START1003s: payload.msg_id 1
and header type 10, reusing the identity for retries of the same SharedLiveSource.
All other fields, START ownership, STOPs and lifecycle remained unchanged.
The initial source was already warming at warmAttempts 8 with no delivered
video. This was a late-retry recovery test, not a pristine first-source request.

Five actual STARTs were modified at +2.125, +3.125, +4.125, +6.126 and +6.129
seconds, on the same source/session station-2. Each was 537 encrypted bytes
and passed same-source/session/key/socket and copied-header/ciphertext-integrity
checks; sanitized identity/header/hash records were persisted immediately.
No target channel-0 1300 header or decoded video arrived, so peer adoption of
type 10 could not be established. The fail-fast negative condition ended the
trial after 8.086 seconds, restoring all 16 hooks. Inspector closed, Node PID
26051/restart count 0 and go2rtc child 26066 remained unchanged.

The base was not wholly quiet: it received 113 channel-1 headers, all rejected
by Door's channel-0 filter (accepted/emitted 0). This wrong-channel traffic was
present before modification: six channel-1 headers had arrived by +2.052 s,
while the first modified START was +2.125 s. Generic incoming command results
were code 0/channel 1; they are not associated with the channel-0 requests.
Door remained stale (123.337 to 130.376 s; stalls 163 to 164), while Front
remained fresh (25 ms at the last sample). No identity correction was retained,
committed or deployed, and no recovery benefit was demonstrated.

Private evidence: `/tmp/eufy-doorbell-runtime-20261023/capture-natural-start-identity.json`;
focused summary: `summary-natural-start-identity.json` in the same directory.


## Actual nonzero-channel STOP A1/B: no durable clearance or Door recovery

A later current-state check met the safe raw-subscription conditions: the base
was receiving channel 1 while its SDK owner wanted channel 0; Floodlight's
ordinary/native go2rtc consumer counts were zero and Front was fresh/private.
The 5.043-second baseline delivered 106 wrong channel-1 headers despite three
natural START0 retries, with no channel-0 output.

Exactly one original A1 STOP1 and one payload-channel/slot-corrected B STOP1 were sent on the
same base session/socket/key. A1 used header type 51, channel 1 and a 141-byte
encrypted body. It was followed by 48 additional channel-1 headers in 3 seconds,
first after 31 ms. B used payload.channel 1, header type 0 and a 152-byte body;
the one synchronous header copy preserved its ciphertext. Its instance send
wrapper was restored immediately after that command. Sanitized actual-wire
hashes and scalar window results were persisted to the service journal.
Other SDK fields were retained; the 152-byte body was not byte-equivalent to
the authenticated app's 124-byte STOP body.

B was followed by 102 channel-1 headers in 10 seconds, first at +1.192 seconds.
The maximum inter-header gap in that window was 2.368 seconds, followed by more
channel-1 output. No channel-0 header or Door decoded frame arrived in the
whole observation. Natural START0 retries continued (first 21 ms after B);
no attached START1 was recorded during B and continuation. Front stayed private
and advanced to 411 frames. Matching base PONGs continued. The result is no
observed durable wrong-binding removal or requested Door0 recovery, despite a
brief timing difference following B. It does not support deploying the STOP
change as a fix for this failure.

The observation ended after 26.131 seconds; all 16 hooks were restored and
inspector closed, with Node PID 26051/restart count 0 unchanged. No source reset,
extra START, nudge or media connection was injected and no SDK fix was committed
or deployed. Private raw capture:
`/tmp/eufy-doorbell-runtime-20261023/capture-orphan1-current.json`;
focused summary: `summary-orphan1-current.json` in that directory. The immutable
per-channel header timeline is the first/last-arrival evidence, rather than an
end-of-window frame counter alone.

## Fresh-source simple START profile: no prompt recovery

A later 23.234-second temporary observation reached a genuinely new Door
SharedLiveSource. The source was absent in preceding snapshots; its first
`live-start` and `startLiveMedia` were at +17736.287/+17736.527 ms, and its first
profiled wire START was at +17738.844 ms. Initial warmAttempts was 1 and no video
had been delivered. This separates the trial from the earlier late-retry
identity experiment.

Four naturally issued STARTs used the simple app-shaped outer fields
`cmd,payload,account_id` and payload fields
`msg_id,ClientOS,streamtype,camera_type,key,timestamp,entrytype`. Request ID 1,
numeric timestamp and header type 10 remained stable for that source. Stream
type stayed 2. The SDK's existing RSA key value was preserved: its string
length was 256, whereas the authenticated simple app examples have length 128.
No source, session, key or socket was replaced.

There were zero base VIDEO datagrams and zero command-1300 headers on any
channel, with accepted/emitted counts also zero. Consequently there was no
wrong-channel-1 traffic and no cleanup STOP1 was sent. The observer ended under
its short negative gate. Although the total observation was 23.234 seconds,
the first profiled request occurred late in that window: exposure after it was
approximately 5.5 seconds. A later generic code-0/channel-0 result was not
request-associated and does not prove acceptance of the profiled request.

This result found no prompt recovery under the tested profile. It is not a
broad disproof of the complete native profile: the RSA representation differed
and the post-request observation was short. It establishes no durable fix or
cause of the original spontaneous cutoff. All 16 hooks were restored, the
inspector closed, and Node PID 26051/restart count 0 and go2rtc child 26066
remained unchanged. No correction was retained or committed as a fix.

Private capture:
`/tmp/eufy-doorbell-runtime-20261023/capture-native-simple-start.json`, SHA-256
`6b34720063d2569be3f4879f2680b472a2bbcc3f185366ec9279afe0e9e0c409`.
