# Retire consumers when their camera feed closes

## Defect

`closeFeed()` removed the SDK readable and scheduled camera-scoped recovery,
but retained the camera's HTTP consumers. Those consumers retained their
FFmpeg decoder references, Matroska clock, and pending source-to-output PTS map.
A replacement feed could therefore enter an existing coded reference chain.
The default playback stall is five seconds; the separate consumer-gap timeout
is 45 seconds, so that timeout did not isolate ordinary feed recovery.

The SDK receipt `sourceId` identifies a source object, not every coded-stream
restart. This fix uses the actual readable-feed retirement boundary and does
not assume that receipt identifiers or sequence counters reset.

## Reproductions before the change

An off-device reproduction called the production stream manager with real Node
`PassThrough` feeds: feed A delivered a keyframe, ended, and was automatically
replaced by feed B. The existing consumer remained open and received feed B's
delta frame without a new keyframe. The result was two opens, one retained
consumer, and pictures from both coded chains in that consumer.

A separate reproduction used the production fresh-egress module and the test
suite's fake FFmpeg process. Starting with timestamp 1000, a replacement-chain
frame with timestamp 1000 produced `duplicate source PTS cannot establish frame
identity`. Timestamp 328 produced `source video timestamp discontinuity
(-672 ms)`. These reproduce the guard failures; they do not establish that every
live occurrence has this cause.

The two stream-manager regressions exercise both feed EOF and the
five-second playback stall. Against the unchanged HEAD implementation, both
fail because the retired consumer remains open. Against the fix, both pass.
They also verify that a sibling camera's feed and consumer remain open, stale
callbacks cannot affect the replacement, and a newly attached consumer waits
for a fresh keyframe before forwarding dependent pictures.

## Change and limits

Retiring an existing feed now clears and ends only that camera's consumers
before destroying its readable. A stale data callback is ignored unless the
readable still owns the slot. Reconnected consumers start with new muxer,
decoder, and timestamp state. Hardware selection, quality, camera holds,
station ownership, SDK behavior, and other camera consumers are unchanged.

This fixes reference-state reuse across bridge feed recovery. It does not fix
the initial upstream media silence. The separate encoder pending-frame limit
requires its own evidence; this reproduction does not establish encoder
capacity or prove the cause of every backlog event.

## Verification

- Before-change regression: two failures at the old-consumer-ended assertion.
- Focused stream-manager, fresh-egress, and Matroska suites: 46 passed.
- Full server suite: 248 passed on Node 26.8.1 using the existing checkout dependencies.
- The bridge uses JavaScript directly; there is no separate server build script.

Private local reproduction and test logs: `/tmp/repro-consumer-epoch.mjs`,
`/tmp/repro-source-epoch.mjs`, `/tmp/bridge-feed-retirement-baseline.log`,
`/tmp/bridge-feed-retirement-focused.log`, and
`/tmp/bridge-feed-retirement-full.log`. These paths are local evidence, not
repository dependencies.

## Live deployment and limits

The candidate was deployed on homebridge at 22:26:39 UTC on October 20.
The deployed stream-manager module SHA-256 was
`f6b17be0ed2a3bde42ba9cd686e6fcd690b912d55d8fd970fe39d7bc18b5d469`;
the service PID was 135098. The SDK bundle remained unchanged at SHA-256
`1dee01fc12a1cea1c208033c6b630ecbde42039c3279b1563025abbf35fb883c`.

The live test did not establish that all reconnects were fixed. At 22:27:58 UTC,
Garage Doorbell reported a -1386 ms source timestamp discontinuity while its
bridge readable remained open and its stall counter was zero. Later, source
silence caused feed retirement at 22:30:33, 22:30:39, 22:31:09 and 22:38:07.
At 22:38:07 all three Fire TVs logged a Garage Doorbell read timeout during
that same bridge recovery; their decoder was the hardware Amlogic decoder.
Encoder pending-frame bounds also fired after deployment.

This change repairs the reproduced cross-feed reference-state defect. The
same-readable timestamp discontinuity, incoming media pauses and encoder
output pauses remain separate unresolved causes; none is attributed to
HomeBase forwarding, RF strength or hardware capacity by this test.

