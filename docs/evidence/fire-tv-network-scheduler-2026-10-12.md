# Network inventory and bounded scheduling trace — 2026-10-12

This is additional diagnosis, not a new player or network policy. No priority,
Wi-Fi lock, UDP, camera quality, or buffer policy changed.

## Read-only inventory

At approximately20:06 UTC, the bridge routed both TVs through wired `eth0`,
1000 Mb/s full duplex. NIC tx/rx/missed/alignment errors were zero; interface TX
reported one lifetime drop. These snapshots do not establish error-free delivery
through the switch/access point or during an earlier event.

TV.85 reported RSSI-57 dBm,292 Mb/s link speed; .58 reported-64 dBm,175 Mb/s.
Both reported5745 MHz. SSID and BSSID were redacted by the device, so this does
not establish that both were associated with the same AP. No active high-performance
Wi-Fi locks were shown. The earlier lock A/B/A lacked consistent benefit; none
was reinstated. Lifetime wlan error counts are not event-specific evidence.

At that snapshot the RTSP IO threads were .85 Garage1960/Door1979 and .58
Garage1344/Door1363, nice0. Decoder threads had nice-10. Thread mappings must be
refreshed after player recreation rather than inferring that old TIDs stay valid.

## Parent's complete TCP observation

The parent socket sampler covered19:58:11.256–20:08:10.758 UTC,1200 samples at
approximately500 ms. API stream inventory mapped45534/45538 to .85 Garage/Door,
60740/60744 to .58 Garage/Door. Differences below are actual socket-counter deltas;
retransmitted bytes are a fraction of all sent bytes, not packet-loss probability.

| Socket / camera | Observed duration s | Sent MB | Retransmitted MB | Ratio % | Peak Send-Q bytes | Minimum advertised window bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| .85:45534 Garage | 571.998 | 329.717 | 1.406 | 0.426 | 1004912 | 403328 |
| .85:45538 Door | 571.998 | 222.452 | 0.412 | 0.185 | 414379 | 422080 |
| .58:60740 Garage | 599.501 | 342.597 | 0.084 | 0.024 | 882371 | 488192 |
| .58:60744 Door | 599.501 | 232.755 | 0.145 | 0.062 | 410651 | 610112 |

The old .85 sockets disappeared at20:07:43.254, so their counters are **not** a
full600-second continuous connection observation. Retained system logs subsequently
showed FireTVKeyPolicyManager `handleShortPressOnHome` at43.867/43.879 and launcher
HOME start at43.895. Network settings opened20:07:54.584, Amazon NSA network
diagnostics at58.223/58.473, advanced diagnostics20:08:02.344, and Eufy Wall's
LEANBACK activity resumed20:08:16.004. Who initiated navigation was not established.
The app's normal `onStop` releases players and `onStart` restores the wall; this
sequence explains foreground departure/resume rather than an SDK restart or a
proven automatic player failure. The precise app lifecycle callbacks were not
retained in the main log buffer; system navigation and socket boundaries were.
New IO threads8950/8951 were present before tracing. The .58 socket identities
remained throughout the TCP window and it had no corresponding HOME event.
The bounded private logs are `tv-recreation-200743/`; bridge journal20:07:25–20:08:00
contained no entries. This navigation interval is excluded from uninterrupted
playback conclusions.

The actual19:58:16–17 retransmission/send-queue episode and matching late frame
receipt are documented separately in `fire-tv-rtp-read-stage-2026-10-12.md`.
Advertised windows remained hundreds of KB in the sampled TCP data; no zero-window
sample was observed. This supports delivery/recovery as a candidate, while neither
RSSI nor nominal link speed identifies the underlying radio/AP/kernel cause.

## Scheduler trace: quiet window, partial retained data

A bounded120-second `atrace` on .85 captured sched/freq/idle categories with a
16384 KiB per-CPU circular buffer, compressed dump. INFO on both TVs and exact Mac
receipt observers were bounded150 seconds; local RX/dynamic-thread samples90 seconds.
The trace began20:08:45.561 UTC; dump/normal cleanup finished20:11:14.518.
INFO ended20:11:13.252, local RX20:11:16.529; Mac exited successfully. All owned
observers closed before the parent's next SDK/bridge deployment.

`tracing_on` was zero before and after. Atrace selected BOOT clock; the previous
inactive LOCAL tracing clock was restored and verified after cleanup. App
`System.nanoTime()` fields use a different clock domain; no direct subtraction
from BOOT trace timestamps was performed. No retained clock-sync marker was
available for a precise absolute-time late-frame join.

The trace reported1467040 retained /2123386 written events. Per-CPU retained ranges
were approximately94.4/89.2/78.6/73.6 seconds; the **common all-CPU range** was only
BOOT144946.472812–145020.050109,73.577 seconds. Prefix overwrite is a material limit.
Frequency reads for CPUs1–3 were permission denied and no cpu-frequency events
were present. Scheduling and idle events were present. Post-dump vendor statistics
reported negative entries/bytes, so they were not used as trustworthy occupancy
counts. The header and parsed retained ranges establish the wrap limitation.

There were no >500 ms sparse outlier records on either TV in this bounded INFO
window. Thus the trace did not capture the earlier late event and cannot rule out
scheduling during it. The parent TCP overlap began20:09:32 and covered only the
last approximately73 seconds of trace; local RX began20:09:44, also partial coverage.

For a quiet scheduling baseline, the dynamic metadata identified IO TIDs8950/8951
(nice0) and decoder TIDs8952/8953(nice-10). Restricting to the common retained range,
tracking wakeup/preempted-runnable to next switch-in, and clearing blocked/running
states produced:

| IO thread | Observed runnable intervals | Median ms | p99 ms | Maximum ms |
| --- | ---: | ---: | ---: | ---: |
| 8950 | 3385 | 0.105 | 2.651 | 11.117 |
| 8951 | 3408 | 0.041 | 2.973 | 9.232 |

A naive analysis across differently overwritten CPU prefixes manufactured multi-
second waits from omitted switch-in events; those results were discarded. The
restricted quiet baseline is not evidence about the absent >500 ms event. Trace
and subprocess observer overhead was not measured as zero; the trace generated
approximately2.12 million events and207 MB of decompressed text. It is not a
normal-production workload or proof that enabling tracing improves latency.

## Falsifiable next measurement

A useful late-event trace must retain all relevant CPUs around an actual exact-
frame outlier and align the BOOT/MONO clocks explicitly. A sleeping read thread,
empty local RX queue, and coincident sender retransmission/ACK delay support a
delivery wait. A thread continuously runnable but not scheduled while bytes are
available locally supports application descheduling. Sparse socket samples alone
cannot distinguish these; neither case warrants changing priority or buffering
without the corresponding observation. An eventual wired-versus-Wi-Fi A/B with
identical source/decoder settings could separately test the wireless path if a
wired connection becomes available; no transport-policy deployment was made.

Private artifacts are in
`.evidence/bridge-network-audit-2026-10-12/tv-scheduler-120s/`, plus current network
inventory and parsed parent TCP summary. Parent TCP files are
`tcp-receiver-joint-600s.jsonl` and `tcp-sched-overlap-180s.jsonl` under
`.evidence/frame-freshness-2026-10-12/`. No camera image payload is committed.


For a future rare-event trace, bounded asynchronous circular scheduling-only
capture could retain prehistory until the first real sparse late record, then
stop/dump after a short post-event interval, with a600-second hard timeout and
normal cleanup. This was only proposed, not started. To avoid direct BOOT/MONO
subtraction, a diagnostic-only sparse bracket of BOOT-before, MONO, BOOT-after
would supply an explicit clock-offset interval for the triggering frame. The
concrete purpose is distinguishing a late frame's blocked read from runnable
unscheduled IO on a retained all-CPU interval. A no-outlier timeout cannot establish
that the problem is fixed; buffer wrap and tracing overhead remain reported limits.


## Sparse clock link and shortened event trial

Source commit`d1fc390` adds BOOT-before/MONO/BOOT-after clock reads **only on sparse
late-frame diagnostics**. The BOOT-minus-MONO offset is bracketed at that sampled
instant. It is not used for freshness admission, pacing, buffering or display.
Tests cover the true offset lying within the interval, invalid/reversed readings,
and explicit clock fields only on a real sparse outlier. All53 existing tests plus
three clock tests passed (44 app+12 library). Installed APK SHA:
`77ca7494ca1506176838854dc5f32301e8222ce542a7fcc83ec29bea8a9fb30a`.
Both TVs were installed sequentially; exact full preferences, hardware AVC and
binary hashes were verified. PIDs11401/10531.

The user requested shorter diagnostic iterations, so the previously proposed600s
trial was reduced to a90s cap. An asynchronous SCHED-only controller started at
20:30:55.200 UTC, targeting the first new .85 outlier with lower age >700 ms and
receive→decode <50 ms, then five seconds of posthistory, or a quiet90s deadline.
Original Mac receipt, INFO and local RX observers were separately bounded90s;
no parser fork was required. Root's TCP120 began20:31:17, missing the trial's first
approximately23 seconds. Pi observers/CPU fixtures were inactive before this trial.

No sparse outlier occurred on either TV, so the trigger did not fire and the new
clock link was **not yet exercised live**. The trace stopped at its quiet deadline;
dump/cleanup ended20:32:47.409 UTC. INFO ended20:32:23.354, local RX20:32:25.820;
Mac exited successfully. `tracing_on=0` and the previous LOCAL trace clock were
verified/restored. All owned observers closed. No SDK reset, quality, priority,
Wi-Fi lock or transport-policy change was made during this trial.

Trace header retained1097477/1104160 events (6683 overwritten), with common all-CPU
BOOT coverage146231.446261–146319.196804,87.751 seconds. It produced180.8 MB of
private decompressed metadata; tracing/observer overhead is not assumed zero.
The Mac received1310 Door/1325 Garage stable stamped frames, no missing/invalid
transport or upper500 ms misses, with all-frame maximum upper receipt ages
254.133/296.533 ms. INFO decoder mean rates were approximately14.9–15.2 fps;
minimum five-second windows11.8–12.8 fps, input queues at most one, no warnings,
missing stamps, unknown clocks, observation blanks or pacing drops.

This short quiet trial establishes bounded cleanup and an installed diagnostic
candidate. It does not establish that earlier network stalls are fixed or decide
blocked read versus descheduling during a late event. Future long runs are reserved
for final candidate validation rather than repeated diagnostic attempts.
