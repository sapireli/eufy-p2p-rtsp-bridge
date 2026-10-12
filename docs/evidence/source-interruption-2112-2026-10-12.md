# Read-only source interruption audit, 21:12 UTC

## Observed boundary

The unchanged bridge PID26350 (started20:12:31) logged two HomeBase feeds with
no bytes for31/32 seconds at **21:13:12.899957/21:13:12.908737 UTC**. This implies
last-byte activity approximately21:12:40–42; the rounded watchdog ages are not
exact ingress timestamps. Both feeds stopped and scheduled reopening.

The bridge announced `streaming` at21:13:15.498666/21:13:15.548525, but both
subsequently hit **awaiting-first-frame warm timeouts** at21:13:35.508607/.517886.
Those announcements therefore do not establish recovered frame delivery. Further
streaming announcements occurred21:13:39.244801/21:13:42.588414. A separate
standalone feed ended21:13:40.147276 and announced streaming21:13:41.826094.
This journal alone does not measure the first fresh picture after recovery.

The bounded21:12:00–21:14:30 service journal has25 rows, no `realtime stalled`,
authentication recovery, or process restart. The all-service journal adds only
three SSH rows and one IPv6 address registration. There are no kernel entries
in21:11:00–21:15:00, and no logged link-down/OOM/time-step/throttling incident.
Absence from these logs does not exclude a transient unlogged host stall.
A settled `ok` cloud poll at21:12:49.859 was separately reported by the root
monitor; temporal proximity does not establish poll causation.

## Later host snapshot and limits

Read-only snapshots around21:15–21:17 show the same process, load averages
1.02/1.37/1.54, CPU pressure around8%, no memory pressure, and sufficient disk
space. IO pressure is materially elevated: full pressure around20% over60s/300s.
This is a **later snapshot**, not an event trace at21:12:40, and does not establish
whether storage, GPU waiting, or another workload caused the interruption.

Ethernet lifetime RX errors/drops/missed were0, TX drops1. UDP lifetime
RcvbufErrors/InErrors were3,020; this must not be summarized as zero UDP loss.
All currently open Node UDP sockets have empty receive queues and per-socket
drop counters0. Earlier closed sockets cannot be inspected, so these lifetime
errors cannot be assigned to or excluded from the incident. Active time-sync
services were absent and NTPSynchronized=false; no logged clock step was found.

No Inspector, packet capture, source commands, bridge restart, buffer/quality
change, or Pi intervention was performed for this audit. The existing bounded
header tap had finished21:08:31, several minutes before this boundary. This
record is insufficient to name a source/RF, SDK, poll, decoder, or scheduling
root cause. In particular, it does not support blaming the Pi pacing plugin or
claiming that the earlier poll-watchdog fix prevented every source interruption.

## Private provenance

All artifacts are under `.evidence/bridge-network-audit-2026-10-12/`; identities
and host details remain private:

| Artifact | SHA-256 |
| --- | --- |
| `incident-2112-bridge-journal.jsonl` | `03514918bc733a7eeb80be697ecc6b54e357fe6f880b0377cb46a27d401c6d97` |
| `incident-2112-all-journal.jsonl` | `6e77685cec290c0bad780eb7dd8ce123faeb904e273d78dc30cf3b3eb38a0cc1` |
| `incident-2112-current-host.txt` | `c0e0a50ab010645ea869104b3e9220a266a586359f288e9a4bf4ae49b6b5f2ef` |
| `incident-2112-current-resources.txt` | `0d2d24a508a42721151984a4c1c47d2c8fe0b2d755a2b1665538774fce6e9f4c` |
| `incident-2112-current-sockets.txt` | `28a061c6ce8baa847500bbcd082471aa8ca7d7a828783167f3e3f5dd1b5fb527` |
