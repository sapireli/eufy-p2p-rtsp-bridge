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
