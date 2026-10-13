# Optional encoded-picture header metadata

`udp-headers IFACE PEER SECONDS OUT --video-header --trigger-port PORT`
accepts a maximum 300-second deadline (the historical trials below used 45 seconds). The first incoming VIDEO gap greater than
500 ms on the selected local port shortens the deadline to five seconds after
that resumed packet. A gap that never resumes cannot trigger; the original maximum
still stops capture. No transport commands, source settings, or buffers change.

Default records remain 40 bytes. `--video-header` emits formatVersion 2, 56-byte
records; the statistics identify recordBytes and formatVersion. Bytes 0–39 retain
the old offsets. Additional little-endian integer fields:

| Offset | Field |
| --- | --- |
| 40 | u32 declared coded chunk bytes |
| 44 | u32 original source timestamp word |
| 48 | u16 encoded-picture sequence |
| 50 / 52 | signed i16 coded width / height |
| 54 | u8 original video flags |
| 55 | u8 header candidate present |

Fields come from the actual SDK `parseVideoFrameHeader` layout: a 22-byte plaintext
media header follows the 16-byte logical header. Extraction requires incoming
VIDEO, DATA, logical magic, command 1300, sufficient snapshot bytes, and bounded
logical/coded lengths. Noncanonical flags and sign bytes are retained as integers;
no invented reserved-zero constraint applies. Encrypted or coded picture bytes
are never written. Body mutation invariance is an offline privacy gate.

The candidate bit alone is not a trusted picture boundary. Offline analysis orders
sequence numbers, removes retransmissions, tracks declared logical bytes, rejects
header coincidences inside a known continuation, and labels missing/reordered
coverage unknown. It distinguishes within a coded chunk, between complete chunks
sharing `(source word, picture sequence, width, height)`, and distinct picture
labels. A logical chunk's declared length is not complete picture length: one
picture can use multiple 64,000-byte logical chunks. Distinct labels do not prove
sensor exposure time. Interior packed headers absent from the snapshot, headers
split across datagrams, timestamp reuse/wrap, and missing sequence coverage limit
classification; unknown must remain unknown.

Offline gates:

- `python3 server/tools/diagnostics/test-udp-video-header.py` compiles the actual C
  parsing functions and verifies SDK-layout fields, noncanonical flags, body
  privacy, truncated headers, malformed length, continuation and wrong type.
- `python3 server/tools/diagnostics/udp-logical-boundaries.py --selftest` verifies
  same-picture chunks, distinct labels, continuation coincidences, retransmissions,
  missing sequences and future reorder dependencies.

Both offline gates passed. Linux compilation with `-O2 -Wall -Wextra -Werror` and
the built-in selftest also passed. One authorized 5.0156-second gate on the existing
bridge recorded 1,551 metadata records, 3.1909% of one CPU core, zero socket drops,
zero missing timestamps and zero truncations. Existing process IDs were unchanged.
The two active station-camera ports yielded 77 and 85 video-header candidates at
their existing coded geometries. Repeated candidate labels include retransmissions
and are not by themselves proof of distinct logical chunks. Sequence analysis
found no missing interval, removed 3 and 11 retransmissions, and retained one
unobserved interior-header interval as unknown. No qualifying >500-ms gap occurred.

Private gate source SHA-256:
`f8043e4f5c27377d9b82fb10049a10ce035133dcab5545b12ed9946b00481b81`.
Private metadata capture SHA-256:
`eb446c663ba9f33aec5481c2dbe15dc877bc5509a273a30adafed63394209a0c`.
The helper/socket and temporary remote files were removed. No source, service,
configuration or viewer changes were made. This short gate is an overhead/privacy
and real-header coverage check, not source-pause causal evidence. A longer bounded
capture requires separate coordination.

## One bounded event-triggered trial

The subsequent single authorized trial reached its 45.0146-second maximum without
triggering on a selected-port >500-ms resumed VIDEO gap. It used 3.2360% of one
CPU core and counted 16,310 socket packets, 16,309 persisted records, zero kernel
socket drops, zero missing timestamps, but **one truncation**. Its exit status was
7, so this is not a clean capture gate and cannot support an absence claim.
The root-owned parallel Mac observers added consumers to existing producers;
that additional consumer load is part of this trial's scope.

The private statistics artifact remains available. The orchestration checked the
nonzero exit before exporting the binary, then its finally block removed remote
files. Consequently the failed-window binary was not preserved and no coded-unit
or quiet-boundary analysis is claimed for this trial. Future orchestration must
export the metadata and statistics even when a gate fails, then report the failure.
No repeated capture was run. The initial five-second zero-error gate remains a
separate result; it does not erase this later truncation or prove pause behavior.
