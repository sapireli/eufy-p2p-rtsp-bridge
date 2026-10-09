# Runbook — eufy-wall (display client)

## Hardware / OS
- Raspberry Pi 3 (recommended), Pi 1/Zero (H.264 only, ≤ 4×720p — see limits), or Debian x86 (VAAPI).
- Raspberry Pi OS **Lite** (Bookworm or Trixie), no desktop. Ethernet preferred.
- `/boot/firmware/config.txt`: `dtoverlay=vc4-kms-v3d`, `gpu_mem=128`, `hdmi_blanking=0` (install script adds them).
- The Pi's RTSP input must be **H.264**. `/api/cameras` reports the camera's source codec, which can
  differ from the transcoded RTSP output; inspect that output with `ffprobe` when troubleshooting.
  The Pi has no HEVC decoder.
  The bridge now defaults to stream copy, so configure hardware transcoding on the server for HEVC
  cameras shown on a Pi.

Camera power claims and LAN peer policy are set on the bridge server. The wall client receives each
camera's mode and stream state: a battery camera can show its retained snapshot while asleep, and an
always-on camera keeps its tile through a brief reconnect.

## Install
The install script expects the repo layout (`deploy/` next to `client/`), so copy both directories:

    make pi3            # on your workstation (or pi1 / pi64 / amd64) → client/bin/eufy-wall-armv7
    ssh pi 'mkdir -p /tmp/eufy-wall' && scp -r deploy client pi:/tmp/eufy-wall/
    ssh pi
    sudo /tmp/eufy-wall/deploy/install-client.sh /tmp/eufy-wall/client/bin/eufy-wall-armv7
    sudo nano /etc/eufy-wall.yaml      # rtsp_base → your server, tiles, layout
    eufy-wall -config /etc/eufy-wall.yaml -dry-run   # shows the tile table + the gst-launch line
    sudo systemctl start eufy-wall && journalctl -fu eufy-wall

For a dedicated HDMI appliance, also run
`sudo bash /tmp/eufy-wall/deploy/configure-client-console.sh` to keep the console
behind the camera overlays black. This optional step disables the local login and
kernel console printing; logs remain accessible over SSH. See troubleshooting below.

## Layouts
`1`, `2x2`, `3x3`, `1+5` (primary 2×2 at `left` or `right` of a 3×3 grid; `center` is not possible with 3
columns). A tile with `aspect: tall` (an E340 in split-view) takes 1 column × 2 rows — in `1+5` that is the
side column next to the primary; if there is no room it is letterboxed in one cell.
For a fixed camera with no `aspect` setting, the client reads `/api/cameras` at startup and marks a
portrait stream `tall` automatically. An explicit `aspect: tall` or `aspect: wide` overrides detection.
If the bridge is unavailable during startup, the client keeps the configured/default layout.

## Dual-lens Split / PiP
The bridge controls each camera's composed view. On the Linux client, switch one by name or serial:

    eufy-wall -config /etc/eufy-wall.yaml -view-camera 'Front Door CLE' -view-mode pip-br
    eufy-wall -config /etc/eufy-wall.yaml -view-camera 'Front Door CLE' -view-mode split

Modes are `split`, `pip-tl`, `pip-tr`, `pip-bl`, `pip-br`, and `single`. The bridge saves the choice and
restarts that camera's stream; the Linux wall reconnects. PiP is landscape, so remove an explicit
`aspect: tall` from that tile's config and restart the Linux wall to detect its new geometry. A TV can
change the same setting from its setup screen; all clients then see the camera's new stream.

## Sink strategy (from Spike B — fill in measured numbers)
| Pi | streams | sink=planes | sink=compositor | notes |
|----|---------|-------------|-----------------|-------|
| Pi 3 | 6×720p15 | | | |
| Pi 1 | 4×720p15 | | | |
- `sink: planes` needs the DRM overlay plane ids: `modetest -M vc4 -p` → "Planes" table → ids whose
  `type` is Overlay and whose possible-CRTC mask includes the selected HDMI CRTC. Put ≥ N ids in
  `planes:`. The client opens the connector's DRM card once and passes the same open descriptor to
  each tile process (`kmssink fd=3 skip-vsync=true`, GStreamer 1.22+). This preserves independent
  tile restarts without competing DRM masters or CRTC page flips; hardware overlays still compose.
- `sink: compositor` needs no ids and works on x86 too.

## macOS preview
The binary runs on macOS for previewing a layout against the real server. Install GStreamer:
```
brew install gstreamer
```
This pulls in the plugin sets. Configure with:
- `sink: window` (displays to an X11/Quartz window instead of KMS)
- `decoder: software` (uses libav H.264 decoder)
- An explicit `screen: { width, height }` in the config (auto-detect reads Linux sysfs and fails on macOS)

KMS sinks (`planes` and `compositor`) are Linux-only. On macOS, `window` is the only valid sink.

## Troubleshooting
- Delayed motion on both the Pi and TVs → check the bridge's outgoing RTP clock before changing
  decoder settings. `latency_ms` sets the Linux RTSP jitter allowance (200 ms by default), not total
  camera-to-screen latency. See [the live clock and startup measurements](evidence/live-timing-2026-10-08.md).
  The Linux V4L2 decoder drains output in a separate task and does not share the TV player's corrected
  wait-for-next-input bug; see [the Linux playback audit](evidence/linux-playback-audit-2026-10-08.md).
- Explicit tile URL becomes a serial-number path after a bridge restart → update the client.
  Configured per-camera URLs now survive early WebSocket hello events and stream-key changes.
- Terminal text visible behind the video → `sink: planes` overlays the Linux console;
  clearing it once does not stop later kernel warnings from repainting uncovered areas.
  On a dedicated display appliance, run `sudo bash deploy/configure-client-console.sh`.
  It masks the HDMI login, clears the black backdrop, disables kernel console printing through
  a persistent sysctl, and installs a root `ExecStartPre` to repeat the clear when the wall starts.
  Kernel diagnostics remain available through `journalctl -k` and `dmesg`; SSH is unaffected.
  This is opt-in because it disables local console diagnostics. To restore the local console,
  remove `/etc/sysctl.d/99-eufy-wall-console.conf` and the service's `console.conf` drop-in,
  run `sudo sysctl -w 'kernel.printk=4 4 1 7'` and `sudo systemctl daemon-reload`, then
  `sudo systemctl unmask getty@tty1.service` and `sudo systemctl start getty@tty1.service`.
  See [the GaragePi diagnosis and live verification](evidence/garagepi-console-2026-10-09.md).
- Black screen, logs say `Could not open DRM`/`Permission denied` → user `wall` must be in `video`+`render`
  and nothing else (X/Wayland/getty splash) may own the display; `systemctl stop getty@tty1` if needed.
- `v4l2h264dec` missing → `apt install gstreamer1.0-plugins-good`; `/dev/video10` missing → kernel/firmware
  without `bcm2835-codec` — check `dmesg | grep codec`.
- `not-negotiated` with parser profile `constrained-high` → compare `h264parse` output with the
  decoder's advertised sink profiles. The bridge's resized VA-API H.264 preset explicitly uses Main
  profile, verified to decode on a Pi Model B Rev 2; it keeps hardware encoding and decoding.
- `no property "force-aspect-ratio" in element "kmssink"` → update the client. `kmssink` already
  preserves the video's display ratio within its render rectangle and has no such property.
- Decoder hangs after a while on kernel 6.6.x (`h264_v4l2m2m` regression) → `journalctl` shows no frames;
  the supervisor restarts the pipeline; upgrade the kernel (`sudo apt full-upgrade`).
- One camera down restarts the whole wall → `sink: compositor` and `sink: window` use one pipeline,
  so an error restarts all tiles. `sink: planes` uses an independent process per tile and restarts
  only the failed tile. The supervisor retries failed processes with the configured backoff.
- Too slow (dropped frames, CPU > 80 %) → lower the secondaries' streaming quality to 720p in the eufy
  app (camera → Settings → Video → Streaming quality; the bridge cannot set it — see the server runbook)
  or use a smaller layout.
