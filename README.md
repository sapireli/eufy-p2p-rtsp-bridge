# eufy-p2p-rtsp-bridge

Eufy cameras → RTSP (server) → grid on a Raspberry Pi's HDMI (client). No Docker.

- `server/` — Node 24 bridge on [`@mega-yfue/eufy-sdk`](https://github.com/mega-yfue/eufy-sdk) with go2rtc for RTSP. Runbook: `docs/runbook-server.md`.
- `client/` — Go `eufy-wall`: layout → one GStreamer pipeline → KMS. Runbook: `docs/runbook-client.md`.
- Design: `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`.

## Upstream
- `@mega-yfue/eufy-sdk` is pinned to the fork's `eufy-wall` branch in `server/package-lock.json`. The contract test in `server/test/sdk-contract.test.mjs` checks the installed API and P2P fixes when that pin moves. SDK PRs #279 and #282 are merged into beta; #235 and #280 remain under review. Moving the pin to upstream `main` depends on those changes reaching `main`. The bridge's power choice uses the SDK's existing per-pull media option.
- Modules vendored from [`ha-eufy-sdk-bridge`](https://github.com/mega-yfue/ha-eufy-sdk-bridge) live in `server/src/vendor/ha-bridge/` (see `VENDOR.md`); the weekly `upstream-sync` workflow opens a PR when they drift. Locally: `server/scripts/sync-upstream.sh [--check|--apply]`.

## CI
`ci.yml` runs `npm test` (server), `gofmt`/`go vet`/`go test -race`/cross-builds (client), and `bash -n` on the deploy scripts.
