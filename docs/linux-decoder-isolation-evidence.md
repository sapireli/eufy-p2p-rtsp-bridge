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

## GaragePi deployment

Built on the Mac (Go ARMv6) and bridge build host (native ARMv6); nothing was compiled on GaragePi. Installed binary records clean source revision `837a21cb09d425de79ee2dcc99722d57f1030572` (`vcs.modified=false`), SHA256 `176f9b30e48fc538f6cbe5de97bf9dd170189f287774299a9829f11b22f641f8`. The installed plugin matches the cross-build hash above and loads `eufytile` on the actual Pi.

Front Door `T8214510242321E6` and Garage CLE `T8425T1124130A0C` are pinned, ranked 100 and 50. Existing `decoder: auto`, two-camera `2x1` plane layout, source URLs and latency settings are preserved. Deployed `-dry-run` selects `v4l2h264dec` for both. Service remains inactive with the TV off; physical playback was not exercised.

Rollback copies on GaragePi use suffix `.before-20261021-priority` for `/usr/local/bin/eufy-wall`, `/usr/local/lib/eufy-wall/gstreamer-1.0/libgsteufypacing.so` and `/etc/eufy-wall.yaml`. Restore the three matching files together if needed.

## Sustained isolation check

A final 60.000-second synthetic run used the exact final native source, 32×32 sources at 10 fps and a 64×32 compositor. The compositor retained PID 154301 at all 12 five-second checkpoints. Healthy delivery advanced `51 → 101 → 151 → 201 → 251 → 301 → 351 → 401 → 451 → 502 → 552` at its periodic reports, with exactly one decoder start throughout. The faulty tile had 29 starts and 28 contained errors and was repointed at 30 seconds. No parent bus error or output-process exit occurred. This establishes sustained synthetic isolation; it does not replace physical playback verification. Build-host records: `/tmp/eufy-composite-tests-20261021/sustained/result.json` and `verification.log`.
