# eufy-p2p-rtsp-bridge

Eufy cameras → RTSP (server) → grid on a Raspberry Pi's HDMI (client). No Docker.

- `server/` — Node 24 bridge on [`@mega-yfue/eufy-sdk`](https://github.com/mega-yfue/eufy-sdk) with go2rtc for RTSP. Runbook: `docs/runbook-server.md`.
- `client/` — Go `eufy-wall`: layout → one GStreamer pipeline → KMS. Runbook: `docs/runbook-client.md`.
- Design: `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`.

## Upstream
- `@mega-yfue/eufy-sdk` is pinned to the fork's `eufy-wall` branch in `server/package-lock.json`. The contract test in `server/test/sdk-contract.test.mjs` checks the installed API and P2P fixes when that pin moves. The upstream `main` pin depends on SDK PRs #235, #279, #280, #282, and [#284](https://github.com/mega-yfue/eufy-sdk/pull/284) reaching `main`; the bridge's power choice already uses per-pull media options and #284's station idle policy.
- Modules vendored from [`ha-eufy-sdk-bridge`](https://github.com/mega-yfue/ha-eufy-sdk-bridge) live in `server/src/vendor/ha-bridge/` (see `VENDOR.md`); the weekly `upstream-sync` workflow opens a PR when they drift. Locally: `server/scripts/sync-upstream.sh [--check|--apply]`.

## CI
`ci.yml` runs `npm test` (server), `gofmt`/`go vet`/`go test -race`/cross-builds (client), and `bash -n` on the deploy scripts.
