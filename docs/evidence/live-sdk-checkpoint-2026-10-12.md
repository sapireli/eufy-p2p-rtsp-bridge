# Live SDK checkpoint — 12 October 2026

This is a deployment checkpoint, not a declaration that playback is fixed.

The bridge service is active, PID 23635, with `/healthz` reporting authenticated,
all three selected streams active, no reported stalls, and go2rtc running.
The installed scoped package is `@mega-yfue/eufy-sdk`; its bundled
`dist/index.js` SHA-256 is:

`48372212a4e16a45fd2601febc5239e21f503339303ad975e79aca78583cddb5`.

The installed lock resolves the fork to
`a9424bf9f92adc0a388aba66157763b4b45f24b2`. The stale `sdk-revision` deployment
marker was reconciled to that commit only after assertions checked both the
bundle hash and lock. This changed the marker, not the running process.
The original go2rtc release binary is restored, SHA-256:

`32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6`.

## SDK evidence and upstream scope

Each change is a separate SDK commit with its own companion Markdown evidence:

| Commit | Change | Evidence in the SDK checkout |
| --- | --- | --- |
| `55dacd3` | Linear packet reassembly rather than repeated growing copies | `docs/evidence/p2p-linear-reassembly-2026-10-12.md` |
| `55d3fea` | Dispose only the closing station session's owned sources | `docs/evidence/p2p-session-owned-teardown-2026-10-12.md` |
| `a9424bf` | Recognize valid END from the current connected peer and clean up its timers | `docs/evidence/p2p-peer-end-recovery-2026-10-12.md` |

The source commits are pushed on `eufy-wall`. No upstream PR has been opened.
[Peer-END and ownership handoff](../sdk-peer-end-pr-handoff.md) provides exact
live packet/journal intervals, artifact hashes, separate test scopes, and clean
upstream branch instructions. Linear reassembly has its own upstream instructions
in its SDK evidence file.

The natural active-peer END test demonstrates prompt close recognition and a
replacement VIDEO packet about 2.88 seconds later. It does not establish a
general jitter fix. The ownership fix has a failing-base/passing-candidate
regression, but no physical HomeBase control-session END was observed; that
live boundary must remain explicit in any PR.

## Work still in progress

Linux same-frame measurements show substantial additional age before completed
depay on that receiver, while a simultaneous Mac received those exact frames
promptly. Decoder output, decoded queue selection, and KMS return are separately
measured; KMS return is not physical scanout proof. The pacing experiment is
being verified separately and is not part of these SDK changes.

Fire TV read-stage diagnostics are undergoing another bounded joint capture;
the first quiet window did not exercise the late-frame diagnostic path.
Independent HomeBase VIDEO quiet intervals also remain unresolved. Preserve
these limits when describing benefits or preparing upstream PRs.
