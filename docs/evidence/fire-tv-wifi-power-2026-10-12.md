# Fire TV Wi-Fi power A/B/A — 2026-10-12

## Result and production decision

A real high-performance Wi-Fi lock request was held for 120 seconds on Fire TV
192.168.23.85. It did **not** produce a consistent improvement in frame age or
arrival cadence relative to the unchanged Fire TV 192.168.23.58. Garage TCP
retransmissions decreased during the request, but Front Door did not improve over
the first baseline, and user-visible timing metrics did not establish a benefit.
This experiment does not identify Wi-Fi power saving as the root cause.

The temporary debug hook and `WAKE_LOCK` permission were removed from the final
client. Both TVs' diagnostic logging was restored to INFO. Freshness enforcement
remained off throughout; observation did not suppress or blank any output.

## What was actually changed and verified

- Both TVs ran the same observation APK, SHA256
  `191daa19b789424c97853cf1bee4a06afbfec66eca892ccf73eb59efe291e9c0`.
- Only `.85` received a temporary `WIFI_MODE_FULL_HIGH_PERF` request. `.58` was
  the concurrent control. Both used the same bounded DEBUG logging throughout.
- Existing player sessions remained open. Application PIDs stayed `.85: 9193`
  and `.58: 8385`; no player retry appeared in either log.
- Native `dumpsys wifi` showed the request as
  `WifiLock{eufy-wall-bounded-wifi-test type=3 uid=10235}`. The HIGH_PERF acquired/
  released counters changed from 1229/1229 to 1230/1229 during the test, then
  returned to 1230/1230. This proves the system accepted and released the request;
  it does not independently prove the vendor radio changed its power mode.
- An independent timer released it at 16:06:05.121 UTC, 120.001 seconds after
  acquisition. It did not depend on the UI thread's expiry timer.
- A passive server TCP collector tracked the same four sockets through all phases:
  `.85` Front Door 42886, Garage 42890; `.58` Front Door 58076, Garage 58080.
- Both clients reported `staleOutputFrames=0`, `observeMissingStamp=0`, and no
  blanking reasons. Hardware decoder names and frame counters continued normally.

## Phase boundaries

Times below are UTC. Analysis excludes one second at each transition.

| Phase | Start | End |
|---|---|---|
| OFF1 | 16:02:34.028 | 16:04:05.120 |
| ON | 16:04:05.120 | 16:06:05.121 |
| OFF2 | 16:06:06.450 | 16:07:37.426 |

The explicit final release and INFO restoration completed at 16:07:37.809.
A request/response check bounded the server wall clock relative to the Mac within
−199 to +45 ms. The transition exclusion exceeds that uncertainty. Device wall
clocks differ, so raw cross-TV log epoch differences are not used as relative
playback latency.

## Transport measurements

Percentages are retransmitted segments relative to segments sent in each guarded
phase, reported by the passive server collector.

| TV / camera | OFF1 retransmissions | ON retransmissions | OFF2 retransmissions |
|---|---:|---:|---:|
| .85 Garage | 0.2269% | 0.1162% | 1.1782% |
| .85 Front Door | 0.3157% | 0.3342% | 0.5358% |
| .58 Garage, control | 0.0087% | 0.0142% | 0.0193% |
| .58 Front Door, control | 0.0663% | 0.0759% | 0.0805% |

The control also deteriorated over time. A single sequence therefore cannot
attribute the later `.85` deterioration to releasing the request. Garage's
retransmission improvement alone does not establish smoother or fresher playback.

## Player timing measurements

These are per-picture **bridge-ingress-to-decoder upper bounds**, including clock
uncertainty. They are not sensor capture age or physical display latency.

| TV / camera | OFF1 p95 age | ON p95 age | OFF2 p95 age |
|---|---:|---:|---:|
| .85 Front Door | 360 ms | 335 ms | 376 ms |
| .85 Garage | 265 ms | 293 ms | 321 ms |
| .58 Front Door, control | 353 ms | 329 ms | 347 ms |
| .58 Garage, control | 259 ms | 271 ms | 240 ms |

The median time from receiving an RTSP access unit to decoded output stayed about
11.6–11.8 ms in all phases. Front Door's p95 received-frame gaps on `.85` were
216/226/224 ms; Garage's were 206/201/223 ms. The unchanged `.58` also showed large
upstream gaps. Holding the Wi-Fi request did not eliminate them.

Matching the same bridge frame IDs gives this comparison of decoder age **upper
bounds**. Differing clock uncertainty prevents treating it as exact relative latency.

| Camera | OFF1 p95 .85 minus .58 | ON p95 | OFF2 p95 |
|---|---:|---:|---:|
| Front Door | 10 ms | 18 ms | 29 ms |
| Garage | 23 ms | 35 ms | 84 ms |

There is no consistent ON improvement in these paired timing measurements.
The concurrent stamped loopback observer reported Front Door p99 bridge-egress
age 406.9/423.4/399.2 ms and Garage 279.3/334.8/249.2 ms across OFF1/ON/OFF2.
Thus OFF2's much larger `.85` client tails occurred while the bridge's p99 was
lower than in ON. Together with the unchanged `.58` control, this locates that
additional tail downstream of bridge egress; it does not identify radio power
saving as its cause.
Codec-render timestamps were also collected, but Android render callbacks can be
batched or omitted; they do not certify physical scanout. See the separate source
and bridge diagnostics for upstream pauses and remaining transport tails.

## Audit and reproduction boundaries

The current direct player had no high-performance Wi-Fi lock. The old Media3
implementation at bridge commit `07927d8` also had no explicit wake-mode request,
no such lock, and no `WAKE_LOCK` permission. Its absence is not a demonstrated
migration regression.

Android documents that a
[HIGH_PERF Wi-Fi lock](https://developer.android.com/reference/android/net/wifi/WifiManager#WIFI_MODE_FULL_HIGH_PERF)
can disable power saving when supported by the hardware. That is a reason to test
it, not proof it fixes these streams.

Private reproduction artifacts are under `/tmp/eufy-wifi-test-repro/`: the temporary
patch, exact tested APK, orchestration and analysis scripts. Raw phase events and
client logs are `/tmp/eufy-wifi-aba-*`; the server collector and summary are under
`.evidence/bridge-network-audit-2026-10-12/`. These raw files are not public artifacts.
The test hook remains absent from production source.
