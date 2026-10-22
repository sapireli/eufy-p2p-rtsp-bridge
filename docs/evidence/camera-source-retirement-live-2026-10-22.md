# Camera-source retirement: live recovery comparison

## Decision

Keep bridge recovery commit `7bf46b9`. In the observed natural hourly interruptions, the updated deployment reduced incoming VIDEO gaps by about 73–75%. Both initial media cuts still occurred. This verifies faster recovery, not prevention or the exact cause of the upstream stop.

| Camera | Earlier raw VIDEO gap | Updated raw VIDEO gap | Reduction |
| --- | --- | --- | --- |
| Front Door | 28.189 / 30.264 s | 7.506 s | 73.4–75.2% |
| Garage CLE | 26.804 s | 7.203 s | 73.1% |

The old bridge reopened readables against the same stalled SDK source until a warm timeout finally retired it. The deployed correction retires a camera source after its first failure, once per failure episode, and provides a confirmed replacement with one bounded startup window. This retains explicit threshold overrides and sibling isolation.

## Exact live sequence

Times below are UTC on October 22, 2026.

| Observation | Front Door | Garage CLE |
| --- | --- | --- |
| Last raw VIDEO before silence | 06:55:35.870270 | 06:56:33.655971 |
| Bridge watchdog | 06:55:41.907515 | 06:56:39.940398 |
| Camera-source retirement logged | 06:55:41.968253 | 06:56:39.962639 |
| First returned raw VIDEO | 06:55:43.376375 | 06:56:40.858642 |
| Bridge reports streaming | 06:55:43.396249 | 06:56:40.918868 |

Each camera had one watchdog hit and one source retirement. Front Door recovered on a new dedicated endpoint after the old endpoint closed. Garage resumed on its retained base endpoint with contiguous VIDEO sequence 36839 → 36840. Control traffic continued during both gaps. The capture identifies encrypted outer commands and ACKs; it does not independently decrypt START/STOP semantics. Journal timestamps anchor the reset operation.

The 200.001-second kernel-interface capture recorded 199,633 packets seen and 83,722 retained metadata rows, with zero drops, truncations or untimestamped packets. Raw capture SHA-256: `ad15982edfa63b3b0548c4fc327c65280a86e787f304ed04ba410a74c3e54037`. [Compact wire result](data/pinned-camera-hourly-20261021/live-hour-wire-comparison-compact.json).

## Fire TV verification

Two active TVs, `.58` and `.100`, recovered both cameras with unchanged app PIDs and hardware pins. All four decoder reopens used Amlogic hardware decoding with `pinned=true`; no hardware reclaims occurred. Their receive/render counters continued through 07:00:13 UTC, including sibling-camera progress across each interruption.

Read-timeout to first new rendered callback was 4.92–4.95 seconds for Front Door and about 4.50 seconds for Garage. These are not whole-freeze durations: coarse five-second progress samples bound Front Door's whole callback pause at roughly 7.4–12.8 seconds and Garage's at 5.7–11.2 seconds. The `.100` Garage player needed one extra retry after RTSP 404. No physical-screen timing is claimed.

TV `.85` stopped all wall players at 06:53:51 UTC, before the observation window, and later became unreachable. It was not restarted; its recovery is unverified. [TV result and timing bounds](data/pinned-camera-hourly-20261021/tv-boundary-result.json).

## Scope and remaining failures

The deployment combines the bridge recovery repair with SDK update `66692a04a2516634277b1a33eb30205071ee7ff3`, as requested. All 265 bridge tests and 119 focused SDK tests passed. Audited SDK retry, join, timestamp and camera-reset methods match the prior on-disk bundle semantically; this was not a strict single-variable A/B of every SDK change. The new SDK artifact hash is verified. Previous process loaded-byte provenance remains unverified, as documented in the main incident record.

Hardware-first processing, software fallback, quality configuration and LAN-only policy were preserved. No periodic restart or new SDK production timeout policy was introduced.

Balcony remains unresolved. It had multiple short SDK-readable EOF/reopen episodes and a separate 31.796-second source-receipt gap involving a P2P connection timeout after prompt retirement. That event is not counted as an improvement. Short EOFs occurred with fresh media immediately beforehand; existing records cannot distinguish an incoming peer END from local SDK session/source disposal. A targeted close-origin observation is the next decision, rather than attributing these EOFs to geometry or decoder backlog.

The 75-minute API observation finished normally at **07:11:14 UTC**, with 4,500 one-second samples and only bridge PID 195316 observed. Following wire recovery, Front Door remained streaming in every sampled observation for **930.041 seconds** (15.50 minutes), and Garage for **872.558 seconds** (14.54 minutes). Neither accumulated another stall. Maximum sampled source-receipt ages in those periods were 279.410 ms and 147.425 ms respectively; these sample values are not sensor capture-to-screen bounds. The sampler recorded all five brief Balcony EOF/reopen episodes and its separate longer timeout episode, rather than equating a zero stall counter with uninterrupted playback. [Sustained result](data/pinned-camera-hourly-20261021/sustained-post-recovery-result.json).

Both observation jobs completed without being restarted. The exact cause of the initial hourly remote media silence remains unproved.


## Short-EOF origin probe

A reviewed listener-only observer ran **07:17:58.492–07:27:58.500 UTC** on Balcony's existing selected socket, source and consumer. It collected only END header metadata and stop/close event stacks, without video payloads, extra consumers, network commands or method replacement. It had a 64-event bound and a ten-minute deadline. No END, stop or close occurred during that interval; the same source, owner, session, socket and public session remained connected/live. This is an **inconclusive reproduction window**, not proof that the EOF issue disappeared.

All four owned callbacks were removed and other callback identities preserved. The observer CLI exited and the loopback inspector was confirmed closed. An earlier preflight guard had incorrectly required a level-2 key on this valid standalone live session; it failed before installing listeners or sending commands. The standalone-only guard was corrected and retested before the actual probe. [Sanitized result](data/pinned-camera-hourly-20261021/balcony-eos-origin-no-event.json).

No production code or camera policy was changed following the empty probe. The next short EOF needs a captured close origin; the next protocol-level prevention intervention for hourly media silence still needs evidence beyond the observed incoming-video cutoff. No connected iPhone was listed by the current device inventory, so an owner-app lifetime comparison was not available in this run.

## Independent follow-up checks

A 45-second read-only check at **07:50:53–07:51:38 UTC** found Front Door and Garage receive/render counters advancing on both active TVs. On `.58`, Front advanced by 607 received / 597 rendered frames and Garage by 600 / 585; on `.100`, the increases were 607 / 601 and 601 / 585. All four players retained `OMX.amlogic.avc.decoder.awesome`, unchanged hardware pins and app PIDs, with no counter resets, player releases, decoder reopens or hardware reclaims in the window. Each camera had nine counter samples spanning approximately 40 seconds. This verifies decoder callback progress, not physical scanout or end-to-end latency. `.85` was absent from the online ADB inventory and was left untouched. [Sanitized client result](data/pinned-camera-hourly-20261021/tv-short-readonly-check-20261022.json).

A separate read-only inventory identified no second active Eufy controller on the bridge host. Homebridge runs configuration, SSH and MQTT plugins; the checked configured command/topic fields and six schedule files contained no Eufy controller references. This excludes an identified competing actor on this host during the audit only; historical transient actors and other hosts remain outside scope. [Sanitized host audit](data/pinned-camera-hourly-20261021/competing-controller-host-audit.json).

An independent review found no actionable defect in recovery commit `7bf46b9`'s one-shot retirement latch, fixed startup deadline, explicit threshold overrides or sibling isolation. These checks justify retaining the measured recovery repair. They do not establish a cause or prevention for the initial upstream cutoff.

## Second hourly boundary

Without another deployment or bridge restart, both initial interruptions recurred during the next hour. The existing systemd journal records Front Door's second watchdog at **07:55:49.404 UTC**, camera-source retirement at **07:55:50.080**, and streaming at **07:55:51.629**. Garage's second watchdog was at **07:56:47.455**, retirement at **07:56:47.482**, and streaming at **07:56:50.397**. All records came from the unchanged bridge PID 195316.

Watchdog-to-streaming was therefore **2.225 seconds** for Front Door and **2.942 seconds** for Garage. These journal intervals exclude the silence before the watchdog and do not measure raw VIDEO gaps or physical-screen freezes. They support repeatable recovery after the second interruption and contradict any claim that the initial cutoff was prevented. No new packet capture or playback intervention was used. [Sanitized journal observations](data/pinned-camera-hourly-20261021/second-hour-recovery-journal.json).

## Closure trial completed without reproduction

The second, bounded listener-only observer ran **07:38:52.926–08:08:52.931 UTC** for 1,800.005 seconds on Balcony's existing source/owner/session/socket. It recorded **zero END, stop or close events**. All identities remained unchanged and connected/live. All four owned callbacks were removed while preserving the identities and order of other callbacks. The observer process exited, the loopback inspector stopped listening, and bridge PID 195316 remained active. [Result and cleanup checks](data/pinned-camera-hourly-20261021/balcony-eos-next-closure-no-event.json).

This is an inconclusive reproduction window. It does not establish that earlier EOFs were fixed, nor implicate incoming END or local SDK disposal. The independent source audit shows that selected-peer END can close the source, but that conditional code path was not observed here; changing it would therefore be speculative. No SDK production change or periodic restart policy follows from this trial.

The initial hourly VIDEO cutoff also remains unexplained despite repeatable faster recovery. An owner-app lifetime comparison is still missing: a fresh, successful `xctrace` inventory listed no connected iPhone. Existing short owner-app captures do not cross the relevant lifetime and cannot supply that comparison. No evidence-backed prevention intervention is available from the completed trials. Further progress requires new causal evidence, such as a captured actual EOF origin or an equivalent owner-app session observed across the hourly boundary. The recovery fix is retained; the broader disconnect objective is not complete.
