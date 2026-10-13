# Pi module A/B: preflight and independent rollback design

## Current state and completed staging

Read-only preflight found service active, MainPID 23349, disabled for boot,
GStreamer PIDs 31796 and 32103, and existing TV power **ON**. This superseded the
earlier OFF reading. No power or service action was issued; future cleanup must
preserve the new external state rather than restore stale OFF information.

A bounded, nice-19 `dpkg-query` completed within its ten-second limit. Installed
image package: `linux-image-6.18.50+rpt-rpi-v6`, version `1:6.18.50-1+rpt1`.
No APT-cache, package install, module reload, network change or reboot was used.

Verified compressed stock copies are staged on the Pi, independently of SSH:

| File in `/var/backups/eufy-wall/brcmfmac-stock-20261012/` | Bytes | SHA-256 |
| --- | --- | --- |
| `brcmfmac.ko.xz` | 129100 | `ba7a84ba04b8da50dae1e59aec0a0312d22aee6742d77acff029949a8b1f408a` |
| `brcmfmac_wcc.ko.xz` | 1540 | `bac5fc8ea2cdbde2d2667294c982240d8cbef55ca82fcb682c951fa51816deeb` |

The directory is root-only mode 0700; copies and manifest are mode 0600. Source
and backup digests matched. Installed stock module files were not altered.

## ABI and build constraints

Target kernel/vermagic is `6.18.50+rpt-rpi-v6 mod_unload modversions ARMv6 p2v8`.
Stock brcmfmac srcversion is `94DA3F3023073BA79C36394`. The kernel enables module
loading, unloading and symbol versions, with module signing not configured;
`modules_disabled` was zero. The stock modules are XZ-compressed.

GCC 14.2.0-19+rpi1 matches the kernel compiler configuration; binutils 2.44 is
available. Matching kernel build headers and `Module.symvers`, plus `make` and
the `xz` command, were absent. Package availability has not been established by
this preflight. These are build prerequisites, not reasons to substitute another
kernel version or upgrade the running image.

`brcmfmac_wcc` holds `brcmfmac`; the core depends on `brcmutil,cfg80211`.
Candidate ABI validation must include the existing WCC module and modversions.
The source/build owner supplies the candidate and its exact provenance; this
document does not authorize loading an unmatched module.

## Existing network recovery mechanism

The interface is managed by ifupdown: `wlan0 inet dhcp`, using the existing
`wpa-conf /etc/wpa_supplicant/wpa_supplicant.conf`. WPA contents were not exported
or rewritten. Existing ifup/down hooks manage WPA and DHCP. The networking unit
is active/enabled; standalone WPA/networkd/NetworkManager units were inactive.

DietPi WiFi monitor is active/enabled, MainPID 498, in its own control-group.
Its current script calls `ifdown`/`ifup`; it has no observed ExecStop command or
direct player-kill command. A monitor recovery iteration could race a planned
driver reload. Its cgroup freezer is available, so a future experiment should
freeze/thaw that unit temporarily without invoking its stop path or changing its
enabled policy. Preserve any preexisting frozen state; thaw only a freeze owned
by the experiment.

`modprobe`, `insmod`, `rmmod`, `systemd-run`, `timeout`, `ifdown`, `ifup`,
`wpa_cli`, `dhclient`, `ip` and `sha256sum` are available. No reconnect command
below has been executed.

## Proposed local rollback worker and sequence

The installed stock path stays untouched. The candidate should be loaded from
an explicit staging path, so a normal `modprobe brcmfmac` during rollback resolves
the original packaged module. A local root worker must be staged and reviewed
before arming the test. It requires no network download or SSH session.

Worker design:

1. Lock against concurrent test/rollback workers; validate target kernel and
   stock-file/backup SHA values from the local manifest.
2. Freeze the active DietPi monitor only if its cgroup is not already frozen;
   record ownership of that temporary freeze.
3. Use bounded `ifdown --force wlan0`, then unload the WCC holder before the core:
   `timeout 10 modprobe -r brcmfmac_wcc brcmfmac`.
4. Reload the untouched packaged core with `timeout 10 modprobe brcmfmac`, and
   the existing WCC module if its normal loading path has not done so.
5. Recover the same configured interface with bounded `ifup --force wlan0`.
   Do not restart all networking, rewrite WPA/DHCP configuration, or create a
   second independent supplicant/DHCP client.
6. Thaw the monitor only when the worker owns its freeze, including error paths.
   Record loaded module identity, interface/address presence and service state
   without printing addresses, SSID, keys or credentials. Do not toggle TV power
   or change client service enablement.

Before any unload, arm a separate local transient timer, for example:

```sh
systemd-run --unit=eufy-brcmfmac-ab-rollback --on-active=90 \
  /usr/local/libexec/eufy-brcmfmac-ab-rollback
```

The test also uses an EXIT/error trap to invoke that same idempotent worker.
Only cancel the timer after verified stock restoration, interface recovery and
owned-freeze cleanup. A passed candidate measurement does not cancel rollback
unless a separately authorized promotion replaces the temporary A/B plan.
All commands in this sequence are **design, not executed actions**.

An independent worker covers loss of SSH/WiFi while the kernel remains able to
run it. It cannot promise recovery from a kernel crash or an uninterruptible
module-unload deadlock. Build/source verification and a bounded experiment remain
necessary. The actual warning/CPU reduction and hardware playback result are
still unverified until the candidate is built and tested.

Private preflight artifacts:
`.evidence/frame-freshness-2026-10-12/linux-rollback-preflight.json` and
`linux-network-recovery-layout.json`. The network configuration was inspected
only for safe interface declarations and control commands, never credential
contents.

## Concrete local worker (prepared, not deployed)

`client/tools/brcmfmac_trial.py` now implements this sequence. Its `trial` action
arms its own local systemd timer before freezing the monitor or taking WLAN down. It waits up to three seconds for the
`cgroup.events` frozen acknowledgement before any unload; failed acknowledgement
restores its owned freeze without removing the stock module.
The timer runs `rollback` after 90 seconds independently of the initiating SSH
session; successful candidate loading **does not cancel it**. The rollback service
retries failures every two seconds, including lock contention, with at most
three starts in a 300-second window. It does not retry indefinitely. Steps have bounded
subprocess timeouts. Neither action invokes TV power, starts/stops the client,
changes client enablement, rewrites interface configuration, or replaces packaged
modules. Module holder removal is explicitly WCC before core.

The root-only JSON manifest must contain:

```json
{
  "kernel": "6.18.50+rpt-rpi-v6",
  "interface": "wlan0",
  "stockSrcversion": "94DA3F3023073BA79C36394",
  "stock": [
    {"installed": "/exact/packaged/brcmfmac.ko.xz", "backup": "/exact/backup/brcmfmac.ko.xz", "sha256": "verified digest"},
    {"installed": "/exact/packaged/brcmfmac-wcc.ko.xz", "backup": "/exact/backup/brcmfmac_wcc.ko.xz", "sha256": "verified digest"}
  ],
  "candidate": {"path": "/root/staged/brcmfmac.ko", "sha256": "audited candidate digest", "srcversion": "audited candidate srcversion"}
}
```

These illustrative path/digest placeholders are intentionally not an executable
trial manifest. Populate them with the verified paths above and the independently
audited build. The manifest, script, and state directory must remain local,
root-owned, and immutable for the duration of the trial. Installed and backup
stock hashes are checked before both trial and rollback. A different candidate
srcversion is required to identify loaded stock versus candidate. Symbol CRC,
vermagic, dependency, source and build audits are separate prerequisites; a hash
check alone is not ABI validation.

Durable state is atomically replaced and fsynced before freeze ownership changes.
A flock serializes independent invocations. A preexisting frozen monitor is never
thawed; a freeze acquired by this worker is thawed on rollback and recoverable
errors. Failed recovery remains retryable, with the timer armed. Rollback checks
loaded stock identity and interface `operstate=up` before completing. That state
is **not** proof of DHCP reachability, restored RTSP playback, or physical output;
those must be verified separately by the deployment owner. A concurrent external
monitor freeze/state-file edit during the trial is unsupported.

Root deployment, not this preparation, would use:

```sh
python3 /root/staged/brcmfmac_trial.py trial --manifest /root/staged/trial.json --state /var/lib/eufy-brcmfmac-trial/state.json
```

A manual `rollback` uses the same manifest/state. The script's bounded synchronous
steps nominally finish before the 90-second timer. An uninterruptible kernel task,
a kernel crash, or loss of local storage can defeat any userspace rollback; this
worker does not promise recovery from them. The independent guard retries lock
contention rather than depending on SSH remaining connected.

Verification: thirteen local tests pass for independent-invocation recovery,
idempotency, preserved external freeze, failed candidate load, retryable failed
interface recovery, rejected stock digest, unfinished-trial protection flock
exclusion, timeout termination of a spawned hook process group, interrupt group cleanup,
asynchronous freeze
acknowledgement ordering, interrupted-owned-freeze resumption, real guard runtime
properties and freezer failure cleanup. The fake host performs no module/network/systemd action. Real local
systemd timer execution, freezer behavior, module switching, warning reduction,
CPU improvement and playback are **not yet tested**.

Read-only stock binary export to `/tmp/ewb-brcmfmac-build/stock-installed/` completed
with both exact verified digests. This enables independent imported-symbol CRC
comparison without a Pi reload. No reboot fallback is implemented; this worker never reboots the Pi. A separately
authorized recovery design would be needed for that option.

No Pi trial script, manifest, timer, module reload
or reboot has been deployed or armed by this preparation.

Guard service settings are explicitly `Type=exec`, `RuntimeMaxSec=90s` and
`TimeoutStopSec=5s`; an ordinary simple service's `TimeoutStartSec` would not bound
its running duration. The freezer acknowledgment is checked even when the requested
freeze value is already one, including resumption after an interrupted owned write.
Only a zero-to-one request acquires new freeze ownership.
