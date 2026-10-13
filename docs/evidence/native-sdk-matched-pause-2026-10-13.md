# Matched physical media pause, SDK receipt and TV cadence

## One bounded run and preservation

One authorized capture ran on the bridge's eth0 HomeBase peer, 2026-10-13
**02:41:24.887138–02:46:24.896028 UTC** (native 300.008890 seconds). Existing
loopback API counters ran in the same window, 02:41:24.860769–02:46:24.870625.
No Node/SDK restart, SDK hook, media consumer, source setting, network change or
Pi action was performed. Native collector source was commit `6214a14`; built
binary SHA-256 `052d7ec6695686adc6f205a7cd4f8378abdf73d571087e978de22afd89658e97`.

All five native artifacts and both API artifacts were exported to the Mac and
matched remote SHA-256 values **before interpreting validity**. Private directory:
`.evidence/frame-freshness-2026-10-12/native-sdk-300-20261013T024124Z/`.
`verified-export.json` preserves the complete per-file hash/size inventory.

| Artifact | SHA-256 |
| --- | --- |
| native/capture.metadata | `c078630e562e6c05cd04465ec201764375504530214967d95d149df174feaf0e` |
| native/capture.stats | `be24602704892f95fd75d2ec66a6b79d744953f6f1096bdb415ad48bbb1ee12c` |
| native/manifest.json | `9932d9a1f5b6626431c633f8817eea05c43c12e7c4096613e2d5d94588d243d8` |
| api-counters.jsonl | `246b1c85499f931e802d272054260c5d33d32414c82d4168442dc91afdefed08` |
| api-manifest.json | `85536a842f9fe721f54696de7e532a4db59f51c27da44986b9ced1116206ee9d` |

The remaining preserved native stdout is empty and the exit file contains zero.
Native integrity passed: 126279 fixed 56-byte records, zero packet-socket drops,
missing timestamps, data/control truncations or I/O errors. Native observer CPU
was **3.4838% of one core**, below the 5% gate. API controller CPU was approximately
0.969%; combined process accounting approximately 4.45%. This does not establish
zero observer effect. API captured 300 samples, no errors, max request 124.888 ms;
its clock epoch remained `c88dfdc72b434ad0`. Pi LAN-clock timeouts in the same
period therefore were not observed by this bridge-loopback API reader.

## Natural Garage gap matched across boundaries

Garage's channel-1 flow on local UDP port 34505 had **2601.518676 ms** with no
incoming parsed DATA-type-1 media datagram: **02:42:59.170621234 →
02:43:01.772139910 UTC**. This type includes audio as well as video; both were
quiet on that flow. Front Door's channel-2 flow on a different local port still
received media, including a shorter 505.515-ms interval. This run was not a
simultaneous total station outage.

Existing API matched Garage SDK receipt sequence 42157/frameId 75161 unchanged
at 02:42:59.861 and 02:43:00.861. Last-video age rose 718.083 → 1706.687 ms;
consumer input and encoded-HTTP output both stayed 20584. At 02:43:01.862 SDK
receipt advanced; encoding followed. Source identity remained unchanged. The TV
collector reported a new shared Garage callback maximum of 2683 ms, first sampled
on the two devices at 02:43:03.964/02:43:06.918 device wall time. Those wall times
are notification bounds with clock uncertainty, not exact pause boundaries.
No TV read timeout/restart occurred in this window.

Kernel timestamps are software packet-tap ingress timestamps, not NIC hardware
completion or real camera capture times. Zero capture drops makes a capture-thread
pause insufficient to explain this kernel-timestamp gap. The pause already exists
before local SDK frame processing, encoding or player presentation; downstream
queued frames alone cannot create it. SDK control/ACK feedback could still affect
a sender, so this does not exonerate every possible SDK policy.

## Logical coverage and ACK evidence

The existing logical classifier was used only on a 13-second crop around this
pause, avoiding its 16-bit half-range limitation over a long capture. It reported
zero missing sequence intervals and zero unobserved interior-header intervals.
The boundary follows a complete channel-1 command-1300 unit: declared 69382 bytes,
coded chunk 69231 bytes, source word 858012167, picture label 64611, keyframe flag
bit set. First resumed datagram was audio command 1301. Consequently the picture
boundary label is **unknown**; a logical unit/header label is not proof of a whole
coded AU or a physical captured picture.

At the quiet edge, sequence 9681 had arrived before the final repeated 9680, so
the apparent 9680→9682 jump is not evidence of a missing datagram. Actual matching
physical outgoing ACK metadata on the same local/peer ports and DATA type:

| Sequence | RX UTC | ACK TX UTC | RX→TX |
| --- | --- | --- | ---: |
| 9680 original | 02:42:59.140067 | 02:42:59.168669 | 28.602 ms |
| 9681 | 02:42:59.142035 | 02:42:59.169358 | 27.323 ms |
| 9680 repeat | 02:42:59.170621 | 02:42:59.172109 | 1.488 ms |
| 9682 after pause | 02:43:01.772140 | 02:43:01.773264 | 1.124 ms |

There is no 2.6-second local ACK delay at this boundary. Outgoing packet-tap
observation does not prove peer receipt or exclude an ACK lost after the tap.

## Decision and code boundary

This matched cadence gap is an actual incoming-media pause, not locally retained
frames alone. It does **not** retrospectively attribute the earlier five-second
paired socket timeout. The previous separate 300-second SDK observer excluded
keyframe gating for its shorter observed pause; this new API-only run has no
assembler/gate counters and cannot repeat that exclusion for every event.

No speculative SDK gate/ACK patch is justified by these records. In the locked
SDK, attached streams stop periodic media-start reassertion once their own media
flows; silence reassertion defaults to 6000 ms, longer than this 2602-ms pause.
Existing startup routing assigns the first attached source to the station base
session and subsequent attached live sources to dedicated media sessions.
Garage's base versus Door's dedicated role is a concrete policy dimension for a
controlled comparison, but is not proven causal here.

Keep `streamType: 2`: historical first-party attached request evidence selected 2;
changing only 2→1 on Front Door T8214 caused motion-time Split→PiP. Garage has no
corresponding first-party selector-1 or latency-benefit evidence. A selector-1 trial
would be an experiment with composition risk, not an app-matching correction.
If another intervention is authorized, assigning Garage a dedicated session by
opening Front Door first is a narrower existing-route A/B that preserves selector,
quality and hardware policy. Its actual route must be verified and any benefit
measured; neither this proposal nor a new behavior change was executed here.
