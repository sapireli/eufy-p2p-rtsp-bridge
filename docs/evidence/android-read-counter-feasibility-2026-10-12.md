# Android read-counter feasibility — 2026-10-12

No new CPU/GC diagnostic was added to the player. No APK installation, playback,
priority, Wi-Fi, buffer or transport-policy change was made. The parent rejected
CPU-only instrumentation as unable to decide blocked delivery versus descheduling.
The following isolated probes establish feasibility for future diagnosis, not a fix.

## Kernel scheduling counters unavailable

On both actual Fire TVs, under run-as and using own `/proc/self`, `schedstat` and
`sched` returned ENOENT. `/proc/sys/kernel/sched_schedstats` was also unavailable.
There is no usable runqueue-duration file on these builds; reporting zero would
be incorrect. Per-packet failed file opens were not added. Task stat/status can
supply coarse CPU/context-switch counts but not the missing runnable duration.

## Isolated native-counter probe

`tv/tools/diagnostics/ReadCpuProbe.java` ran in an `app_process` DEX process,
without replacing the wall app. It benchmarks public Android Debug native/API
calls and controlled sleep, local socket read, and busy work on the same TID.
Each full run, including push/startup, took1.56 seconds on .85 and1.60 on .58;
owned temporary DEX files were removed. The benchmark is a separate ART process,
so its GC counts do not describe the wall player's heap.

The [Android Debug reference](https://developer.android.com/reference/android/os/Debug#threadCpuTimeNanos())
describes threadCpuTimeNanos as an indication compared with a prior call, with
unsupported result-1 and nanosecond units not implying matching accuracy. We did
not treat this API as a runqueue-time counter.

CPU-call costs use200 batches of100 calls, after1000 warmup calls; values include
volatile assignment and amortized timing overhead. They are batch means, not
worst individual call durations.

| Probe | .85 | .58 |
| --- | ---: | ---: |
| Thread CPU counter median / p95 per-call ns | 1147 / 2230 | 1956 / 3272 |
| Controlled30 ms sleep elapsed / CPU ms | 30.177 / 0.133 | 30.177 / 0.126 |
| Controlled40 ms local socket read elapsed / CPU ms | 41.325 / 0.206 | 40.980 / 0.154 |
| Controlled20 ms busy work elapsed / CPU ms | 20.047 / 14.548 | 20.323 / 17.390 |

Every controlled span confirmed matching before/after TID. This API distinguished
CPU execution from the controlled wait in these probes. Low CPU compared with wall
elapsed still does **not** distinguish network blocking from thread descheduling,
GC pauses, or other non-execution. A cheap call is not automatically useful enough
to justify per-packet player instrumentation; none was deployed.

## ART GC statistics

`RuntimeKeysProbe.java` enumerated actual supported runtime keys in another isolated
process, taking1.14/1.18 seconds. Both builds returned null for
`art.gc.total-time-waiting-for-gc`; the enumerated eight-key set did not include
that metric. Available keys covered GC count/time, allocated/freed bytes, blocking
GC count/time, and rate histograms.

The [runtime-stat reference](https://developer.android.com/reference/android/os/Debug#getRuntimeStat(java.lang.String))
marks these approximate and runtime-dependent. GC and blocking-GC times are in
milliseconds. They are process-wide cumulative GC-run metrics, not an exact
per-read pause or waiting-time measurement. A delta could support correlation;
a zero delta does not certify that a frame saw no runtime pause.

Four GC lookups were separately benchmarked in100 batches of10 calls. Median
lookup costs were approximately1.5–7.2 µs; p95 values2.1–11.2 µs. Real player heap,
collector contention and scheduling were not represented by this small fresh
process; its lookup costs do not bound production tails. No per-frame GC lookups
or polling observer was added.

## Matching source interruption and current quiet run

The exact EufyWall INFO interval21:12:40–21:13:40 was no longer retained, so cumulative
Door late counters202/203 cannot be attributed to post-recovery frames. Retained
SYSTEM MediaCodecLogger records on both TVs did show two hardware clients becoming
zero at21:12:46.848–47.273, followed by repeated single-client registration/release
at aboutfive-second intervals, then a new registration around21:13:39.914/39.980.
This matches outage/retry behavior; it does not independently identify the bridge
or decoder cause. No app-process restart was observed; current PIDs11401/10531.

A separately authorized short INFO+Mac receipt window ran21:21:09.296–21:22:09.308
UTC. Both owned observers exited successfully. Neither TV logged a new sparse
outlier; Door lower-late counters had delta0, as did Garage. Input queues were
sampled zero and hardware decoded counters progressed. Mac Door859/Garage856 stable
frames had maximum upper ingress receipt ages361.338/193.540 ms, with no missing
stamp, invalid transport, or upper500 ms miss. This is a quiet60-second observation,
not evidence that historical stalls are fixed or a late-event CPU/scheduling join.

## Reproduction and private artifacts

Compile the two standalone Java classes with a JDK and Android platform jar,
dex with d8 min-api28, push the owned DEX to `/data/local/tmp`, and invoke the
chosen main class with `app_process`. These tools are outside app/module source
sets; do not include them in the wall APK or run them as an acceptance workload.
Remove the temporary DEX afterward and bound each process to30 seconds.

Private JSON/results are `threadcpu-device-probe.json`,
`art-runtime-keys-device-probe.json`, `schedstat-feasibility.json`,
`tv-source-interruption-2112/`, and `tv-source-recovery-60s/` under
`.evidence/bridge-network-audit-2026-10-12/`. No camera image payload is committed.
