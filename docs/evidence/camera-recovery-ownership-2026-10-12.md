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

Deployment and controlled live recovery are not yet verified in this record.
Source pauses and Linux playback timing remain independent unfinished work.
