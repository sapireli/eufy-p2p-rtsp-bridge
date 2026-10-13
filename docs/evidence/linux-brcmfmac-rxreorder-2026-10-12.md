# GaragePi USB WiFi reorder fault and CPU starvation

## Proven observation, not a completed repair

GaragePi emitted repeated kernel warnings in the USB WiFi receive path:

```text
drivers/net/wireless/broadcom/brcm80211/brcmfmac/fwsignal.c:1731
brcmf_fws_rxreorder
```

A current observed 7.6505-second kernel journal span contained **59 warning
events producing 1794 journal entries**, approximately 30.4 entries per event.
The event rate was 7.71 warnings/second. Counting warning headers and their
associated `cut here` lines together would incorrectly double the event count.
Addresses, IP/MAC values and raw exception-stack words were redacted.

The common stack was USB RX completion → `brcmf_rx_frame` →
`brcmf_fws_rxreorder` → warning/stack dump. A PID printed in that stack identifies
the interrupted task; it does not assign the WiFi fault to journald, GStreamer,
the decoder or DRM. A separate recent journal window contained 3891 kernel
priority-4 entries / 275,352 message bytes, versus only eight client observation
entries / 6696 bytes. Both camera processes had `GST_DEBUG=2`, no `-v`, and no
per-frame timing trace. This was not a deliberately verbose client probe.

No driver patch, module reload, network change, warning suppression, resolution
change or pacing policy change was performed. The existing power state was OFF
while the service remained active and disabled for boot; physical output was
unobservable. These states were preserved.

## Actual device provenance

| Field | Read-only result |
| --- | --- |
| Kernel | `6.18.50+rpt-rpi-v6` |
| Warning build string | `Raspbian 1:6.18.50-1+rpt1` |
| Module package | `linux-image-6.18.50+rpt-rpi-v6` |
| Module srcversion | `94DA3F3023073BA79C36394` |
| Module vermagic | `6.18.50+rpt-rpi-v6 mod_unload modversions ARMv6 p2v8` |
| WiFi driver version | `6.10.198.66` |
| Firmware version | `01-32bd010e` |
| USB bus location | `1-1.3` |

The package owns the module under `/usr/lib/modules`; querying the equivalent
`/lib/modules` path did not match because of the merged filesystem layout.

## Source condition and remaining causal boundary

The official Raspberry Pi `rpi-6.18.y` source has the matching warning at line
1731. When firmware signals a new hole, the host flushes its reorder-slot ring
and warns if `pend_pkts` remains nonzero. This establishes a failed pending-count
invariant after the flush. Adjacent occupied-slot replacement paths free the old
packet and increment the pending count for its replacement without decrementing
the removed slot. That is an independently reviewable bookkeeping hypothesis;
the live trace did **not** record duplicate-slot metadata or prove which earlier
branch created the mismatch. The branch source is not asserted to be the exact
installed package's source commit. [Official driver source](https://raw.githubusercontent.com/raspberrypi/linux/rpi-6.18.y/drivers/net/wireless/broadcom/brcm80211/brcmfmac/fwsignal.c).

The warning storm adds real logging/stack-dump work. It is a concrete contributor
to investigate, not proof that it explains every WiFi delay or every playback
pause. Correcting the generating invariant requires a separate controlled
repair and live validation; merely reducing kernel verbosity would hide it.

## Native scheduler and CPU evidence

The reader helper directly identified PID 29482 / TID 29493 polling RTSP FD 20,
inode 599524. PID/TID start ticks 19346029 / 19346234 remained unchanged in the
later scheduler window. The helper's five-second identity/overhead gate passed
at 0.387% of one core; its subsequent 30-second extension completed. Across the
whole 35-second observation, task execution increased 1.6843 seconds and
runqueue waiting increased 9.2552 seconds. This is cumulative scheduler delay,
not a continuous nine-second stall or a decoder-lock diagnosis.

The detailed `kernel.sched_schedstats` knob was initially zero, yet these
per-task counters advanced. They must not be declared stale merely from that
knob. The kernel reference implementation exports execution runtime plus
`sched_info.run_delay`; `CONFIG_SCHED_INFO` is independent of detailed scheduler
statistics. [Counter definitions](https://docs.kernel.org/scheduler/sched-stats.html),
[reference proc implementation](https://raw.githubusercontent.com/torvalds/linux/v6.12/fs/proc/base.c),
[reference sched-info availability](https://raw.githubusercontent.com/torvalds/linux/v6.12/include/linux/sched/stat.h).

A separately authorized accounting-enabled five-second gate measured execution
487.8 ms and runqueue-delay increments 4664.8 ms for the same task identity.
All six task snapshots were runnable; no fresh FD proof was caught, so that
invocation rejected its longer extension. Global idle ticks did not increase.
Native observer CPU was 0.629%. The accounting setting was saved as zero,
temporarily set to one, protected by an EXIT trap and an independent 60-second
restore timer, then restored to zero and the timer stopped. Kernel accounting's
additional overhead was not quantified; this is not a neutral benchmark.
The source of the knob's overhead is documented by the
[kernel sysctl reference](https://www.kernel.org/doc/html/v6.7/admin-guide/sysctl/kernel.html#sched-schedstats).

### Separate whole-process five-second census, accounting knob zero

The census read process names and start/user/system fields only, with no command
lines or payloads. Its own cost was 49.065 ms / 5.0846 seconds = 0.965% of a core.
All 85 process identities matched at both endpoints; no new, exited or reused
PID was seen at those endpoints. Work from a process existing only between
endpoints could still be missed.

| Consumer | User + system tick increment | Approximate one-core usage |
| --- | --- | --- |
| GStreamer PID 29481 | 134 + 59 | 37.96% |
| GStreamer PID 29482 | 92 + 50 | 27.93% |
| journald PID 96 | 55 + 40 | 18.68% |
| selected event, softIRQ, VCHIQ and brcmf workers | 0 + 52 | 10.23% |
| Go client PID 23349 | 1 + 9 | 1.97% |
| Avahi PID 192 | 6 + 2 | 1.57% |

`CLK_TCK=100`. Global increments were user 297, system 152, softIRQ 51 and idle
2 ticks: approximately **99.60% busy**. WLAN received 2,573,091 bytes during the
census, approximately 4.05 Mbps. Compiler, probe and its SSH ancestry were
excluded from the workload table. Process user/system increments totaled 500
ticks versus 502 global ticks. Global softIRQ time overlaps process system
accounting and must not be added as an independent workload bucket.

Thus saturation persisted with the detailed accounting knob zero. The two
GStreamer workloads were dominant, and journald consumed significant remaining
capacity; there was no unexplained rogue process in this census. Counter endpoint
reads are not instantaneous, and these percentages are a short-window result.

## Provenance limits and artifacts

The census's first version omitted explicit start/end monotonic timestamps. Its
PID birth ticks, elapsed time and file metadata support an approximate bounded
window, not exact simultaneous journal attribution. The historical broadened
journal range returned no retained records. The warning-rate observations above
are **different current windows**; a retention cause was not proven. Future
census output now includes explicit timestamps without repeating the capture.

Private metadata artifacts under `.evidence/frame-freshness-2026-10-12/`:
`linux-native-reader-sched-flag0.txt`,
`linux-native-reader-sched-flag1-gate5.txt`, `linux-native-cpu-census5.txt`,
`linux-journal-current-attribution.json`, `linux-kernel-warning-attribution.json`
and `linux-brcmfmac-provenance.json`.

The diagnostic helpers are [reader probe](../../client/tools/rtsp_reader_probe.c)
and [CPU census](../../client/tools/cpu_census_probe.c). Their focused parser
self-tests passed; neither changes service, media, network or scheduler policy.
