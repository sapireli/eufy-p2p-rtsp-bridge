# Device silence is not poll-loop failure

## Observed interruption

At **19:57:17.039457 UTC on 12 October 2026**, the live bridge logged
`realtime stalled (poll idle 1800s) — re-establishing`. Its watchdog then called
`eufy.disconnect()`: all three selected feeds stopped at 17.062126–17.065402.
Authentication was restored at 17.110854 and streaming announcements followed
at 18.514140, 18.626127 and 19.270454. Both TVs recreated their players at this
boundary without an application installation or player-policy intervention.

This is a witnessed watchdog-triggered media interruption. It does **not** prove
that the preceding cloud polls succeeded: the old code did not observe their
completion. Do not attribute this particular reset to a natural peer END or to
the simultaneously started passive observer.

Private journal artifact:
`.evidence/frame-freshness-2026-10-12/bridge-recreate-boundary-journal.txt`;
SHA-256 `19c7d0e9f71287735b0ef51274bdc5041cfac5b3fc75f69ac0ee5438c8b61a05`.
[Fire TV exact-frame capture](fire-tv-rtp-read-stage-2026-10-12.md) records the
player boundary and keeps counter epochs separate.

## Contract defect

The bridge subscribed to `deviceState` as if it were a heartbeat emitted by every
healthy cloud poll. In SDK `a9424bf`, `pollOnce()` emits that event only for
`diff.reported`: a device whose last-seen timestamp advanced. The SDK regression
`does not fire when last-seen didn't advance` explicitly verifies silence after
successful unchanged polling. Missing device reports therefore cannot diagnose
a hung poll loop. The old watchdog also treated configured `pollMs: 0` as the
default interval through `||`, creating a timeout for deliberately disabled work.

## Correction and gates

The SDK companion change supplies typed, metadata-only `pollComplete` statuses
(`ok`, `partial`, `error`) for settled passes. A settled error still lets the loop
rearm; it is not a hung promise. Authentication expiration retains the existing
SDK error/session-recovery path. The bridge now tracks that completion event,
removes the `deviceState` heartbeat subscription, and disables the poll-idle
deadline when polling is disabled. Push connection recovery remains separate.

The new six-case bridge regression verifies unchanged successful completion,
settled partial/error completion, actually missing completion, device-report
separation, disabled polling, and push-down recovery with disabled polling.
Against the unmodified watchdog, three cases fail: unchanged success, settled
partial/error, and disabled polling. The candidate passes all six. The complete
bridge suite passes **195 tests**. Tests use a controlled clock; they do not
substitute for a sustained live deployment.

The health endpoint also exposes a count, last completion time and last status
under `poll`. These are observations of settled loop passes, not a synthesized
camera last-seen or capture-age signal. They allow the live contract to be checked
without attaching an Inspector or changing the poll interval.

## Verification limit at this checkpoint

The correction is staged, not deployed. The SDK event's complete tests and
companion upstream evidence must accompany its eventual pinned dependency.
Deployment must preserve camera settings and wait for the active passive capture
window. Live proof must show poll completion reaching the bridge and no false
poll-idle reset beyond the original 30-minute boundary. This change does not
claim to fix the distinct short source pauses or per-receiver TCP delivery stalls.
