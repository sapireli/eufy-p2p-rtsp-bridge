# Runbook — eufy-wall-bridge (server)

## Install (Debian, arm64 or x86, no Docker)
    git clone <this repo> && cd eufy-p2p-rtsp-bridge
    sudo deploy/install-server.sh
    sudo nano /etc/eufy-wall-bridge.env      # EUFY_EMAIL / EUFY_PASSWORD / EUFY_COUNTRY (dedicated account!)
    sudo nano /etc/eufy-wall-bridge.yaml     # lan.cidr, cameras, defaults
    sudo systemctl start eufy-wall-bridge && journalctl -fu eufy-wall-bridge
The install script apt-installs Node 24 (NodeSource), rsync and **ffmpeg**. Copy-mode streams use
go2rtc's raw HTTP source; FFmpeg is used when `go2rtc.transcode` selects a hardware transcode.

## First-run login (2FA / captcha)
    curl -s localhost:3000/auth/status
    # {"state":"require_2fa","method":"email"}  → get the code from email/SMS:
    curl -s -X POST 'localhost:3000/auth/tfa?code=123456'
    # {"state":"require_captcha"} →
    curl -s localhost:3000/auth/captcha -o captcha.png   # open it
    curl -s -X POST 'localhost:3000/auth/captcha?code=AB3D'
    # {"state":"pending"} after a failure → curl -s -X POST localhost:3000/auth/retry
The session token is saved in /var/lib/eufy-wall-bridge/.eufy-session.json; later restarts need no code.
Opening the eufy phone app with the SAME account kicks the bridge (state "reauth") — use a dedicated account.

## Check
    curl -s localhost:3000/healthz | jq          # auth ok, streaming [...], blocked {}, go2rtc running
    curl -s localhost:3000/api/cameras | jq      # source codec and geometry, before RTSP transcoding

    # On another LAN device, confirm the installer-created Avahi service resolves:
    avahi-browse -rt _eufy-wall._tcp
    ffplay rtsp://<server>:8554/<sn>             # from any machine on the LAN

## RTSP decoder compatibility
The resized Linux VA-API H.264 preset uses `-profile:v main`, with the existing scale, GOP and
no-B-frame settings. This avoids automatic `constrained-high` output rejected by the Pi Model B's
GStreamer V4L2 sink caps. Encoding remains on VA-API; the Pi decodes on V4L2. Copy streams and other
encoder presets are unchanged. Use `ffprobe` on the advertised RTSP URL to inspect the output;
`/api/cameras` describes the incoming camera stream, which can still be HEVC or a larger resolution.
See [the live Pi deployment evidence](evidence/pi-hdmi-2026-10-08.md).

### Hardware first, software fallback on Linux

When transcoding is enabled, each Linux producer first tries VA-API decoding,
scaling and H.264 encoding against its actual camera stream. If that path reports
a hardware failure, it retries with CPU decoding/scaling and VA-API encoding;
if hardware encoding is unavailable, it retries with CPU decoding and libx264.
Network errors, malformed input and consumer disconnects use normal reconnect
handling rather than causing a software downgrade. Software encoding can use
substantially more CPU; fallback availability does not guarantee real-time speed.

Unset `go2rtc.vaapi_device` discovers `/dev/dri/renderD*`; set it to select a particular
render node. Hardware decoding is the default. `go2rtc.vaapi_decode: false` (or
`BRIDGE_GO2RTC_VAAPI_DECODE=false`) explicitly skips GPU decoding for troubleshooting,
while still trying hardware encoding before software. The service's render/video
supplementary groups permit device access. Logs beginning `[bridge-ffmpeg]` identify
the attempted path and any downgrade.

On the tested Atom x5-Z8350 with Debian 13, `i965-va-driver` advertised codec support
but GPU scaling failed. Installing `i965-va-driver-shaders` from Debian's `non-free`
component enabled VideoProc and passed concurrent live H.264/HEVC tests. Check
`vainfo --display drm --device /dev/dri/renderD128` and test the actual streams before
relying on the hardware path. Older Ivy Bridge hosts cannot decode HEVC through
VA-API and will fall back to CPU decoding while retaining hardware encoding.
See [the Atom tests and migration evidence](evidence/atom-bridge-migration-2026-10-08.md).

## Live timing
`go2rtc.max_height` defaults to zero, preserving source dimensions. A nonzero
value caps transcoded height while preserving aspect ratio; streams shorter than
the cap stay at their native size. Select a ceiling verified with all displays
and all concurrently playing tiles. Codec capability declarations can differ
from actual portrait-stream support, so successful playback must verify the
decoded dimensions and frame delivery. This option does not change the camera's
source quality setting.

The camera API and WebSocket `hello` advertise `rtspTcpPacketSize: 8192`.
TCP clients can request `?pkt_size=8192` on the reported go2rtc stream URL.
This repacketizes the existing compressed video for that consumer; it does not
resize, re-encode, change frame rate or add a playback buffer. The ordinary
`rtsp` URL stays unchanged for clients using UDP, where larger packets can exceed
the network MTU. Updated Linux clients discover this hint automatically and
preserve explicit URLs unless the operator opts into packet-size tuning.
See [the measured Pi CPU bottleneck and same-camera delay](evidence/pi-garage-delay-2026-10-09.md).

The full VA-API decode path uses input `-thread_type:v slice`, retaining automatic
thread count and parallel work within a frame where supported. On the tested Atom, FFmpeg
frame threading held at least four future frames before delivering decoded
output; slice-only threading removed that hold while retaining hardware acceleration.
Software decoder fallback retains its normal threading. Encoder processing depth
and client buffering are unchanged. See [the stage measurements and limits](evidence/firetv-latency-investigation-2026-10-09.md).

The resized Linux VA-API preset timestamps raw HTTP input by arrival time and preserves those
timestamps through H.264 encoding. Raw Annex-B has no container PTS; synthesizing time from its
nominal frame rate caused the outgoing media clock to run ahead of delivery on the tested Garage
stream. Startup analysis is limited to 100 ms / 256 KiB while retaining the probed reference frames.
These limits do not bound camera buffering or time waiting for a live keyframe.

To compare outgoing video clock progression with arrival time:

```sh
python3 server/scripts/measure-rtsp-clock.py rtsp://<server>:8554/<stream-key>
```

A ratio near one verifies clock rate, not motion-to-screen latency. Compare a real movement with
the display to check a constant delay. See [the measurements and limits](evidence/live-timing-2026-10-08.md).

## Camera settings the wall depends on
- Streaming quality: set the device's quality in the eufy app when needed. The bridge reports the live
  source `codec` in `/api/cameras`; a wall tile must declare `codec: h265` if its RTSP output is HEVC.
- Dual-lens (E340 doorbell/floodlight, S340): the bridge sends `dual_view` only when configured for that
  camera or under `defaults`. It does not change a device setting on an unset value.
  The TV setup screen and Linux `-view-camera` command can change one camera at a time through
  `POST /api/cameras/<serial>/view?mode=split|pip-tl|pip-tr|pip-bl|pip-br|single`. The bridge stores these
  overrides in `/var/lib/eufy-wall-bridge/dual-view-modes.json`, reapplies them after a restart, and
  reports the chosen `dualView` in `/api/cameras`. Delete that JSON entry to return to YAML/app settings.
  The camera composes the image before transmission, so switching modes adds no transcoding load.
  RTSP consumers reconnect when its resolution or aspect changes; the camera's P2P feed stays open.
  On the tested T8214 Front Door,
  `pip-br` showed both lenses with the lower view inset. On the tested T8425 Garage, PiP commands were
  accepted but the live frame showed only the main lens; use `split` to keep both Garage lenses visible.
- The bridge explicitly requests `streamType: 2` on every camera pull. The SDK forwards it as the
  wire field `streamtype` on starts and retries while retaining its existing defaults for other callers.
  The HomeBase default value `1` caused a
  T8214 Split feed to become PiP during motion, including in the phone app while the bridge was open.
  See [the measured diagnosis](dual-view-stream-selection.md). The bridge and its SDK dependency carry
  the explicit choice, so Android TV and Linux clients receive the corrected stream without an app update.
- A battery device reports charging without proving that its external input can support continuous video.
  The SDK keeps its battery budget by default. For a camera whose installation supports a persistent
  stream, set `cameras.<sn>.power_override: always-on` in the bridge YAML. This records a bridge-side
  installation choice. The bridge passes `powered: "wired"` on each media pull and makes `always` the
  default mode. It sends no command to the camera. The station's idle P2P policy still follows the SDK's
  device classification. `mode: always` on a battery-budgeted
  camera requires that claim; the bridge refuses the conflicting configuration. `/api/cameras` reports
  `powerOverride`, `powered`, and `mode` so the decision is visible.

## Force-LAN
Set the bridge YAML to your local subnet, then restart the service:

```yaml
lan:
  cidr: 192.168.1.0/24
  force: true
```

This is a bridge-wide policy covering station control and per-camera media sessions.
It persists in `/etc/eufy-wall-bridge.yaml`; installer updates preserve that file.
If a local connection fails, the bridge retries without accepting a WAN/relay peer.

`lan.force: true` asks the SDK to reject peers outside `lan.cidr` before connecting a control or media
session. Cloud broker lookup remains available. The bridge also closes any connected session whose
peer is outside that CIDR and marks the camera
`blocked: wan-path <ip>` in /healthz and /api/cameras (HTTP 423 on /stream). The stream manager retries
with backoff. Cloud authentication and lookup may still generate internet traffic; an outside-subnet
UDP capture alone does not identify camera media. Verify connected peer addresses and distinguish
P2P data traffic from cloud lookup replies when checking a packet capture.
If a station cannot establish a local connection, add its LAN IP under `lan.station_addresses`.

## Security (LAN-trust model)
Nothing on the bridge is authenticated: `:3000` (HTTP /stream, /api, /auth) and `:8554` (RTSP) trust
every host on the LAN, so run it on a network you control (a VLAN with the TVs is ideal) and never
port-forward it. The go2rtc API is pinned to `127.0.0.1:1984` and its WebRTC listener is removed by
the bridge when it writes go2rtc.yaml (the upstream generator would open both on all interfaces).
Credentials live only in `/etc/eufy-wall-bridge.env` (mode 600) and the session token in
`/var/lib/eufy-wall-bridge/`.

## Robustness behaviour
- 12 s without video bytes → feed restarted with backoff 2→60 s (`stalls` counter in /healthz).
- 45 s gap → HTTP consumers (go2rtc) disconnected so they reconnect cleanly.
- 5 min continuous failure on any camera → process exit(1); systemd restarts it (`Restart=always`).
- go2rtc child dies → restarted after 3 s.
- Cloud poll silent ≥ 30 min or push down ≥ 15 min → re-login in place, else exit(1).

## Upgrading
- From the Mac checkout used for this installation: `deploy/update-debian.sh`. It syncs `server/`
  and `deploy/`, reruns the idempotent installer, restarts an already-running service, and checks
  `/healthz`. Server credentials, camera config, and session live outside the code directory and
  are preserved. Use an SSH key in `~/.ssh/eufy-wall-debian` or set `EUFY_WALL_SSH_KEY`.
  The current default target is `root@192.168.23.158`; pass a different SSH host as the first
  argument to update another installation.
- On DietPi, the installer adds `+ eufy-wall-bridge` to
  `/boot/dietpi/.dietpi-services_include_exclude`. Verify with `dietpi-services status`;
  the bridge then appears in the service menu alongside native DietPi services.
- SDK: update the `eufy-wall` Git dependency in `server/package-lock.json`, run `npm ci && npm test`
  (the contract test checks the installed SDK surface), then rerun the install script.
- Vendored ha-eufy-sdk-bridge modules: `server/scripts/sync-upstream.sh` shows diffs; `--apply` copies;
  update the SHA in server/src/vendor/ha-bridge/VENDOR.md.

## Verified devices (fill in from spike A)
| sn | model | power | codec | WxH | dual view | p2p peer |
|----|-------|-------|-------|-----|-----------|----------|
