# Doorbell cutoff: current evidence, 2026-10-23

The initiating cutoff remains unexplained. No tested STOP, ACK, keepalive or timeout change has demonstrated a durable playback fix. The separate authenticated-ECC dispatch defect is fixed in an unpublished-to-production SDK branch; it does not prevent the upstream cutoff.

## Decisive comparisons

**Other bridge HomeBase streams are not required.** A4 restarted the original multi-camera configuration: API freshness began at+40.184s, then reached7.770s stale at+51.252s. Its raw hooks were blind because observer CommonJS classes differed from the running ESM classes; this run supports no raw-packet claim.

SOLO disabled exactly five sibling HomeBase cameras through the supported settings API and performed a matched restart. Doorbell first became fresh at+10.091s, then reached6.997s stale at+52.206s:42.115s from first observed freshness to that sample. Three distinct sampled outages occurred. A cached-registry inspection confirmed one actual HomeBase source, channel0, live with one consumer, and matching runtime prototypes. This excludes concurrent *bridge* HomeBase sources as a necessary condition; recording, radio conditions, peer behavior and other external clients remain unexcluded. Original settings were restored.

**Allowing the SDK's retry did not recover this endpoint.** A supported diagnostic changed only `stall_ms` from5000 to15000, preserving startup30000. After last video at+69.199s, the same source/session with one consumer allowed SDK reassertion. Four full STARTs occurred6.018/9.027/12.035/15.032s into silence; raw, assembled and emitted video did not resume, while PONG continued. Bridge reset followed at15.137s. The5s/6s ownership race exists, but extending the timeout was not a fix. Exact original settings content was restored at revision10; effective stall5000/startup30000.

**ECC dispatch is a separate proven defect.** Before, valid ECIES unwrap produced32 bytes, but a truthy legacy result bypassed the configured ECC decoder. Authenticated-first dispatch then emitted42 authenticated-GCM plus1084 plaintext frames:1126 frames, approximately13.51fps across83.336s, before another source reset. Fork commit[`cfaf257`](https://github.com/sapireli/eufy-sdk/commit/cfaf257bb53a6011570428cbd8765364ce2e42d7), branch `fix/prefer-authenticated-ecc-decoder`, passed4446 tests. It is **not deployed**. The SDK's `docs/agents/authenticated-ecc-decoder-proof.md` contains the canonical proof.

ACK batching reduced packets79.33% without eliminating outages and was removed. Periodic1139 received no recovery credit:120 frames already flowed before injection; removing it for120s caused no recurrence. A fresh no1139 source then ran180s without a6s cutoff. Necessity is unproven; candidate code remains undeployed.

## Remaining limits and restored state

Bridge NIC receive/error/drop and softnet counters were zero. All900 ACK-sized UDP probes reached the Mac; this tests that LAN peer, **not HomeBase receipt**. A comparable native-app session longer than30s is unavailable: iPhone Developer Mode is disabled (`devicectl`10005); no RVI capture remains active. Official T8214 documentation describes wired battery charging and120s event clips, not a live-view duration limit. Firmware or a normal timeout is not established as the cause.

Temporary hooks and overrides are removed; inspector closed. Playback source, SDK pin and original settings content remain unchanged by these diagnostics.

## Private evidence hashes

Files reside in `/tmp/eufy-doorbell-runtime-20261023/` unless stated otherwise.

| Artifact | SHA256 |
|---|---|
| `capture-restart-A4.json` | `f3689f044d723f4f721e8c060ec0e70835e79fb15c877d7edac275aad01b9a3e` |
| `capture-solo-B.json` | `443b157cbf94ad6a4d66d520211b17c98eda1a8c7d952f3394a8a473ac9a0fbb` |
| `actual-registry-during-solo.json` | `4914213a9674a67aeadf6daec2593279a7fb1e120db6de5541e6f12ef97f59cc` |
| `capture-stall-ownership-hot.json` | `54d4dc703afa29853275753f16d2d815c181db87218395e46249bbe98b524343` |
| `capture-fresh-native-ECC.json` | `89d730782cb92e971468b7f06a6658373fa08ed7c9c7b74598e38ee2c453a2ba` |
| `capture-fresh-ECC-dispatch.json` | `0ac3d1bd025cfab834b8b52dd54496fca0632e05fb8b15db87d189b4a1c039df` |
| `/tmp/eufy-ack-sized-udp-lan-check-20261023.json` | `b8dabb757b5fdeffd157186c0fa10ae8f4e2aa4c7abcb473a15bb68baf4dc44b` |
