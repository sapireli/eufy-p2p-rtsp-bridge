# Vendored from mega-yfue/ha-eufy-sdk-bridge

- Upstream: https://github.com/mega-yfue/ha-eufy-sdk-bridge
- Commit: 1154026d179ef523e28d75a4554844f055549cba
- License: Apache-2.0 (see upstream LICENSE). Copyright the ha-eufy-sdk-bridge authors.
- Files (verbatim, DO NOT EDIT — wrap them from ../../*.mjs instead):
  - go2rtc-config.mjs  ← upstream go2rtc-config.mjs  (go2rtc.yaml generator)
  - auth.mjs           ← upstream src/auth.mjs        (login / 2FA / captcha / re-auth state machine)
  - watchdog.mjs       ← upstream src/watchdog.mjs    (poll + push liveness watchdog)

- Adapted (not verbatim, not synced by the script):
  - upstream streams.mjs → `src/stream-clients.mjs` (adds `localAddresses`, an `onClient` hook for the
    LAN guard, and in-flight de-duplication). When bumping the SHA, diff upstream `streams.mjs` by hand
    against it and port behavioural changes.

Update with: `server/scripts/sync-upstream.sh` (prints a diff per file; review, copy, bump the SHA here).
