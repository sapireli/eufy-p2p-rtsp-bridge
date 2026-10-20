# Drain queued UDP sends before closing a P2P session

## Demonstrated defect

`P2PSession.close()` queues END through `send()` and immediately closes its UDP
socket. An attached media STOP queued immediately before close can be discarded
along with END. The API can resolve successfully without either datagram reaching
a local receiver. This is a session lifecycle defect independent of camera model.

The real-UDP regression test binds sender and receiver sockets on loopback,
constructs the actual `P2PSession`, queues attached STOP on channel 2, then awaits
`close()`. In 20 original-code repetitions the receiver observed zero STOP and
zero END packets. With the candidate it observed both packets in all 20 trials,
STOP before END and both from the original sender port. The original test fails;
the candidate passes. The receiver has a bounded 100 ms guard per repetition.

The candidate sends END on the captured existing socket and waits for its send
callback before closing that socket. This lets prior sends on the socket drain
without a fixed sleep, new socket, changed peer, or changed media command format.
The callback also resolves on a reported send error; tests cover that case,
missing peer, and repeated close. A local send callback is not a peer ACK and
does not guarantee remote delivery.

The Node API describes the [UDP send callback](https://nodejs.org/api/dgram.html#socketsendmsg-offset-length-port-address-callback)
as confirmation that a datagram was sent; the [socket close callback](https://nodejs.org/api/dgram.html#socketclosecallback)
reports closure of the underlying socket. Waiting for closure does not replace
waiting for send completion. These references were checked against current Node
26.12 documentation; the measured hosts ran macOS Node 26.8.1 and Linux Node
26.11.1. The reproduction and wire observations remain the behavioral evidence.

## Live original-code shutdown observation

The original bridge stop returned success. Its packet-header capture reported
`[7015, 0]` observer statistics, with no observer drops. The entire capture
contains zero outgoing END packets. Its one outgoing DATA packet was at capture
offset 0.5345 seconds, about 2.35 seconds before the shutdown cluster. Using only
the wire clock, 2947 HomeBase DATA packets followed the last ACK to that peer,
continuing for 7.266929 seconds; 473 Balcony DATA packets continued for
7.066436 seconds after the last ACK to that peer.

The candidate shutdown capture reported `[3470, 0]`. It contains three outgoing
END packets at capture offsets 2.940380, 2.940495, and 2.940609 seconds. Four
outgoing DATA packets appeared in the 2.871–2.940-second shutdown cluster. Their
encrypted bodies were not inspected, so they must not all be labeled STOP.
The last incoming HomeBase media packet preceded the last END by 7.519 ms;
Balcony's preceded it by 20.686 ms. No incoming DATA appeared after the last END
in this capture. This shows an operational difference in shutdown signaling and
the observed peer media flows, without proving remote receipt of any particular
datagram or establishing a playback-pause cause.

Mac event times and Linux wire `SO_TIMESTAMPNS` times have approximately 0.4
seconds of clock offset. Earlier calculations of 6.468/6.263 seconds after the
Mac-reported stop return are invalid as precise shutdown timings. All intervals
above use one wire clock; API event timestamps establish lifecycle order only.

The completed playback comparison did not meet the freeze goal:

| Condition | Samples | Source-age samples over 500 ms | Maximum sampled age |
| --- | ---: | ---: | ---: |
| Original before | 201 | 4 | 1296.039 ms |
| After drained shutdown | 199 | 5 | 1153.383 ms |
| After original shutdown restored | 201 | 9 | 1105.734 ms |

The candidate fixes the demonstrated shutdown-send defect, but this comparison
provides no repeatable playback improvement. Treat shutdown reliability as a
separate issue from the periodic freeze. The live SDK was restored to original
bundle `1dee01fc…`; this candidate is not deployed on the live bridge.
Live playback artifact SHA-256:
`1dd2509e682c47095d4df94716fbe4f0d0d4e65ede86e921e5770d97bc2d7f39`.

## Verification and scope

The candidate passed 29 focused tests across close-drain, autonomous-session-close,
and session-manager tests. The build passed according to the retained manifest.
The change is limited to send completion before socket closure; ACK batching,
start-request differences, quality, and stream selection are outside its scope.

Base revision: `afcbc3fe5a66d488120dd3d29bcadd1e0eba2c77`.
Original bundle SHA-256: `1dee01fc12a1cea1c208033c6b630ecbde42039c3279b1563025abbf35fb883c`.
Candidate bundle SHA-256: `f78b12fa67caf86ceadac78c3f11b4e960197cefe04c742ef9fc7cbafc414e29`.

## Retained evidence

The canonical private copy is under ignored
`.evidence/front-door-pause-2026-10-20/close-trial/`, with working copies under
`/tmp/eufy-close-trial-artifacts/`. No credentials,
session material, decrypted command bodies, or media are included in this record.

| Artifact | SHA-256 |
| --- | --- |
| `close-drain.patch` | `dd7b0573a3187c046e68c2492500a4a1ea9f8ddd0d46ce00f1041351381a0b7d` |
| `baseline-tests.log` | `e4a784a4c3724bc142f0c4ee95b968a623dd4a241b0bafbe2876815aef53dd61` |
| `candidate-tests.log` | `3518f709dd9ac190a33918d253e51cbb4f86c62a1d9e19b1606710cd96924aed` |
| `manifest.json` | `c354025d85a3de6db498a5fb8c444af3ea66a98ec8fd4ee200657377bd4ebd81` |
| `/tmp/front-original-shutdown-wire.json` | `cccfd57cbc4248c07e0106c50b8d7f3dac3ba65e50425772cc1a0cdf3010b93f` |
| `/tmp/front-original-shutdown-events.json` | `a83261b5626d56794aa4437eaafea58de8c5c1dc1277d9aee89d5aaaa7c47c43` |

For upstream review, keep the claim to loss of queued shutdown sends and the
actual-class UDP reproduction. Review the completed live comparison separately
before adding any playback benefit claim. Upstream preparation uses an isolated
branch from `beta-0.5.0`, with the real-UDP regression independently failing on
base revision `425c877`.

## Upstream PR

[SDK PR #478](https://github.com/mega-yfue/eufy-sdk/pull/478) targets
`beta-0.5.0` with signed commit `b2170896826777685135a198e24506e923d576a6`.
It includes only the close change, real-UDP regression, and sanitized companion
evidence. Full `npm run verify` passed on Node 24.5.0 and separately on Node
26.8.1: 247 test files, 4378 tests, and 96 documented snippets, plus the remaining
format, type, guard, build, ESM, and example checks. The GitHub template check
passed. These checks do not establish that upstream CI has run.

The upstream PR is focused on queued UDP send loss at session close. Keep the upstream description
focused on shutdown-send loss; the playback comparison belongs only in this
bridge investigation record. The detached trial worktree and private artifacts
retain the initial reproduction; the upstream change uses a clean beta base. Cross-reference
[evidence/front-door-periodic-pause-2026-10-20.md](evidence/front-door-periodic-pause-2026-10-20.md)
for the separate unresolved playback investigation.
