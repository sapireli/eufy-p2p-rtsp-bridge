# Postboot stock-driver CPU baselines

These two five-second samples were taken on GaragePi after the user rebooted it,
on boot `87817643-a7c6-4180-8259-e8c47e6f3d5f`, with the stock Wi-Fi driver.
They precede the private native plugin compilation. They are different windows,
not a controlled driver or process-priority A/B, and do not prove smooth output.

| Measurement | Initial boot-maintenance sample | After maintenance exited |
| --- | --- | --- |
| Pi monotonic interval, ns | 324417409000–329482811000 | 652477676000–657540932000 |
| Elapsed seconds | 5.065402 | 5.063256 |
| Observer CPU | 0.904213% | 0.870250% |
| Kernel warning headlines | 0 | 0 |
| CPU idle ticks | 0 of 501 | 113 of 506 |
| Received bytes | 2,604,381 | 4,083,612 |
| GStreamer process CPU ticks, both cameras | 220 | 271 |

Both kernel counters report available=1, complete=1, with no sequence gaps,
malformed records, drain-cap hits or read errors. Zero warnings applies only to
these observed intervals, not the whole boot or future operation.

In the first window, apt PID 843/start tick 27054 used 225 CPU ticks, about 44.4%
of one core. Its observed parent chain led through dietpi-update to
dietpi-postboot.service. The two media processes used about 43.4% combined.
The apt process exited naturally before the second window. A subsequent attempt
to adjust its priority failed at the initial missing /proc/843/stat read, before
any priority was changed. There was no deployed priority intervention.

The second window had approximately 22.3% idle CPU. The same two GStreamer
identities (723/start 17807 and 724/start 17810) used about 53.5% combined.
Different throughput and natural maintenance completion confound comparison.
This establishes that maintenance competed for the single core in the first
window and that the earlier driver warning storm was absent in both samples.
It does not establish that either was the sole playback cause.

Private raw artifacts under `.evidence/frame-freshness-2026-10-12/`:

- `linux-fresh-stock-after-reboot-census.txt`: SHA-256
  `69de1ac0d31685432a5c91ccee6417d505a1fcf996a34916ebd38f5b31f716d5`.
- `linux-fresh-stock-after-apt-census.txt`: SHA-256
  `89931115fd1b1689f3d81a370bada8639f586e706f5e453c1d2f56d612ee3280`.

Later playback logs and compilation intervals must not be represented as results
of these CPU samples. Live generation-fix deployment and a clean sustained
playback gate remain necessary.
