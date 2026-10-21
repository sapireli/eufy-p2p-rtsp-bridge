# Garage Doorbell battery policy and motion tiles — 2026-10-21

The live bridge configuration now sets Garage Doorbell to `power_override: battery` and `mode: on_motion`. Its existing Split view is retained. After restarting the service, `/api/cameras` confirmed `powerOverride: battery`, `powered: false`, `mode: on_motion`, `held: false`, and `streaming: false`. Its motion hold duration remains 60 seconds. It is no longer continuously opened as an always-on feed.

The motion tile on each Fire TV (.58, .85 and .100) now watches Garage Doorbell, Garage Interior Door CLE, Front Yard CLE and Solar Wall Light Cam. All four are enabled battery cameras according to the current bridge API. The stale Front Door entry was removed from .100's watch list so hardwired cameras do not displace the motion tile.

Each TV preference file was backed up before editing, then read back after relaunch. All three confirmed the new battery-only list. Fixed tiles and hardware pins remain Front Door and Garage CLE; Balcony and Wyze remain in the right stack. GaragePi retains its separate Front Door/Garage CLE side-by-side configuration with no motion box.

Private preference backups: `/tmp/eufy-wall-motion-battery-20261021-002031`. Bridge configuration rollback: `/etc/eufy-wall-bridge.yaml.before-battery-20261021T042031Z`. No account credentials or complete private configuration are copied into this record.

This verifies the persisted policy and motion selection configuration. No physical motion event was generated for this check, so successful battery-camera wake/playback is not claimed.


## Linux parity and deployed improvement

The Linux DRM-plane path already supervises each camera independently, retains healthy siblings and the parent black backdrop, bounds decoded-frame queues, and recovers stalled pipelines. The added regression in `996a462` exercises repeated real child-process failures while verifying that the healthy sibling process remains intact. Android's retained tile surface has no literal Linux counterpart: the failed overlay process is recreated while sibling overlays and the parent background remain.

The audit did find a gap in automatic decoder fallback. Commit `44ba021f9dd16775b47f2ef3711152b4b269a5f4` adds software retry for recognized hardware-decoder-owned initialization, device, input-format and allocation errors in independent DRM-plane tiles. Hardware stays first, explicit decoder choices remain authoritative, and network/source failures, generic negotiation errors and corrupted media do not select software. Native rendition failure still tries compatible hardware playback first. Unchanged control updates retain the selected fallback.

`make test` passed the C pacing test and all 11 Go packages. Targeted tests cover error classification, immediate decoder exit, software retry, healthy sibling retention and cancellation when descendants retain pipe writers. Parent-owned pipes ensure the final decoder error is read before choosing the retry.

The ARMv6 binary was built off-device from clean commit `44ba021` and atomically installed on GaragePi at 04:27 UTC. SHA-256: `b6f6d94adb4501eecce9aa16123524fc750af7b9a92f256562eefa107e9a273d`. The installed binary ran successfully on the Pi; its dry-run selected `v4l2h264dec` for both camera processes and preserved the 1920×1080, 2×1 layout. The previous binary is retained at `/usr/local/bin/eufy-wall.before-decoder-fallback-20261021T042755Z`.

The TV and wall service remained off. Physical HDMI playback and a real hardware failure were not exercised during this deployment; the fallback execution path is verified by process-level regressions.
