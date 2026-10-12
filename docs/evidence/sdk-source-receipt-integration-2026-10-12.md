# SDK receipt preservation through the live bridge

## Change

The bridge opts into the SDK's `receiptMetadata` object-mode Readable API.
SDK source commit `1fbec7e45aaa62241cdc33acc8ec58baafa88879` on `eufy-wall`
creates immutable process-monotonic receipts at original shared-source media
arrival. Queued delivery and retained keyframes carry the same receipt object.

The bridge unwraps `{ frame, receipt }`, preserves `receipt.monotonicUs` in its
existing frame stamp, and assigns one bridge frame ID per retained receipt.
A WeakMap keeps that identity without retaining frames after the SDK releases
them. Replayed delivery does not renew the clock or mint another bridge ID.
Legacy unwrapped frame callers still measure Readable delivery; they do not
receive a claim about an earlier SDK arrival. Invalid envelopes close only their
feed through its existing error handler instead of escaping the data callback.

`/api/cameras` exposes the last SDK source ID, sequence, original monotonic
receipt, bridge frame ID and current receipt age. Strings encode bigint fields.
The SDK sequence includes audio; its gaps are not lost-video counts. A source ID
identifies a shared-source instance, not a camera exposure or network session.

The wire stamp stays in the bridge's process-monotonic clock domain with scope
`bridge-ingress`. This now includes SDK retention before Readable delivery. It
does not measure first datagram arrival, camera capture time, or sensor exposure.
Capture time remains unverified. Observation mode, playback policy, packet
transport, quality, codec and decoder/encoder settings are unchanged.

## Local gates

Bridge tests cover an aged replay retaining the same time and frame ID, queued
receipt age, a rebuilt source receiving a distinct identity, invalid/future
receipts, real stream-manager envelope fanout and guarded feed errors. The
combined source fixture uses actual SDK SharedLiveSource/Readable code and the
bridge recorder over synthetic transport; a five-second observation offset
leaves original receipt and frame ID unchanged.

The full bridge suite passed 206 tests against the interim SDK bundle. After
the SDK's default timed-event JSON compatibility hardening, 31 focused bridge
tests passed against the final bundle. SDK evidence separately records its
interim full-suite and final focused/type/build gates.

```sh
node server/scripts/verify-sdk-receipt-replay.mjs /path/to/eufy-sdk
```

This is a source-boundary fixture, not a live media capture. It prints the SDK
checkout revision and bundled-source hash; an uncommitted source diff must be
considered when interpreting the checkout revision.

## Exact live checkpoint

One coordinated bridge restart occurred at **22:38:08 UTC**. Node PID changed
from 26350 to 30219. Actual scoped SDK runtime bundle SHA-256:
`a5fc1853c9ed07ed04a46bbbd6de1c039c922492714457621d8992a1ee4ff1cf`.
The SDK source marker and lockfile now identify commit `1fbec7e`.
The stock go2rtc binary remained unchanged at SHA-256
`32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6`.

Thirty seconds of API samples showed stable source identities for Front Door
and Garage, sequence increases of 972 and 927, and zero unmapped encoder
frames. Last reported compatible egress counts were 748/747 input/output and
729/728. Cumulative stale reference counts were 4 and 19, with maximum egress
ages 1597.485 ms and 646.538 ms. These are truthful expired reference stamps,
not proof that all output was fresh or that latency is solved.

Balcony's feed ended at 22:39:06.149800, reopened, and obtained a new source
identity. It was not uninterrupted during this window. The journal contained
no missing/invalid receipt errors. This model had independent feed endings
before deployment; this run alone does not establish their cause.

Both Fire TVs were sampled from 22:38:38.393 to 22:39:08.574 UTC after recovery.
All four players used hardware AVC decoding and advanced without errors,
retries, counter resets or new definite freshness misses. Decoded increases:
Front Door 420/396; Garage 391/376. Last reported frame-age bounds were
164–171/162–170 ms for Front Door and 77–83/80–88 ms for Garage.
These bounds concern SDK/bridge receipt, not sensor capture or physical pixels.

The automatic rollback timer was canceled only after these checks; its backup
remains under `/opt/eufy-wall-bridge/source-receipt-backup-20261012`.
Node PID 30219 stayed unchanged during subsequent metadata checks.

## Bounded live API receipt probe

One metadata-only reader attached to the already-existing Front Door source
through the SDK's real public camera API. The frozen envelope and receipt
retained a time **585.427 ms older than observation**, with the expected source
identity. The reader was immediately destroyed; no second encoder/login or new
source was created. The SDK's existing join watch may reissue a media start;
that operational effect is part of this probe, not a passive packet capture.

The keyframe sampled before the asynchronous open had an earlier sequence.
Another keyframe arrived during opening, so this probe does **not** prove object
identity with that pre-open sample. Exact retained-prime identity is proven by
the synthetic SDK regression; the live probe proves original receipt metadata
on the existing-source API path. No payload was serialized. Inspector and SSH
forwarding were closed, restoring their prior disabled state.

## Outstanding requirements

This fixes a freshness-observation defect. It does not repair the independent
network-interface video gaps, station-wide recovery blast radius, Linux socket
backlog, or all visible stutter. Sustained smooth low-latency playback remains
unverified and the overall goal remains active.

Private metadata artifacts are under `.evidence/frame-freshness-2026-10-12/`
(`source-receipt-live-30.jsonl`, candidate manifest and TV post-recovery records)
and `.evidence/bridge-network-audit-2026-10-12/sdk-live-receipt-prime.json`.

SHA-256 metadata provenance:

- API samples: `b99b27c70230d6587775e84979a883650cd89641198342a5d3a8da1eacc06d53`.
- Candidate files manifest: `0a310d070d55748348f037f8485bb0efc7eb6301a8372d6adda39d46dbd96b4a`.
- TV summary: `48ffd04711045856e75666a6472e07a8014a7b970f235868f4a2808ef6160533`.
- Live API probe: `b282d61c2ee679cf5356204bddd0e598c984eb6cc73650d70a17ccb51c1a17a4`.

