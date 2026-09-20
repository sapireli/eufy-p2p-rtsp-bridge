# Vendored from mega-yfue/ha-eufy-sdk-bridge

- Upstream: https://github.com/mega-yfue/ha-eufy-sdk-bridge
- Commit: 1154026d179ef523e28d75a4554844f055549cba
- License: Apache-2.0 (see upstream LICENSE). Copyright the ha-eufy-sdk-bridge authors.
- Files (verbatim, DO NOT EDIT — wrap them from ../../*.mjs instead):
  - go2rtc-config.mjs  ← upstream go2rtc-config.mjs  (go2rtc.yaml generator)
  - auth.mjs           ← upstream src/auth.mjs        (login / 2FA / captcha / re-auth state machine)
  - watchdog.mjs       ← upstream src/watchdog.mjs    (poll + push liveness watchdog)

- Not vendored: upstream's `streams.mjs` (a client per camera). This bridge uses ONE logged-in client
  for control and every stream — the SDK opens a media session per camera on demand, and multiple
  logins on one account/identity displace each other's cloud session, which breaks the DSK/cipher
  lookups a P2P connect needs. See `src/sdk-adapter.mjs`.

Update with: `server/scripts/sync-upstream.sh` (prints a diff per file; review, copy, bump the SHA here).
