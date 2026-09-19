# eufy camera wall — Phase 1: wired always-on bridge + Pi HDMI grid

## Context

Goal: show eufy camera livestreams on TVs via headless Raspberry Pis (and optionally x86 Atom boxes) in grid
layouts (1, 2x2, 3x3, 1+5). One small Debian server (arm64 or x86) runs the eufy bridge and offers RTSP; each
TV has a thin display client. **Scope decision: wired/powered cameras, always-on only.** Battery cameras,
motion-triggered streaming and on-demand holds are explicitly Phase 2 (design sketched at the end so nothing
built now blocks it).

Research conclusions (repos cloned in the session scratchpad for reference):

- **No native Go/Rust eufy P2P client exists**; every working implementation is Node. Bropat's
  `eufy-security-client` is unmaintained; the successor is **`@mega-yfue/eufy-sdk`** (clean-room, Apache-2.0,
  Node ≥24.5, very active). We build on it exclusively.
- **`mega-yfue/ha-eufy-sdk-bridge`** (same author) is most of the server already: login once, `/stream/<sn>`
  Annex-B over HTTP, bundled **go2rtc** → RTSP, 2FA/captcha handling, poll/push watchdog, and the essential
  **session-per-streaming-camera workaround** (`streams.mjs`: a HomeBase tags all media channel 0 so two
  cameras on one P2P session arrive byte-identical; 5 concurrent cameras measured OK at ~4.4 Mbps).
  Wired cameras stream unbounded in the SDK, so always-on needs no budget/hold logic. What it lacks for us:
  force-LAN, pinning quality/codec, pinning dual-lens view mode, a media stall watchdog.
- **`slu125/eufy-frigate-bridge`**: robustness patterns to port (stall watchdog 12 s, exit-after-stall 300 s
  so the supervisor restarts, lock `videoStreamingQuality` because "Auto" flips resolution mid-stream).
  `eufy-local-control` / `EufyView`: nothing reusable.
- **Codec**: the camera's quality setting decides — 2K → H.265, 1080p/720p → H.264. `videoStreamingQuality`
  is writable through the SDK. Pi 3 (and Pi 1) have no HEVC hardware → server pins an H.264 quality.
- **Pi 1 cannot host the server** (ARMv6: Node 24 unofficial builds only, ~200 MB RSS on one 700 MHz
  core) but **can be a display client** once streams are H.264: it has the same VideoCore IV decoder and HVS
  compositor as the Pi 3, and the client is a Go static binary (`GOARM=6`) + GStreamer (`v4l2h264dec`,
  `kmssink`), both available on 32-bit Pi OS through Trixie. Its limit is the single slow core handling
  RTSP depay for N streams and the shared decode block (~4×720p or ~9×SD): realistic layouts are `1`/`2x2`
  at 720p, `1+5` only with secondaries at Low quality. Targets: **Pi 3 first, Pi 1 and x86 Atom second**;
  same client, different decoder element / quality budget.
- **Force-LAN**: eufy-sdk has `localAddresses` + `noBroadcast` but no local-only mode; `sendLookups()`
  (`src/transport/p2p/p2p-session.ts:754`) always also sends the PPCS cloud lookup, and whichever path answers
  `CHECK_CAM` first wins. It exposes `eufy.getP2pSessions()` / `p2pConnect`, and the session holds the peer
  `connectAddress`.
- **Dual-lens devices** (Doorbell E340 T8214, Floodlight E340 T8425, S340) send **one composited stream**;
  composition is a writable device setting `CMD_DOORBELL_DUAL_VIEW_MODE` 2700 / `_MODE2` 6243 (E340):
  `12` = split-view (stacked, taller than wide), `2–5` = PiP corners, `0` = single (S340). eufy-sdk has the
  command IDs in `src/transport/p2p/commands.ts` but no capability wrapper. Decision: **pin mode 12**, treat
  as one *tall* tile. Open eufy-sdk issue #200 (T8214 livestream fails) must be verified in the spike.

## Architecture

```
eufy cloud (login, FCM)      ┌────────── server: eufy-wall-bridge (Node 24) + go2rtc ──────────┐
HomeBase / wired cams (LAN) ─▶│ sdk-adapter → stream-manager → HTTP /stream/<sn> (Annex-B) → go2rtc → RTSP :8554 │
                              │ lan-guard · pins (quality, dual view) · stall watchdog · /healthz · /api/cameras   │
                              └──────────────────────────────────────────────────────────────────────────────────┘
                                                        │ RTSP (H.264), one connection per tile
                              ┌───────── client: eufy-wall (Go, static binary) on Pi 3 / x86, ×N TVs ─────────┐
                              │ config.yaml → layout engine (spans) → generates ONE GStreamer pipeline →      │
                              │ spawns gst-launch-1.0 (v4l2h264dec → kmssink planes) → supervises/restarts    │
                              └──────────────────────────────────────────────────────────────────────────────┘
```

Monorepo (directory is empty; `git init` first):

```
eufy-p2p-rtsp-bridge/
  server/     Node 24 ESM: @mega-yfue/eufy-sdk + go2rtc binary; systemd unit + install script
  client/     Go 1.23 `eufy-wall`: layout engine + pipeline generator + supervisor (no cgo)
  deploy/     install scripts, systemd units, Pi setup notes (no Docker anywhere)
  docs/       config reference, runbook (first-run 2FA/captcha, adding cameras), phase-2 design
```

### Server: `eufy-wall-bridge`

Own thin service on `@mega-yfue/eufy-sdk`, vendoring the proven `ha-eufy-sdk-bridge` modules.

**Keeping upstream updates cheap**
- `@mega-yfue/eufy-sdk` is a pinned exact-version npm dependency, never vendored. Every SDK call goes through
  `server/src/sdk-adapter.mjs` (login/2FA/captcha, device list, `openReadable`, `setProperty`, raw command
  send, `getP2pSessions`, events) so an SDK API change touches one file. `server/test/sdk-contract.test.mjs`
  asserts the exports/signatures we depend on, so a version bump fails loudly rather than at runtime.
- `ha-eufy-sdk-bridge` is `"private": true` (not on npm) → copy needed modules **unmodified** into
  `server/src/vendor/ha-bridge/` with `VENDOR.md` (upstream commit SHA, Apache-2.0 notice):
  `streams.mjs`, `go2rtc-config.mjs`, `src/auth.mjs`, `src/watchdog.mjs`, and the `/stream` handler pattern
  from `src/http-routes.mjs:101-135`. `scripts/sync-upstream.sh` clones upstream HEAD and diffs it against the
  vendored copies so updating is a reviewed merge. All our logic lives outside `vendor/` and wraps it.
- Send upstream PRs for the generic pieces (SDK: `p2pLocalOnly` option, `dualCamViewMode` capability;
  bridge: media stall watchdog) to shrink our diff over time.

**Modules (`server/src/`)**
1. `config.mjs` — `config.yaml` + env overrides:
   ```yaml
   eufy: { email: …, password: …, country: US }      # dedicated account (eufy allows one login/account)
   lan: { cidr: 192.168.1.0/24, force: true, station_addresses: { T8010XXXX: 192.168.1.50 } }
   defaults: { quality: "Full HD (1080P)" }           # any H.264-yielding value from the SDK property manifest
   cameras:
     T8410XXXX: { name: Garage }
     T8214XXXX: { name: Door, dual_view: split }       # split | pip-tl | pip-tr | pip-bl | pip-br | single
     T8113XXXX: { enabled: false }                    # battery cam: skipped in Phase 1
   ```
   Cameras not listed are included if wired (`!dev.has("battery")`), excluded if battery, with a log line.
2. `pins.mjs` — at boot and on device (re)appearance: write `videoStreamingQuality` if it differs; for
   dual-lens models send raw command 6243 (E340) / 2700 (others) with the configured mode (default 12).
   Re-apply after re-login/watchdog recovery. Sniff the codec from the first Annex-B NALs (SDK
   `sniffAnnexbCodec`); expose `codec` per camera; loud warning if still H.265.
3. `stream-manager.mjs` — wraps vendored `streamClientFor(sn)` + `cam.openReadable()`. Always-on: keeps an
   internal null consumer per enabled camera so the P2P pull stays warm and an RTSP client gets video
   instantly (SDK primes late joiners with the cached IDR). Media stall watchdog ported from
   eufy-frigate-bridge: no bytes 12 s → destroy + reopen with backoff `[2,4,8,15,30,60]` s; 300 s of continuous
   failure → `process.exit(1)` for systemd restart.
4. `lan-guard.mjs` — pass `localAddresses` (from `lan.station_addresses`) to the SDK; on `p2pConnect`, read
   the session's connect address from `getP2pSessions()`; if `lan.force` and it is outside `lan.cidr`, close
   the session, mark the camera `blocked: wan-path`, log, and retry with backoff. Surfaced in `/healthz` and
   `/api/cameras`.
5. `http.mjs` — `GET /stream/<sn>` (Annex-B, `video/H264`; go2rtc pulls this), `GET /snapshot/<sn>`,
   `GET /healthz` (auth state, streaming set, blocked set, stalls, push/poll liveness), `GET /api/cameras`
   (sn, name, model, codec, width/height, dual, streaming, blocked, `rtsp://<host>:8554/<sn>`),
   `POST /auth/tfa?code=` and `POST /auth/captcha?code=` + `GET /auth/captcha` (image) for first-run.
6. go2rtc — config generated per camera (vendored generator): `ffmpeg:http://127.0.0.1:3000/stream/<sn>#video=copy`
   (remux only). RTSP at `rtsp://server:8554/<sn>`.
7. Ops — **no Docker.** `deploy/install-server.sh` for Debian (arm64/x86): installs Node 24 from the
   NodeSource apt repo (or the official tarball), downloads the matching go2rtc release binary into
   `/opt/eufy-wall-bridge/bin/`, `npm ci --omit=dev`, creates a `eufy-wall` system user, installs
   `deploy/eufy-wall-bridge.service` (`Restart=always`, `WorkingDirectory=/opt/eufy-wall-bridge`,
   `EnvironmentFile=/etc/eufy-wall-bridge.env`). The service spawns go2rtc as a child process with the
   generated config (as upstream's `bin/start.sh` does) so one unit manages both. Tokens in
   `/var/lib/eufy-wall-bridge/`, structured logs to journald, `/healthz` for monitoring.

### Client: `eufy-wall` (Go)

A ~300–500 line static binary (`GOARCH=arm GOARM=7` for 32-bit Pi OS, `arm64`, `amd64`), no cgo. It does
three things: compute the layout, generate a GStreamer pipeline string, and supervise `gst-launch-1.0`.

1. `internal/config` — `config.yaml`:
   ```yaml
   rtsp_base: rtsp://192.168.1.10:8554
   layout: 1+5                # 1 | 2x2 | 3x3 | 1+5
   primary_position: left     # 1+5: left | center
   screen: { width: 1920, height: 1080 }   # or auto from DRM mode
   decoder: auto              # auto | v4l2 (Pi) | va (x86 VAAPI) | software
   tiles:
     - { camera: T8410XXXX, role: primary }
     - { camera: T8214XXXX, aspect: tall }          # split-view dual-lens → 1×2 span
     - { camera: T8420XXXX }
     - { camera: T8411XXXX, span: { cols: 1, rows: 1 } }   # explicit override allowed
   ```
2. `internal/layout` — pure function `(layout, screen, tiles) → []PlacedTile{Rect, Camera}` over a cell grid
   (1x1, 2x2, 3x3; 1+5 is the 3x3 grid with the primary spanning 2×2 at left or centre-top). Rules: `tall`
   tiles get span 1×2; explicit `span` wins; fill remaining cells top-left → bottom-right; a tile that
   cannot fit its span falls back to 1×1 and letterboxes (`force-aspect-ratio=true` on the sink); surplus
   tiles are reported as an error at startup. Table-driven unit tests for every layout × with/without a
   tall tile.
3. `internal/pipeline` — renders one `gst-launch-1.0` command: per tile
   `rtspsrc location=… latency=200 protocols=tcp ! rtph264depay ! h264parse ! <dec> ! <sink>`.
   Pi: `<dec>=v4l2h264dec`, `<sink>=kmssink plane-id=<n> render-rectangle="<x,y,w,h>" force-aspect-ratio=true
   fd=<shared>` — each tile on its own DRM plane, composited by the VideoCore HVS (zero CPU copies).
   x86: `<dec>=vah264dec` (fallback `avdec_h264`) → `compositor` with `sink_N::xpos/ypos/width/height` →
   `kmssink`. Software fallback for debugging on a laptop: `avdec_h264 … ! compositor ! autovideosink`.
4. `internal/supervisor` — spawns the pipeline, restarts on exit with backoff (1→30 s), and kills/restarts it
   if the process logs no progress for N seconds (`GST_DEBUG=rtspsrc:4` heartbeat or a periodic `kmssink`
   `last-sample` check is Phase 2; Phase 1 relies on process exit + backoff). One pipeline means one bad
   RTSP source restarts all tiles — accepted for Phase 1; per-tile isolation is a Phase 2 item.
5. Ops — `deploy/eufy-wall.service` (`Restart=always`, user in `video`+`render` groups), Pi notes:
   Raspberry Pi OS Lite Bookworm/Trixie, `dtoverlay=vc4-kms-v3d`, `gpu_mem=128`, `hdmi_blanking=0`,
   no X/Wayland; packages `gstreamer1.0-tools gstreamer1.0-plugins-{base,good,bad}`.
   Makefile: `make pi1 (GOARM=6) | pi3 (GOARM=7) | pi64 | amd64`.

## Implementation phases

**Phase 0 — repo + spikes (de-risk first)**
- `git init`, skeleton, README with the diagram, `docs/phase-2-motion-battery.md` carrying the sketch below.
- Spike A (server, throwaway script under `server/spikes/`): eufy-sdk login → list cameras → set quality →
  `openReadable()` on each wired cam → write 10 s to files → `ffprobe` each: codec must be H.264, note
  width/height. On the E340(s): send view-mode 12, confirm the stream is one stacked frame and record its
  exact resolution/aspect (drives the `tall` span math). Confirm `getP2pSessions()` exposes the peer address.
- Spike B (Pi 3, throwaway `gst-launch-1.0` lines): serve 6 looping H.264 files via go2rtc on the server;
  test (a) 6× kmssink on separate planes with a shared `fd`, (b) single `compositor`, (c) `glvideomixer`.
  Record CPU, dropped frames, kernel version (6.6.x `h264_v4l2m2m` hang regression), `gpu_mem`. Pick (a)
  unless it fails; the pipeline generator is written against the winner. Repeat the winning pipeline on the
  **Pi 1** with 1, 4 and 6 streams at 720p and SD to find its ceiling and document the supported layouts.

**Phase 1 — server**: config, sdk-adapter + contract test, vendored modules + `VENDOR.md` + sync script,
`/stream`, go2rtc config, `/healthz`, `/api/cameras`, first-run auth endpoints. Verify: `ffplay rtsp://server:8554/<sn>`
for every wired camera.

**Phase 2 — server hardening**: pins (quality, dual view) with re-apply on recovery, codec sniff + warning,
always-on warm consumers, stall watchdog + exit-after-stall, lan-guard. Verify: 30-min soak with no restarts;
briefly power-cycle the HomeBase → streams recover; bogus `lan.cidr` → cameras report `blocked`.

**Phase 3 — client**: config, layout engine (tests), pipeline generator (golden-string tests per
layout/decoder), supervisor, systemd unit, cross-build. Verify on Pi 3 with 1+5 incl. the tall doorbell tile.

**Phase 4 — ops**: install scripts, systemd units, runbook, Pi image notes, upstream PRs
(`p2pLocalOnly`, dual-view capability, stall watchdog).

## Key files to reuse (scratchpad clones)

- `ha-eufy-sdk-bridge/streams.mjs` (session-per-camera), `go2rtc-config.mjs`, `src/auth.mjs`,
  `src/watchdog.mjs`, `src/http-routes.mjs:101-135` (`/stream` handler), `bin/start.sh` (spawning go2rtc alongside Node),
  `docs/docker-compose.md` (first-run 2FA/captcha flow).
- `eufy-sdk/docs/connectivity.md` (`localAddresses`, `noBroadcast`, `getP2pSessions`, `p2pConnect`),
  `docs/live-media.md` (`openReadable`, shared source, IDR priming), `src/transport/p2p/commands.ts`
  (2700 / 6243), `src/transport/p2p/annexb.ts` (`sniffAnnexbCodec`).
- `eufy-frigate-bridge/src/index.js` — watchdog thresholds, backoff table, `EUFY_STREAM_QUALITY` rationale.

## Risks

- Pi 3 decode budget ≈ 9×720p at ~14 fps aggregate; 1+5 at 1080p is over budget. Mitigation: per-camera
  `quality` (primary 1080p, secondaries 720p) — the server config supports it per camera.
- H.265-only camera models can't be shown on Pi 3 (x86 with VAAPI can). Server-side transcode is a possible
  later fallback, not built.
- DRM master/plane sharing across kmssink instances — settled by Spike B.
- eufy-sdk #200 (T8214 doorbell stream fails) — settled by Spike A.
- Single login per eufy account: use a dedicated account; the phone app on the same account evicts the bridge.
- Audio: out of scope (upstream go2rtc path is video-only too).

## Phase 2 sketch (not built now, kept compatible)

Battery cameras + motion: server adds a policy engine (`always | on_demand | on_motion` with holds), extends
the SDK 45 s battery budget only while held, emits `motion`/`streamState`/`hold` over a WS (`/ws`, same envelope
as ha-bridge). The client grows a WS listener and per-tile pipelines so a tile can swap between a snapshot
placeholder and live RTSP when the server reports a hold; layouts gain `motion: latest` dynamic tiles.
Nothing in Phase 1 (config shape, layout engine, RTSP naming, `/api/cameras`) needs to change for this.

## Verification (end-to-end)

1. Server up (`systemctl start eufy-wall-bridge`): `curl :3000/healthz` → `auth.state: ok`; `/api/cameras` lists
   each wired camera with `codec: H264` and the E340 with `dual: split` and its measured tall resolution.
2. `ffplay rtsp://server:8554/<sn>` plays each camera; 30-min soak with `/healthz` stalls = 0.
3. `tcpdump -ni eth0 udp and not net <lan.cidr>` on the server shows no media traffic with `lan.force: true`;
   with a bogus CIDR the cameras go `blocked` and no stream is served.
4. Pi 3: `eufy-wall` renders the configured layout; `top` CPU < 60 %; `kill` the gst process → restarts ≤ 5 s;
   unplug Ethernet 30 s → all tiles recover.
5. `go test ./...` (layout, pipeline golden strings) and `npm test` (config, pins, sdk contract) green.
