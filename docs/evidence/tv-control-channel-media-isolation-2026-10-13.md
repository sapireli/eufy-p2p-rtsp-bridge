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

The candidate is built but **not installed or deployed** in this task. Parent review should decide whether to perform a short controlled control-only disconnect with existing hardware RTSP playback, confirming no media recreation and later control reconnection. Such a test must preserve preferences/source sessions and distinguish metadata-triggered releases on the subsequent hello. No further observer or behavioral change is justified by the historical counter reset alone. Physical smoothness and the separate source-arrival pause remain unproven.
