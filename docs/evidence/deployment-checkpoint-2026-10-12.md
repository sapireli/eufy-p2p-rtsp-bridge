# Deployment checkpoint — 2026-10-12

## Running artifacts

- Bridge `192.168.23.158`: all 28 production `server/src` files match the local
  committed source byte for byte. Authenticated, all three configured live sources
  active, go2rtc running. All six compatible stream names and two native aliases
  survived concurrent startup codec discovery.
- SDK runtime remains unchanged: SHA256
  `3a15c296a54311782f242aa5446b23971671e4ee6016d43ecb927f4ec7fc3fc8`.
  This work does not contain an SDK patch.
- Both Fire TVs run APK SHA256
  `6d77d83296f119f94bee876d1691010327012a1f58f9548e5039e15f7c8b8455`.
  The installed APK hashes match the built artifact. Original preferences were
  restored exactly after the bounded native test; both original AVC camera tiles
  have advancing hardware decode/render counters, empty measured input queues and
  no missing stamps. Timing logging is back at INFO; the debug profile probe is off.
- GaragePi `192.168.23.73` has binary SHA256
  `eba236cba7382d3b1767936b6331814711bd1aef443294e6c155762bad9ddf61`.
  Its help command runs. Service remains inactive and disabled to preserve its
  power policy. This update was not a physical playback test. The native plugin
  remains SHA256
  `c5b0e3cb7086697aec3bc258e9eedf7797510ad2455aad8d65365eb991214e8f`.

## Changes verified on the live path

Complete-picture Matroska input removes an FFmpeg parser dependency on a later
picture. Finite FLV output removes go2rtc's large zero-length PES dependency.
Prefix inspection and transport allocation avoid unnecessary whole-picture CPU
work. HTTP output now distinguishes an ordinary high-water signal from an actual
queued-byte limit; it does not deliberately wait or discard encoded references.
Concurrent configuration generation is serialized and published atomically.

The native HEVC path shares the existing source without creating an encoder.
Android fixes preserve HEVC aggregation NALs, assemble each RTP timestamp through
its marker, and initialize MediaCodec with ordered VPS/SPS/PPS. The real captured
aggregation packet reproduced byte for byte; both TVs sustained 25 decoded fps
in the steady part of the bounded **720p** live probe. This does not establish
1080p capacity or advertised high-tier level-5 support. Production automatic
selection remains conservative about profile, tier and level; the test did not
rewrite the camera bitstream or change camera quality.

Android's 43 unit tests and the Linux Go suite pass. The serial server suite passed
185 checks before the final added batched-output regression; the final 17 egress
checks, including that regression, passed on the actual bridge. The profile and
configuration deployment passed 33 targeted checks there. Timing fixtures on the
weak bridge were run serially after concurrent fixture CPU contention caused a
negative-control timing failure; no test deadline was enlarged.

## Freshness boundaries

Production uses version-2 **observation** mode. The failed 500 ms display-expiry
experiment is not enabled. Ordinary camera titles are shown; capture-clock
verification remains a diagnostic fact, not a playback error.

Ingress-to-receipt timing, producer stability and picture association are measured.
Camera capture time is still unverified, and upstream arrival pauses still occur.
The observer cannot prove screen presentation or freshness before SDK emission.
No physical Google TV test was performed. No Inspector listener remains enabled.

Detailed proof and limits:

- [Bridge timing and capacity audit](bridge-network-timing-audit-2026-10-12.md)
- [HEVC packet and physical playback proof](android-hevc-rtp-aggregation-2026-10-12.md)
- [Freshness implementation and failed enforcement gate](frame-freshness-2026-10-12.md)
- [Pinned RTSP source and downstream changes](../../tv/rtsp-client/UPSTREAM.md)
