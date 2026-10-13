# Fresh-stock control: network recovery failed

## Actual operation and result

The root ran `stock-trial` with worker commit `42ab506`, using the packaged
Broadcom driver, not the repaired candidate. The initial loaded source version
was `94DA3F3023073BA79C36394`; stock/backup digests and the local IPv4 route passed
the read-only preflight. The candidate was staged separately and **never loaded**.
The installed module paths were not overwritten.

Systemd accepted `ewb-brcmfmac-stock-control.service`, invocation
`916cad917ba744d89c36c88480a73a38`. SSH then became unreachable at the existing
Pi address. Checks at 00:39:10 and 00:40:21 UTC failed. By 00:41:35 UTC, a bounded
LAN SSH endpoint scan found four endpoints and none offered the Pi's previously
known host key. A subsequent two-packet ICMP check received no replies; another
SSH attempt timed out. This rules out a discovered SSH address change, not every
possible network or kernel state.
SSH was still unreachable at 00:46:57 UTC.

The worker's code arms a separate local 90-second rollback before `ifdown`.
Its actual timer/service state, loaded module identity after shutdown, WLAN
binding, DHCP state and journal are inaccessible. Do not claim that rollback
completed, that the kernel crashed, or that a particular recovery defect caused
this outage without recovering those records.

**No CPU/warning comparison or candidate playback result exists.** The native
census was compiled and self-tested before this operation, but never sampled.
The bridge remained healthy with all three source feeds; the Fire TV gate is
[recorded separately](tv-current-info-gate-2026-10-13.md).

## Concrete worker defects found during recovery review

1. Rollback skipped a core reload whenever the loaded source version matched
   stock. That is insufficient when the WLAN device or USB binding is missing.
2. `ifup` can start background WPA/DHCP descendants. A transient service with
   default `KillMode=control-group` can kill those descendants when its main
   worker exits. Process groups and daemonization do not move processes out of
   their systemd cgroup. The behavior follows the
   [official systemd lifecycle documentation](https://raw.githubusercontent.com/systemd/systemd/v257/man/systemd.kill.xml).

The second mechanism was reproduced independently on the bridge with harmless
sleeping child processes: service main exit left no live child, while scope main
exit left its child alive in an active scope. Explicit cleanup removed that
child. This proves the process-lifetime defect in the proposed recovery path; it
does **not** establish the Pi's actual WPA/DHCP process lineage during this outage.
The retained repeat proof spans 00:45:53.510941–00:45:54.722224 UTC: service
child PID36994/starttick5214907 disappeared after main exit, while scope child
PID36999/starttick5214964 remained alive with the same identity until owned cleanup.
Its metadata SHA-256 is
`a4ed8d8cee78e816939afce948e20386250d78c7f8e52db995dec6b453d37baa`.

Worker commit `5753708` corrects scoped daemon lifetime, missing-netdev reload,
and unsafe monitor-freeze checks. All 25 local regression tests pass, and the
harmless systemd proof passes. **That corrected worker was not installed on the
unreachable Pi**, and its actual network recovery remains unverified.

Freezing the Wi-Fi monitor also requires examining its actual descendants: it
must not freeze an existing network daemon or an `ifupdown` lock holder. The
preflight recorded the monitor's main PID and freezer support, not its complete
process membership. The corrected worker rejects unsafe descendants before
arming and again after freeze acknowledgment; the Pi's actual membership still
needs inspection before another trial.

## Required next evidence

Recover local state and unit journals, inspect WLAN/USB bindings and the cgroups
of the named WPA/DHCP processes, then restore stock networking and the existing
client state. Do not launch another reload merely because a remote observation
timed out. Only after recovery and the worker lifecycle checks pass may the
fresh-stock/candidate comparison resume.

Private incident metadata:
`.evidence/frame-freshness-2026-10-12/linux-stock-reload-incident-20261013.json`.
The firmware/driver repair remains an offline-tested, ABI-checked candidate;
there is no measured live playback improvement from it.
