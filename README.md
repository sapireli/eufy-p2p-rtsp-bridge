# eufy-p2p-rtsp-bridge

Eufy cameras → RTSP bridge → display wall on Raspberry Pi, Debian, or macOS. No Docker.

- `server/` — Node 24 `eufy-bridge` with [`@mega-yfue/eufy-sdk`](https://github.com/mega-yfue/eufy-sdk), go2rtc, an integrated setup wizard, and YAML config commands. [Server runbook](docs/runbook-server.md).
- `client/` — Go `eufy-wall` with an integrated setup wizard, terminal 32×32 layout editor, YAML config commands, and per-tile GStreamer recovery. [Linux and Pi runbook](docs/runbook-client.md); [macOS runbook](docs/runbook-client-macos.md).
- Design: `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`.
- [Plug-and-play plan and remaining release gates](docs/plug-and-play-setup-plan.md).

Both binaries can generate, validate, explain, and apply YAML. The interactive paths are optional: see the [server config reference](docs/config-server.md), [client config reference](docs/config-client.md), and [layout reference](docs/layouts.md) to write a config directly. `config validate -` and `config apply -` read stdin, including over SSH. There is no HTTP config upload endpoint.

Release artifacts and installers are implemented, but physical Pi/Debian display qualification, real Eufy camera recovery trials, clean-host installation, and macOS signing/notarization remain open. The runbooks distinguish tested behavior from unverified hardware profiles.

## Upstream
- `@mega-yfue/eufy-sdk` is pinned to the fork's `eufy-wall` branch in `server/package-lock.json`. The contract test in `server/test/sdk-contract.test.mjs` checks the installed API and the P2P fixes when that pin moves.
- Modules vendored from [`ha-eufy-sdk-bridge`](https://github.com/mega-yfue/ha-eufy-sdk-bridge) live in `server/src/vendor/ha-bridge/` (see `VENDOR.md`); the weekly `upstream-sync` workflow opens a PR when they drift. Locally: `server/scripts/sync-upstream.sh [--check|--apply]`.

## CI
`ci.yml` runs server tests, Go race tests and cross-builds, installer tests, and syntax checks. Its first-party server and aggregate Go coverage gates each require more than 80%.
