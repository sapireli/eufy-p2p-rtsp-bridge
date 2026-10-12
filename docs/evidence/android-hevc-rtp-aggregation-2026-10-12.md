# Android HEVC aggregation and access-unit delivery — 2026-10-12

## Observed root cause

The pinned rtsp-client-android5.6.3 implementation did not implement RFC7798
aggregation packets (payload type48). A real five-second native RTSP observation
contained120 AP packets in126 marked pictures (~25fps). The old parser discarded
those payloads. Supplying proper codec configuration allowed isolated HEVC output,
but could not restore the dropped pictures; the earlier experiment remained bursty.

The fix parses the AP's two-byte header and each network-order uint16 NAL length,
validates the entire payload before emission, and emits each original NAL with its
own AnnexB start code. Nested packetization, invalid NAL headers and overruns are
rejected. Malformed packets poison that timestamp's complete access unit.

HEVC dispatch is separate from the AVC NAL switch: HEVC VCL types6/7/8 overlap
AVC SEI/SPS/PPS values. Prefixes and slices sharing one RTP timestamp are assembled
only until that timestamp's marker. There is no wait for another picture or added
playout buffer. Missing markers cannot borrow prefixes into a following picture.
The whole-unit bound is4MiB, matching the bridge. FU payloads shorter than4 bytes
are rejected without out-of-bounds reads.

## Actual-packet proof

A private native AP fixture contains two NALs: SEI type39 (57 bytes) and VCL type1
(779 bytes). No image payload is committed.

- Raw AP:842 bytes, SHA256
  `f31c4d2b7624b076d008d10fbeb2c76e5b6679e04bcd15342804d0ee3e204b44`.
- Independently parsed AnnexB:844 bytes, SHA256
  `e7d56a46a4b51d70504b6b84b6dd9388a5d0f182ed0e6a679692003983f18857`.
- The actual compiled Kotlin parser's output matched the independent Python
  length-parser output byte for byte.

Ten library tests cover AP ordering/length/header checks, incomplete FU bounds,
open-fragment isolation, same-timestamp marker grouping, malformed middle packets,
whole-unit bounds and recovery without borrowing another timestamp.

## Physical proof and scope

Candidate APK SHA256:
`6d77d83296f119f94bee876d1691010327012a1f58f9548e5039e15f7c8b8455`.

Both Fire TVs temporarily displayed their two existing AVC cameras plus native
HEVC during an explicit bounded debug profile probe. The production profile guard
remained conservative; no SPS bytes or source quality settings were changed.

- Probe starts epochms:1791827061916 /1791827063013.
- First render observed epochms:1791827068350 /1791827068436.
- Both selected actual hardware `OMX.amlogic.hevc.decoder.awesome`.
- **Actual decoded size was1280x720.** The source had changed its in-band geometry;
  this is not a successful1920x1080 capacity test.
- At13:45:21–13:45:26 local device log time, decoded counts rose
  1331→1456 on the first TV and1336→1461 on the second:125 frames in5 seconds
  (25fps). Queued units were0; pending media estimate239–281ms; missing stamps0.
  Informational render callbacks rose1087→1198 and1095→1208.
- There was an initial~4-second source burst/gap with a temporary queue. The queue
  subsequently drained; this correction does not claim those upstream pauses fixed.
- Captured final timing-tail samples had native ingress→decode upper-age medians
  100ms (19 samples) and99ms (24 samples), maxima127/206ms. Informational codec
  render age medians181ms on both, maxima200/241ms. These are a short retained
  logcat tail, not a complete12-second distribution or physical display timestamps.
- Autonomous proof window ended1791827178446. Exact full original preferences
  were restored and apps relaunched at1791827179226 /1791827180026.
- Per-frame logging returned toINFO on both. Original two-camera AVC hardware
  playback recovered with rising counters, no missing stamps and no blanking.

The observer overwrote rolling INFO snapshots; DEBUG logging then evicted older
INFO lines from logcat. Consequently the counter samples above were read in the
recorded tool output, while the retained timing files contain only the final tail.
Do not treat the private INFO file as a complete115-second trace.

The HEVC source advertises High Tier Level5.0; these TVs report maximum Level4.1.
The successful720p debug probe does not alter the default advertised-profile policy.
Native auto-selection remains disabled for unsupported or unknown capabilities.

## Reproduction and private artifacts

`tv/rtsp-client` vendors exact upstream tag5.6.3 source before this patch; see
[UPSTREAM.md](../../tv/rtsp-client/UPSTREAM.md). Private fixture and tail logs are
under `.evidence/bridge-network-audit-2026-10-12/eufy-native-actual-ap*` and
`eufy-ap-native-timing-tv*.log`. The fixture contains encoded image bytes and must
remain private. There is no SDK change in this correction.
