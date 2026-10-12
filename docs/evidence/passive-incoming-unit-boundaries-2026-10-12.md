# Passive incoming logical-unit boundaries

## Purpose and scope

The earlier same-window NIC/RTSP capture showed continuous source-media words after physical VIDEO transport quiet intervals, followed by faster delivery. That identifies delayed delivery upstream of bridge ingress, without identifying the camera, HomeBase, or peer ACK acceptance as the cause. See [the paired capture evidence](source-media-word-delivery-2026-10-12.md).

The next discriminator is whether a physical quiet interval interrupts an already-started logical unit or occurs after a complete unit and before the next header. `server/scripts/capture-udp-metadata.py --incoming-logical-headers` adds that diagnostic bookkeeping. It does not change production sockets, ACKs, reordering, control commands, stream type, codec, or quality. The option is disabled by default. The first bounded live trial below records its actual coverage limitation.

## Interpretation

Incoming DATA must have the expected `f1 d0`/`d1` envelope, a supported data-type byte, and an exact declared wire length. At an inferred logical boundary, the fixed 16-byte header must have `XZYH` magic, a whitelisted command and a logical length no greater than 8 MiB. Other fixed bytes are recorded as integers rather than treated as validation rules not present in the SDK. The type byte is recorded as an integer; it is not a codec or picture-type assertion. Supported VIDEO transport commands are 1300 and 1301; the latter is audio. A logical unit is not necessarily a complete decoded picture.

The first valid full header at a packet start is **tentative**. Only a valid subsequent header at the calculated contiguous boundary establishes a **chain**. The tracker never scans arbitrary continuation payload for magic. It skips the exact remaining logical payload length, even when those bytes mimic an otherwise valid header. Initial fragmented headers are deliberately not reconstructed while the boundary is unknown.

For a chain with no unresolved sequence gap, `incomingLogical.boundary` is:

- `unit`: a known unit still has `remainingPayloadBytes`; command, channel, sign and type are integers.
- `header`: the last unit is complete and no bytes of the next header have arrived.
- `partial-header`: some bytes of the next fixed header have arrived; `partialHeaderBytes` is an integer.
- `unknown`: framing or ordering cannot safely establish the boundary.

To classify a quiet interval, use the last **ordered, chain-confirmed** boundary before it and the contiguous recovery metadata afterward. Duplicate/old packets do not advance accounting. Do not turn an `unknown` interval into a between-unit claim, and do not equate a repeated unit ID after endpoint reset with the earlier unit. The next capture must still report packet-tap drops, truncation, missing timestamps, process epoch, and peer/port identity.

## Reordering, bounds, and privacy

Sequence accounting is modulo 65536. Out-of-order arrivals retain only integer payload lengths and timestamps, never packet bodies. When a missing sequence arrives, previously buffered metadata can advance an already-known unit if its length fits wholly inside that unit. A reordered packet that crosses a boundary cannot supply the unseen header, so synchronization is discarded. Any unresolved pending sequence makes the reported boundary unknown. Gaps beyond 64 sequences or a full 64-entry pending map discard passive synchronization rather than grow memory. There are at most 64 endpoint/data-type states and 128 packed-unit iterations per packet.

Well-framed END and CAM_ID messages forget state for only their anonymous peer/local-port/peer-port endpoint. Malformed END does not reset it. This conservatively handles endpoint reuse; it is diagnostic lifecycle bookkeeping, not transport authentication or a session-generation guarantee.

The JSON contains only anonymous peer IDs, existing packet metadata, fixed logical-header integer fields, counters, and diagnostic labels. It contains no packet body, encrypted bytes, IP/MAC address, account identifier, key, or image. Temporary fragmented-header storage is restricted to the inferred fixed 16-byte header; no reordered body is cached. Raw datagrams exist transiently in the receive/parser call and are not written. An arbitrary ciphertext sequence can theoretically mimic valid headers at an initially unknown boundary; tentative/chain labels are framing evidence, not cryptographic authenticity or proof of sensor capture time.

## Verification

`node --test server/test/capture-udp-metadata.test.mjs server/test/capture-incoming-logical.test.mjs`: **7 tests passed**. The tests cover:

- Same-size opaque-body substitution leaves metadata/state identical; a header-shaped known continuation creates no new unit.
- Calculated fragmented headers, unknown initial fragments, partial-header classification, packed empty units, malformed fixed fields/envelopes, unsupported commands/data types, and oversized logical lengths.
- Sequence wrap, duplicates, metadata-only out-of-order continuation accounting, refusal to infer an unseen reordered header, bounded reorder reset and endpoint eviction.
- Valid endpoint END/CAM_ID reset, malformed END preservation, and opt-in/default isolation.

These synthetic parser/privacy regressions do not prove live header coverage or identify a source-pause cause. The later authorized live trial below is reported separately.

## First bounded live trial: coverage remained unknown

A single paired 60-second run was subsequently authorized. The kernel tap ran **20:36:17.185514–20:37:17.193175 UTC** on the unchanged live SDK 830fa1c/b23 process; the Mac attached to the two existing compatible RTSP producers. No new SDK subscription, Inspector hook, quality/stream-type change, transport tuning, or service restart was requested. Coordinated Pi-only native fixture work may overlap this window; this is not an isolated CPU benchmark. Both owned tap and RTSP observer processes exited after the bounded measurement.

The tap wrote **54,481 metadata rows**, with **zero drops, missing timestamps, or truncated packets**. VIDEO transport had 26,333 ordered packets and 769 duplicate/old packets. **Every logical boundary remained unknown**, with no tentative or chain-confirmed unit-start event. This establishes a diagnostic coverage failure, not evidence that the transport lacks logical headers. The raw payload was not retained, so this artifact cannot retroactively distinguish a packet-start alignment issue from an unsupported fixed-header field. Do not classify these pauses as within-unit or between-unit.

The two anonymous HomeBase media endpoints delivered 9,618 and 9,612 VIDEO datagrams respectively. Their consecutive-datagram physical quiet intervals greater than 150 ms numbered 35 and 49; their largest were **404.847 ms** and **325.810 ms**, both followed by a sequence advance of one. Camera mapping from this tap is unverified because no header channel was accepted. The standalone endpoint delivered 7,872 VIDEO datagrams and had no interval above 150 ms. Those are datagram counts, not picture counts.

The concurrently observed compatible streams delivered **913 Front Door** and **903 Garage** unique stamped frames, with zero missing/invalid stamps, abandoned access units, duplicates, or transport-invalid frames. Bridge-ingress-to-Mac-receipt age upper bounds were:

| Stream | Median | p95 | Maximum |
| --- | ---: | ---: | ---: |
| Front Door | 81.707 ms | 183.944 ms | 260.295 ms |
| Garage | 89.089 ms | 169.281 ms | 233.727 ms |

These ages begin at bridge SDK ingress, exclude pre-ingress delivery holds, and are neither sensor-capture age nor presentation age. There is no new source-pause root-cause conclusion from this short trial.

Private metadata provenance (not committed):

- `.evidence/bridge-network-audit-2026-10-12/incoming-logical-nic-60.jsonl`, SHA-256 `2df2533b53c6e621dd0d076ec95e63d09dcf59ba9f02b1d4fc39437459c8c417`.
- `.evidence/frame-freshness-2026-10-12/compat-mac-incoming-logical-60s.jsonl`, SHA-256 `129250f70fb853dbef54288beaf25f15b92613969621d828e1f24f86860b9992`.
- `.evidence/bridge-network-audit-2026-10-12/incoming-logical-60-analysis.json` contains exact anonymous endpoint/sequence boundaries and per-interval control-message counts.

Next instrumentation must establish fixed-header coverage before another pause-classification claim. It must still avoid scanning opaque continuation bodies, retain no raw payload, and leave production transport policy unchanged.

## Diagnostic alignment after the first coverage failure

The deployed SDK source was inspected without modifying runtime or recording
packet bodies. `codec.ts:parseDataFrameHeader` reads command, logical length,
channel, sign and type; it does not assert bytes10/11/15, restrict channel to64,
or whitelist sign bytes. `p2p-session.ts:reassemble` supports packed units and
split 16-byte headers. Its `handleFrame` leaves command1300/1301 media payloads
raw for the media decoder. In `video.ts`, encryption applies to the media payload
after the fixed XZYH header. Therefore whole-logical-header encryption is not an
explanation supported by these source paths.

The first diagnostic implementation invented magic/reserved/sign/channel
restrictions. That is a testably incorrect SDK-compatibility assumption. A
synthetic header with fixed bytes10..15 =255,127,100,255,253,42 was passed through
the **actual current SDK `parseDataFrameHeader`**, bundled directly from source
with esbuild. It returned command1300, logical length4, channel100, sign255 and
type253. The old diagnostic rejected it. The aligned diagnostic accepts it as a
tentative first header and establishes a chain on a second calculated header.
This is source/parser proof, not evidence that the device emitted those bytes.

The diagnostic now keeps exact envelope/magic, command/data-type whitelist,
8MiB bound and sequence/boundary rules, while recording bytes10/11 as integer
`headerFlags` and byte15 as integer `headerTail`. An unknown packet-start candidate
also reports only a rejection label: `no-packet-start-magic`,
`short-packet-start-header`, `unsupported-command`, or `oversized-logical-unit`.
Those labels do not disclose packet bytes. Known continuations are still skipped;
header-looking opaque body bytes cannot create a new unit or rejection label.

Seven tap tests pass, including the new SDK-accepted noncanonical-header case
and rejection-label/privacy cases. Initial mid-unit attachment and header
alignment remain limitations; this change does not scan unknown continuation
bodies or cache reordered payload. The first capture lacks raw bytes and these
rejection labels, so **we cannot claim the invented restrictions caused its zero
header coverage**. A further coordinated short trial must establish that fact
before any physical pause can be classified. No SDK production code, camera
setting, source subscription, transport policy or live service has been changed
for this diagnostic correction.

## Aligned 60-second live trial: pauses between completed units

The aligned tap ran **21:07:31.561287–21:08:31.565018 UTC** on the unchanged
SDK830/b23 process, without Inspector, camera changes, source restart, or
transport tuning. Its Mac two-compatible-stream observer started earlier;
source-word comparisons below use only actual overlapping receipt/stamp data.
The tap wrote **50,230 rows**, zero drops/missing timestamps/truncation, and
24,974 incoming VIDEO transport datagrams. Both owned processes completed;
the remote helper/output were removed after local export and matching SHA.

The actual fixed media headers now establish coverage. **6,117 unit starts**
were accepted: standalone channel0=2,431, HomeBase channel1=1,836, channel2=1,850.
Every media start had nonzero `headerTail` (38,47,94 respectively), and audio
starts used `headerFlags=2`. The old invented byte15==0 rule rejects every one;
its byte10 whitelist also rejects those audio headers. This proves a concrete
current-device mismatch in the diagnostic parser. It does not retroactively
restore the discarded raw bodies from the previous trial. The aligned VIDEO
rows contain no header-rejection labels; only9 initial rows remained unknown.

| Source (accepted channel) | VIDEO datagrams | Physical gaps >150ms | Known phase before gap | Largest gap |
| --- | ---: | ---: | --- | ---: |
| Garage (1) | 7,492 | 25 | 25 complete-unit/header boundaries | 786.833ms |
| Front Door (2) | 9,304 | 60 | 59 complete-unit/header boundaries;1 initial unknown | 703.095ms |
| Standalone (0) | 8,178 | 0 | No qualifying interval | — |

**No qualifying interval interrupted a chain-confirmed in-progress unit.**
All25 Garage intervals resumed at consecutive seq+1. Door had58 seq+1 and2
seq+2 intervals; those two have an unobserved sequence and cannot establish
loss-free sender silence. The initial unknown Door interval is not classified.
The strongest intervals below are known complete boundaries followed by seq+1.
This localizes the measured stalls before the next logical unit is supplied,
rather than waiting for the tail of an already-known unit. It does not identify
which camera/HomeBase/peer state held that next unit.

- Garage: **21:08:03.429323–21:08:04.216156 UTC**,786.833ms.
  The last datagram's matching ACK reached the outgoing interface tap2.927363ms
  later. In the preceding1s,348 unambiguous matched ACKs had median3.917688ms,
  p95=14.801400ms, max=16.779061ms.
- Door: **21:07:55.867331–21:07:56.570425 UTC**,703.095ms.
  Its last datagram's ACK followed0.755275ms later. The preceding1s had4
  unambiguous matches: median0.920106ms,p95/max=2.454075ms.

## Same-window physical ACK limits

Matching uses anonymous peer/local-port/peer-port/data-type/sequence and the
first outgoing ACK at or after each incoming packet, bounded to2s. Cases where
a repeated same-sequence packet arrives before that ACK are excluded as
ambiguous. There were243 such exclusions and one unmatched standalone packet
1.177ms before capture end. Sequence wrapping is keyed within this bounded
window; no cross-connection or later-window ACK attribution is used.

| Source | Unambiguous matches | Median RX→ACK | p95 | Maximum |
| --- | ---: | ---: | ---: | ---: |
| Garage | 7,397 | 5.881062ms | 41.509261ms | 70.006910ms |
| Door | 9,210 | 17.805412ms | 47.875511ms | 73.727348ms |
| Standalone | 8,123 | 1.412075ms | 16.107362ms | 79.908760ms |

The outgoing taps show the final ACKs left the host early in the two longest
intervals. They do **not** show peer receipt or acceptance. This excludes a
host-side ACK remaining unsent for the entire measured quiet interval; it does
not exclude firmware ACK processing, an on-wire loss, peer-window state, or
upstream media generation/forwarding.

Adjacent stamped RTSP pictures around recovery retain source words advancing
68ms for Garage and63ms for Door, with exact matching RTP advances. Their SDK
ingress gaps are804.021ms and1,085.189ms respectively. These are media-timeline
labels, not sensor capture time; the larger Door ingress gap also spans time
outside the single selected NIC quiet interval. Do not attribute its entire
ingress gap to that one physical interval.

The Mac delivered931 Door/903 Garage unique valid stamped frames, no missing
or transport-invalid stamps. Bridge-ingress→receipt upper ages: Door median
141.788ms,p95=302.156ms,max=380.567ms; Garage87.310/158.576/217.441ms.
These do not include upstream delay or measure screen presentation.

**Observer-cost limitation:** the tap used18.477290s user+2.609185s system over
60.164260s (~35.05% of one CPU core), maxRSS16,680KiB. This is a material
scheduling confound for cadence and ACK latency; it was not a zero-cost baseline
and does not prove the measured pause frequency absent instrumentation. CPU
accounting covers the tap only, not bridge/encoder CPU. No further tap was run.
The output already uses default block buffering without per-packet flush;
a larger write buffer has not been measured as an optimization.

Private provenance:

- `incoming-aligned-nic-60.jsonl`, SHA-256
  `2c65ae15d51f3c8916944612f2509fd3b869ab471d8148a40502e4f44de47ff3`.
- `compat-mac-incoming-aligned-60s.jsonl`, SHA-256
  `6fba2617d5459ebbf670b98d45036997b15bae9bdc8496ab04952da574370d80`.
- `incoming-aligned-60-analysis.json` and `incoming-aligned-60-runtime.txt`
  retain exact boundaries, fixed integer fields, exclusion counts, and resource
  accounting under the private evidence directories.

## Same-endpoint liveness during the longest intervals (offline audit)

No additional capture was started. The same metadata was filtered by exact
anonymous peer/local-port/peer-port identity inside each longest quiet interval.

During Door's703.095ms interval, the same endpoint sent a zero-body PING at
**21:07:55.906209008 UTC** and received a zero-body PONG at
**21:07:55.907176633 UTC**: outgoing→incoming kernel-tap delta **0.967625ms**.
That PONG was the only incoming same-endpoint packet in the interval: no ACK,
CONTROL/BINARY DATA, or alternate-data-type audio was present. VIDEO transport
itself includes command1300 and1301, so both its video and audio units were quiet.
The other HomeBase endpoint continued143 VIDEO datagrams, including8 command1300
and8 command1301 starts on channel1; first/last arrivals were21:07:55.873332897
and21:07:56.560647196 UTC. Thus the Door interval is media-channel silence while
that endpoint and the same HomeBase's other media path demonstrably respond.
It is not global IP-peer silence.

During Garage's786.833ms interval, **no incoming packet of any observed protocol
type arrived on that same endpoint**. Five final outgoing VIDEO ACKs departed;
no scheduled PING occurred inside the interval. Therefore this particular
endpoint cannot be called heartbeat-alive from this interval alone. The other
HomeBase endpoint continued269 VIDEO datagrams, including16 command1300 and16
command1301 starts on channel2, first/last21:08:03.439236321 and
21:08:04.074785359 UTC. The HomeBase was not globally silent, but its per-camera
endpoint liveness during this subsecond interval remains unprobed.

The unit-start counts above include logical media chunks and audio; they are
not decoded-picture counts. PING/PONG lacks a request identity in this metadata;
this bounded interval contains exactly one matching endpoint pair, not a general
cross-window RTT attribution. These findings do not identify camera RF quality,
peer ACK acceptance, sensor capture age, or the firmware queue responsible.
The same35.05%-core observer-cost limitation applies.

A possible next causal experiment is a coordinated short A/B/A comparison of
**actual HomeBase media subscriptions2→1→2**, preserving streamType2 and quality.
Merely removing a UI/RTSP consumer does not remove the shared SDK source and
would not test this hypothesis. That experiment would deliberately pause one
feed and must restore exact source/view policies, guard startup transients,
and use validated low-cost timing or the existing Mac stamped-frame observer.
A repeatable change in the other camera's pre-ingress pause distribution could
implicate concurrent serving, without establishing a specific firmware cause.
No such intervention was performed, and no SDK ACK/policy change is justified
by this offline analysis.
