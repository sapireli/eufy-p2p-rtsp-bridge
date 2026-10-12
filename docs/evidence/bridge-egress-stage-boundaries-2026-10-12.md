# Preserved SDK receipt to bridge egress: diagnostic boundaries

## What the existing live witness establishes

The 23:31:01.482–23:31:46.667 UTC scoped Front Door reset trial contains the same Garage late frame IDs 7359–7374 on both Fire TVs, epoch `fa606ba1a53ca431`. See [the TV reset evidence](tv-scoped-source-reset-2026-10-12.md).

| Device | Decode age lower bound | AU receipt to decoded output | Inferred age lower bound at AU receipt |
| --- | --- | --- | --- |
| .85 | 508.443–737.242 ms | 8.157–18.569 ms | 497.282–726.133 ms |
| .58 | 507.638–738.618 ms | 10.208–20.258 ms | 495.531–727.173 ms |

Fifteen of the sixteen frames were already more than 500 ms old at AU receipt. The first crossed that threshold during short decoding. These measurements exclude a deep decoder queue as the explanation for this particular burst. They do not locate the earlier delay.

The Inspector `queryObjects` request before the reset may force heap traversal or GC; its duration was not measured. Bridge API timestamps are request starts, not exact handler-return times. No source-wire capture covered this interval. Therefore the burst cannot be causally attributed to the camera reset, a station session, the GPU, or network delivery.

## Offline code audit

The original monotonic receipt is minted at SDK `SharedLiveSource.onVideo`, before parameter-set processing and consumer delivery. It is not a NIC or sensor timestamp. The SDK's bounded consumer queue and Node Readable buffering retain that receipt when delivery pauses. The bridge attaches a flowing `data` handler, and its timed consumer does not pause the SDK Readable when encoder stdin exceeds its high-water hint. The existence of queues therefore does not prove they held the observed frames.

At bridge `onChunk`, synchronous parameter-set extraction, codec/geometry handling, and preceding consumer callbacks occur before the timed consumer receives the frame. The timed consumer then muxes input, queues an accepted stdin write, and joins encoder stdout bytes with a matching `stats_mux_pre` record. Those stages previously had only one final egress-age counter. Startup probing, shared GPU work, pipe delivery, and Node event-loop delay were indistinguishable.

No demonstrated playback-delay bug was found in this audit. No buffer size, playback pacing, SDK queue, encoder argument, timeout, freshness deadline, source subscription, or camera configuration is changed by the instrumentation.

## Opt-in diagnostic contract

Before constructing `createFreshEgress(ctx)`, a bounded collector may supply:

```js
ctx.maxFreshEgressTimingRecords = 256; // valid range: 1..4096
ctx.onFreshEgressTiming = record => records.push(record);
```

An injected collector should do constant-time bounded work: append the metadata record, and serialize only after observation ends. The factory itself adds no timers, per-frame logging, JSON serialization, or payload capture. The hook is absent by default. Its maximum is shared across all consumers created by that factory, and the factory stops timing new frames after the row bound is reached. Setting `ctx.freshEgressTimingActive = false` dynamically disables every additional clock read and new timing object; previously accepted compressed references are still delivered. Hook exceptions cannot break reference-frame delivery. A quiet run may produce no rows.

Records are emitted only for frames whose preserved receipt is at least their existing `maxAgeMs` old immediately before the HTTP write. Timestamps are same-process monotonic microseconds, serialized as decimal strings. The immutable receipt and the SEI stamp retain their original identity and time.

| Field | Actual boundary |
| --- | --- |
| `ingressMonoUs`, `sourceId`, `sourceSequence` | Original SDK receipt, not NIC arrival |
| `serverEpoch`, `frameId`, `sourceTimestamp` | Exact existing freshness identity/source media word |
| `bridgeEntryUs` | Bridge `onChunk` entry, before parameter extraction |
| `consumerDispatchUs` | Immediately before invoking this timed consumer |
| `consumerEntryUs` | Timed consumer callback entry |
| `stdinQueuedUs`, `encodedPts90k` | Immediately before accepted encoder stdin write, exact association key |
| `encodedJoinedUs` | Node callback after a complete encoded packet and its matching stats record are joined |
| `httpWriteUs`, `httpWriteReturnedUs` | Node HTTP write invocation and synchronous return |
| `stdinQueuedBytes`, `pendingFrames`, `encoderAttempt` | Enqueue snapshot and actual attempted encoder path |
| `httpQueuedBytes` | Node queued HTTP bytes after accepting the write |

Direct-copy records omit encoder fields. A missing bridge-entry field means the consumer was invoked outside the instrumented stream manager; it must not be inferred from another timestamp.

`stdinQueuedUs → encodedJoinedUs` includes stdin drain, FFmpeg decode/filter/encode, stdout/stats pipe readiness, and Node scheduling. It is not a GPU duration. `httpWriteReturnedUs` is not socket delivery or RTSP receipt. Clock age remains scoped to SDK receipt, not sensor capture.

The intervals decide the next action: an old frame at bridge entry points before bridge dispatch; late dispatch with timely bridge entry points at synchronous bridge work; timely enqueue with late packet join points at the broad encoder/pipe interval. They do not justify modifying any stage until a live witness isolates it.

## Bounded entry-point activation

The bridge entry point initializes the optional collector before constructing the timed egress factory. It is enabled only by an explicit absolute `EGRESS_TIMING_FILE` path. `EGRESS_TIMING_SECONDS` defaults to 30 and must satisfy `0 < seconds <= 45`.

The initializer exclusively creates a new file with `O_EXCL | O_NOFOLLOW` and mode 0600. Existing files and symlinks disable observation without modification. The file is metadata-only JSONL. Observation begins 15 seconds after initialization, and expires at the fixed startup delay plus duration, even if no aged frame arrives. Both timers are unreferenced. Expiry, early shutdown, 256 records, or an output failure disables timing and closes the file. Disabled or unset observation creates no hook, file, or timer.

Only aged hook callbacks serialize and write a row. These synchronous diagnostic writes can themselves perturb timing, so enabled observation is not claimed to have zero overhead. File-write elapsed time is outside the recorded `httpWriteReturnedUs` interval; a future frame may reflect that diagnostic work. A live run must record actual duration and overhead, and must not use Inspector heap enumeration to activate this collector.

## Verification and deployment scope

The focused suite (`fresh-egress`, `stream-manager`, and `sdk-camera-recovery`) passed 49/49 tests locally, including the bounded initializer. New deterministic tests verify:

- 90 ms of synchronous bridge processing is reflected between bridge entry and dispatch, without rebirthing the SDK receipt.
- A 700 ms encoder-output hold is reflected between enqueue and packet join, while source receipt and output SEI remain unchanged.
- Disabled diagnostics add no extra bridge/egress clock reads and produce identical video bytes.
- Fresh frames emit no records; a one-record bound disables further clocks; an observer exception cannot interrupt compressed references.
- Delayed activation and fixed expiry, early shutdown, private file permissions, existing-file/symlink collisions, invalid durations, the 256-row limit, and failed/short output writes obey the bounded lifecycle.

These are diagnostic-contract tests, not proof of a real-world delay fix. This instrumentation has not been deployed or used for a new live capture in this task.
