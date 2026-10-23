# Doorbell cutoff: current evidence, 2026-10-23

The initiating cutoff remains unexplained. No tested STOP, ACK, keepalive or timeout change has demonstrated a durable playback fix. The separate authenticated-ECC dispatch defect is fixed in an unpublished-to-production SDK branch; it does not prevent the upstream cutoff.

## Decisive comparisons

**Other bridge HomeBase streams are not required.** A4 restarted the original multi-camera configuration: API freshness began at+40.184s, then reached7.770s stale at+51.252s. Its raw hooks were blind because observer CommonJS classes differed from the running ESM classes; this run supports no raw-packet claim.

SOLO disabled exactly five sibling HomeBase cameras through the supported settings API and performed a matched restart. Doorbell first became fresh at+10.091s, then reached6.997s stale at+52.206s:42.115s from first observed freshness to that sample. Three distinct sampled outages occurred. A cached-registry inspection confirmed one actual HomeBase source, channel0, live with one consumer, and matching runtime prototypes. This excludes concurrent *bridge* HomeBase sources as a necessary condition; recording, radio conditions, peer behavior and other external clients remain unexcluded. Original settings were restored.

**Allowing the SDK's retry did not recover this endpoint.** A supported diagnostic changed only `stall_ms` from5000 to15000, preserving startup30000. After last video at+69.199s, the same source/session with one consumer allowed SDK reassertion. Four full STARTs occurred6.018/9.027/12.035/15.032s into silence; raw, assembled and emitted video did not resume, while PONG continued. Bridge reset followed at15.137s. The5s/6s ownership race exists, but extending the timeout was not a fix. Exact original settings content was restored at revision10; effective stall5000/startup30000.

**The cutoff is visible before Node receives media.** A simultaneous NIC capture and Node observer recorded a 5.826203 s VIDEO gap on the same HomeBase-to-bridge tuple, `192.168.23.233:10985 → 192.168.23.158:47072`. Node received no VIDEO during that wire gap, while a peer PONG arrived inside it. Capture reported zero kernel drops. Event-loop delay peaked at 164.102 ms overall and 138.412 ms near the gap, not seconds. This excludes a Node-only receive/callback backlog for that interval; it does not distinguish HomeBase transmission from camera-to-HomeBase or network delivery problems.

**Native evidence includes a longer, authenticated live request.** A private copy of the October 20 iPhone capture, recovered by removing its extra 24-byte prefix without changing the original, has 160.540 s of channel 2 VIDEO, maximum raw VIDEO UDP gap 1.038 s, and an authenticated START-to-STOP interval of 160.891566 s. Its START uses `streamtype:2`, matching the bridge, with `camera_type:0`, `entrytype:0`, and a 128-character hexadecimal key that validates as a P-256 public point. Its STOP is wrapped 1350/1004 with `payload.channel:2`. All 28 unique complete outbound 1350 commands authenticated; no key or account values were exported. The other October 9 captures supply 26.278 s and 103.099 s channel 2 VIDEO windows but lack their START/gateway exchange. These references are useful comparisons, not proof of the current physical channel 0 camera's behavior.

During the 160.540 s native window there were no UDP PING/PONG or 1139 commands on the inspected direct HomeBase connections. ACKs and VIDEO continued; incoming ACKs had a 158.741 s gap between initial requests and STOP. This motivated a media-backed path-health experiment, rather than a claim that a faster heartbeat was required.

**Media-backed health did not prevent the cutoff.** The final temporary experiment covered all HomeBase sessions, including new private instances. Candidate phase B began at +30.080 s; the first valid observation was at +85.656 s, after a 55.576 s recovery prefix. The raw-gap decision at +178.446 s was 92.790 s after that first valid observation. Raw media last arrived at +172.396 s, followed by 6.050 s without raw media before the decision. The source stopped producing valid output while still LIVE with one consumer; the consumer subsequently dropped. The wrapper suppressed 83 PINGs, retained initial/idle probing and observed no `pathStale` events. All five hooks and the getter were restored. This candidate remains uncommitted and undeployed; it is not a playback fix.

**ECC dispatch is a separate proven defect.** Before, valid ECIES unwrap produced32 bytes, but a truthy legacy result bypassed the configured ECC decoder. Authenticated-first dispatch then emitted42 authenticated-GCM plus1084 plaintext frames:1126 frames, approximately13.51fps across83.336s, before another source reset. Fork commit[`cfaf257`](https://github.com/sapireli/eufy-sdk/commit/cfaf257bb53a6011570428cbd8765364ce2e42d7), branch `fix/prefer-authenticated-ecc-decoder`, passed4446 tests. It is **not deployed**. The SDK's `docs/agents/authenticated-ecc-decoder-proof.md` contains the canonical proof.

ACK batching reduced packets79.33% without eliminating outages and was removed. Periodic1139 received no recovery credit:120 frames already flowed before injection; removing it for120s caused no recurrence. A fresh no1139 source then ran180s without a6s cutoff. Necessity is unproven; candidate code remains undeployed.

**Sibling END suppression also failed.** An offline audit found other-tuple END packets2.148s and1.149s before the two captured VIDEO gaps, but two other post-start ENDs were followed by healthy target video. A subsequent45s original baseline sent three sibling ENDs. Without restarting playback, the intervention suppressed three ENDs from the unused channel1 private sessions. The same already-healthy Doorbell source then reached5.008s without raw VIDEO at68.439s of exposure, still LIVE with one consumer. Its raw count advanced5765→16279 and valid-frame count504→1499 before the gap. All three hooks were restored; this intervention is rejected as a fix. This excludes those sibling ENDs as a necessary trigger for the observed recurrence, not every possible session-control interaction.

## Remaining limits and restored state

Bridge NIC receive/error/drop and softnet counters were zero. All900 ACK-sized UDP probes reached the Mac; this tests that LAN peer, **not HomeBase receipt**. Existing native captures include the longer windows above; a new controlled phone recording is currently blocked by iPhone Developer Mode being disabled (`devicectl`10005). No RVI capture remains active. Official T8214 documentation describes wired battery charging and120s event clips, not a live-view duration limit. Firmware or a normal timeout is not established as the cause.

Temporary hooks and overrides are removed; inspector closed. The media-health candidate and all temporary runtime interventions were reverted. Production retains SDK48e4f2f and the original settings content at revision10. The initiating cutoff remains unresolved.

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
| `capture-media-health-all-HB.json` | `cf6daec0567c30a08df85fac3995f2b1639b2fed81cc9c8443c54163aed9d221` |
| `kernel-door-boundary.pcap` | `1e4c71377890d29e0d17312ed47bc82a98460e7e2afbc20f3ac8044c7dc7e0eb` |
| `capture-kernel-boundary.json` | `3ed2f9b8d58bbaa4fa12814c4a0ed3ca7e93b0860a24bd0d9cfed57e74aee66b` |
| `kernel-boundary-exact-times.json` | `baef8940846c105e85ec846401bf41387ea10afb3d4e6fa75c3574a8ba476295` |
| `kernel-boundary-summary.json` | `e16c421203588dc19917de940f1e311b56e5d78525dd1bb4c2c4b7d99401e2df` |
| `kernel-door-boundary-tcpdump.log` | `adb7987d607fbd719db85ba702aa776da37bc2e9725f10123f3cef79cbec7bc0` |
| `~/Downloads/eufy-iphone-fresh.pcap` | `bbb2b16c18424d105a5c1aa44a700deb3b5b458c1d5f14f7de23fb379f0419e3` |
| `/tmp/eufy-iphone-fresh-recovered.pcapng` | `6afc490fa6b68d9819e5b9f414c68da621580818df8383a2ec422cd159c5990d` |
| `/tmp/eufy-fresh-native-start-stop.json` | `d2164da7dbcdc07d294ef62b3714da77986f86bfa364290ccda439681e3a7b86` |
| `/tmp/eufy-longer-native-liveness-census-20261023.json` | `a6569e42348397c2010393744bd4f9e2eabdb7d91ee33a3393fe66c759449415` |
| `/tmp/eufy-kernel-end-gap-comparison-20261023.json` | `7f0ef68ee7f2820f4cdfa5c66b3bbd786737cb1491d664883a55a6a38d55677e` |
| `/tmp/eufy-doorbell-runtime-20261023/capture-sibling-END.json` | `1d3dc687b9d3103ddbfe67186f5a0b9ac84cfaaedfd7d796ecc454e18911cb97` |
