# Runbook — eufy-wall (display client)

## Hardware / OS
- Raspberry Pi 3 (recommended), Pi 1/Zero (H.264 only, ≤ 4×720p — see limits), or Debian x86 (VAAPI).
- Raspberry Pi OS **Lite** (Bookworm or Trixie), no desktop. Ethernet preferred.
- `/boot/firmware/config.txt`: `dtoverlay=vc4-kms-v3d`, `gpu_mem=128`, `hdmi_blanking=0` (install script adds them).
- Cameras must stream **H.264** (the bridge's /api/cameras shows `codec`). The Pi has no HEVC decoder.

## Install
    make pi3            # on your workstation (or pi1 / pi64 / amd64) → client/bin/eufy-wall-armv7
    scp client/bin/eufy-wall-armv7 pi:/tmp/ ; scp -r deploy client/config.example.yaml pi:/tmp/
    sudo deploy/install-client.sh /tmp/eufy-wall-armv7
    sudo nano /etc/eufy-wall.yaml      # rtsp_base → your server, tiles, layout
    eufy-wall -config /etc/eufy-wall.yaml -dry-run   # shows the tile table + the gst-launch line
    sudo systemctl start eufy-wall && journalctl -fu eufy-wall

## Layouts
`1`, `2x2`, `3x3`, `1+5` (primary 2×2 at `left` or `right` of a 3×3 grid; `center` is not possible with 3
columns). A tile with `aspect: tall` (an E340 in split-view) takes 1 column × 2 rows — in `1+5` that is the
side column next to the primary; if there is no room it is letterboxed in one cell.

## Sink strategy (from Spike B — fill in measured numbers)
| Pi | streams | sink=planes | sink=compositor | notes |
|----|---------|-------------|-----------------|-------|
| Pi 3 | 6×720p15 | | | |
| Pi 1 | 4×720p15 | | | |
- `sink: planes` needs the DRM overlay plane ids: `modetest -M vc4 -p` → "Planes" table → ids whose
  `type` is Overlay. Put ≥ N ids in `planes:`.
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
- Black screen, logs say `Could not open DRM`/`Permission denied` → user `wall` must be in `video`+`render`
  and nothing else (X/Wayland/getty splash) may own the display; `systemctl stop getty@tty1` if needed.
- `v4l2h264dec` missing → `apt install gstreamer1.0-plugins-good`; `/dev/video10` missing → kernel/firmware
  without `bcm2835-codec` — check `dmesg | grep codec`.
- Decoder hangs after a while on kernel 6.6.x (`h264_v4l2m2m` regression) → `journalctl` shows no frames;
  the supervisor restarts the pipeline; upgrade the kernel (`sudo apt full-upgrade`).
- One camera down restarts the whole wall (single pipeline) — expected in Phase 1; the bridge keeps the
  others warm so they return in ~2 s.
- Too slow (dropped frames, CPU > 80 %) → lower secondaries' quality on the server (`cameras.<sn>.quality:
  "HD (720P)"`) or use a smaller layout.
