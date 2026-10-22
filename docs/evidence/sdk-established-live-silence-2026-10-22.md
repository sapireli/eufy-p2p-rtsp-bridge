# Established attached source during video silence

This is an evidence handoff for SDK maintainers. No production change or timeout policy is proposed here.

## Reproduce

Base: `sapireli/eufy-sdk` branch `eufy-wall`, exact commit
`66692a04a2516634277b1a33eb30205071ee7ff3`.

Use a clean SDK worktree at that revision. Apply [the characterization patch](data/pinned-camera-hourly-20261021/sdk-established-live-silence-characterization.patch) with `git apply`, install its dependencies, then run from the SDK worktree:

```sh
rtk proxy ./node_modules/.bin/vitest run src/transport/p2p/__tests__/established-live-silence-characterization.spec.ts
rtk proxy npm run typecheck
```

Result: **1 test passed**, followed by a successful typecheck. The final focused run completed in 361 ms.
The test is a characterization that passes on the unmodified base. A future fix needs its own failing regression.

The test uses production `P2PSession`, `LiveStream` and `SharedLiveSource` code. It bypasses the initial
network handshake and intercepts the session send boundary. It decrypts only synthetic outbound envelopes
to distinguish START (`1003`) from STOP (`1004`); no capture keys or account data are present.

One consumer remains attached throughout. A valid synthetic keyframe establishes video delivery. Then
60 valid control notifications arrive, one per second, with no more video. Fake timers advance 60 seconds.
The same source and owner remain in place; the connection remains marked connected.

Before cleanup:

| Observation | Result |
| --- | ---: |
| Video frames delivered | 1 |
| Control notifications parsed | 60 |
| Outbound media START requests | 20, including initial start |
| Outbound media STOP requests | 0 |
| Consumer stop/error events | 0 / 0 |
| Attached consumers | 1 |

`SharedLiveSource.state` remains `live`. That is an internal lifecycle label, not a public promise of
frame freshness. The demonstrated behavior is that this attached consumer receives neither new video nor
a terminal event while the SDK continues START requests. Explicit disposal sends STOP during cleanup.

## Code path

The first keyframe clears `SharedLiveSource`'s startup watch. Later attached-camera video silence arms
`LiveStream`'s existing stall recovery: a full START after six seconds, then START every three seconds.
That path never sends STOP. Healthy transport control does not imply that video is advancing, and there
is no post-start video deadline in this path that retires or fails this consumer.

`resetCameraStream()` is a different operation: it disposes the source and closes a dedicated media
session. Its sibling-isolation tests are in `media-session-opens.spec.ts`. A consumer has no public
in-place STOP/START recovery operation.

## Natural and controlled device observations

Measurements below preceded this latest SDK update. They establish the reported failure mode; they do
not constitute a live verification of a candidate change at commit `66692a0`.

- T8425 attached-camera run: accepted command-1300 DATA, assembled units and delivered frames all stopped
  at 874. No keyframe gate or video-gap event occurred. The original source receipt remained unchanged
  for about 27 seconds. A replacement stream later delivered 1,394 frames on the same P2P session.
- T8214 attached-camera run after a one-shot station ping: DATA, assembled and delivered counts stopped
  together at 821, with no keyframe gate or video-gap event. Recovery replaced the stream and session.
- T8214 run after a one-shot START during normal delivery: delivery initially continued across the
  request, but the next original-hour boundary still stopped all three counters together at 974.
  The host's source-receipt age reached 30.176 seconds. A replacement stream/session delivered 1,900 frames.
- A separate controlled T8425 STOP/START used the same SDK source, owner and session. The first SDK
  keyframe arrived 570.844 ms after START with a 2304×2592 geometry header. A second keyframe arrived
  2.606 seconds later at 1920×2160; subsequent observed headers stayed at that size. This was an
  83.685-second post-START observation, with no host stall-counter increment. It does not prove that
  STOP/START is always necessary, optimal, or free of startup side effects.

Packet analysis of the naturally occurring hour-boundary cuts found incoming raw VIDEO stop while
control traffic continued. Logical DATA/assembly/gating observations therefore locate these cuts before
SDK media decoding. No old-session gateway-key negotiation was observed at either captured cut.

## Private artifact provenance

The deployment marker reported fork base `7fe9cc55aac899f862ffcfadae366b22d104220f`,
patch `e2231c562e439a22a7e16acd11c80cb297cc40a3`, and bundle SHA-256
`569200ac0f3a504efba46bca33962e82be96034776542b37f171b6e379d1d0ab`.
These are marker claims. A later extraction of the rollback SDK index produced a different SHA-256
(beginning `326643`), so the marker does not establish the prior artifact's bytes. The old process's
loaded SDK bundle was not independently fingerprinted; its exact runtime code remains retrospectively
unverified. The packet and SDK-event observations remain measurements of that process, with this
attribution limit. They must not be presented as device verification of the complete tree at either
marker commit.

The subsequent `66692a0` deployment has a separately verified artifact hash (beginning `030161`). That
verification belongs to its deployment checkpoint. This characterization test binds to the exact local
source commit above; it does not turn the earlier device observations into a live run of that commit.
Raw captures and operational identifiers remain outside this worktree.

Sanitized summaries identify their private inputs by SHA-256:

| Input | SHA-256 |
| --- | --- |
| T8425 SDK boundary observation | `f4dcd6a887a5e4528865e01d3a78669d18beaf97574f2eac1cfe417e4bbdd2e5` |
| T8214 SDK boundary after station ping | `8686a1fc3e94db7f666e537c25f4d02f0679d34b9c079a5d7587257824033967` |
| T8214 SDK boundary after START | `778d22e8eb90c75af247c51606a65e5b5f4420f3db3cc7979aa3802efb61a5a2` |
| T8425 controlled STOP/START SDK frame observation | `5a7194c6d22887c14e2949c0ce92c5104cbd4d6c2dde56e8554a4307b7940309` |

## Limits and next decision

The synthetic test does not run sockets, heartbeat negotiation, a peer implementation or a camera.
It assumes a connected transport and injects logical control frames after transport reassembly. It
cannot prove why the device stops video, that START would fail on a real peer, or which recovery deadline
belongs in the SDK. No new consumer joins during the simulated silence, so a join/reuse startup watch
cannot account for its result.

The existing live comparisons support investigating an established pull that no longer produces video
while control remains healthy. They do not prove a firmware expiry contract or establish a universal
one-hour lifetime. Current owner-app captures last less than three minutes and cannot establish its
behavior across that boundary. Choosing a new timeout or shipping periodic restarts requires additional
protocol and recovery evidence.
