# Stock-control outage: read-only postboot hook and daemon evidence

## Boundary

This records the Pi after recovery by a new boot, not the failed boot's execution.
Snapshot UTC: **2026-10-13 01:05:33.082558**. Boot ID:
`87817643-a7c6-4180-8259-e8c47e6f3d5f`. The previous boot journal was unavailable
(parent found only boot 0), so these observations cannot prove which old-worker
step failed, whether its ifup succeeded, or why its rollback did not restore SSH.
No candidate module was loaded during the failed stock-control attempt.

All actions for this document were bounded, nice-19, read-only SSH metadata reads.
No module/network/client/power/freeze action, old rollback execution, timer arming,
CPU census or playback probe was issued. The parent separately restored the
existing client service. Snapshot fields are non-atomic; the main read took
2.519213 seconds on the Pi.

## Observed current state

- Kernel `6.18.50+rpt-rpi-v6`; loaded core srcversion
  `94DA3F3023073BA79C36394` (stock).
- Client `MainPID=671`, active, disabled for boot. This establishes process/service
  state, not physical output or a latency result.
- Monitor cgroup unfrozen (`cgroup.freeze=0`), sole observed member PID 505,
  start tick 7321, comm `dietpi-wifi-mon`. The failed boot's monitor descendants
  remain unknown; current membership does not reconstruct them.

| Current daemon | PID | Start tick | PPID | Cgroup |
| --- | --- | --- | --- | --- |
| wlan0 WPA supplicant | 425 | 5091 | 1 | `/system.slice/ifup@wlan0.service` |
| wlan0 DHCP client | 433 | 5198 | 1 | `/system.slice/ifup@wlan0.service` |
| eth0 DHCP client | 381 | 4920 | 1 | `/system.slice/ifup@eth0.service` |

No daemon command line, interface address, SSID, password or WPA configuration
contents were exported. Named process comm, PID/start tick, parent and cgroup were
read once. Current network daemons belong to existing persistent ifup units,
rather than the completed diagnostic worker unit.

## Actual installed hook source

Both `/etc/network/if-pre-up.d/wpasupplicant` and
`/etc/network/if-post-down.d/wpasupplicant` resolve to
`/etc/wpa_supplicant/ifupdown.sh` (SHA-256
`9d493c00b36cf074070ac84890390dea850b99108856c25a7e945b46c19664b7`).

The actual `/etc/wpa_supplicant/functions.sh` has SHA-256
`a9a4e0cfa3827e3cca535c559e3f09c114a063794de3c1c9cc4c201322730014`.
Safe source lines, with variable names intact and no expanded configuration:

```sh
# line 203
WPA_SUP_OPTIONS="-s -B -P $WPA_SUP_PIDFILE -i $WPA_IFACE"
# lines 270–272
start-stop-daemon --start --oknodo $DAEMON_VERBOSITY \
  --name $WPA_SUP_PNAME --startas $WPA_SUP_BIN --pidfile $WPA_SUP_PIDFILE \
  -- $WPA_SUP_OPTIONS $WPA_SUP_CONF
```

The `-B` option backgrounds the actual supplicant. This establishes that the
worker's original plain-ifup daemon-lifetime concern applies to installed hooks.
It does **not** establish that the failed boot reached this command or that
systemd actually killed that boot's supplicant. The independent harmless bridge
[service/scope proof](linux-brcmfmac-rollback-preflight-2026-10-12.md) establishes
the systemd lifetime mechanism; old Pi journal/cgroup evidence is absent.

## Staged worker identity

`/var/lib/eufy-wall-brcmfmac-trial/brcmfmac_trial.py` remains the old worker:
SHA-256 `845000a6b9577705dfbdfc4a51c769d7ef907345d6fa78e02d148b505268b7d9`,
exactly the repository file at commit `42ab506`. It was read/hashed, not changed
or executed. Local correction `5753708` has not been deployed by this inspection.
The persisted old restoring state must not be treated as a new trial invitation
or proof of working recovery.

## Private artifacts and limits

- `.evidence/frame-freshness-2026-10-12/worker-postboot-hooks.json`: SHA-256
  `3e5d5a9bb03d40bc02ed830f1df28fc125078d641e106f5fc6273aed39ab6e8a`.
- `worker-postboot-wpa-invocation.json`: SHA-256
  `ec2add24d69235f9c443dc122c1909e4470e01467be01b7410fe9f9c77f01460`.
- `worker-postboot-wpa-background.json`: SHA-256
  `324ccfe0865ec0bda4d65e8db5775bbb346a559295ac8a4ce5fc979c2b213456`.

All owned SSH reads finished before the parent's next baseline. The new boot
confounds any immediate warning/CPU comparison with the failed preboot ring state.
Neither the candidate patch effect nor corrected Pi rollback is verified here.
