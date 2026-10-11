# Running bridge and client deployment — 2026-10-11

Application revision checked: `c285172677377ad0507ca2be69e2d3e69ceb9788`.

## Bridge: 192.168.23.158

All 22 checked server source modules, package manifest and lockfile match the
checkout by SHA-256. The initial check found the deployed lockfile still pinned
SDK `d35abff`, while the repository pins fork commit
`e31254f1e5f82a57fed3a3c6fbe9bd1e0a1ef5a3`.

Built the clean SDK checkout at that exact commit, packed its generated runtime,
and staged it on the Atom host using the existing unchanged external dependencies.
All **123 bridge tests passed, none skipped**, against the staged SDK on the host.
Replaced the deployed SDK and aligned both package-lock files and installed
dependency metadata. No SDK source changes were made.

Rollback copy: `/opt/eufy-wall-bridge/sdk-backup-before-e31254f-20261011`.
The original bundle SHA-256 was
`8d97e4ff7daa442a3c052b6208941bfe3a066f295b5d7a306e3abf7fa9e8b9ad`.
The deployed bundle, matching the freshly built pinned checkout, is
`4b4739f83cbe7b834c45a68f4e991a8d742d6d8e0b7fcf24cb3e39984c915032`.
Packed archive SHA-256:
`ab35c1ffee1c5e17d8505d09c0b23f0d24728ad10ea2004b6958409e5bb1d62c`.

The restarted service PID was **11898**. Health reports authentication OK,
go2rtc running, Front Door/Garage/Balcony streaming, and zero camera stalls.
Both HomeBase and Balcony P2P peers remain LAN addresses; LAN-only policy is active.
Both active FFmpeg producers retain VA-API decoding, scaling and H.264 encoding,
fragmented timestamped MPEG-TS input and passthrough encoder timestamps.

A 30-second post-restart RTSP clock probe, excluding three settling seconds,
observed 405 unique stable picture timestamps per main camera. Front Door's
RTP/wall ratio was **1.0001**, Garage's **0.9993**; both were about **15 fps**,
with **zero timestamp regressions**. This verifies live media delivery and clock
progression, not glass-to-glass latency or elimination of incoming pauses.

## Fire TVs: 192.168.23.85 and 192.168.23.58

Both installed APKs exactly match the local built/distributed APK SHA-256:
`73028d782a68d61bc05788a37bb81c05de4bebc35e4e191ad0bd8f8a192f08c9`.
Package `com.eufywall.viewer`, version **0.5.2**, code **7**.

The first app was already running (PID **14123**). The second app was launched
from its previously foreground TV player and ran as PID **12422**. Both restored
their saved walls and recovered through the bridge restart. Independent
SurfaceFlinger actual-present timestamps advanced for both camera surfaces on
each TV across two snapshots approximately 17 seconds apart. This establishes
new physical display presentations, not merely an installed APK or decoder callback.

## GaragePi: 192.168.23.73

The installed ARMv6 binary matches the current local artifact:
`4dcdba5aca441acf9f02b008778d574542e4f6c72a98a2508f01b8cda74caf11`.
The native plugin matches the tested final build:
`e2f7e6a8993bfd6bc8289699552fc14cb4c15dab15a658ae98d4cf4fe6572c64`.

Started the client service for this requested running-code check; PID **18189**
was active. Its two real display pipelines use V4L2 H.264 hardware decoders,
`latency=0`, `buffer-mode=none`, one superseding decoded picture, `eufypacing`,
and clocked KMS sinks. The black background and shared DRM descriptor remain.
Both planes **98** and **109** showed changing framebuffer IDs across three
snapshots after bridge recovery. Unlike the earlier headless gate, this check
observes active physical KMS presentation. Visual smoothness and brightness
were not inspected by a person during this deployment check.

Boot enablement and the existing TV-power script were unchanged.
