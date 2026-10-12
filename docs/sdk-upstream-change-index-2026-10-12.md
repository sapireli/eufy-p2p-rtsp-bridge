# Independent SDK changes: evidence and upstream handoff

All four changes are committed and pushed on the SDK's **eufy-wall** branch.
The live bridge package, lock and revision marker identify `830fa1c`; deployed
bundle SHA-256 is
`b23b4ddaa58ad4669cd3a528de71f9570c3f14ffe7609062820be822c281b83b`.
This index does not imply that the fork has been merged upstream. No upstream
PR has been created for these changes.

| SDK commit | Isolated change | SDK companion evidence |
| --- | --- | --- |
| `55dacd3` | Assemble fragmented logical payloads with one completion copy | `docs/evidence/p2p-linear-reassembly-2026-10-12.md` |
| `55d3fea` | Dispose only live sources owned by a closing station-control session | `docs/evidence/p2p-session-owned-teardown-2026-10-12.md` |
| `a9424bf` | Honor a valid END from the current peer/socket and stop live-stream timers | `docs/evidence/p2p-peer-end-recovery-2026-10-12.md` |
| `830fa1c` | Report typed settled cloud-poll completion independently of device-state changes | `docs/evidence/cloud-poll-completion-2026-10-12.md` |

## What each proof establishes

**Linear reassembly:** the synthetic allocation regression preserves byte identity
and reduces cumulative concatenation from 26,967,943 to 235,549 bytes for the
same 234,129-byte logical frame/fragment pattern. Live large-key assembly elapsed
median fell 22.096→3.686 ms, with exact payload-size completion copies. Before
and after bundle hashes, frame-record counts, longest dispatch gaps, physical
packet/ACK limits and upstream-main applicability are in its companion file.
This is a generic fragmented-frame CPU/copy-cost fix, not a one-camera workaround.
Peak memory was not measured; source pauses were not cured and one live stream's
longest dispatch gap was worse. See [linear reassembly handoff](sdk-linear-reassembly-pr-handoff.md).

**Ownership:** a structural regression executes the real router close-event and
fan-out paths with synthetic sessions. The base wrongly disposes an independent
media source; the fix preserves that source's consumer delivery and an in-flight
media-key reservation. Explicit closeAll still closes everything. **No physical
HomeBase station-control END has been observed.** The live standalone END test
does not substitute for that ownership scenario. No source-pause improvement is
claimed for this structural lifecycle repair.

**Peer END:** real UDP regressions fail on the base and pass on the candidate,
including current-peer close, foreign/stale endpoint isolation and malformed
framing. A natural active-peer END was then observed in a zero-drop 600-second
physical capture: bridge feed-ended logging followed in 25.666 ms and replacement
VIDEO arrived 2,879.499 ms after END. No outgoing DATA continued on the ended
connection. Earlier ignored-END waiting produced a separate 22.681-second VIDEO
gap. The events are sequential, not identical-scene timing trials; why the peer
closed is unknown. See [peer-END and ownership handoff](sdk-peer-end-pr-handoff.md)
for exact boundaries, hashes and old-SDK shutdown exclusions.

**Poll completion:** four new regression cases fail on the prior SDK, and all
five pass with typed `pollComplete({status:'ok'|'partial'|'error'})` after each
settled pass. Unchanged deviceState semantics remain intact; pending promises
and disabled scheduling produce no completion. The host's previous misuse of
deviceState triggered a forced all-feed reset at19:57:17 despite that event being
a state-diff signal. Earlier actual poll completion was unmeasured; the API supplies
loop-progress observability, not a cloud-success or camera-reachability verdict.

After deployment as PID26350 at20:12:31, the **first actual completion was observed
20:22:43.317 UTC**, status `ok`, count1, lastCompletedAt1791836563317. The host
reported all three sources active, go2rtc running, authentication valid and no
lost session. The fifteen-minute rollback was canceled only after this observation
and healthy checks. All155 dist files and package.json (156 package files total)
were byte-identical to the built candidate. The private full-dist manifest SHA is
`aeeb899f2cc699c81314a98986cc6f2bd57ff6538154be768a08aee805ece6d0`.
**Thirty-minute survival was still pending when this update was recorded**; this
first completion does not substitute for that later watchdog gate. SDK and host
watchdog integration are separate changes; the host's companion is
`docs/evidence/poll-watchdog-contract-2026-10-12.md`.

The latest combined candidate passed **4,108 tests in 236 files**, typecheck and build.
This was fork validation. Each companion file distinguishes its original gate
and its later live evidence; it is not clean upstream-base verification.

## Instructions for the future PR preparer

1. Inspect current upstream main, search existing issues/PRs, and create isolated
   branches from that base. The inspected main `7d1f016` contained all three
   underlying source patterns; recheck current source before applying them.
2. Port each narrow fix and its meaningful base-failing regression, with its own
   sanitized companion evidence. Keep these as separate commits/PR scopes;
   never merge unrelated eufy-wall history to obtain one fix.
3. Run each clean upstream branch's complete required tests/typecheck/build.
   Record base failure and candidate success; fork tests do not replace this gate.
4. Preserve ACK/timing/gating, streamType 2, camera composition/native quality,
   hardware preference/fallback and LAN policy. Explain ownership before activating
   END on a station-control connection with independent media siblings.
5. Keep private packet captures, journal identifiers, addresses, keys and home
   images out of upstream attachments. Companion evidence carries checksums and
   sanitized measurements; raw metadata remains under the operator's private
   `.evidence` directory.

The remaining heartbeat-alive HomeBase VIDEO quiet intervals are a separate
investigation. This index is conclusive evidence/handoff for the stated copy,
ownership and explicit-close defects; it is not a claim that all live playback
pauses or capture-age uncertainty are resolved.
