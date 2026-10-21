# Linux hardware priority and isolated fallback

## Problem and intervention

Planes already ran separate camera processes, but did not arbitrate hardware priority. Compositor/window mode previously put every decoder on the output pipeline bus: a fatal decoder error restarted the wall.

Automatic decoding now applies camera pins and numeric ranks. Only a decoder-owned resource failure can reclaim a lower-ranked unpinned camera using the same decoder factory. The victim releases hardware before the requester retries, and continues in software. Explicit decoder choices remain authoritative.

Compositor/window mode now uses one private decoder pipeline per `eufytile`, with bounded decoded-frame handoff to the persistent compositor. Source changes and recovery affect that tile. The output/display sink is still shared.

## Verification, 2026-10-21

- `make test`: C pacing regression and all 11 Go packages passed. Config/supervisor race tests also passed.
- GStreamer 1.26.2 synthetic runtime test on the off-device build host: faulty source repeatedly failed while healthy output advanced from 51 to 101 frames; healthy decoder start count stayed 1. Updating the faulty slot retained the compositor process and healthy decoder.
- A synthetic one-slot hardware decoder generated an actual allocation bus error: lower-priority camera released hardware and switched to software; pinned requester acquired hardware. Low camera delivered 51 frames, high camera 40; no parent bus error. This verifies arbitration, not a physical decoder slot limit.
- All-idle compositor produced five black frames in about 0.502 seconds.
- Native ownership, timestamp mapping, bounded backoff, and rapid atomic replacement tests passed. The replacement regression held inode, size and modification-time second constant while changing nanoseconds, and verified the latest pipeline was applied.
- Production native source compiled with `-Wall -Wextra -Werror`. Final `gsteufytile.c` SHA256: `92dcae082baf12282eede461323de4d0e73ce43ab0283d486369624022efb838`.
- ARMv6 plugin cross-build SHA256: `1d465453643cbc31bf18ba1a6618659718dffa1790c670cda52d20f9a9a2207c`.

These synthetic tests establish decoder lifecycle isolation and resource handoff. They do not establish physical HDMI/X11 output, sustained camera playback, or sensor-to-screen latency. GaragePi was TV-off/service-inactive during this work; deployment preserves that state. No bridge or SDK changes are required.
