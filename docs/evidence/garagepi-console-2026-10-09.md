# GaragePi console behind live video, 2026-10-09

## Diagnosis

The user reported terminal text behind the two live video tiles on `garagepi.lan`,
confirmed as GaragePi at `192.168.23.73`. The client was already running with the
HDMI login masked, a startup console-clear helper, and two hardware-decoded KMS
overlay planes. Its main process was PID 24414. Video overlays leave uncovered
areas of the primary Linux console visible.

The kernel command line still contained `console=tty1`. At `01:48:39` EDT, the
Broadcom Wi-Fi driver emitted a warning at
`brcmfmac/fwsignal.c:1731 brcmf_fws_rxreorder`, followed by a stack trace. Later
messages included `brcmf_inetaddr_changed: fail to get arp ip table err:-52`.
The captured `/dev/vcs1` text contained that kernel trace. Disabling getty and
clearing the console once therefore did not prevent subsequent kernel messages
from painting text underneath the video.

This establishes the source of the visible text. It does not diagnose or fix the
underlying Wi-Fi driver warning. Those diagnostics remain in the kernel journal.

## Installed client version

The installed ARMv6 executable's SHA-256 was
`5264b92c5d7c8945b2976607a79462c19e66ffbb17b6bfd1ac72c9facc1d2fc5`.
`go version -m` identified a clean build from commit
`871659ebc3d3589175127287c168840f8b91252a`, with Go 1.27.1 and `-trimpath`.
`git diff 871659ebc3d3589175127287c168840f8b91252a..a0195e4 -- client` was empty.
Thus the installed binary already contained the latest client source. A different
repository HEAD reflected later server, TV and documentation changes; this was
not an old-client regression. No client executable or playback code was replaced.

## Deployed fix

The opt-in `deploy/configure-client-console.sh` now reproduces the dedicated-display
setup rather than relying on an untracked one-off command. On GaragePi it:

- Kept `getty@tty1.service` masked.
- Installed `deploy/eufy-wall-clear-console.sh` as
  `/usr/local/libexec/eufy-wall-clear-console`.
- Set the kernel's console-printing level to zero while retaining its other printk
  settings through `/etc/sysctl.d/99-eufy-wall-console.conf`.
- Kept the root service `ExecStartPre` clear, now also suppressing kernel console
  printing before clearing the backdrop to black and hiding the cursor.
- Preserved the existing HDMI-wait drop-in and running video pipelines.

The helper writes the first printk value directly. During validation,
`dmesg --console-off` set it to the kernel's minimum of 1; writing zero provides
the intended zero console threshold and agrees with the persistent sysctl.
Neither operation removes kernel messages from the journal.

## Live verification

After setup, `/proc/sys/kernel/printk` was `0 4 1 7`. All **16,080** characters
read from `/dev/vcs1` were spaces. A uniquely labelled warning written through
`/dev/kmsg` appeared in `journalctl -k`, while the console capture remained
byte-for-byte unchanged and blank. This exercises the original failure mechanism:
a new kernel warning after the startup clear.

The service remained active with the same main PID 24414. Twelve DRM snapshots
showed **10 framebuffer changes in 11 adjacent comparisons** for each hardware
video plane (98 and 109), confirming both continued advancing during the fix.
This sample does not establish exact video frame rate or camera-to-screen latency.

Persistence was checked through the installed sysctl file and service ExecStartPre,
and both shell scripts passed syntax checks. A cold reboot was not performed;
initial boot text may appear before the display service clears the console.
The setup is opt-in because dedicated appliance console policy should not be
applied to a shared desktop automatically. Restoration steps are in the client runbook.

## Private capture provenance

Captures are in the Git-ignored `.evidence/garagepi-console-2026-10-09/`.

| File | SHA-256 |
| --- | --- |
| `installed-eufy-wall-armv6` | `5264b92c5d7c8945b2976607a79462c19e66ffbb17b6bfd1ac72c9facc1d2fc5` |
| `vcs-before.bin` | `2994e61eccf45bb6112608a5459d8dff8cab2e5d908d981d73c73967858ef967` |
| `vcs-after-clear.bin` | `e432ca2fff7ef5298a6ea7a3afea37d9be1cf82c67f92fddb1c7120e232c938a` |
| `vcs-after-warning.bin` | `e432ca2fff7ef5298a6ea7a3afea37d9be1cf82c67f92fddb1c7120e232c938a` |
| `verification-kernel-log.txt` | `28eac4eb3d5abd735cf691cec4b2dd4412a00b30eff5fc4950b992300a6d0de8` |
| `post-fix-status.txt` | `03e07b6c57b486fa6936aa25fc84e27d2867c881095a08b6b9124b7d74c36a49` |
| `drm-after-fix.txt` | `44dc4d3b8aac21615cf8d23e96e61f7718140315db780ffcb7a40d36fa26a338` |
