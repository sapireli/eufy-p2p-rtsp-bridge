# SDK retained-prime receipt scope audit

One focused offline fixture bundled the actual SDK SharedLiveSource and
openReadableFromConsumer source with esbuild, then used the bridge's actual
createFreshness.record function. The transport was a synthetic EventEmitter;
no device, network, decoder, SDK source modification, or live service change
was involved.

A frame arrived at synthetic wall1000ms/monotonic1000000us. After detaching and
reattaching inside linger at wall6000ms/monotonic6000000us, the source replayed
**the identical frame object** as its retained keyframe. Consumer.onMedia still
exposed the original timestampMs1000, so its retained age was5000ms. The object-
mode Readable emitted only the LiveVideoFrame, with no receipt metadata. Bridge
record(frame) restamped it at6000000us and assigned new frameID2 (original1),
giving the replay a reported bridge-ingress age0. Its sourceTimestamp word stayed
unchanged. The focused assertions passed; all owned source/readable timers were
closed and the original Date.now restored.

This proves a freshness scope gap for retained SDK replay: time first received
by the SDK source is lost before bridge stamping. It does **not** prove a camera
capture age, explain the31-second source pauses, or identify the reason for the
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
