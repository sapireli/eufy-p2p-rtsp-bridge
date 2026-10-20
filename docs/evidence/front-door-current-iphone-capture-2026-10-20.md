# Current iPhone Front Door capture, October 20

## Capture integrity and provenance

The operator recorded the first-party iPhone app through the Mac's remote virtual
interface, then stopped capture. The operator did not watch playback closely,
so this recording supplies packet evidence, not a report of visible smoothness.

`Downloads/eufy-iphone-fresh.pcap` is 35,603,180 bytes, SHA-256
`bbb2b16c18424d105a5c1aa44a700deb3b5b458c1d5f14f7de23fb379f0419e3`.
A classic-PCAP 24-byte global header precedes an intact PCAPNG section. Standard
readers mistake its section header for a classic packet and reject the file.
The original is untouched. A working copy excludes exactly those first 24 bytes:
35,603,156 bytes, SHA-256
`6afc490fa6b68d9819e5b9f414c68da621580818df8383a2ec422cd159c5990d`.

An independent strict scan validates all 61,011 blocks, their alignment,
length/trailer pairs and exact EOF: 1 section header, 6 interface descriptions,
25 Apple custom blocks and 60,979 enhanced packet blocks. `capinfos` and `tshark`
read the recovered copy successfully. No packet-stream corruption or recapture
requirement follows from the extra wrapper. Which program introduced the mixed
container is unproven.

Capture bounds are 19:18:58.311301–19:21:53.647985 UTC, 175.336684 seconds.
The bridge was not stopped for this capture. It therefore does not isolate
first-party behavior from concurrent SDK activity on the same system.

## Incoming media

There is one VIDEO flow, carrying channel-2 1600×2200 picture headers:
27,187 VIDEO datagrams over 160.547502 seconds. A metadata census finds 2789
explicit command-1300 headers and 2381 distinct picture-header identities after
deduplicating timestamp, video sequence, geometry and channel. Header starts are
not completed decoded pictures or evidence of actual display timing.

| Header-arrival interval threshold | Count |
| --- | ---: |
| Greater than 500 ms | 34 |
| Greater than 750 ms | 10 |
| Greater than 1000 ms | 2 |

The largest distinct-header arrival gap is 1129.924 ms while source timestamp
advances 71 ms. Another is 1077.507 ms with a 71 ms source advance. The longest
raw VIDEO datagram gap is 1038.132 ms between consecutive transport sequences.
These establish uneven current first-party packet arrival. They do not prove
what the app displayed, a camera capture clock, or why the sender/path paused.

## Outgoing commands and transport

Times in this section are offsets from the first packet on the target app flow.
The target app flow contains 36 unique complete outbound control frames with
contiguous sequences 0–35. A start-shaped outer 1350/channel-2/type-10 frame is
observed at +1.841 seconds; a stop-shaped outer 1350 frame at +162.733 seconds.
Their bodies remain encrypted. Inner command IDs, stream type, account/power
fields and exact semantic equality to SDK requests are not newly verified.

After startup queries there are no unique control commands between +3.981 and
+162.733 seconds. Command 1139 appears once before live view, not periodically.
The app sends three initial empty PINGs and no periodic PING during approximately
160 seconds of live video. The SDK sends a heartbeat every five seconds. This is
an observed protocol difference, not a demonstrated defect or a justified fix:
the app's media flow also contains long arrival gaps.

Video ACKs contain 27,187 sequence entries in 6243 packets, averaging 4.355 entries
and at most 17 per ACK. Audio ACKs average 2.957 and control ACKs 1.064 entries.
The earlier reversible ACK-batching trial already tested that mechanism without
a repeatable playback improvement; this capture does not justify repeating it.

## Current capture replay through SDK reassembly

The same isolated actual-`P2PSession.onData` replay used for the historical archive
was run once with this current capture's incoming VIDEO flow and recorded receipt
times. It processed 27,187 datagrams over 160.547502 seconds and emitted 2387 raw
command-1300 events. No datagram remained held after any arrival, no `videoGap`
event occurred, and no event was emitted from a reorder timer or after the replay.
Each raw video event completed synchronously on its completing input datagram,
with zero added waiting at one-millisecond virtual timing resolution. The largest
raw-event interval was 1130 ms.

Thus this current first-party input retains its roughly 1.13-second logical-frame
pause through SDK reassembly without an SDK reorder extension. Ordinary fragment
completion contributes to the difference between first-header, raw-datagram and
complete-raw-event intervals. Raw logical units are not playable pictures; the
2387 and 2381 counts must not be interpreted as a loss count. Bootstrap begins
mid-session. ACK feedback, media decryption, access-unit assembly, live gating
and wall-clock CPU scheduling are outside this replay's scope.

## Decision

Do not change player, encoder or heartbeat policy on this evidence alone.
The useful next comparison is first-party live view with the bridge stopped,
under unchanged camera quality/view settings. That separates app-only behavior
from possible concurrent bridge session effects. A short comparison must be
interpreted with the natural variability documented in the earlier trials.

Private packet data remains outside the committed tree. Sanitized census files
are retained in ignored `.evidence/front-door-pause-2026-10-20/fresh-phone/`.

| Census | SHA-256 |
| --- | --- |
| `eufy-iphone-fresh-media-analysis.json` | `7a34c5a42f8a1d41d1f52246749d4be15cdf2bc3e6add3fe25daed71fb7af9dd` |
| `fresh-phone-control-census.json` | `c138a560011c59387220ae2b5f7f78773b04e97bc887d18b3204cd2b3aae33c7` |
| `front-fresh-offline-replay-result.json` | `ce748e1809a45d5b504623b49e3070ac681bd27efac2d7de1013adc6c8364302` |
