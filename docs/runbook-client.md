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

With `sink: planes`, the client owns a black primary display layer behind the camera
overlays. It allocates one static image at startup; no background video player or
continuous image processing is required.

## Layouts
`1`, `2x2`, `3x3`, `1+5` (primary 2×2 at `left` or `right` of a 3×3 grid; `center` is not possible with 3
columns). A tile with `aspect: tall` (an E340 in split-view) takes 1 column × 2 rows — in `1+5` that is the
side column next to the primary; if there is no room it is letterboxed in one cell.
For a fixed camera with no `aspect` setting, the client reads `/api/cameras` at startup and marks a
portrait stream `tall` automatically. An explicit `aspect: tall` or `aspect: wide` overrides detection.
If the bridge is unavailable during startup, the client keeps the configured/default layout.

## RTSP packet processing

The bridge advertises `rtspTcpPacketSize` per camera in `/api/cameras` and the WebSocket
`hello` snapshot. For generated camera URLs, the Linux client uses this hint to request
larger TCP RTP packets from go2rtc. This reduces packet processing on slower CPUs while
preserving the encoded frames, resolution, frame rate, timestamps, and configured
`latency_ms`. A reconnect can obtain the hint even if the startup HTTP request failed.
If the client connects before bridge login completes, the bridge sends a fresh
camera snapshot once its registry is ready; the client can learn the stream keys
and packet-size hints on the existing connection.

Explicit tile `url:` values remain unchanged by automatic discovery. When using a known
go2rtc server with explicit URLs, opt in with `rtsp_packet_size: 8192`. The valid override
range is 256–65535; `rtsp_packet_size: 0` disables automatic tuning. An omitted option
means automatic discovery, and an older/unavailable bridge leaves the original URL
unchanged. A positive override replaces only the URL's `pkt_size` query option and
preserves other options. Other RTSP servers may not support this go2rtc query option.

The change does not add a playback queue or periodically discard compressed frames.
See [the Pi processing and delay measurements](evidence/linux-rtp-packet-size-2026-10-09.md).

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
- Pi falls behind while the same camera stays current on the TVs → check CPU idle time,
  go2rtc consumer drops, and TCP send/receive queues. In the measured Pi Model B case,
  RTP packet processing saturated the CPU and go2rtc queued older video for this client.
  Updating the bridge/client enables the advertised TCP packet-size optimization.
  For explicit go2rtc tile URLs, set `rtsp_packet_size: 8192`; first verify the option
  in the printed `-dry-run` pipeline. Do not treat `latency_ms: 200` as a bound on every
  queue in the server/network/decoder path.
- Delayed motion on both the Pi and TVs → check the bridge's outgoing RTP clock before changing
  decoder settings. `latency_ms` sets the Linux RTSP jitter allowance (200 ms by default), not total
  camera-to-screen latency. See [the live clock and startup measurements](evidence/live-timing-2026-10-08.md).
  The Linux V4L2 decoder drains output in a separate task and does not share the TV player's corrected
  wait-for-next-input bug; see [the Linux playback audit](evidence/linux-playback-audit-2026-10-08.md).
- Explicit tile URL becomes a serial-number path after a bridge restart → update the client.
  Configured per-camera URLs now survive early WebSocket hello events and stream-key changes.
- Terminal text visible behind the video → update the Linux client. Earlier `sink: planes`
  versions placed video over the console framebuffer. The client now allocates its own black
  primary layer and restores the previous layer when it exits. Clearing `/dev/vcs1` alone does
  not verify the displayed pixels; DietPi also prints a banner late in startup. Kernel logging
  does not need to be disabled to keep text out of the application's backdrop.
  If the earlier console workaround was installed, remove
  `/etc/sysctl.d/99-eufy-wall-console.conf` and the service's `console.conf` drop-in,
  run `sudo sysctl -w 'kernel.printk=4 4 1 7'` and `sudo systemctl daemon-reload`.
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
