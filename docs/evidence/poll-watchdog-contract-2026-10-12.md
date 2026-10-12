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

## Later deployed checkpoint

The original staged checkpoint is superseded by the deployed SDK `830fa1c`,
bundle `b23b4ddaa58ad4669cd3a528de71f9570c3f14ffe7609062820be822c281b83b`,
and the host integration. PID 26350 began at 20:12:31 UTC and remained alive
through all 366 five-second health samples 20:15:24.483332–20:45:49.483455.
Actual settled `ok` poll completions reached the bridge at 20:22:43.317,
20:32:44.432, and20:42:45.314. Authentication, session, go2rtc and stall indicators
were healthy at every sample. The bounded 109-row service journal through 20:47:00
contains no `realtime stalled` event. This passes the original 30-minute watchdog
boundary with the same process; all 155 dist files plus package.json match the
built SDK package. The rollback timer was canceled only after the first observed
completion and healthy checks.

This is not uninterrupted-media proof. One source independently stopped at
20:24:52.150943, reopened at 20:24:52.154226, and resumed streaming at
20:24:55.015341. All three sources appear in 365/366 health samples; one sample during
that reopen has two. Its cause is not established by this record. No false poll-idle
recovery or process restart accompanied it. Separate short source pauses and
receiver-side issues remain distinct.

Private health artifact `.evidence/frame-freshness-2026-10-12/poll-completion-live-1830s.jsonl`,
SHA-256 `ea25fe47ef54a0227a1b49850315f8e4dbf31a93d7eaec3f92f94767203dbc60`.
Bounded journal `poll-completion-live-journal.jsonl` in the same directory,
SHA-256 `3f90289f85d63a514b67c7220bc843f1210fd9f79b2b5471d426fd83687f2ec2`.
[The SDK change index](../sdk-upstream-change-index-2026-10-12.md) points to the
independent upstream handoff; no upstream PR or clean upstream-base validation
is implied by this fork/live gate.
