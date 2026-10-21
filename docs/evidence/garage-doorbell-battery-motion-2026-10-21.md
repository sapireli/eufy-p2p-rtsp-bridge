# Garage Doorbell battery policy and motion tiles — 2026-10-21

The live bridge configuration now sets Garage Doorbell to `power_override: battery` and `mode: on_motion`. Its existing Split view is retained. After restarting the service, `/api/cameras` confirmed `powerOverride: battery`, `powered: false`, `mode: on_motion`, `held: false`, and `streaming: false`. Its motion hold duration remains 60 seconds. It is no longer continuously opened as an always-on feed.

The motion tile on each Fire TV (.58, .85 and .100) now watches Garage Doorbell, Garage Interior Door CLE, Front Yard CLE and Solar Wall Light Cam. All four are enabled battery cameras according to the current bridge API. The stale Front Door entry was removed from .100's watch list so hardwired cameras do not displace the motion tile.

Each TV preference file was backed up before editing, then read back after relaunch. All three confirmed the new battery-only list. Fixed tiles and hardware pins remain Front Door and Garage CLE; Balcony and Wyze remain in the right stack. GaragePi retains its separate Front Door/Garage CLE side-by-side configuration with no motion box.

Private preference backups: `/tmp/eufy-wall-motion-battery-20261021-002031`. Bridge configuration rollback: `/etc/eufy-wall-bridge.yaml.before-battery-20261021T042031Z`. No account credentials or complete private configuration are copied into this record.

This verifies the persisted policy and motion selection configuration. No physical motion event was generated for this check, so successful battery-camera wake/playback is not claimed.
