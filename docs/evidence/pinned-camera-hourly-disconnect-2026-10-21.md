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

The guarded original-boundary comparison is now scheduled for **01:30:15 UTC**, 220 seconds of UDP/SDK observation and 224 API samples. Its authoritative remote process was verified alive as PID **184205** at 00:57:28 UTC; script `/tmp/eufy-front-nudge-hour-capture.py`, output directory `/var/lib/eufy-wall-bridge/front-nudge-hour-20261022/`, SDK file `/var/lib/eufy-wall-bridge/front-nudge-hour-sdk-20261022.jsonl`. The observer validates the WeakRefs, exact sequence 3, dedicated session and original tuple before attaching; the Python runner also rejects bridge PID changes or Front Door reopen/stall changes. The inspector is closed while waiting. This is a pending live comparison, not a completed test. See [front-nudge-hour-scheduled.json](data/pinned-camera-hourly-20261021/front-nudge-hour-scheduled.json).

## Original-hour comparison: attached reassertion rejected

The guarded comparison completed **01:30:18–01:33:58 UTC**. All source/owner/session WeakRefs, exact sequence 3, selected tuple and bridge/source-lifetime guards passed before observation. The one managed attached START did **not** extend the original lifetime:

- Original recovery START was **00:31:19.782631 UTC**, first VIDEO **00:31:20.055731**. The controlled reassertion was **00:49:56.993987**.
- VIDEO stopped at **01:31:20.942688**, **3601.160057 seconds after the original START**, but only **2483.948701 seconds after the reassertion**. It resumed **30.264257 seconds later** on a new channel-2 endpoint. Stalls rose **86 → 89**.
- There was no outgoing DATA or END on any HomeBase endpoint for 50 seconds before the cut. The final received VIDEO sequence was ACKed in **0.789 ms**. Five heartbeat pairs and 79 CONTROL packets continued during the silence. END and recovery commands followed the initial loss.
- SDK accepted DATA, assembled units and delivered frames stopped together at **974**, with **zero Front Door gates, stage divergence or video-gap event**. API receipt age reached **30.176 seconds**. The one recorded `videoGap` belonged to Balcony.
- Recovery replaced the source, LiveStream and public session. New stream/session 4 delivered **1900** units without gating. Cleanup's old-owner session-pointer equality is not equality with the new active public session. The owned diagnostic stop ran; listener totals are current counts, not an original-count equality claim.
- Tap integrity: **216393 packets seen / 154894 retained metadata rows / zero drops, truncations or untimestamped packets**. Both capture processes exited zero.

The reassertion was not merely transport-ACKed and ignored: incoming VIDEO's header TYPE changed **11 → 12**, with first TYPE 12 media **29.450 ms after the command**. At the later original-hour cutoff, the stream still used TYPE 12. This proves an active header/request-tag effect, not the exact semantic meaning of TYPE or lease renewal. Restoring periodic attached STARTs is not justified by this failed one-shot test. No such policy was deployed, and the shifted 01:49 deadline no longer belongs to the retired source.

Evidence: [exact request/deadline anchors](data/pinned-camera-hourly-20261021/front-original-and-nudge-deadline-anchors.json), [wire result](data/pinned-camera-hourly-20261021/front-attached-nudge-original-hour-result.json), [SDK result](data/pinned-camera-hourly-20261021/front-nudge-original-hour-sdk-summary.json), and [header TYPE correlation](data/pinned-camera-hourly-20261021/front-pilot-video-type-echo-comparison.json). Private full records remain in `/var/lib/eufy-wall-bridge/front-nudge-hour-20261022/` and `front-nudge-hour-sdk-20261022.jsonl`.

A separate serializer audit found no saved primary fixture proving the SDK's `0x0a + (level2Seq & 0x7f)` header-ID formula. The authenticated app capture uses TYPE 10, and older direct experiments demonstrate the field is echoed by media. But earlier fixed-TYPE/app-shaped trials did not improve their measured short-window problems and did not cross an hour. That difference alone does not establish the hourly cause or justify changing the serializer.


## Explicit STOP/START recovery trial, October 22 01:46 UTC

A bounded intervention tested whether explicitly ending the inactive remote pull before requesting it again restores media sooner than repeated START alone. It did not dispose the SDK source, replace its LiveStream, reconnect P2P, or reopen the bridge feed. On a qualified Garage pause, the observer sent one STOP1004, waited 200 ms, and invoked the existing owner's START1003 path. This was an experiment, not a deployed timeout policy.

Garage VIDEO was already absent for **2.022 seconds before STOP**. The last raw packet arrived at **01:46:31.759306 UTC**, STOP at **01:46:33.781117**, START at **01:46:33.983692**, and the first returned VIDEO at **01:46:34.468206**. START-to-VIDEO was **484.515 ms**, with a total raw gap of **2.708900 seconds**, compared with the previously captured 26–30-second recovery gaps. Both commands were ACKed. The endpoint stayed unchanged and VIDEO sequences continued **18524 → 18525**; Front Door continued. This establishes restoration in this trial, not why the original pull stopped. The exact prior START anchor for this Garage lifetime has not been established, so this trial alone does not prove an hourly expiry.

Recovery was not uniformly smooth. The first returned SDK keyframe arrived 570.844 ms after START with a 2304×2592 SDK geometry header. The next frame, another keyframe at the prior 1920×2160 size, arrived **2.606 seconds later**. The remaining 1,214 frames retained that size. These are SDK header readings, not an independent SPS geometry check. Source timestamps advanced without resets or regressions. Both SDK source/owner/session identities were preserved. Garage's cumulative bridge stall counter stayed 77 across 224 API samples; post-START SDK observation lasted **83.685 seconds**.

The capture recorded 225,599 packets seen and zero drops, truncations or untimestamped packets. Observer cleanup removed its callbacks by identity and preserved other callback identities. Compact results: [wire](data/pinned-camera-hourly-20261021/garage-stop-start-wire-result.json) and [SDK cadence](data/pinned-camera-hourly-20261021/garage-stop-start-sdk-summary.json). Full private records remain at `/var/lib/eufy-wall-bridge/garage-expired-pull-reset-20261022/`.

### Confirmed bridge recovery configuration discrepancy

Deployed config, adapter and stream-manager module hashes matched the checkout. The effective `stall.recreateClientAfter` remained **8**, carrying the old whole-station-teardown policy, although recovery now invokes camera-scoped `resetCameraStream()` and the stream-manager comment says default 1. The installed SDK implements that method. Genuine new media clears the consecutive failure counter, so a cumulative stall number is not the reset threshold. At the Front Door 01:31 episode the bridge retried three times without retiring the stalled source; the SDK's 20-second warm timeout finally retired it. This establishes delayed stale-source retirement in the bridge. Correcting that policy remains distinct from preventing the original upstream media stop.


## Guarded recovery repair and SDK update deployed

Bridge commit `7bf46b9` corrects the camera-scoped retirement default from eight failures to one. A successful reset is latched once per failure episode. Because this confirms replacement of the old SDK source, the replacement receives one bounded 30-second startup window; historical receipt evidence is retained. Cached primes and readable reopens cannot reset that deadline. Expiry consumes the grace and subsequent retries return to ordinary per-readable timing, rather than inheriting an already-expired anchor. Genuine advancing media re-arms the next failure episode. Explicit configured retirement thresholds remain supported.

The naive default-only repair failed a seven-second replacement-start regression: it repeatedly killed the replacement under the five-second playback watchdog. That version was rejected. Tests now cover seven-second startup, cached-prime/reopen handling, deadline expiry and later recovery, false/throwing reset attempts, sibling isolation and rearming. This corrects delayed stale-source retirement, without claiming prevention of the original wire-side media stop. The SDK's reset call does not guarantee that dedicated socket closure has already completed when the promise resolves.

The fork advanced to `66692a04a2516634277b1a33eb30205071ee7ff3`; dependency commit `6dc8077` locks that revision. Compatibility review found no bridge adapter API changes required: arrival-time envelopes, raw source timestamps, camera reset, power overrides and LAN filtering remain supported. The ordinary-consumer cached-keyframe fix is retained. The SDK exposes no public source-generation identity, so this repair grants new-source grace only after a successful explicit reset.

All **265 bridge tests** passed against the exact new local SDK installation. An independent off-device SDK gate passed **119 tests across nine files**, covering shared sources, readable bootstrap, reuse/start failures, video configuration, camera-scoped reset isolation, close-drain, unreachable sessions and level-2 RSA. These gates do not prove live hourly recovery.

The bridge and off-device-built SDK were deployed at approximately **05:55:25 UTC**, service PID **195316**. The installed SDK bundle SHA-256 is `030161934a0b2f2f9363a5166dabb4a5218d2749b3a662091ee2352bae4c39d5`, matching the local artifact. The effective retirement threshold is one; hardware decoding and forced LAN remain enabled, and the hardware-processing module is byte-identical to its rollback backup. All three always-on Eufy feeds and Wyze returned live. Garage required two 20-second startup warm-timeout retries before its first video; this was not a clean-startup result.

A one-second API observer, PID **195415**, runs for 75 minutes. A bounded metadata-only wire capture, scheduler PID **195556**, is scheduled for **06:54:55–06:58:15 UTC**, guarded by the deployed bridge PID. It will compare a spontaneous interruption and recovery against the earlier 26–30-second gaps, rather than treating deployment restart as improvement. Both jobs were verified live after creation. Their results remain pending. Private rollback and observations are under `/var/lib/eufy-wall-bridge/update-20261022-recovery/`. Compact provenance is in [the deployment record](data/pinned-camera-hourly-20261021/bridge-recovery-deployment-20261022.json).

Provenance caution: the previous marker named a parent revision plus a fix commit; it did not by itself establish that the entire deployed bundle equaled the detached fix-commit source tree. The new deployment records the locked complete revision and verifies the actual built bundle hash. Prior wire/SDK-event observations remain valid; source-level claims about the previous complete tree require that distinction.


### Post-deployment short live gate

The independent read-only steady-state sample at **05:58:51.961–05:59:21.961 UTC** passed its bounded gate. Bridge PID stayed 195316. All three Eufy sources advanced with zero stalls in all seven samples; ingress/output counts advanced equally (Front Door 448/448, Garage 450/450, Balcony 739/739). Sampled maximum source-receipt ages were 224, 65 and 268 ms respectively. These are sample ages, not bounds on every intervening frame.

All three Fire TV app PIDs remained stable. Front Door and Garage used `OMX.amlogic.avc.decoder.awesome` and rendered counts advanced on every TV; before/after queue snapshots were 0–1. No retry, release or bridge stall/reopen messages appeared in this window. This proves current media delivery and hardware-decoded progress, not physical display latency or the yet-unobserved hourly recovery. Garage's initial cold-start failures are excluded from this steady-state gate. Balcony's separate software-decoding delay remains: latest decoder-age samples were approximately 0.5–1.2 seconds. [Compact live result](data/pinned-camera-hourly-20261021/bridge-update-short-live-verification.json).


## SDK behavior reproduction and prior-artifact qualification

An isolated characterization against exact latest SDK `66692a0` uses production P2PSession logical receive, encrypted outbound command builders, LiveStream and SharedLiveSource. One continuously attached consumer receives an initial valid keyframe, followed by 60 seconds of synthetic control notifications with no VIDEO. The SDK emits 20 START requests including the initial one, zero STOPs and no consumer terminal event before explicit cleanup. The test and typecheck pass. This passes on the unmodified base and is evidence of current behavior, not a regression for a proposed SDK repair. Socket/heartbeat negotiation and a real peer are outside its scope. [Runnable handoff and patch](sdk-established-live-silence-2026-10-22.md). No production SDK change was made for this characterization.

Independent review also compared the actual rollback on-disk SDK bundle with the new verified bundle. Relevant attached retry, next-live-keyframe join, arrival-time forwarding and camera-reset methods are semantically equivalent. The previous on-disk bundle hashes to `326643d70f5926439bfbdb1c1dfc022e4ff4eab3dd4657fdfd19ac6123f22ded`, conflicting with the previous revision marker's `569200...` claim. The old PID's loaded bytes cannot now be verified retrospectively. Consequently prior device observations are attributed to their measured event/packet artifacts, not to a proven complete source revision. The new deployment's complete locked revision and actual bundle hash are independently verified. [Bounded comparison](data/pinned-camera-hourly-20261021/bounded-previous-new-sdk-lifecycle-comparison.json).


## Completed live gate: recovery improvement retained

The deployed camera-scoped retirement repair passed the natural-boundary comparison. Front Door raw VIDEO gap fell from 28.189–30.264 seconds to **7.506 seconds**; Garage from 26.804 to **7.203 seconds**. Each had one source retirement, with sibling progress retained. Both continued without another stall for roughly 14–15 minutes after recovery, through the 75-minute observation deadline. The service PID stayed unchanged. Two active TVs retained hardware pins and recovered using hardware decoders; `.85` had stopped its wall before the boundary and is not claimed verified.

Both initial upstream cuts still occurred. Balcony also has unresolved short SDK-readable EOFs and a separate connection-timeout episode. Faster source retirement is verified; uninterrupted playback and the exact initial stop mechanism remain unproved. [Full live result, limits and device evidence](camera-source-retirement-live-2026-10-22.md).
