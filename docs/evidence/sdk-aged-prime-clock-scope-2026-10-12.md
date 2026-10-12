# SDK retained-prime receipt scope audit

## Source and runtime boundaries

The executed fixture used SDK checkout HEAD
`51bf8df21c71bb5067cb6a7e0ee5c4c18a57bfef` on `eufy-wall`. That commit is
documentation only; the audited SharedLiveSource/readable-egress source has no
diff from source commit `830fa1c962efea746b053821c50d92a76442d5e9`.
The live runtime checkpoint remains source 830fa1c, bundle SHA-256
`b23b4ddaa58ad4669cd3a528de71f9570c3f14ffe7609062820be822c281b83b`.
The fixture bundled local TypeScript source, **not the exact live deployed dist
artifact**; no live-path validation is claimed by source equivalence alone.
No source clock-preservation API was implemented in the SDK.

One focused offline fixture bundled the actual SDK SharedLiveSource and
openReadableFromConsumer source with esbuild, then used the bridge's actual
createFreshness.record function. The transport was a synthetic EventEmitter;
no device, network, decoder, SDK source modification, or live service change
was involved.

A frame arrived at synthetic wall time 1,000 ms and monotonic time 1,000,000 us.
After detaching and reattaching inside linger at wall time 6,000 ms and
monotonic time 6,000,000 us, the source replayed
**the identical frame object** as its retained keyframe. Consumer.onMedia still
exposed the original `timestampMs = 1000`, so its retained age was 5,000 ms. The object-
mode Readable emitted only the LiveVideoFrame, with no receipt metadata. Bridge
`record(frame)` restamped it at 6,000,000 us and assigned new frame ID 2
(original 1), giving the replay a reported bridge-ingress age 0. Its sourceTimestamp word stayed
unchanged. The focused assertions passed; all owned source/readable timers were
closed and the original Date.now restored.

This proves a freshness scope gap for retained SDK replay: time first received
by the SDK source is lost before bridge stamping. It does **not** prove a camera
capture age, explain the 31 s source pauses, or identify the reason for the
later production interruptions. The original bridge-ingress clock currently
observes Readable callback receipt; it cannot be described as original source
receipt for primed or queued SDK frames.

A minimal future seam would preserve an immutable **monotonic source receipt**
and identity when the source first receives a frame, through queue/prime and
object-mode Readable delivery. The bridge could then retain that receipt instead
of minting a new freshness time for replay. Existing Consumer.onMedia exposes a
wall-clock timestamp, not this monotonic datum; converting Date.now retrospectively
is unsuitable for a strict monotonic-age contract. No such API or SDK change was
implemented, committed, or deployed in this audit. Any eventual SDK change needs
its own companion evidence and live-path validation.

Private metadata-only provenance:

- Result `sdk-readable-aged-prime-result.json`, SHA-256 `3caa1052e4ced2ac9b3a48401b533eecb764a6ad9d26ccb0b1bfac9bfba91285`.
- Executed harness `sdk-readable-aged-prime-repro.mjs`, SHA-256 `1c9c4bfb550699783bf311756713948c3d5f3196c29e0d1a2eb30d4d24208a23`.

Both reside in `.evidence/bridge-network-audit-2026-10-12/`; neither contains
actual media or device identities. These are synthetic source-boundary results,
not a substitute for deployed SDK/freshness validation.

## Portable executed reproducer

The tracked script has assertions for identical frame identity, retained original
wall receipt, missing receipt fields on the Readable frame, and the bridge's
new zero-age stamp. It requires the SDK checkout's esbuild dependency and never
opens a camera/network connection:

```sh
node server/scripts/reproduce-sdk-aged-prime.mjs /path/to/eufy-sdk --out /tmp/aged-prime-result.json
```

The portable version was executed once and passed the same focused assertions.
Its timers/readables are disposed and Date.now restored in a finally block.

- Tracked script SHA-256: `fa583bd1df754f60fe7b0487eddd25f40d6b96059bd3f4afec22f628dc2afda9`.
- Private portable result SHA-256: `2bb73a254056cbcd0e2559b2c1a71fdeacbc9e94a783a67b1a5771ade186d94b`.

This is an offline proof of a receipt-preservation contract gap. An eventual
upstream SDK change must preserve monotonic receipt and source identity through
prime/queue/delivery, with its own companion evidence and real live-path gate;
reconstructing strict monotonic age from the wall timestamp is insufficient.
