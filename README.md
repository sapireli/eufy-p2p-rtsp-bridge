# eufy-p2p-rtsp-bridge

Eufy cameras → RTSP (server) → grid on a Raspberry Pi's HDMI (client). No Docker.

- `server/` — Node 24 bridge on [`@mega-yfue/eufy-sdk`](https://github.com/mega-yfue/eufy-sdk) with go2rtc for RTSP. Runbook: `docs/runbook-server.md`.
- `client/` — Go `eufy-wall`: independent hardware-decoded tiles on KMS overlays, or a shared compositor. It discovers bridge TCP packet-size hints to reduce RTSP processing overhead without changing video resolution. Runbook: `docs/runbook-client.md`.

The optional [frame freshness protocol](docs/frame-freshness.md) follows each
picture from bridge ingress through encoding to client output. Camera capture
age is unverified; the protocol does not infer it from camera clock overlays.
- Design: `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`.

## Upstream

- Managed live-start selector: [SDK PR evidence and handoff](docs/sdk-streamtype-pr-handoff.md),
  with [sanitized motion traces](docs/evidence/streamtype-motion-2026-10-07.json).
- `@mega-yfue/eufy-sdk` is pinned to the fork's `eufy-wall` branch in `server/package-lock.json`. The contract test in `server/test/sdk-contract.test.mjs` checks the installed API and P2P fixes when that pin moves. SDK PRs #279 and #282 are merged into beta; #235 was closed without merging, and #280 remains under review. The fork retains #235's active-stream teardown and #280's peer-selection callback, so the bridge cannot move to upstream `main` while it requires those behaviors. The bridge's power choice uses the SDK's existing per-pull media option.
- Modules vendored from [`ha-eufy-sdk-bridge`](https://github.com/mega-yfue/ha-eufy-sdk-bridge) live in `server/src/vendor/ha-bridge/` (see `VENDOR.md`); the weekly `upstream-sync` workflow opens a PR when they drift. Locally: `server/scripts/sync-upstream.sh [--check|--apply]`.

## CI
`ci.yml` runs `npm test` (server), `gofmt`/`go vet`/`go test -race`/cross-builds (client), and `bash -n` on the deploy scripts.

### go2rtc deployment default

Server installation and ordinary updates use pinned go2rtc 1.9.14 with the verified
TCP framing repair. A failed or incomplete write closes only that connection,
preventing later packets from continuing a damaged stream. There is no experimental
enable flag. Updates build or verify the cache before installing atomically. See
the [server runbook](docs/runbook-server.md) and
[writer decision and live evidence](docs/evidence/go2rtc-writer-decision-2026-10-12.md).
