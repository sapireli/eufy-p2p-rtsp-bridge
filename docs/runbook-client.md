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

## Motion outlines

With the independent DRM-plane renderer, each bridge motion notification flashes
the matching camera tile blue for five seconds (alternating 250 ms on and off). Another
event restarts that tile's pulse; different cameras pulse independently. Fixed
tiles and motion-following tiles use the camera currently assigned to the tile.
Hello snapshots and stream-state changes do not flash a border.

The feature defaults to on. To disable it, set `motion_flash: false` in
`/etc/eufy-wall.yaml` and restart `eufy-wall.service`; use `true` to enable it
again. This setting is separate from motion-following camera selection.

The border uses the existing primary-plane background and a four-pixel gutter
inside each tile. Only border pixels change during the short pulse. There is no
continuous renderer, decoded-picture copy, or motion-triggered player restart.
The compositor/window backends do not currently draw motion outlines. Cameras
must supply motion events through the bridge; a plain external RTSP stream alone
does not provide motion detection.

Android TV exposes the corresponding saved setting under **Edit wall → Flash on
motion**. Disabling it preserves the remote's ordinary blue focus outline.

## Install
The install script expects the repo layout (`deploy/` next to `client/`), so copy both directories:

    make pi3            # on your workstation (or pi1 / pi64 / amd64) → client/bin/eufy-wall-armv7
    ssh pi 'mkdir -p /tmp/eufy-wall' && scp -r deploy client pi:/tmp/eufy-wall/
    ssh pi
    sudo /tmp/eufy-wall/deploy/install-client.sh /tmp/eufy-wall/client/bin/eufy-wall-armv7
    sudo nano /etc/eufy-wall.yaml      # rtsp_base → your server, tiles, layout
    eufy-wall -config /etc/eufy-wall.yaml -dry-run   # shows the tile table + the gst-launch line
    sudo systemctl start eufy-wall && journalctl -fu eufy-wall

For a stable bridge hostname, set `rtsp_base: rtsp://homebridge.lan:8554`.
The client uses that hostname for RTSP, the HTTP API and WebSocket events; it does
not replace it with a saved numeric address. Verify `getent hosts homebridge.lan`
on the client first. This name is normally provided by LAN DNS; mDNS hostnames
normally end in `.local`. `rtsp_base: auto` remains available for service discovery.

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

## Bounded playback timing

The installer builds the `eufypacing` GStreamer element for the destination CPU and installed
GStreamer ABI (`gcc`, `pkg-config`, `libgstreamer1.0-dev`). The client discovers it through
`/usr/local/lib/eufy-wall/gstreamer-1.0`. Updates include the plugin source and rebuild it.
A missing plugin logs an explicit warning and retains immediate display without stabilization.

Live tiles decode every compressed frame before a one-frame downstream-leaky raw-video queue.
The pacing element maps decoded RTP presentation timestamps to local pipeline time using the
minimum arrival offset from the last two seconds, with one 67 ms allowance. It clamps the
scheduled timestamp to at most 67 ms ahead of the local clock. A newest decoded picture arriving
late is scheduled immediately; discarding it against an older clock offset would extend a source
pause into an artificial freeze. Repeated/out-of-order timestamps are rejected, and explicit
timestamp discontinuities reset the mapping. It neither sleeps
the decoder nor maps/copies video pixels. KMS follows that clock (`sync=true`) with bounded
lateness. The element's latency query reports zero: its allowance is already in the new PTS,
so GStreamer must not add source/decoder latency a second time. Physical display/decoder
latency and upstream camera/network queues remain separate; 67 ms is the scheduling bound,
not a measured camera-to-screen guarantee.

The shared-plane `skip-vsync=true` remains necessary: independent processes share one DRM event
queue; timing comes from GStreamer rather than competing DRM page-flip waits. Window sinks use
the same raw-video pacing, but `autovideosink` lacks a portable child-sink lateness property.

The default `latency_ms` is zero for ordered RTSP/TCP, so the raw-video allowance provides the
single intentional client timing margin. Existing explicit values remain unchanged. A config
still specifying 200 adds that upstream jitter allowance; use `latency_ms: 0` with this plugin
to avoid double buffering. Source interruptions longer than the margin remain visible.

With the native pacing element, `rtspsrc buffer-mode=none` preserves media RTP timestamps.
The native mapper owns clock drift compensation. Per-packet `tcp-timestamp=true` is intentionally
not enabled: the [official source documentation](https://gstreamer.freedesktop.org/documentation/rtsp/rtspsrc.html#rtspsrc:tcp-timestamp)
warns that receive timestamps can cause problems when a server sends bursts. Measuring the native
path confirmed that applying receive-time compensation before decoded pacing distorted cadence.
The [buffer-mode enum](https://gstreamer.freedesktop.org/documentation/rtsp/rtspsrc.html#Rtspsrc-buffer-mode)
documents `none` as using RTP timestamps only. Missing native-plugin support retains the original
immediate display path and logs a warning.
It intentionally does not enable RTP `drop-on-latency`: dropping encoded reference packets can
corrupt subsequent pictures. Only decoded frames are discarded.

See [GstBaseSink timing](https://gstreamer.freedesktop.org/documentation/base/gstbasesink.html)
and [queue limits](https://gstreamer.freedesktop.org/documentation/coreelements/queue.html).

## Troubleshooting
- Pi falls behind while the same camera stays current on the TVs → check CPU idle time,
  go2rtc consumer drops, and TCP send/receive queues. In the measured Pi Model B case,
  RTP packet processing saturated the CPU and go2rtc queued older video for this client.
  Updating the bridge/client enables the advertised TCP packet-size optimization.
  For explicit go2rtc tile URLs, set `rtsp_packet_size: 8192`; first verify the option
  in the printed `-dry-run` pipeline. Do not treat `latency_ms: 200` as a bound on every
  queue in the server/network/decoder path.
- Delayed motion on both the Pi and TVs → check the bridge's outgoing RTP clock before changing
  decoder settings. `latency_ms` sets the Linux RTSP jitter allowance (0 ms by default with a separate 67 ms decoded-frame pacing margin), not total
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


## Advertised bridge-ingress freshness

A compatible bridge can advertise per-camera SEI freshness stamps. `mode: observe` preserves existing pacing and presentation policy; it does not activate deadline drops, blanking or freshness-triggered restarts. With the updated native plugin and independent planes, it associates the exact encoded stamp with decoded PTS and emits cumulative `eufy-observe` statistics every five seconds. An older plugin, a non-planes sink, or a manually overridden resource retains legacy playback and reports presentation age as unmeasured. `mode: enforce` activates the guards below. Version 2 requires an explicit `observe` or `enforce` mode; older clients ignore version 2. Older v1 capabilities without a mode retain enforce compatibility. The binary SEI stamp format remains version 1. In enforce mode, the Linux client reads stamps before decoding and checks their original PTS association after decoding; a late or unverified picture is suppressed. Expired independent DRM video planes are cleared to the existing black backdrop even when no frames arrive, while compressed decoding continues. Fresh pictures resume without a reconnect; actual transport/decoder failures still restart.

This bounds age since bridge ingress only. Camera capture age remains unverified. Strict freshness requires the native `eufystamp`/`eufypacing` plugin pair, monotonic clock polling and independent shared DRM overlay planes. Missing required components fail closed for advertised strict feeds. Legacy unadvertised RTSP feeds continue to work. See [implementation and verification boundaries](evidence/linux-ingress-freshness-2026-10-12.md).


## Automatic native rendition selection

The bridge can advertise `nativeRendition` alongside its compatible H.264 URL.
A tile with omitted `codec` or `codec: auto` may select native H.265 when an actual
GStreamer hardware factory is installed (`v4l2h265dec`, `v4l2slh265dec` or
`vah265dec`) **and its actual sink capability structure explicitly lists the
advertised HEVC profile, level and tier tuple**. Missing source metadata or
missing decoder level/tier fields declines automatic native selection. A merely
higher nominal level does not establish high-tier support. `decoder: software`, explicit tile codecs, explicit URLs and still
images retain their configured paths. Merely installing `avdec_h265` does not
qualify as hardware support.

Factory discovery and the explicit capability tuple still do not prove that a
particular resolution or bit depth successfully initializes that hardware. If the native gst process fails—including
negotiation failure or its existing watchdog—the supervisor selects its compatible
plan once and retries that plan thereafter. Repeated unchanged event updates do
not retry the failed native plan. A materially changed plan or client restart may
attempt native again. Compositor fallback switches the shared wall; independent
KMS planes fall back per camera. Existing compatible decoder discovery still
prefers hardware and uses software when hardware is unavailable.

Freshness metadata belongs to the selected rendition's exact URL. The native
resource cannot borrow its compatible sibling's contract, downgrade an enforced
compatible contract, or select enforcement without the required stamp reader,
clock mapper and shared DRM plane support. Native HEVC strict rendering is **not
physically verified on Linux** by the unit tests; those verify plan construction
and resource binding, not a displayed frame deadline.

On 2026-10-12, read-only inspection of GaragePi found `armv6l`, no hardware HEVC
factories, and `eufy-wall.service` inactive and disabled. It therefore keeps its
compatible streams. No display, CEC, service activation or installation was
performed for this change. The Pi artifact is built with `GOOS=linux GOARCH=arm
GOARM=6 CGO_ENABLED=0`; other Linux targets need their own architecture build.


Deployment boundary: the ARMv6 client binary was updated on GaragePi on
2026-10-12 and its `-h` invocation checked; the service remained inactive and
disabled. The native GStreamer plugin was not replaced by this client feature.
No active playback or physical native-HEVC test was performed. Transport packet
size hints/overrides are retained when selecting a native URI; other URI selector
parameters belong to that native resource and are not copied from its sibling.


### Observe-only Linux measurements

`eufy-observe` reports matched/missing metadata, unknown clock mappings, frame-ID
order, age upper-bound histograms and maximums after the one-buffer decoded
queue, and again after the synchronous KMS sink call returns. Clock uncertainty
includes the clock request round trip and a conservative 1000 ppm projection
allowance. Pictures above the advertised age budget are counted, not rejected.
Missing stamps or an invalid clock also continue displaying. The plugin never
starts its expiry thread or clears DRM planes in observe mode.

These are bridge-ingress-to-software-presentation measurements, **not camera
capture age or confirmed physical scanout**. `sinkPushMaxNs` measures the sink
call; `longestDecodedGapNs` measures raw-buffer arrival after the decoded queue;
`quietNowNs` records the current no-buffer tail when a report is emitted.
Neither identifies the stage that caused a pause. `scanoutVerified=false` is
always explicit. DRM plane framebuffer IDs can confirm driver-state progression,
but repeated IDs are reused pool buffers and do not prove a frozen image.

Native integration verification (with the rebuilt plugin on its library path):

```sh
cc -std=c99 -O2 -Wall -Wextra -Werror $(pkg-config --cflags gstreamer-1.0) \
  client/gst/freshness_probe.c -o /tmp/eufy-freshness-probe \
  $(pkg-config --libs gstreamer-1.0)
/tmp/eufy-freshness-probe --observe
/tmp/eufy-freshness-probe
```

The observe probe deliberately submits expired, absent, and unknown-clock
metadata and verifies all four raw buffers reach its sink without an expiry
message. The separate default probe retains strict suppression, expiry and
same-pipeline recovery coverage. Neither synthetic probe establishes hardware
scanout; sustained target-device measurements are required separately.
