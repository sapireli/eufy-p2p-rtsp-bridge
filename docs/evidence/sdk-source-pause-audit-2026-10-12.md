# Where the remaining live-video pauses begin

## Conclusion and boundary

Two passive 90-second captures found pauses **before SDK video delivery**.
They do not support changing SDK keyframe gating, ACK scheduling or session
selection as the repair for those observed pauses. They also do not establish
a camera firmware defect or identify which upstream link/process delayed the
frames. That distinction remains unresolved.

The captures did not change camera quality, stream selection, topology or
control settings. Freshness enforcement was disabled during this investigation;
the legacy live-video path was being observed. No SDK source changes were made.
Only timing, packet metadata and anonymous session/socket/peer identities were
retained. Camera pictures, keys, account identifiers and complete device serials
were not recorded by the diagnostic tool.

## Capture 1: exclude session-global keyframe gating

Window: **2026-10-12 15:25:59.666–15:27:29.669 UTC**.

The SDK currently emits `videoGap` at session level. Each LiveStream subscribed
to that session discards its incomplete unit and waits for its own next keyframe.
That design could suppress another camera if multiple cameras shared a session;
it must not be assumed safe without measuring actual membership and gate cost.

In this deployment the cameras did **not** share a media session:

| Camera | Runtime trace identity | Channel | Attached to HomeBase |
| --- | --- | ---: | --- |
| Garage | `station-3` | 1 | Yes |
| Front Door | `station-1` | 2 | Yes |
| Balcony | `station-5` | 0 | No |

The observer intercepted completed access units before the LiveStream gate and
the subsequently emitted frames:

- 4,960 assembled units and **4,960 emitted frames**.
- **Zero** `videoGap` events, incomplete-unit discards or gated assemblies.
- Zero observer overflow.

Thus gate cost cannot be measured from this window because no gate occurred.
It nevertheless directly excludes gating as the cause of the pauses that did
occur within the same window. It does not prove that session-global gating is
safe for other consumers that genuinely share a multi-camera session.

### Front Door's 2.233-second pause

| Stage | Observer elapsed time | Source word |
| --- | ---: | ---: |
| Previous picture emitted | 31,672.365 ms | 817,428,283 |
| Next keyframe's first observed UDP start header | 33,791.549 ms | 817,428,348 |
| Keyframe's reassembled P2P frame delivered | 33,903.542 ms | 817,428,348 |
| Complete keyframe emitted | 33,905.172 ms | 817,428,348 |

The emitted-picture gap was **2,232.807 ms**, but the source word advanced only
**65 ms**. The next keyframe contained **229,734 encoded bytes**. Its first
observed start header arrived **2,119.441 ms** after the preceding start header.
After that header, frame reassembly took approximately **111.993 ms**, followed
by approximately **1.630 ms** to complete and emit the access unit.

Garage emitted 34 frames and Balcony 56 frames during this Door pause. A
station-global gate or a two-second JavaScript event-loop stop does not explain
that simultaneous continued delivery.

The full six-byte wire number was epoch-like. The previous UDP header arrived
at Unix millisecond `1791818791338`, with wire value `1791818790715`: numeric
difference 623 ms. The next keyframe header arrived at `1791818793458`, with
wire value `1791818790780`: difference **2,678 ms**. That same-camera apparent
wire age increased by 2,055 ms. The following three source words advanced
69/66/70 ms while their headers arrived within a 2.468 ms burst.

This is evidence of uneven upstream delivery/backlog relative to the source
clock. Neither the six-byte number nor the printed OSD clock has independently
verified sensor-capture semantics; this is not an absolute capture-age claim.

**Limitation:** this first UDP tap recorded frame-start headers only when they
began at the datagram body's first position. It did not record every datagram
or every potentially embedded later header. Therefore this result is stated as
“first observed start header,” not “no UDP traffic for 2.119 seconds.” Capture 2
addresses that missing observation.

## Capture 2: every video datagram and its ACK

Window: **2026-10-12 15:34:59.950–15:36:29.960 UTC**.

This observer recorded every video-type DATA datagram at `P2PSession.onMessage`,
before SDK sequence reordering. It recorded SDK ACK invocation, actual
`dgram.Socket.send` identity, destination peer identity, callback completion and
error code. It also observed media start/stop calls and retained only channel,
topology, force and stream selector flags.

These are process receive-callback timestamps, not NIC/kernel arrival
timestamps. They exclude the SDK reorder/assembly stage but cannot locate
unobserved delay before the callback. Physical-interface capture is the next
boundary needed for that distinction.

### ACK results

| Measurement | Result |
| --- | ---: |
| Incoming video-type datagrams | **37,132** |
| ACK send callbacks | **37,132** |
| ACK callback errors / still pending at capture end | **0 / 0** |
| ACK sent on a different socket from its received packet | **0** |
| ACK sent to a different peer from its received packet | **0** |
| Observer overflow | **0** |

| Camera | Receive→ACK invocation median / P99 / maximum | Socket-send→callback median / P99 / maximum |
| --- | --- | --- |
| Front Door | 0.078 / 0.329 / 6.975 ms | 0.259 / 2.330 / 54.613 ms |
| Garage | 0.079 / 0.336 / 15.841 ms | 0.222 / 2.703 / 35.237 ms |
| Balcony | 0.099 / 0.373 / 4.183 ms | 0.275 / 2.016 / 22.250 ms |

All three sockets reported receive buffers of **425,984 bytes** and send buffers
of 212,992 bytes. A small receive-buffer ceiling alone is not evidence that a
particular pause was caused by kernel loss. This capture does not claim to
measure kernel packet-drop counters.

Successful UDP callbacks prove local submission/completion, **not that the
camera/HomeBase received or accepted each ACK**. The measurements do exclude
hundreds of milliseconds of SDK ACK invocation delay, mismatched local sockets
and wrong destination peers in the observed window.

No media start or stop was issued for either attached camera during the window.
The thirty `startLiveMedia` calls were exclusively Balcony's standalone
three-second keepalive path. The SDK distinguishes that call from an actual
full restart when an own-session channel is already started. The observation
therefore cannot be described as thirty camera restarts.

Again there were zero `videoGap`, unit-discard or gate events. All 4,938 assembled
units were emitted.

### Garage's 714 ms pause was already present on incoming video traffic

The longest Garage emitted-frame gap was **713.670 ms**, ending at a **146,894
byte keyframe**. Its source word advanced only **68 ms**.

The last observed duplicate datagram before the quiet interval was sequence
4514 at elapsed 52,332.611 ms. The next incoming video datagram was sequence
4517 at 52,961.887 ms: **629.275 ms with no video-type datagrams received on
that session**. Sequences 4515 and 4516 had already arrived before the quiet
interval; this was not a missing-sequence hole at the transition.

| Sequence | Receive elapsed | ACK invocation | ACK callback |
| --- | ---: | ---: | ---: |
| 4515 | 52,328.599 ms | 52,328.627 ms | 52,328.828 ms |
| 4516 | 52,329.361 ms | 52,329.419 ms | 52,332.297 ms |
| 4514, duplicate | 52,332.611 ms | 52,332.682 ms | 52,332.920 ms |
| 4517, next new packet | 52,961.887 ms | 52,962.120 ms | 52,962.470 ms |

The next keyframe's reassembled header was delivered at 53,041.198 ms and its
video unit emitted at 53,043.308 ms. Most of the pause happened before incoming
video resumed; the SDK then spent approximately 79 ms receiving/reassembling
that keyframe and 2 ms completing/emitting it.

Repeated datagram sequences were observed: 269 for Door, 298 for Garage and
959 for Balcony during this window. Those duplicates alone cannot distinguish
sender retransmission from another source of network duplication.

### Keyframe association

Of Garage's 48 emitted-frame gaps longer than 200 ms, **43 ended at keyframes**;
there were 46 keyframes in the window. Door had 89 such gaps, of which 33 ended
at keyframes, so its jitter was not exclusively keyframe-related. This supports
investigating upstream complete-frame production/transfer and sender bursts.
It does not establish which device or radio/network segment caused them.

## Current request and route: preserve explicit selector 2

A separate read-only options snapshot at **15:46:06.973–15:46:08.981 UTC**
verified the running LiveStreams' options:

| Camera | Requested `streamType` | Runtime topology | Target identifier prefix |
| --- | ---: | --- | --- |
| Garage | **2** | Attached, channel 1 | `T8030` |
| Front Door | **2** | Attached, channel 2 | `T8030` |
| Balcony | **2** | Own-session, channel 0 | `T8423` |

The prefix is retained instead of the complete device identifier. Both attached
streams dial HomeBase. The explicit selector agrees with the historical
[authenticated app-request handoff](../sdk-streamtype-pr-handoff.md).
`streamType`/wire `streamtype` is not the dual-view composition selector or the
other own-session start branch's `video_type` field. No selector was changed.

The current SDK source has no public `openReadable` option to bypass HomeBase
for an attached camera:

- `resolveSession()` obtains `parentSn` from the resolved device topology.
- `openMediaSession(parentSn,channel)` creates an independent session for a
  camera but still passes **parentSn** to endpoint resolution.
- `SharedSourceHints` exposes stream type, power and retention options; it does
  not expose a media-route override.
- `localAddresses` is explicitly keyed by **parent-station serial**. It changes
  that resolved parent's address, not the camera's topology or handshake.
- `acceptP2PPeer` accepts/rejects candidate peers; it does not create a separate
  direct-camera endpoint or change the selected parent.

This is a source/API fact, not proof the hardware cannot use another route.
Eufy's [model-specific E340 FAQ](https://service.eufy.com/article-description/Floodlight-Cam-E340-FAQ?urlName=Floodlight-Cam-E340-FAQ)
describes both HomeBase support and operation without HomeBase, plus dual-band
Wi-Fi. It does not document a third-party SDK option for bypassing HomeBase
while retaining the current attached-device configuration. A generic older
Floodlight NAS/RTSP article is not sufficient evidence to declare a particular
T8425 firmware's alternative route supported or unsupported.

## What a subsequent repair must prove

These observations narrow the boundary but are **not a completed root-cause
repair**. A firmware, radio, HomeBase forwarding or sender-window diagnosis
still requires corresponding evidence. A different request/transport must be
shown to preserve the required selector, camera composition and native quality;
it cannot be inferred from hardware marketing or substituted silently.

There is no evidence here supporting a selector change, resolution reduction,
larger presentation buffer, SDK gate removal or ACK/socket patch. Transport
capture outside the process can establish whether packets/ACKs leave or arrive
at the physical interface; observing the camera/HomeBase side or an independently
captured equivalent first-party session is needed to locate the upstream delay
more precisely. No such control experiment is claimed in this document.

## Reproduction and provenance

`server/scripts/capture-sdk-gaps.mjs` observes an already-enabled, explicitly
supplied loopback inspector; it never opens one or sends camera commands.
Maximum capture is 110 seconds with a cleanup guard of at most 120 seconds.
All installed method wrappers restore in `finally`, including partial hook
installation failure. A normal capture uses 90 seconds and records at most
200,000 rows; the two measured windows reported no overflow.

```sh
rtk proxy node server/scripts/capture-sdk-gaps.mjs \
  --inspector http://127.0.0.1:19231 --seconds 90 \
  --out .evidence/frame-freshness-2026-10-12/sdk-ack-capture.json
```

Private files and SHA-256:

| Artifact | SHA-256 |
| --- | --- |
| `sdk-gap-capture.json` | `39f8c87d97d09fd2a8ad9bb15329602c8125ffd67e7607bd4d2dd3ebafa6e381` |
| `sdk-ack-capture.json` | `de27f057dd02511f42cad2a5a9f1a7fe68158faa9d27d346c7158de184c64475` |
| `sdk-route-options.json` | `d47b811064ae93226488f4040ba6f8b72e8bf011a2582fadd228b7ed6372c9d9` |

The artifacts are under `.evidence/frame-freshness-2026-10-12/`; derived
`sdk-gap-analysis.json` and `sdk-ack-analysis.json` retain the exact selected
stage records. Session/socket/peer IDs are capture-local and must not be joined
between windows as permanent device identities.


## Physical-interface correlation: 15:55:47–15:57:17 UTC

A third passive run observed the physical `eth0` interface and the existing SDK
sessions concurrently, without changing camera requests or source settings.
The interface observer used Linux `AF_PACKET` and `SO_TIMESTAMPNS`; these are
**kernel software packet-arrival timestamps, not hardware NIC timestamps**.
It saved only lengths, ports, anonymous peer IDs, sequence numbers and timing.
No video payloads or complete device identifiers were saved.

The packet tap ran **15:55:47.201326–15:57:17.203708 UTC**. Its receive-buffer
was 425,984 bytes; 56,990 packets reached the tap, with **zero tap drops**, zero
truncations and zero missing timestamps. It retained 38,476 relevant UDP records.
The overlapping SDK observer ran **15:55:48.190–15:57:18.199 UTC**: 38,306 video
receive callbacks, 38,306 completed ACK callbacks, and **4,955 assembled units =
4,955 emitted units**, with no `videoGap`, unit discard, gating or overflow.
Every observed camera retained explicit `streamType: 2`.

| Stream | Largest SDK picture-emission gap | Physical video-datagram quiet interval inside that gap | Boundary sequence numbers |
| --- | ---: | ---: | --- |
| Garage | 906.767453 ms | **843.100918 ms** | 55269 → 55270 |
| Front Door | 614.923452 ms | **465.844445 ms** | 64359 → 64360 |
| Balcony | 541.139792 ms | 61.872798 ms | 13256 → 13255, out of order |

Garage's physical boundaries were **15:56:56.025363989** and
**15:56:56.868464907 UTC**, local UDP port 36637. The same consecutive sequence
numbers reached the SDK 842.967206 ms apart. The subsequent picture was a
66,188-byte **interframe**, with source-word increment 66: the longest pause in
this run was not exclusively an IDR phenomenon. Front Door's boundaries were
**15:57:12.038844678** and **15:57:12.504689123 UTC**, local port 58175; its next
picture was a 227,788-byte keyframe, also with source increment 66.

For these two selected gaps, packets were already absent at the interface before
the SDK received them. A JavaScript event-loop backlog, SDK keyframe gate, or
client presentation queue cannot explain that portion of the pause. This locates
it upstream of the host's application receive path; it does **not** distinguish
camera production, camera-to-HomeBase forwarding, HomeBase scheduling, or the
network path. The smaller physical quiet interval on Balcony does not explain its
entire picture gap; its out-of-order datagrams require separate reassembly analysis.
No SDK patch is justified by these measurements.

**ACK boundary:** this IPv4 packet-tap run captured ingress only. It does not
verify physical ACK transmission or peer receipt. Earlier ACK callbacks establish
successful local socket submission, with the limitations described above.

**Instrumentation exclusion:** exporting the completed inspector result was
followed by a 7,795.721 ms Door RTSP completion gap and a 7,675.242 ms Garage gap.
Those intervals began after the SDK capture ended and are excluded from the
source-pause evidence. An overlapping independent loopback RTSP observation
joined source timestamps to RTP timestamps with an exact constant phase for 957
Door and 977 Garage units within the observed window; the longest observed
SDK-ingress-to-loopback-completion ages were 673.8 and 996.3 ms respectively.
These measure complete encoded access-unit receipt, not capture or display age.
The export-associated stall is an observer perturbation, not production-source
proof, and must not be attributed to the camera.

Reproduction uses `server/scripts/capture-udp-metadata.py` on Linux with root or
`CAP_NET_RAW`, bounded to 90 seconds, alongside `capture-sdk-gaps.mjs`. Both
observers had closed their sockets/restored their hooks before deployment resumed.

| Private artifact | SHA-256 |
| --- | --- |
| `wire-udp-metadata.jsonl` | `5025953bed65a1f2696fa27895ac106dfa9cb479ff17cea556d68c95f45e6991` |
| `sdk-wire-correlated-capture.json` | `b00ba1dfddec1807f352b2d20c1c78b0c60c787824cc279dd63550498bdac510` |

Both are in `.evidence/frame-freshness-2026-10-12/`. Derived
`kernel-sdk-gap-analysis.json` records the exact boundaries; duplicate sequence
numbers are matched by session, packet length and nearest wall-clock time rather
than assuming each sequence number occurs only once. The independent loopback
trace is `.evidence/bridge-network-audit-2026-10-12/sdk-correlated-loopback.jsonl`.


### Export perturbation mitigation (implemented, not live-validated)

The original observer returned its entire approximately 35 MB row array in one
`Runtime.evaluate` response. This serialized the result inside the running bridge
and coincided with the excluded post-capture 7.8-second delivery pause. The tool
now restores all hooks **before exporting**, returns only a small header, and
retains the bounded 200,000-row metadata array in an identified diagnostic cache.
The host requests at most **500 rows per response**, sequentially, with a 10 ms
yield between requests. Late ACK callbacks no longer append after capture ends.
A `finally` block deletes the cache on success, timeout or export failure; an
independent 120-second cache expiry guard covers a disconnected inspector. A
canceled capture cannot subsequently install an orphaned export cache.

Mock-inspector tests exercise exact ordering for 200,000 rows, the 500-row response
bound, sequential calls/yields, failure cleanup, short batches, and cache identity
isolation. Syntax and four focused tests passed. **No new live capture was run.**
This reduces one known serialization mechanism; it does not prove the observer
is nonintrusive or that the production pause has been eliminated. Any future
measurement must still compare observer-on/off delivery and exclude perturbed
export intervals.


## Offline multi-camera session audit

The current router does **not** open a new control session for each live consumer.
`mediaProviderFor().live()` and `.openReadable()` both use `sharedLiveSourceFor()`;
its cache is keyed by parent station plus camera channel. Additional viewers of
the same camera reuse that source. `openSession()` uses the session manager's
coalesced acquisition by key, rather than opening a socket on every call.

For attached cameras, the first live source claims the station's existing session.
A second concurrent camera uses `<parent>#live:<channel>` only when the existing
station session is already claimed. This reservation is installed before awaiting
connection setup, so simultaneous starts cannot both claim the same tunnel.
The dedicated media connection still authenticates/dials the parent station;
it is not a direct connection to the attached camera. A media-only connection
is deliberately unannounced to avoid duplicating station-control notifications.
`stationSessionInUse()` checks connection ownership rather than merely counting
any busy camera. The source's stop closes its dedicated media connection.

These are source-code facts in `src/transport/p2p/command-router.ts`:
`mediaProviderFor`, `sharedLiveSourceFor`, `stationSessionInUse`, `openMediaSession`
and `openSession`; `__tests__/media-session-opens.spec.ts` contains distinct-socket,
simultaneous-start and lifecycle coverage. The SDK's explanatory comments describe
prior first-party display captures and concurrent-camera reproduction, but those
older captures were **not independently repeated in this audit**.

The public live options do not expose a switch to consolidate attached-camera
media sessions or bypass the parent station. Consolidating the two active cameras
would contradict the SDK's stated one-camera-per-tunnel routing contract and
requires wire evidence before it can be proposed as a repair. Conversely, the
three streams observed here alone do not constitute a complete station-session
inventory: an earlier snapshot or abandoned source could have retained the
station session. A read-only inventory of all session keys, source states,
consumer counts and source-to-session assignments would establish whether an
extra unused connection exists. This audit did not run that additional probe or
change session ownership. Nothing in the current packet-gap data proves redundant
control connections or a HomeBase queueing defect.

Attached live streams settle periodic start reassertion after their own media
arrives and resume it on channel silence; the capture recorded no attached
`startLiveMedia` or `stopLiveMedia` calls during the measured windows. Therefore
these particular sub-second pauses are not correlated with the bridge repeatedly
sending a camera start. Standalone Balcony's periodic start calls are distinct
and must not be counted as attached-camera restarts.


## Read-only link/topology boundary

Subsequent host inspection establishes that the bridge's physical `eth0` link
is **1,000 Mbps, full duplex**, up. Both previously captured sender peers are
directly routed on that interface in the same LAN subnet, without an IP gateway.
Private capture-argument mapping identifies the attached Front Door and Garage
sender as HomeBase, and the separate Balcony sender as its standalone camera.
Neighbor entries exist for both peers. Current bridge configuration forces LAN
peer selection within the LAN CIDR and supplies no station-address overrides.

This proves the bridge is wired and receives these streams from LAN peers. It
**does not prove HomeBase is wired**, that either camera reaches HomeBase through
a particular radio band, or that a Wi-Fi/hardware queue is the bottleneck. An
Ethernet receiver can receive a Wi-Fi sender through an access point; ARP entries
and local IPs do not reveal that upstream physical path.

The SDK exposes a cached `wifiRssi` camera property (reported parameter 1142), but
its description does not specify physical units or a calibrated quality scale.
That live cache is not exposed by the bridge camera API or persisted in the SDK's
authentication session file. No inspector was opened or authentication file read
to retrieve it. Therefore there is no current signal-strength or HomeBase-uplink
measurement in this evidence. The remaining camera→HomeBase/link boundary cannot
be subdivided with the permitted host state alone.

IP/MAC details are retained only in the mode-0600 private artifact
`.evidence/frame-freshness-2026-10-12/private-peer-link-metadata.json`, SHA-256
`8219bb953340bf936dcc131513f3c85e8e6ed8c95759aa089f4256399cf3451b`. No upstream interface, firmware or camera controls were changed.


## ACK transmission at the interface, with observer-overhead control

The original IPv4-specific tap could not observe outgoing packets. The tool now
uses `ETH_P_ALL` with a **kernel classic-BPF IPv4/UDP filter**, followed by strict
userspace IPv4/UDP/protocol validation. It stores ACK sequence lists and timing,
not payloads. Outgoing timestamps remain **kernel software packet-tap times**:
they establish that the local stack produced an outgoing packet at this interface,
not successful NIC transmission, delivery over the network, or peer acceptance.

### Concurrent SDK and interface capture

The SDK window was **16:13:01.684–16:14:31.691 UTC**, on the complete-AU
Matroska production path. The enclosing 95-second packet tap retained 82,162
metadata records with **zero drops, truncations or missing timestamps**.
Every observed DATA packet inside the clipped SDK window had a matching physical
ACK packet, paired by capture-local UDP port and sequence in arrival order:

| Stream | DATA RX / paired ACK sequences | RX→physical ACK median / p99 / maximum |
| --- | ---: | --- |
| Garage | 12,781 / 12,781 | 26.680 / 152.754 / 185.601 ms |
| Front Door | 13,979 / 13,979 | 44.028 / 146.354 / 179.804 ms |
| Balcony | 11,937 / 11,937 | 4.073 / 78.993 / 103.803 ms |

One additional Garage ACK corresponds to a DATA boundary outside the clipped
window. All 38,698 SDK ACK callbacks completed without error. Assembled and
emitted unit counts both equaled 4,953; no gap/gate/discard event was observed.
The SDK's receive-callback→ACK-invocation median was approximately **0.075 ms**
for both attached streams. Matching physical RX records to SDK callbacks by
sequence and packet length put most additional time **before the callback**:
Garage median 25.930 ms, Door 43.244 ms (wall-clock comparison uncertainty 1 ms).
This observation alone cannot distinguish normal host delivery/scheduling from
observer overhead.

Garage's longest physical video quiet interval was 833.312718 ms. Its three ACK
packets during that interval had all reached the outgoing tap within 1.926625 ms
of the final DATA packet; another 831.386093 ms passed without video arrival.
Door's 426.240859 ms interval similarly had its final ACK at 1.192600 ms, followed
by 425.048259 ms without video. This does not support withholding the final local
ACK as the explanation for either entire pause.

### Interface-only repeat: no target-process hooks

To test whether SDK instrumentation created the delay, a separate **90-second
interface-only** capture ran **16:19:41.462823–16:21:11.462936 UTC**, after the
final APK startup had settled. No SDK hooks, inspector evaluations, producer
traces or camera commands ran during this window. The tool retained 70,391
metadata records with **zero tap drops, truncations or missing timestamps**.

| Stream | Positive RX→ACK pairs | RX→physical ACK median / p99 / maximum |
| --- | ---: | --- |
| Garage | 9,902 | 16.323 / 75.446 / 93.335 ms |
| Front Door | 13,763 | 31.276 / 100.134 / 146.064 ms |
| Balcony | 11,310 | 2.079 / 69.776 / 90.150 ms |

The sole unpaired Door DATA record arrived **0.896 ms before the tap closed**;
its ACK is right-censored by capture termination. It is not evidence of a missing
ACK. Balcony had reopened on a different local port; its identity here follows
the private standalone peer mapping, rather than reusing a prior session ID.

The later attached-stream medians were 10–13 ms lower than during SDK observation.
This supports treating the heavier observer as a possible source of additional
delay. The comparison is sequential, with different source traffic, **not a
randomized identical-packet experiment**; it does not quantify overhead precisely.
Substantial 16/31 ms ACK delay nevertheless remained without target-process hooks.
That residual requires a host receive/scheduling investigation, not an assumption
that the SDK's fast ACK-invocation interval measures physical ingress latency.

More decisively, Front Door had a **2,351.903911 ms video-datagram quiet interval**
on the physical interface without SDK instrumentation. Consecutive sequence numbers
54432→54433 bounded it. The final ACK reached the outgoing tap 0.955838 ms after
the earlier DATA packet; **2,350.948073 ms** then passed without video arrival.
Garage similarly had 515.218405 ms between consecutive 39725→39726, with ACK at
1.808312 ms followed by 513.410093 ms without video. Thus the SDK observer and
local final-ACK delay cannot account for those entire quiet intervals. Peer receipt
and the camera/HomeBase/network production boundary remain unverified.

### Capture validity and resource limits

Two earlier trials are excluded from absence/missing-ACK claims: unfiltered
`ETH_P_ALL` collected unrelated TCP and dropped 32,995 tap packets; the first
UDP-filtered interface-only trial dropped 50 packets with its receive buffer
capped at 425,984 bytes. Their private filenames contain `-dropped`.

The final repeat used `SO_RCVBUFFORCE` **on the temporary observer socket alone**:
8 MiB requested, 16 MiB reported by Linux. No global `rmem_max`, production SDK
socket, system configuration or camera setting was changed. That socket closed
at capture end. The kernel filter avoids unrelated TCP export traffic before
userspace receive, and the bounded inspector export avoids a monolithic result.
These mitigations do not establish zero observer cost. Five focused mock/parser
tests passed; real RX/TX observation and zero-drop capture are separately reported.

Private provenance under `.evidence/frame-freshness-2026-10-12/`:

| Artifact | SHA-256 |
| --- | --- |
| `sdk-physical-ack-capture.json` | `b86312c43d14f7ace1fce15f69608dc912649ebe848623101bd0d75a82b58c08` |
| `wire-udp-ack-metadata.jsonl` | `ce476129651b6f2cb6876bf557b6ff056acd5ce6594f957edd4fd996ec88a97f` |
| `wire-udp-ack-nic-only.jsonl` | `75d4246133adf94d9cf761ef494fee8f49a599eeb95c8fa36d03fbbe52aed2cb` |

Derived `physical-ack-analysis.json` and `nic-only-ack-analysis.json` retain
selected sequence/timestamp boundaries, pairing counts and distribution details.
No SDK, protocol, selector, bitrate or resolution change is justified by this
capture alone.

## Subsequent exclusion: commands during the 18:21 Garage pause

The after-linear-reassembly capture contains a Garage logical-frame gap of
3,183.404816 ms. Physical VIDEO RX on anonymous HomeBase peer `peer-1`, local
port 58455 / peer port 19719, was quiet for **3,162.906655 ms**, between sequence
14444 at Unix millisecond 1791829274220.6802 and sequence 14445 at
1791829277383.587. Consecutive sequences exclude a sequence hole at this boundary;
they do not prove that no earlier loss affected the remote sender.

Filtering **all peer-1 ports**, direction `out`, messageType `208` (DATA), over
[1791829269220.6802, 1791829282383.587] milliseconds — five seconds before the
last VIDEO RX through five seconds after the next — yields **zero records**.
The capture contains 40 outgoing DATA records overall. This excludes a newly
issued SDK start/stop/channel DATA command within this exact interval, including
commands on the sibling HomeBase session. It does not exclude an earlier command
with a persistent effect, another unobserved sender or peer-side ACK handling.

The final VIDEO ACK reached the outgoing interface 1.422925 ms after RX.
During the VIDEO quiet interval the same session still exchanged peer PING/PONG,
host PING/PONG and CONTROL sequences 78 and 79. CONTROL ACK delays were
1.093 and 1.540 ms. Local pending-ACK accounting reached zero. Physical ACK TX
is not proof the peer received or accepted it. No global session outage or
multi-second Node stoppage explains this selective VIDEO silence.

### Consumer and recovery paths audited without changing production

Source references below refer to SDK commit `55dacd3` and bridge commit
`d2662b2`; they are control-flow facts, not a causal repair:

- SDK `src/transport/p2p/live-stream.ts`, `settleKeepalive` / `armStallWatch`:
  attached full-start interval stops on the first own-camera frame. Recovery
  defaults to keepAliveMs × 2 = **6,000 ms** silence; this 3.163-second pause
  cannot itself reach that recovery threshold.
- SDK `src/transport/p2p/shared-live-source.ts`, `onVideo`, `watchReusedStream`:
  initial warm retries stop on the first keyframe; a reused-source join nudges
  once and clears its retry watch on the first subsequent video frame.
- Bridge `server/src/stream-manager.mjs`, `attachConsumer` / `ensureWarm`:
  an HTTP consumer joins the bridge fan-out. `ensureWarm` returns immediately
  when `slot.feed` or `slot.opening` already exists. Only `openFeedInto` calls
  `sdk.openFeed`, and `server/src/sdk-adapter.mjs` opens the SDK readable with
  explicit streamType 2. An HTTP producer reconnect therefore does not cause
  a new SharedLiveSource consumer while the existing SDK feed remains open.
- SDK `src/client/eufy-mega.ts`, `pollOnce`: cloud polling refreshes the registry,
  capabilities and events; it does not directly issue a media start.

At 18:36:51 UTC the live bridge journal showed two compatible hardware-egress
starts after a new RTSP observer; 18:36:55 showed TV holds. The inspected
18:36:30–18:38:30 journal contained no feed error/reopen. This is consistent
with egress producer replacement while SDK sources remain open, but absence
of a normal-level journal entry alone is not proof that no command was sent.

Official [Eufy Smart Display E10 product documentation](https://www.eufy.com/products/t87a0120?fliter_category=security-new-camera)
advertises four simultaneous live feeds and HomeBase S380 bundles. It does not
publish this firmware's PPCS session/window limits or prove that our subscription
sequence matches the app. A universal one-camera-per-HomeBase limitation is not
established by this investigation.

Private provenance under `.evidence/bridge-network-audit-2026-10-12/`:

| Artifact | SHA-256 |
| --- | --- |
| `goal-nic-after.jsonl` | `7ea88c0646031ec6f6227dc169963c89ae9d679a6abbf7950787684dadeef6a1` |
| `sdk-reassembly-goal-after.json` | `994588d5e975f11ab94a94bf7e31384ba2b29e600638456971115cee01b620f1` |
| `sdk-reassembly-garage-quiet-correlation.json` | `059e7c530309bf86bd4c153a7928d76aa1f1392cd337efa4559a82ae43fe427c` |

Next falsifiable observation: record only fixed unencrypted DATA header fields
(command ID, channel, sign code and type), all session ports and timestamps
alongside VIDEO RX/ACK. No encrypted body, pictures or SDK method hooks are
needed. An attached outer command 1350 does not reveal its encrypted inner
1003/1004 selector; complete absence of outgoing DATA excludes both, while
presence requires a separately authorized bounded command-call chronology.
No ACK batching, selector, topology, quality or buffer change is justified yet.

### Optional passive command-header chronology

`server/scripts/capture-udp-metadata.py --command-headers` now supports that next
observation. It inspects only the 16-byte unencrypted logical header at the
start of an **outbound DATA/type-0** packet, after its transport prefix. The
JSON includes integer `commandId`, `commandChannel`, `commandSignCode` and
`commandType` fields. It does not scan later bytes for a header, assemble
fragments, retain raw headers, decrypt or save encrypted/media bodies. Incoming
VIDEO and command bodies remain metadata-only. Packet buffers are temporary;
using a memoryview avoids copying the entire media body during parsing.

The default capture format remains unchanged. Capture-start metadata reports
whether the option was requested. Capture duration remains bounded to 120
seconds; packet-tap loss/truncation counters must be checked before interpreting
absence. No live capture was run for this addition.

Two parser tests pass, including privacy assertions that arbitrary encrypted
bodies of equal length produce identical output, no raw bytes/views appear in
rows, and disabled, incoming, VIDEO, short and continuation packets never
produce command-header fields. Commands encrypted inside outer 1350 remain
unidentified; this option must not be described as observing inner 1003/1004.

Example for a separately coordinated Linux capture (peer supplied privately):

```sh
rtk proxy python3 server/scripts/capture-udp-metadata.py \
  --interface eth0 --peer '<LAN peer>' --seconds 90 \
  --command-headers --out '<private metadata.jsonl>'
```

## Passive fixed-header repeat: 18:42:52–18:44:52 UTC

A separately coordinated 120-second interface-only capture ran
18:42:52.225802–18:44:52.228083 UTC. It recorded 89,804 metadata rows with zero
tap drops, truncations or missing timestamps. The temporary observer socket was
closed at completion; no Inspector hooks, camera commands or production settings
were changed by the observer. Running SDK bundle SHA was
`4ddbf934ccab2ebee30832fdacb7a1874e308278f3db2f41aa98503b6e938cf4`.

There were **zero outgoing DATA commands to HomeBase across all ports for the
entire 120 seconds**. Its stable sessions had these largest VIDEO quiet gaps:

| Local / peer port | Consecutive sequence boundary | Quiet | Final VIDEO ACK delay |
| --- | --- | ---: | ---: |
| 58455 / 19719 | 64517→64518 | 832.938643 ms | 1.351875 ms |
| 49574 / 23094 | 16887→16888 | 333.479425 ms | 0.959075 ms |

The 832.939-ms interval was 18:44:30.717160–18:44:31.550098 UTC.
Host PING during it received a peer PONG 0.600850 ms later. Neither session had
an outgoing DATA command within five seconds either side of its longest gap.
Incoming VIDEO logical header channels were not retained; these ports are
reported anonymously rather than inferred from byte volume. No three-second
HomeBase pause occurred in this window, so this is not evidence it was cured.

### Distinct standalone session-close case

Across anonymous standalone peer-2's old and new ports, VIDEO was absent for
**22,681.253485 ms**, from 18:42:56.113669 UTC (old port 43944, sequence 3431)
to 18:43:18.794922 UTC (new port 39504, sequence 0). Per-port gap statistics
alone would hide this replacement interval and must not represent the whole
camera observation.

The old session's last incoming packet of any kind was **F1F0 END** at
18:42:56.151354 UTC, 37.685486 ms after its final VIDEO packet. No incoming
PING/PONG/control traffic followed on that old session. The SDK continued
sending 1139 keepalives and PING. STOP 1004 was physically emitted at
18:43:12.122681 UTC, 16.009012 seconds after VIDEO ceased; the live bridge
journal then reported stopped / feed ended / reopening. A new outer 1700 start
was emitted at 18:43:18.671776, and the journal reported streaming at 18:43:19.
The stop thus followed the silence; it cannot explain its initial onset.

SDK `codec.ts` names F1F0 as `ResponseMessageType.END`. Current
`p2p-session.ts:onMessage` has no incoming END branch and logs it as UNHANDLED,
so prompt peer-close handling is a concrete candidate for improving recovery
in this separate case. Connected-peer verification, wire semantics, observable
UDP tests and deployed recovery evidence are still required before a fix.
This does **not** explain the HomeBase quiet interval with heartbeat/control
traffic still alive. No END-handling SDK change is claimed here.

All 42 decoded fixed-header outbound commands were standalone: 38 keepalives
1139, two gateway queries 1100, one stop 1004 and one outer start 1700. Inner
encrypted command content was not retained or decrypted.

Private files under `.evidence/bridge-network-audit-2026-10-12/`:
`command-nic-120.jsonl` SHA-256
`e479c547e2e2131008597994f5fd81be1496c697d5b736629810aaea15463292`;
`command-nic-120-analysis.json` retains exact selected rows and the cross-port
standalone boundary. Temporary script SHA-256 was
`8e045fb69aa8994b8f913fd0fac877999c8d9b62c3eabf354ed703393904e2c0`.

The optional header capture additionally records integer `declaredPayloadBytes`
from the four-byte transport envelope. This permits a future END witness to
compare declared length with actual packet bytes without retaining payload.
Privacy tests cover zero-length END, a mismatched declared length and a valid
opaque one-byte payload. Existing private captures predating this field cannot
establish their wire-declared length retrospectively. No new live witness has
been recorded for this parser addition alone.
