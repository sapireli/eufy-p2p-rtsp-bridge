# Passive incoming logical-unit boundaries

## Purpose and scope

The earlier same-window NIC/RTSP capture showed continuous source-media words after physical VIDEO transport quiet intervals, followed by faster delivery. That identifies delayed delivery upstream of bridge ingress, without identifying the camera, HomeBase, or peer ACK acceptance as the cause. See [the paired capture evidence](source-media-word-delivery-2026-10-12.md).

The next discriminator is whether a physical quiet interval interrupts an already-started logical unit or occurs after a complete unit and before the next header. `server/scripts/capture-udp-metadata.py --incoming-logical-headers` adds that diagnostic bookkeeping. It does not change production sockets, ACKs, reordering, control commands, stream type, codec, or quality. The option is disabled by default. It has not yet been deployed or used for a live capture.

## Interpretation

Incoming DATA must have the expected `f1 d0`/`d1` envelope, a supported data-type byte, and an exact declared wire length. At an inferred logical boundary, the fixed 16-byte header must have `XZYH` magic, a whitelisted command, supported magic/sign fields, bounded channel, reserved zero bytes, and a logical length no greater than 8 MiB. The type byte is recorded as an integer; it is not a codec or picture-type assertion. Supported VIDEO transport commands are 1300 and 1301; the latter is audio. A logical unit is not necessarily a complete decoded picture.

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

`node --test server/test/capture-udp-metadata.test.mjs server/test/capture-incoming-logical.test.mjs`: **6 tests passed**. The tests cover:

- Same-size opaque-body substitution leaves metadata/state identical; a header-shaped known continuation creates no new unit.
- Calculated fragmented headers, unknown initial fragments, partial-header classification, packed empty units, malformed fixed fields/envelopes, unsupported commands/data types, and oversized logical lengths.
- Sequence wrap, duplicates, metadata-only out-of-order continuation accounting, refusal to infer an unseen reordered header, bounded reorder reset and endpoint eviction.
- Valid endpoint END/CAM_ID reset, malformed END preservation, and opt-in/default isolation.

These are synthetic parser/privacy regressions. They do not yet prove live header coverage, classify an actual source pause, or identify a cause. No new live capture was started during the coordinated Pi pacing measurements.
