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
    curl -s localhost:3000/api/cameras | jq      # codec must be "h264" for Pi clients
    ffplay rtsp://<server>:8554/<sn>             # from any machine on the LAN

## Camera settings the wall depends on
- Streaming quality: set the device's quality in the eufy app when needed. The bridge reports the live
  `codec` in `/api/cameras`; a wall tile must declare `codec: h265` if that stream is HEVC.
- Dual-lens (E340 doorbell/floodlight, S340): the bridge sends `dual_view` only when configured for that
  camera or under `defaults`. It does not change a device setting on an unset value.
- A battery device reports charging without proving that its external input can support continuous video.
  The SDK keeps its battery budget by default. For a camera whose installation supports a persistent
  stream, set `cameras.<sn>.power_override: always-on` in the bridge YAML. This records a local SDK claim,
  sends no command to the camera, and makes `always` the default mode. `mode: always` on a battery-budgeted
  camera requires that claim; the bridge refuses the conflicting configuration. `/api/cameras` reports
  `powerOverride`, `powered`, and `mode` so the decision is visible.

## Force-LAN
`lan.force: true` asks the SDK to reject non-private IPv4 peers before connecting. The bridge closes any
connected control or media session whose peer is outside `lan.cidr` and marks the camera
`blocked: wan-path <ip>` in /healthz and /api/cameras (HTTP 423 on /stream). The stream manager retries
with backoff. Verify with: `sudo tcpdump -ni <iface> udp and not net <lan.cidr>` — no sustained traffic.
If a station keeps connecting via WAN, add its LAN IP under `lan.station_addresses`.

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
- SDK: update the `eufy-wall` Git dependency in `server/package-lock.json`, run `npm ci && npm test`
  (the contract test checks the installed SDK surface), then rerun the install script.
- Vendored ha-eufy-sdk-bridge modules: `server/scripts/sync-upstream.sh` shows diffs; `--apply` copies;
  update the SHA in server/src/vendor/ha-bridge/VENDOR.md.

## Verified devices (fill in from spike A)
| sn | model | power | codec | WxH | dual view | p2p peer |
|----|-------|-------|-------|-----|-----------|----------|
