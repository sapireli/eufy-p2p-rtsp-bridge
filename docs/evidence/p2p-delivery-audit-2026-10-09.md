# P2P delivery and acknowledgement audit — October 9, 2026

This is a read-only audit of the deployed SDK and private packet captures. It
introduces no scheduling buffer, request change, SDK change, or added playback
latency. The user withdrew the apparent ten-second camera offset as a camera
clock comparison; the remaining target is smoother playback.

## What the SDK does

The deployed `P2PSession.onData()` sends a single-sequence acknowledgement
immediately, before sequence reordering, frame reassembly, or event delivery.
`send()` calls the selected socket's `send()` directly; no acknowledgement
batching timer or intentional acknowledgement wait exists. Promotion of a
winning cloud probe replaces the session's socket, so subsequent acknowledgements
use that promoted socket.

The initial UDP socket requests a 4 MiB receive buffer. Cloud punch probe sockets
do not make that request, and promotion does not request it either. This is a
separate configuration omission. The inspected production process had three
sockets, each with `rb425984` and zero socket drops at the inspection instant.
The host receive-buffer ceiling was 212,992 bytes, which Linux doubles in the
reported socket value. These observations do not implicate a winning unconfigured
probe in this runtime, or prove that host buffering never contributes to a pause.

## All received datagrams acknowledged before observed pauses end

The private, zero-capture-drop file
`.evidence/firetv-latency-2026-10-09/passive-p2p-bpf-buffered.jsonl`
contains kernel-timestamped incoming DATA and outgoing ACK headers. The replay
sorts records by kernel time and maintains a set per production UDP port, keyed
by data type and sequence number:

1. Incoming DATA adds a sequence if it is not already pending.
2. Each outgoing ACK removes every sequence listed in that ACK.
3. Immediately before the next incoming video datagram ends a measured pause,
   the replay checks the pending set across **all data types**, including older
   datagrams and retransmissions.

| Stream | Incoming video datagram pause | Previously received datagrams still awaiting an outgoing ACK at pause end |
|---|---:|---:|
| Front Door | 890.7 ms | 0 |
| Front Door | 400.4 ms | 0 |
| Front Door | 222.0 ms | 0 |
| Garage | 439.8 ms | 0 |
| Garage | 403.4 ms | 0 |
| Garage | 369.2 ms | 0 |

This is stronger than checking only the last packet's ACK: none of the earlier
received packets remained unacknowledged locally at those boundaries. It does
**not** establish that the HomeBase received those ACKs. A packet dropped between
the observation point and the sender, or a datagram never observed at the bridge,
is outside this proof.

Earlier inspector observations also show long access-unit gaps spanning
consecutive incoming transport sequences. One 1,719.5 ms Front Door access-unit
gap contains only three video datagrams, at approximately +26, +1,696, and
+1,719 ms. One 1,247.2 ms Garage gap contains only three, at +37, +51, and
+1,247 ms. The pauses therefore precede SDK reassembly for those examples;
the video reorder wait and access-unit assembler do not create those particular
incoming silences.

## Request comparison and its limits

The production bridge explicitly passes `streamType: 2` to `openReadable()`.
This matches the selector in the archived authenticated phone request described
in [the stream-selector handoff](../sdk-streamtype-pr-handoff.md). Composition
`video_type` is a separate setting; the selector is not a universal view-mode
label.

The current attached-camera start helper differs from the archived app request
in other fields: it adds inner `accountId` and outer `mChannel`/`mValue3`, omits
inner `msg_id`, identifies `ClientOS` as Android, and computes the header stream
slot from the level-2 sequence. These are observed structural differences, not
evidence that any of them causes the present delivery cadence. No production
request field was changed during this audit.

The archived phone capture's SHA-256 is
`5290131e60869fa2950045854afd520cf6ac328a1941995648ee91448305c344`.
The contemporaneous capture analyst found irregular phone ingress too: during
its active 10.75-second window, source-header arrival median was 41.6 ms, p95
82.3 ms, and maximum 540.5 ms; source-clock steps had median 67 ms and maximum
76 ms. This historical comparison has different scenes, network conditions,
and session length. It does not prove equal displayed smoothness or justify a
request-field repair.

## Conclusions bounded by the captures

- The SDK has no intentional ACK delay, and the audited pauses do not leave
  older received datagrams waiting for a locally transmitted ACK.
- Some long pauses exist before SDK reassembly. Sender-side scheduling, the
  camera-to-HomeBase path, and network delivery remain possible sources.
- The ordinary bridge's single-camera A/B did not eliminate irregular delivery;
  see [the full investigation](firetv-latency-investigation-2026-10-09.md).
- Historical phone ingress also arrives irregularly. Smooth phone display alone
  does not prove uniform delivery or a bad SDK request.
- Original camera timestamps are parsed but discarded by the deployed SDK's
  `unitOf()` and video event. A separate metadata-only candidate remains held
  outside the deployed tree; it adds no waiting and is not itself a delivery fix.

Do not interpret the last point as authorization to add a playout buffer. The
current user requirement is smoother video without deliberately increasing
latency.
