# Native UDP source-pause observation — 2026-10-12

## Purpose and limits

Live sources still pause intermittently. This bounded capture checks whether
VIDEO-data-type datagrams themselves stop arriving before SDK assembly, filtering,
and delivery. It creates no SDK session, attaches no media consumer, and changes
no stream request, camera quality, service, app, or SDK source.

Linux `AF_PACKET`/`SO_TIMESTAMPNS` timestamps are **kernel software packet-tap
timestamps**, not hardware NIC, camera capture, or physical display timestamps.
Arrival gaps at this boundary cannot be manufactured solely by a later SDK
keyframe gate, but do not distinguish camera scheduling, HomeBase scheduling,
upstream link behavior, or local kernel receive scheduling. Consecutive sequence
numbers across a gap do not prove the entire network path is lossless.

## Observer and overhead gate

`server/tools/diagnostics/udp-headers.c` uses a kernel classic-BPF IPv4/UDP peer
filter and at most 96 captured header bytes. The persisted fixed 40-byte records
contain only timestamps, ports, protocol lengths, sequence/type, and decoded
logical command/channel fields when a header is present. No packet payload or
IP/MAC address is persisted. `recvmmsg` drains up to 32 packets per syscall;
buffered binary writes avoid per-packet JSON/log allocations.

`--selftest` runs offline synthetic parser bounds, port/sequence/command/channel,
ACK count, fragment/non-UDP/non-peer negative tests and an interpreter of the
exact BPF program. Synthetic fixtures use documentation-only addresses. The
native compiler (`gcc -O2 -Wall -Wextra`) produced no warnings; tests passed before
each live gate.

| Observation | Process CPU / elapsed time | One-core process CPU | Records | Packet-socket drops |
| --- | --- | --- | --- | --- |
| Initial per-packet receive, 5 seconds | 295.121 / 5005.171 ms | **5.8963% — failed gate** | 4,890 | 0 |
| Batched receive, 5 seconds | 193.957 / 5004.226 ms | **3.8759% — passed** | 2,601 | 0 |
| Batched receive, 30 seconds | 1186.297 / 30006.477 ms | **3.9535%** | 17,163 | 0 |

No 30-second capture followed the failed initial gate. The batching change was
tested before a second five-second gate. Traffic rates differed between gates;
they are not a controlled equal-load benchmark. The 30-second run independently
remained below 5% process CPU, with zero missing timestamps, truncations, or
packet-socket drops. This measures observer process CPU, not all possible kernel
overhead; the observer remains a perturbation whose measured cost is disclosed.

## Actual 30-second result

Capture: **22:30:13.570–22:30:43.576 UTC**. Node PID 26350 and its existing UDP
ports remained unchanged; no observer process remained after completion.

The selected peer supplied traffic on local UDP ports 42362 and 51120. A third
Node UDP socket, 43529, existed but supplied no selected-peer records. Port numbers
are capture-local observations, not persistent camera identities. Logical
headers directly identified CMD_VIDEO_FRAME (1300) channel 1 on port 42362
(470 headers) and channel 2 on port 51120 (465 headers). This does not assume which
user-facing camera owns a channel.

| Local UDP port | Inbound VIDEO-data-type records | Longest arrival gap | UTC boundaries | Sequences | Sibling-port datagrams inside gap |
| --- | --- | --- | --- | --- | --- |
| 42362 | 4,239 | **929.652 ms** | 22:30:35.267784 → 22:30:36.197435 | 55388 → 55389 | 4 |
| 51120 | 4,299 | **608.952 ms** | 22:30:28.414282 → 22:30:29.023234 | 40667 → 40668 | 161 |

These gaps were already present at the kernel tap, with consecutive inbound
sequence numbers across their boundaries. Other-port traffic continued in both
intervals. VIDEO-data-type records can carry both video and audio commands;
these are transport-arrival gaps, not a decoded-picture cadence measurement.
No new SDK per-stage hook was used, so exact SDK receipt/assembly correlation and
camera mapping remain separate evidence requirements.

Every in-window inbound VIDEO packet on port 42362 had a matching outgoing ACK
tap; 4298/4299 matched on 51120, where a capture-edge packet can have its ACK
outside the window. Arrival-to-ACK tap median/p99/max were 7.805/50.969/69.096 ms
and 15.343/69.019/73.628 ms respectively. ACK tap timestamps show the local stack
produced a packet, not physical NIC transmission, peer receipt, or acceptance.

## Reproduction and retained artifacts

Compile and self-test the C observer on Linux. Invoke it with interface, peer,
bounded duration (1–45 seconds), and a private binary output path. Save its JSON
stderr as `capture-stats.json`, the output as `capture.bin`, then run
`python3 server/tools/diagnostics/analyze-udp-headers.py CAPTURE_DIRECTORY` offline.
Do not extend a capture whose five-second process CPU/drop gate fails.

Private retained artifacts:
`.evidence/frame-freshness-2026-10-12/native-nic-observer/`.

| Artifact | SHA-256 |
| --- | --- |
| `capture.bin` | `11ec3b8d825a1e371b8ce8814923d27a055eb3912c8312892daa0330077ce5fa` |
| `capture-stats.json` | `aeec893600988fd4c87538b820d8e16cef86366b8d7a1e91fdb7642ddc9217cf` |
| Captured native C source | `6e33c01122a9b9dce0656e8a3f645956579e8d9c7853c6a803e2dd21c9a95fe4` |

This is evidence of pre-SDK arrival pauses, **not a fix or a camera-side root-cause
claim**. No further capture was started after this bounded run.

The committed tool additionally fails its exit status on receive, file-write,
flush, or packet-statistics errors. This fail-closed diagnostic guard was added
after the capture and recompiled/self-tested offline; the exact historical C
source hash above refers to the source that produced the retained measurements.
