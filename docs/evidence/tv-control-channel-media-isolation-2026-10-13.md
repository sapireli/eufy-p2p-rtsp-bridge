# Preserve RTSP media when the control WebSocket disconnects

## Observed boundary and attribution limit

The current60s gate found smaller per-SN decoder counters than the earlier180s gate, with the same application PIDs. The counters are constructed per `LowLatencyRtspPlayer` and are never reset in place. This establishes player/diagnostic-object replacement between the two observations. Retained logs do not locate that event or establish its cause. See [the current gate](tv-current-info60-gate-2026-10-13.md).

This patch does **not** claim that a WebSocket disconnect caused the historical replacement. Its evidence is a concrete production code path: both `MainActivity` WebSocket `onFailure` and `onClosed` previously released every RTSP player, changed camera labels, and scheduled control reconnection. A control transport failure does not establish failure of independently connected RTSP media. That release was unnecessary when the video connection continued decoding normally.

## Narrow change

Both control-disconnect callbacks now use the same `ControlChannelRecovery` handler. For the current visible connection, it logs the actual failure class or close code and schedules the existing five-second control reconnect. Its effect interface deliberately has no player, decoder, or label action. Obsolete/hidden socket callbacks do nothing. Reconnect scheduling removes a duplicate pending reconnect callback.

Existing explicit camera-state transitions, RTSP failures, first-output/live decoded-progress timeouts, Activity stop, and decoder recovery still release/recover media. No buffer, frame admission, codec choice, source URL, retry duration, heartbeat policy, camera configuration, or server code is changed. Metadata-based release behavior is a separate pending issue and remains unchanged.

Actual player releases now produce one small INFO record with exact SN, reason, and decoded/received counters. Control failure also emits one INFO record. These are event records, not new per-frame logging or an observer. They provide a reason for future replacements without expanding live diagnostic capture.

## Deterministic verification

The actual production handler used by both callback branches is tested with failure and close events while decoder output progresses for six seconds. The test media store is synthetic and is not owned or changed by the handler; its identity assertions do not establish actual Activity callback integration or display behavior. The production handler requests control reconnect, and the unchanged decoder-progress watchdog remains healthy under simulated output progress. The same test checks that a real five-second media output stall still expires. A separate test verifies obsolete/hidden callbacks cannot schedule reconnect or report loss.

The test checks the isolated recovery contract and production callback wiring was reviewed; it is not an Android UI/WebSocket network integration test. All **49 app tests and14 vendored RTSP tests** passed, and the current source built successfully with the existing local Android toolchain.

Candidate APK SHA-256: `1da7cdcb4a75751fc4831db85a6e8708c2a8b12130df0476299286bd08273830`.

## Next decision

The parent approved installation and a narrowly scoped HTTP/control interruption; the live result below proves media survival in that event. It does not establish the historical reset cause. Metadata-triggered releases remain a separate issue. Physical smoothness and the separate source-arrival pause remain unproven.


## Deployed controlled proof

Both TVs were updated with `adb install -r`, preserving signature/data. Each existing Activity was deliberately force-stopped/launched after the update; these are intentional deployment boundaries, not natural failures. The exact installed APK SHA above was verified on both. Rollback APK `fe0b8fd35a94d6b10fd45af7d11d5eb809cc905e5dc54d112401ea4a789b7e96` was pulled beforehand. Preference files were byte-identical before installation, after startup, and after the controlled test. New app PIDs were780/.85 and31852/.58.

Ordinary INFO observation ran01:46:07.353–01:47:07.361 UTC. Both TVs used hardware AVC with rising decoder counters, queue0, and no warning/retry/release. The background Garage callback maximum reached4899ms on **both** TVs with pacing resets6 and unknown-clock count3 before the control intervention. Those callback maxima are informational, not physical-panel proof; the source/pacing concern remains open and is not attributed to this fix.

A separate controlled INFO window ran01:48:36.984–01:49:36.991 UTC. Before action, the bridge had four established .85 HTTP3000 connections and two independent RTSP8554 tuples to client ports49058/49060. The parent explicitly approved one `ss -K dst 192.168.23.85 sport = :3000`, executed01:49:06.860–01:49:07.191 UTC. Its output contained only .85 port3000 tuples (including a FIN-WAIT-2 HTTP tuple). No other address, RTSP8554, interface, firewall, source, SDK session, or Pi was targeted.

The actual application emitted:

```text
Control socket lost reason=failure:SocketException; preserving 2 RTSP players; reconnect in 5000ms
```

Its device-wall timestamp was01:49:08.219; clock-bracket artifacts are retained, so this is not claimed as exact reaction latency. **No player release, counter decrease, new first-frame event, retry, or media error occurred** in the recorded window. Both exact8554 tuples remained49058/49060 afterward, and hardware decoder output continued. .85 Garage decoded+890 over its65.013s statistics span; Front Door+837 over55.008s. .58, whose control connections were untouched, continued Garage+866/Front Door+900 over60.007/60.017s. All four decode500ms counter deltas were0 and queue samples0.

New .85 HTTP3000 connections appeared afterward. This confirms HTTP reconnection activity and the code's reconnect schedule, but no explicit hello/upgrade log was emitted; a completed WebSocket handshake is not independently claimed from TCP tuples alone. Clock HTTP was also interrupted, so this is **HTTP/control interruption**, not a pure WebSocket-only fault.

Garage pacing-reset counters increased by1 in the controlled statistics spans on each TV; the shared callback maximum stayed4899ms. Those background timing effects are not demonstrated repaired by control isolation. The controlled evidence supports only the concrete claim that this real control failure did not unnecessarily tear down healthy independent RTSP players.

All60s collectors terminated. Private metadata is retained under `.evidence/frame-freshness-2026-10-12/tv-control-loss-deploy/`, including exact intervention/tuple records, preference hashes, installed APK hashes, filtered INFO logs, manifests, and per-SN summaries. No pixels/media payload were captured. A separately authorized sustained INFO gate follows; no new transport/pacing experiment is implied.
