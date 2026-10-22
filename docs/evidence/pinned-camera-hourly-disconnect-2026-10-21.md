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

The station-ping comparison is still pending its original boundary. The waiting capture job is not restarted. Its future Node observer now also records a strict integer-only projection of decoded 1351 notifications and numeric command results, observing dedicated media sessions directly. Public SDK `p2p` events omit those dedicated sessions. Command 6246 `payload.num` is retained numerically without assigning it a status/expiry meaning. The helper passed five tests for privacy projection, field validation, new-session registration, exact/idempotent listener restoration, deadlines, limits and file safety. It has no network-command path.

A separate request-mode audit found no basis for changing `streamType: 2`: the authenticated historical app start uses the same selector. Camera display `video_type` is a different field. The fresh app capture cannot establish encrypted inner fields. Other historical wrapper differences (`msg_id`, `ClientOS`, duplicated account field and outer routing fields) have no demonstrated connection to the hourly lifetime and were not changed.

Upstream code provides a useful scope check: bropat's [no-data 1139 sender](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L799) and [two-second scheduler](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L4308) exist, but [the scheduler is gated on energySavingDevice](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L1373). Its [battery classification applies to the station endpoint](https://github.com/bropat/eufy-security-client/blob/3bfef130bdcbff8f2f68286a5f63ec7e48177202/src/p2p/session.ts#L4608), not an attached camera. This is not evidence for a HomeBase renewal interval; no periodic policy was enabled.
