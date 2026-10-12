# Pinned RTSP library

Source: https://github.com/alexeyvasilyev/rtsp-client-android
Tag: `5.6.3`
Commit: `4abac4b09d3f5441c255bb7a0217ddeebb36d368`

`src/main` was copied unchanged from that revision in baseline commit `fcf7fc7`.
The current tree includes separately reviewed HEVC AP/dispatch/access-unit patches;
see [actual-packet and physical evidence](../../docs/evidence/android-hevc-rtp-aggregation-2026-10-12.md). This local Android library
replaces the same-version JitPack dependency, so transport corrections can be
reviewed and reproduced as source. Upstream MIT license is preserved in LICENSE.
Build settings match the application toolchain; dependency versions match upstream.
Subsequent changes are separate commits with unit and live-path evidence.
