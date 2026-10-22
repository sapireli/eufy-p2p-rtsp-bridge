# Pinned-camera disconnect investigation

## Confirmed observations

On October 21 all three Fire TVs (.58, .85 and .100) run client 0.6.9/code 18 and persist hardware pins for Front Door CLE and Garage CLE. Front Door uses `OMX.amlogic.avc.decoder.awesome` on each. A pin protects its hardware lease from local preemption; it does not prevent upstream media silence or an RTSP producer closing.

At 23:30:30 UTC the running bridge logged Front Door with five seconds without new source media and closed its feed. Further watchdog/reopen cycles occurred at 23:30:36, :44 and :50. New media resumed at 23:30:55. Fire TVs .58 and .85 recreated their hardware decoders at 23:30:57.079 and .106 respectively. Their synchronized restart follows the shared bridge interruption. Historical TV logs expired, so missing reclaim/error lines are not proof that no earlier resource error occurred.

The 24-hour bridge history contains a stronger lifetime pattern. Front Door has 26 watchdog episodes (94 individual watchdog hits), including 20 directly following a prior streaming event by 3602–3607 seconds and two interrupted episodes preserving the underlying hourly phase. Garage has 23 episodes (81 hits), with 21 at 3601–3613 seconds, median 3604. Their phases diverge when one camera's source is recovered independently. Balcony's two watchdog hits form one irregular episode. This supports a per-camera pull or media-session lifetime; it does not establish the device/transport owner.

The deployed SDK manifest identifies branch `eufy-wall`, revision `7fe9cc55aac899f862ffcfadae366b22d104220f`, fix `e2231c562e439a22a7e16acd11c80cb297cc40a3`. Source review found no one-hour media timer or media-key TTL. Attached start commands contain no explicit duration, wired sources have no battery budget, and attached retry timers are silence-based. A station-side hard limit remains unproven.

## Parallel tests

1. Peer-side lifetime: compare physical UDP VIDEO DATA arrival with PING/PONG, ACK and END/control activity across the next predicted Garage boundary.
2. SDK delivery: compare accepted DATA, assembled units, keyframe gates and delivered frames, retaining stream/session identity changes.
3. Bridge recovery: reproduce cold-source replacement after previous successful receipt, versus reattaching to the same cached source. Historical receipts must not be mistaken for a new generation's live state.

A 120-second control observation completed without reconnecting media. It delivered 1807 Front Door, 1802 Garage and 3005 Balcony frames; no completed unit was gated. Original SDK listener counts were restored with zero mismatches, and the temporary localhost inspector closed. This does not reproduce the hourly failure.

The next bounded capture targets Garage's source start at 22:45:09 UTC: observation from 23:44:09 across the expected 23:45 boundary. Playback configuration, timeouts, quality and pins remain unchanged.

## Predicted boundary captured

The 180-second paired capture ran from 23:44:11.550 to 23:47:11.572 UTC, without restarting the bridge or cameras or changing timeouts, pins or quality. The predicted Garage failure occurred.

- Incoming VIDEO DATA on Garage's UDP endpoint stopped at **23:45:08.781580** and resumed at **23:45:35.585617**, a **26.804036-second** gap. The preceding stream became live at 22:45:09, placing the cut almost exactly one hour later.
- Wire sequence resumed **51956 → 51957** on the same endpoint, with no END packet. During the gap the endpoint still exchanged five PING/PONG pairs, one reverse pair, and 71 acknowledged CONTROL packets (including confirmed 1351 notifications). These are control, not audio frames.
- No outbound DATA command preceded the cut during the 57-second captured baseline. The first outbound recovery command followed the cut at 23:45:14.869113. Acknowledgment proves packet delivery, not semantic acceptance.
- SDK accepted DATA, assembled units and delivered frames stopped together at **874**, with **zero gated completed units**, `waitingKeyframe=false`, and no `videoGap` event. SDK delivery and bridge ingress receipts agreed within **335 microseconds**.
- Bridge source-receipt age increased while the same receipt remained unchanged. The five-second watchdog then disconnected the feed. New Garage stream ownership appeared on the **same P2P session**, then delivered 1394 frames without stage loss.
- Front Door and Balcony continued through Garage's interruption. Front Door separately had a 0.518-second wire gap later in the capture; that short gap did not trigger this watchdog.
- Packet-tap integrity: **183838 packets seen, zero dropped, zero untimestamped, zero truncated**. It retained only protocol/header metadata, no packet payloads. SDK observer cleanup restored original listener counts with **zero mismatches**, closed its temporary localhost inspector, and left bridge PID 151516 unchanged.

This proves a per-camera peer-video interruption before SDK delivery and client decoding in the captured hourly Garage episode. The independent hourly Front Door pattern and its synchronized TV/bridge recovery support the same failure class. Camera versus HomeBase ownership, the exact expiration mechanism, and the owner app's renewal behavior remain unproven. No periodic restart or unverified attached-camera keepalive was deployed.

## Recovery hypothesis disposition

An isolated production-module test also reproduced a separate bridge defect: after an explicit SDK source retirement, old receipt history can classify a cold replacement under the five-second live watchdog. A first frame arriving at seven seconds is accepted on an initial cold open but rejected after this replacement. Same-source cached primes must still retain the five-second limit.

A prototype generation timer passed its focused tests but review found that SDK sources can also retire autonomously. The current arrival-time readable envelope does not expose SDK source-generation identity; a per-slot timer could therefore misclassify another new generation. The prototype is parked outside the deployment branch. It was not deployed or treated as the cause of the observed hourly stop.

## Evidence

Committed compact evidence is under [data/pinned-camera-hourly-20261021](data/pinned-camera-hourly-20261021/): per-camera episode history, TV pins and hardware diagnostics, the bridge incident, runtime SDK provenance, and independent wire/SDK conclusions. Private full metadata captures remain on homebridge under `/var/lib/eufy-wall-bridge/hour-boundary-20261021/` and `garage-hour-boundary-20261021.jsonl`. Local analysis is under `/tmp/eufy-pin-disconnect-20261021/`.

## App-shaped station-ping hypothesis

Existing owner-app capture exports were independently reproduced. The current Front Door trace has 160.548 seconds of VIDEO, with contiguous outbound command sequences and one startup command 1139, not periodic renewal. That ping is on **station channel 255**, 1.463 seconds before the start-shaped command, and exactly matches the SDK void-command builder. A camera-channel-2 ping would not match it. None of the available owner-app traces crosses an hour. Encrypted 1350 bodies do not establish their inner command.

A controlled one-command test sent that app-shaped 1139 on Front Door's existing dedicated session, without restarting it or changing settings. On-wire transmission occurred at **23:53:49.737645 UTC**, sequence **4**, channel **255**. The matching ACK arrived **8.964 ms** later. During the 12-second packet observation Front Door received **1379 VIDEO packets**, maximum inter-packet gap **222 ms**, no END and no gap of 500 ms or more. Subsequent API samples kept stalls at **82** with frames advancing. Packet-tap drops/truncations/untimestamped counts remained zero. This proves delivery and no immediate disruption, not renewal benefit.

The original Front Door source began at 23:30:55 UTC. A bounded follow-up is scheduled for **October 22, 00:29:55–00:32:59 UTC**, across its original expected 00:30:55 boundary. The comparison aborts if bridge PID or Front Door source lifetime changed beforehand. At scheduling, the authoritative remote timer process is PID **182293**, script `/tmp/eufy-front-ping-boundary-capture.py`; outputs go to `/var/lib/eufy-wall-bridge/front-ping-boundary-20261022/`. No permanent keepalive policy, source timer, codec, pin or quality change is deployed.

The recovery prototype also failed an actual-SDK autonomous-retirement counterexample: distinct cold sources could be created without a bridge reset, and its slot timer killed a replacement only one second old. Current HEAD accepted the replacement's first frame at seven seconds. Candidate code and red/green evidence are parked at `/tmp/eufy-h3-startup-generation-parked/h3-local-evidence/HANDOFF.md`. The deployment branch's production files remain unchanged.

## Follow-up prerequisites checked

The station-ping comparison completed across its original boundary; its result is recorded below. The waiting capture job was not restarted. Its future Node observer now also records a strict integer-only projection of decoded 1351 notifications and numeric command results, observing dedicated media sessions directly. Public SDK `p2p` events omit those dedicated sessions. Command 6246 `payload.num` is retained numerically without assigning it a status/expiry meaning. The helper passed five tests for privacy projection, field validation, new-session registration, exact/idempotent listener restoration, deadlines, limits and file safety. It has no network-command path.

A separate request-mode audit found no basis for changing `streamType: 2`: the authenticated historical app start uses the same selector. Camera display `video_type` is a different field. The fresh app capture cannot establish encrypted inner fields. Other historical wrapper differences (`msg_id`, `ClientOS`, duplicated account field and outer routing fields) have no demonstrated connection to the hourly lifetime and were not changed.

Upstream code provides a useful scope check: bropat's [no-data 1139 sender](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L799) and [two-second scheduler](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L4308) exist, but [the scheduler is gated on energySavingDevice](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L1373). Its [battery classification applies to the station endpoint](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L4608), not an attached camera. This is not evidence for a HomeBase renewal interval; no periodic policy was enabled.

## ACK starvation alternative checked

An independent pass over the same Garage wire capture accounted for all **8618 unique VIDEO sequences** received before the cut (8704 datagrams including duplicates). Every received sequence had an outbound ACK before recovery. The final sequence, 51956, was acknowledged **1.370725 ms** after arrival. During the final second, 201 received sequences had median ACK delay **4.6216 ms** and maximum **22.325449 ms**; the last ten seconds contain no observed sequence hole. This excludes SDK withholding of ACKs at the captured interface. Peer receipt of those outbound ACKs remains unmeasured. No ACK-policy change is justified by this capture.

Across recovery, the raw media timestamp advanced 26614 units while SDK delivery elapsed 26.925918 seconds. Healthy pre-cut observations advance at approximately 1000 units per second, consistent with an advancing source timeline across the pause rather than playback of the immediately next historical frame. Raw timestamp origin and unit are not a verified sensor-capture clock. The numeric evidence and input hashes are in [garage-ack-starvation-check.json](data/pinned-camera-hourly-20261021/garage-ack-starvation-check.json).

## One-shot ping intervention rejected after live comparison

The scheduled capture completed October 22, **00:29:58.073–00:32:58.073 UTC**, after its original-lifetime guards passed. Front Door still stopped receiving VIDEO at **00:30:51.867019 UTC**, near the original hourly boundary despite the earlier station-channel 1139. Video resumed at **00:31:20.055731**, a **28.188712-second** wire gap, on a new endpoint confirmed by channel-2 logical headers. Stalls rose **82 → 86**. Garage and Balcony continued. This rejects the tested one-shot intervention; it does not establish that every possible periodic ping policy is ineffective. No periodic policy was enabled.

Front Door's old endpoint continued six PING/PONG exchanges and 81 CONTROL packets during the silence. The bridge sent END at **00:31:19.087333**, **27.220 seconds after video stopped**; the peer answered END at :19.097660. Session closure therefore followed the original media cut. SDK accepted/assembled/delivered counts stopped together at **821**, with zero gated units, no keyframe wait and no `videoGap`. Bridge ingress and SDK final delivery agreed within **314 microseconds**. Recovery replaced both stream and session and delivered another **1444** units without stage divergence. Three later wire gaps of roughly 0.58–0.64 seconds remain visible; this is not evidence of uniformly smooth playback.

No numeric 6246 notification preceded the cut. `num=1` notifications began about 6.1 seconds afterward, during recovery attempts; 18 old-session command results had code 0. A base-session `num=0` occurred near replacement. These values have no verified expiry/status meaning, and the result events do not identify the originating command.

Capture integrity: **187464 packets seen, zero drops, zero truncations, zero untimestamped; 128784 metadata rows retained**. Both capture processes exited successfully. The inspector is closed, the capture process is gone, bridge PID **151516** is unchanged, and all three camera APIs report streaming afterward. Cleanup's aggregate count check reports one original-session listener mismatch after Front Door's original stream/session retired; this is not a zero-mismatch cleanup claim. The observer implementations remove their owned callbacks by identity; the count-only checker cannot distinguish a legitimate retired-stream removal from an observer leak.

Compact evidence: [front-ping-boundary-result.json](data/pinned-camera-hourly-20261021/front-ping-boundary-result.json) and [front-ping-notify-summary.json](data/pinned-camera-hourly-20261021/front-ping-notify-summary.json). Full private records remain under `/var/lib/eufy-wall-bridge/front-ping-boundary-20261022/`, `front-hour-after-station-ping-20261022.jsonl`, and `front-ping-notify-20261022.jsonl`.

### Remaining decision boundary

The captured failures originate before the SDK's media-delivery pipeline, and hardware pinning cannot prevent that upstream silence. The exact camera/HomeBase/request-contract mechanism remains unproven. There is no evidence-backed permanent prevention change to deploy from this trial. A validated owner-app session crossing the same lifetime would distinguish a vendor-side interruption from a missing renewal/request behavior; the available short app captures cannot do that.

Eufy's [Smart Display E10 documentation](https://service.eufy.com/article-description/What-is-the-maximum-duration-for-viewing-the-live-stream-on-the-Smart-Display-E10) says wired cameras and battery cameras in wired mode have no automatic live-view limit. Its scope is that display's supported behavior, not this SDK protocol. It does not justify calling the observed hour a universal vendor hard limit.

The cleanup checker was also reproduced offline with the actual SDK's `LiveStream.stop()`: removing its own DATA and video-gap handlers changes the retired session's original totals from 1/1 to 0/0, which the live script counts as one session mismatch. Identity checks in that reproduction confirmed both observers removed their owned callbacks from retired and replacement sessions, restored assembler methods, and preserved SDK/third-party callbacks. This verifies the cleanup implementation under retirement, not retrospective live identities; the original live script saved counts only. See [observer-cleanup-identity-repro.json](data/pinned-camera-hourly-20261021/observer-cleanup-identity-repro.json).

The bounded existing-capture inventory found no hour-long owner-app trace. The longest validated recovered iPhone capture is **175.337 seconds**; the other known captures are shorter. At the follow-up check, Xcode listed the iPhone offline, so a new owner-app observation cannot currently run. See [capture-duration-inventory.json](data/pinned-camera-hourly-20261021/capture-duration-inventory.json). The missing comparison requires the phone connected and Front Door live view left open across at least 65 minutes; it must retain the initial command exchange and the original boundary. A shorter trace cannot distinguish the hourly behavior.

## SDK maintenance assumption: controlled attached reassertion

The missing long app trace is not the only route forward. A fresh source audit found a narrower SDK hypothesis: `LiveStream.settleKeepalive()` suppresses the attached-camera FULL 1003 reassert after its first frame, justified by measured contention between cameras sharing one session. That suppression is unconditional on `homeBaseAttached`. The current Front Door stream has a **dedicated one-camera session**, but still receives the suppression. The original comment's 40-second successful stream observation does not establish hour-long maintenance behavior. This is an assumption mismatch to test, not yet a proven lease-renewal defect.

Key/auth and ownership audits did not demonstrate another trigger. Garage recovered using the same RSA and level-2 session keys. Neither capture has a gateway-info 1100 preceding the cut; Front Door's sole 1100 belongs to its new recovery session. Source teardown and session close require outgoing DATA or END, absent on **all HomeBase endpoints** for the 50 seconds preceding both cuts. See [key-auth-maintenance-audit.json](data/pinned-camera-hourly-20261021/key-auth-maintenance-audit.json) and [ownership-maintenance-audit.json](data/pinned-camera-hourly-20261021/ownership-maintenance-audit.json).

A controlled test called the existing Front Door `LiveStream.nudge()` **once**, at **00:49:56.992 UTC**. It sent the managed attached **1350/1003**, preserving selector 2 and the current channel/account/session. It sent no STOP, opened no consumer, and did not change clients, quality, codecs or permanent policy. An initial preflight failed to reserve its output because the root-created directory was not writable by the service user; it stopped before hooks or the command. That artifact was retained separately. The corrected test reserved output first and completed successfully.

Immediate measured result:

- Exactly one outgoing target DATA command: outer 1350, channel 2, sequence **2 → 3**, ACK **8.129 ms** later. The encrypted inner command is established by the managed method used, not decrypted wire contents.
- Front Door's wire sequence continued **61976 → 61977** across the command, gap **20.765 ms**. Garage continued **8659 → 8660**, gap **20.595 ms**. Neither endpoint changed or sent END; neither had a wire gap of 500 ms or more in the 100-second capture.
- Front Door's SDK delivered-frame gap across the command was **42.319 ms**, with a **70-unit source timestamp step**, both surrounding frames non-keyframes. There was no timestamp reset/duplicate/regression or geometry change. All 1380 observed Front Door frames remained **1600×2200**; Garage's 1365 frames remained **1920×2160**.
- All source, LiveStream-owner, session and public-session identities remained equal before/after the 90-second SDK observation. Front Door stalls stayed **86**, Garage **77**. Both stayed streaming in all 101 API samples. Owned callback removal preserved the other callback identities on both streams.
- The SDK receipt-gap maximum was 412 ms in Front Door's 20-second baseline and 561 ms in its 70-second post-command window, the latter 62 seconds after the command. Unequal windows and a startup burst do not establish a jitter improvement or regression. Wire continuity and SDK receipt cadence are different measurements.
- Tap integrity: **94098 packets seen / 46809 retained metadata rows / zero drops, truncations or untimestamped packets**. No payload bytes were stored.

This establishes immediate continuity for this one reassertion. It does **not** prove prevention of the hourly cut or justify a periodic policy. The new command also allocates a media-header slot; it must not be described as a byte-identical renewal ping. The next comparison targets the original **01:31:20 UTC** source boundary. If that boundary passes, a later observation near **01:49:56 UTC** can determine whether the cut follows the reasserted START instead.

At **00:55:13.271 UTC**, a read-only continuity check confirmed the same tuple, exact command sequence 3 (no intervening command on that session), dedicated ownership and recent media. It stored WeakRefs to the current source/owner/session for the future observer to validate. The inspector closed. These references do not keep a retired source alive.

Immediate evidence: [command](data/pinned-camera-hourly-20261021/front-attached-nudge-command.json), [wire](data/pinned-camera-hourly-20261021/front-attached-nudge-immediate-wire.json), [SDK cadence](data/pinned-camera-hourly-20261021/front-attached-nudge-immediate-sdk.json), and [continuity marker](data/pinned-camera-hourly-20261021/front-attached-nudge-continuity-marker.json). Full private records are under `/var/lib/eufy-wall-bridge/front-attached-nudge-20261022/`. No SDK or bridge production code change has been made.

### Separate Balcony incident during the baseline

Before the reassertion, Balcony also lost input and then repeatedly failed P2P connection establishment. Its historical peer `192.168.23.89` failed neighbor resolution and three pings, while Front Door/Garage and bridge authentication/push/poll remained active. Existing logs do not prove its current attempted address, so this is not a confirmed Wi-Fi cause or evidence of the same hourly mechanism. See [balcony-current-incident-20261022.json](data/pinned-camera-hourly-20261021/balcony-current-incident-20261022.json).
