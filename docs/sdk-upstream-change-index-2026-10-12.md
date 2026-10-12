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

The later live gate is complete: deployed PID 26350 stayed alive in all **366**
health samples from **20:15:24.483332–20:45:49.483455 UTC**. Actual `ok`
completions were observed at **20:22:43.317**, **20:32:44.432**, and
**20:42:45.314 UTC**. Authentication remained valid, go2rtc running, sessionLost
false, and reported stalls zero at every sample. A 109-row service journal from
20:12:31–20:47:00 contains no `realtime stalled` event. The unchanged process
survived more than 33 minutes after deployment, beyond the old 30-minute timeout.
The rollback timer was canceled after the first actual completion plus healthy
checks. All 155 dist files and package.json (156 total) were byte-identical to
the built candidate; full-dist manifest SHA is
`aeeb899f2cc699c81314a98986cc6f2bd57ff6538154be768a08aee805ece6d0`.

**Continuity boundary:** 365/366 samples contained all three selected sources.
One independent feed stopped/reopened at20:24:52–20:24:55, with two sources in
the intervening sample. Its cause is not established here. No poll-idle reset or
process restart occurred at that boundary. This validates the watchdog contract,
not uninterrupted playback or a source-pause cure. The SDK companion now records
the exact deployed checkpoint and private health/journal hashes. SDK and host
watchdog integration remain separate changes; see
[host contract evidence](evidence/poll-watchdog-contract-2026-10-12.md).

The latest combined candidate passed **4,108 tests in 236 files**, typecheck and build.
This was fork validation. Each companion file distinguishes its original gate
and its later live evidence; it is not clean upstream-base verification.

## Instructions for the future PR preparer

1. Inspect current upstream main, search existing issues/PRs, and create isolated
   branches from that base. The inspected main `7d1f016` contained all three
   first three underlying source patterns; recheck current source before applying them.
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
