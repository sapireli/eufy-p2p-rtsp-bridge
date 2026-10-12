# SDK linear reassembly PR handoff

The SDK candidate removes repeated copying while receiving fragmented P2P logical
frames. Its need is general: every additional continuation previously copied the
whole accumulated payload again. A 234 KB runtime regression copied 26.97 MB before
versus 235.55 KB after, with identical delivered bytes. This is one change; ACKs,
reorder waits, gating, quality and buffering policy are unchanged.

## Authoritative evidence

The companion SDK evidence file is
`docs/evidence/p2p-linear-reassembly-2026-10-12.md` in the SDK checkout. It must travel
with the SDK commit. It records complete deployed hashes, the exact before/after
capture windows, byte identity, per-stream assembly work, delivered logical-video
counts and longest dispatch gaps. The source metadata is private under this project's
`.evidence/bridge-network-audit-2026-10-12/`; do not attach camera payloads or identifying
capture records to the upstream PR.

The change and its evidence were committed together as
`55dacd367068b0d90460561e20c9410a2ec03724` and pushed to
`sapireli/eufy-sdk` branch `eufy-wall`. The bridge lock now resolves that revision.
This is the downstream fork commit, not an upstream PR or clean-main test result.

The live host ran SDK base `35b9581d79d414602c63e7240677cb15aa09cf63` on branch
`eufy-wall`, then the candidate bundle with SHA-256
`4ddbf934ccab2ebee30832fdacb7a1874e308278f3db2f41aa98503b6e938cf4`.
The approximately 230 KB stream's median assembly work fell 22.096 → 3.686 ms,
p95 30.149 → 5.329 ms and maximum 32.542 → 5.786 ms. The other two streams also
improved. The full fork suite passed 234 files / 4,097 tests; this is **not** a clean
upstream-main validation result.

## What the PR should claim

Lower cumulative copying and synchronous reassembly cost across fragmented logical
frames. Bytes and existing transport behavior are preserved. Whole-process CPU
34.301 → 26.000 seconds per 60-second window is supporting observation, with scene
and process-work caveats; it is not an isolated CPU attribution.

Do not claim a source-pause fix. Delivered logical-video counts before/after were
1,487/1,501; 899/901; and 914/901. Longest dispatch gaps were 435/801, 515/3,183,
and 1,008/849 ms. Stream B's worse after pause contains 3,162.907 ms of video UDP
quiet before SDK receipt, even while its same session exchanged PING/PONG and
CONTROL ACKs. Peak RSS and sensor/display age remain unmeasured.

## Isolated upstream preparation

Actual upstream `main` was inspected at
`7d1f016362465b0747405c5dc83124c86b2c630b`; it still has the same growing-buffer
`reassemble` algorithm. Create a fresh branch from the current upstream main,
search existing PRs/issues for this fix, and apply only:

1. `src/transport/p2p/p2p-session.ts`: chunk-list state and single completion copy.
2. `src/transport/p2p/__tests__/data-reassembly.spec.ts`: synthetic allocation/byte regression.
3. The sanitized evidence companion appropriate for the PR.

Do not merge or cherry-pick the entire `eufy-wall` history. The host observer's
compatibility changes belong here, not upstream in the SDK. Run the upstream full
gate and its failing-base allocation check before opening the PR. No PR has been
created by this task.
