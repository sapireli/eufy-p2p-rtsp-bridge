# UDP pause boundaries: complete logical units, not interrupted transfers

## Existing-data result; no new live capture

The native 30-second capture at **22:30:13.570–22:30:43.576 UTC** already retained
the fixed logical header fields needed for this question. It was analyzed
offline; no service, app, SDK session, camera setting, or subscription was changed,
and no additional observer/viewer was started.

All **seven** captured VIDEO-data-type quiet intervals longer than 500 ms began
**after a known complete logical unit**. None interrupted an already-started
declared-size CMD_VIDEO_FRAME (1300) transfer in this capture.

| Capture-local UDP port | Quiet interval | Previous logical unit | Bytes remaining at quiet start | Next explicit command |
| --- | --- | --- | --- | --- |
| 42362 | 534.169 ms | Audio 1301, channel 1, 235 bytes | 0 | Video 1300 |
| 42362 | 533.452 ms | Audio 1301, channel 1, 201 bytes | 0 | Video 1300 |
| 42362 | **771.953 ms** | Video 1300, channel 1, 1818 bytes | **0** | Video 1300 |
| 42362 | 894.050 ms | Audio 1301, channel 1, 208 bytes | 0 | Video 1300 |
| 42362 | **929.652 ms** | Audio 1301, channel 1, 203 bytes | **0** | Video 1300 |
| 42362 | 518.232 ms | Audio 1301, channel 1, 197 bytes | 0 | Video 1300 |
| 51120 | **608.952 ms** | Audio 1301, channel 2, 206 bytes | **0** | Video 1300 |

These ports and wire channels are directly captured metadata. No user-facing
camera mapping is assumed. Transport VIDEO type carries both 1300 video and
1301 audio, so a media-subchannel quiet interval must not be mislabeled as an
unfinished video transfer merely because its transport type is VIDEO.

## Reconstruction and uncertainty handling

`server/tools/diagnostics/udp-logical-boundaries.py` consumes only the fixed
metadata records. The logical header declares payload size, excluding its own
16 bytes; a new-header datagram contributes `UDP body − 8 − 16` payload bytes,
and continuation datagrams contribute `UDP body − 8` bytes.

The first copy of each transport sequence establishes the retained metadata.
The two ports had **50/56 duplicates**, which do not consume payload twice.
Sequence ordering is reconstructed within this bounded window (less than half
the 16-bit sequence space). There were **zero missing sequence intervals**.
If reconstruction would depend on a packet arriving after the examined quiet
boundary, the classification becomes unknown. If a datagram has more bytes than
the current unit needs, a subsequent unseen interior header may exist; that
state is also unknown until the next explicit header.

Each port had **one** such unobserved-interior-header interval, neither of which
established a selected quiet boundary. Every reported boundary above had a known
size exhausted exactly, with no future-arrival dependency. This is a metadata
reconstruction, not payload decryption or an SDK method hook.

Meaningful offline fixtures pass for an interrupted known-size video transfer,
a completed transfer with a duplicate, missing data, an unobserved interior
header, and an out-of-order dependency arriving after the boundary.

## What changes the next action

This capture does **not** support changing SDK assembly on the theory that these
pauses strand incomplete logical video transfers. The [native observer proof](native-udp-source-pause-2026-10-12.md)
places the quiet intervals before SDK processing, and this reconstruction places
their boundaries after complete logical units.

It still does **not** distinguish camera production, HomeBase forwarding,
peer send-window/ACK acceptance, or another upstream scheduling hold. A complete
logical unit is not necessarily a complete encoded access unit: the 22-byte
media header's total-picture length was not retained in this artifact, so
continuation of a picture across logical units cannot be excluded here. The
[older source-word capture](source-media-word-delivery-2026-10-12.md) establishes
continuous media labels and burst arrival, not sensor capture semantics.

The next causal experiment should vary the **actual SDK-owned HomeBase media
subscriptions** and compare sibling cadence, rather than merely closing a TV
view. No subscription experiment was run in this task. It requires an ownership
plan that preserves healthy sources and avoids the bridge's broad station-close
recovery path. Repeating a redundant source-word capture would not resolve that
causal boundary.

## Reproducible provenance

Run `python3 server/tools/diagnostics/udp-logical-boundaries.py --selftest`, then
pass the private capture directory to produce `logical-boundaries.json`.

| Artifact | SHA-256 |
| --- | --- |
| Existing `capture.bin` | `11ec3b8d825a1e371b8ce8814923d27a055eb3912c8312892daa0330077ce5fa` |
| Existing `capture-stats.json` | `aeec893600988fd4c87538b820d8e16cef86366b8d7a1e91fdb7642ddc9217cf` |
| Derived `logical-boundaries.json` | `f42b5416347f0c5d5c10edea6f053beb4cc08a9382f264d3c9ae2a49d807d529` |

Private artifacts remain under
`.evidence/frame-freshness-2026-10-12/native-nic-observer/`. Its original measured
process overhead was 3.9535% of one CPU core, with zero packet-tap drops, missing
timestamps, or truncations. Those are capture-level facts, not an assertion of
zero end-to-end network loss.
