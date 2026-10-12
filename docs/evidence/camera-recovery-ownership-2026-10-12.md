# Camera recovery must preserve sibling streams

## Defect and boundary

The old `dropStreamClient(camera, station)` iterated the station key and every
`station#...` connection. A single camera retry therefore closed healthy sibling
media and station/control connections. This is a recovery amplification defect;
it does **not** explain the upstream silence that happened before recovery began.

The bridge now calls `EufyMega.resetCameraStream(camera)` for ordinary retries.
SDK ownership identifies the cached source and its actual media connection.
Only that source is retired; its dedicated connection, when present, is closed
and awaited. A shared base/control connection remains available to other users.
This operation does not guarantee a new peer address for a base-owned source.

Deliberate whole-station LAN policy transitions and the existing loopback DEBUG
station-drop operation use the separately named `dropStationClient(station)`.
They retain their intentional station-wide scope. An older SDK cannot silently
fall back to unsafe station-wide camera recovery.

## Package and verification

SDK fork: `sapireli/eufy-sdk`, branch `eufy-wall`, source
`d711f98e8f584d14e8685a322cb4784abddc7b01`; bundle SHA-256
`d5db321bbd4ce03397dc0d2b8bd92de3603b5edc3f71bfe0be63b5a464835c92`.
The lock pins this exact source; runtime dependencies and SDK version are unchanged.

The original bridge adapter fails the serial-scoped regression by attempting to
close a shared base. The candidate delegates the exact camera serial. A deferred
cleanup test verifies that the bridge waits for reset completion before acquisition
and coalesces concurrent warm requests. The SDK tests acquire both base-owned and
dedicated-owned cameras through the real router/session manager with a faked network
session, verifying sibling ownership and queued/live delivery.

SDK focused suites: 39 tests passed, typecheck/build passed. Bridge focused
adapter/LAN suites: seven tests passed. All 210 bridge tests passed with the exact
candidate SDK bundle installed locally. The SDK companion evidence and upstream
handoff are in `docs/evidence/camera-stream-reset-2026-10-12.md` in that repository.
Synthetic frame ordering is not measured live freeze time. The public reset API
does not cancel external concurrent acquisition; the bridge serializes this path.

## Controlled production ownership test

The exact bundle and host changes were installed together at 23:28:41 UTC.
The parent process was 31548. A first 45-second ordinary API/TV gate passed.
The API retained each source identity; Door input/output increased 636/636 and
Garage 655/655, without new stale or unmapped units. Both TVs continued hardware
AVC decoding with no errors or resets. Camera settings and client preferences
were preserved.

The public SDK operation reset only Front Door at **23:31:13.280–13.295 UTC**,
returning true. The three existing public P2P session objects were unchanged and
connected immediately afterward. In the 44.3-second sampled API window, Front
Door received a new source UUID, was not streaming for three sampled rows, and
recovered. Garage's source UUID stayed unchanged, its SDK receipt sequence grew
4190→5551 (video and audio combined), and it was streaming in every sampled row.
Garage input/output increased 667/665. Both TVs kept their Garage decoder counters
without resets, timeouts, or errors while Front Door's player recovered.

**Ownership isolation passed; timing neutrality did not.** Garage recorded 14 new
aged bridge-output units, and both TVs decoded the same 16 aged frame IDs
7359–7374. Their ingress→decode age lower bounds were approximately 508–739 ms;
receive→decode took only approximately 8–20 ms. This locates most of that age
before the decoder, without assigning it to SDK delivery or the encoder.

The test used an inspector heap query to find the existing client before calling
the public API. Its cost was not measured, and it may disturb timing. The API
observer's timestamps are request starts, not response completion times. No NIC
trace ran during this window. The aged burst therefore cannot establish that the
reset itself, station behavior, GPU startup, or a particular SDK stage caused the
delay. A future causal test must separate heap discovery from the reset and
measure the relevant receipt/encoder boundaries. No sustained smoothness claim
follows from this ownership test.

## Retained deployment and rollback boundary

The original 240-second safety timer fired at **23:32:43–44 UTC**, restoring the
previous SDK and restarting the parent. The earlier timer-stop command raced its
already-dispatched service. This was a deployment rollback, not a natural stream
failure. The exact candidate was repromoted at **23:35:43.769 UTC**, with a longer
safety deadline so short checks could finish without that race.

After the final gates, both the new timer **and service** were explicitly stopped
and confirmed inactive. Parent **31925** remained active with the exact candidate
bundle SHA above and the verified default go2rtc writer repair
`7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585`.
The rollback backup remains available at
`/opt/eufy-wall-bridge/camera-reset-backup-20261012/rollback.sh`.

The final ordinary API window covered 44.09 sampled seconds, with both source
UUIDs stable and cameras streaming in every row. Door input/output increased
662/660; Garage 664/666 (asynchronous window endpoints, not a dropped/extra-picture
claim). Neither camera had new stale or unmapped units. The separate final
both-TV gate, **23:36:27.554–23:37:12.763 UTC**, had all four hardware AVC decoders
progressing with zero sampled input queues and no errors, retries, counter resets,
or new definite ingress-age misses above 500 ms. No inspector or reset ran during
that final ordinary window. Physical panel smoothness remains unverified by these
counters; source pauses and Linux timing remain unfinished work.

Private metadata is under
`.evidence/frame-freshness-2026-10-12/camera-reset-live/` and the one-shot SDK result
under `.evidence/bridge-network-audit-2026-10-12/sdk-live-camera-reset.json`.

| Artifact | SHA-256 |
| --- | --- |
| `post-deploy45.jsonl` | `947a358b58ffc2b6ae9dc5492bc69561745a66342e65693603b315f6f9dd542c` |
| `camera-reset45.jsonl` | `dd08b682e16e2843513fcb0b4f7c4445bfa79525076abffc589b84aa67b420fb` |
| `sdk-live-camera-reset.json` | `e3815863188bddb7028c9c031592b1404e6adf73dec34a34f4219114e9cc7894` |
| `retained-final45.jsonl` | `84590ec147fcadea9942d3f3a872aba0d915437ebc84d3e5f6563ce6c9468de5` |
