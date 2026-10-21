# Rebased SDK deployment — 2026-10-21

## Runtime identity

The live bridge on `homebridge.lan` was updated at 04:15:20 UTC to SDK `eufy-wall` commit `9f56bc199eebaec6025c9b16171f7869508ef0e4`.

The SDK was built off-device from a clean detached worktree. The deployed `dist/index.js` SHA-256 is `326643d70f5926439bfbdb1c1dfc022e4ff4eab3dd4657fdfd19ac6123f22ded`. The root and installed dependency locks and `/opt/eufy-wall-bridge/sdk-revision` identify that commit. The previous SDK, bridge adapter, freshness module, stream manager and locks are retained at `/opt/eufy-wall-bridge/sdk-upgrade-20261021-9f56bc1/backup`.

## Required bridge migration

The rebased fork removed `receiptMetadata` and its native monotonic receipt envelope. Its supported replacement is `arrivalTime: true`, yielding `{ frame, timestampMs }`. The timestamp is captured with `Date.now()` at original shared-source arrival and survives the SDK queue.

The bridge requests that envelope and projects its original age into the bridge monotonic clock before fanout. A three-second-old queued picture remains three seconds old. Repeated delivery of the same picture keeps its receipt identity. A local sequence distinguishes different pictures arriving in the same millisecond. Invalid/future arrival timestamps and wall-versus-monotonic clock steps exceeding the configured freshness budget are rejected.

These projected IDs and sequences are bridge-local. They are not the removed SDK source identity or original source sequence. Arrival projection assumes a stable host wall clock and has millisecond timestamp precision; small clock corrections within the configured budget can affect the estimate. This does not establish sensor capture time. The deployment retains observation mode, hardware-first/software-fallback policy, stream selection, quality, and output dimensions.

The old opt-in SDK gap diagnostic depends on removed private receipt identity and cannot supply the former source-to-slot correlation with this SDK. It was disabled in the running service; no diagnostic provenance from that observer is claimed here.

## Verification

- Clean SDK `npm run verify`: 4,414 tests across 252 files, formatting, typechecking, architecture guards, build, ESM import, 98 snippets and examples passed.
- Bridge: all 259 tests passed against that exact built SDK. Added cases cover queued age, repeated-picture identity, same-millisecond frame advancement, malformed/future arrival values, and host-clock steps.
- Service PID 151200 remained active with zero automatic restarts; the kernel journal contained no GPU hang/reset entries after deployment at the time checked.
- All three Fire TVs reported rendered pictures for Front Door, Garage CLE, Balcony and Wyze after reconnecting. Front Door and Garage CLE used hardware decoders; latest sampled compressed queues were zero for all four feeds.
- Seven API samples over 30.14 seconds (04:17:19–04:17:49 UTC) kept Front Door, Garage CLE, Balcony and Wyze live. Output counts advanced by 425, 451 and 754 for Front Door, Garage CLE and Balcony respectively. Their SDK arrival ages in the final sample were 234.877, 79.512 and 44.344 ms. These are source-arrival estimates, not capture-to-display latency measurements.

Garage CLE needed about 80 seconds and several existing startup retries before first media. Garage Doorbell continued to report SDK startup timeouts during this window; no successful playback is claimed for it. GaragePi's display was off, so this deployment did not verify physical Linux presentation. The short deployment check is not a long-duration playback guarantee.

Private observation file: `/tmp/eufy-sdk-9f56bc1-sustained.json`. Deployment manifest: `/opt/eufy-wall-bridge/sdk-upgrade-20261021-9f56bc1/manifest.json`.
