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

## Client version before this fix

The installed ARMv6 executable's SHA-256 was
`5264b92c5d7c8945b2976607a79462c19e66ffbb17b6bfd1ac72c9facc1d2fc5`.
`go version -m` identified a clean build from commit
`871659ebc3d3589175127287c168840f8b91252a`, with Go 1.27.1 and `-trimpath`.
`git diff 871659ebc3d3589175127287c168840f8b91252a..a0195e4 -- client` was empty.
Thus the installed binary already contained the latest client source. A different
repository HEAD reflected later server, TV and documentation changes; this was
not an old-client regression. No client executable or playback code was replaced.

## Initial console workaround (superseded)

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

## Initial verification and its limitation

After setup, `/proc/sys/kernel/printk` was `0 4 1 7`. All **16,080** characters
read from `/dev/vcs1` were spaces. A uniquely labelled warning written through
`/dev/kmsg` appeared in `journalctl -k`, while the console capture remained
byte-for-byte unchanged and blank. This exercises the original failure mechanism:
a new kernel warning after the startup clear.

The service remained active with the same main PID 24414. Twelve DRM snapshots
showed **10 framebuffer changes in 11 adjacent comparisons** for each hardware
video plane (98 and 109), confirming both continued advancing during the fix.
This sample does not establish exact video frame rate or camera-to-screen latency.

The user still saw text after boot. The VCS-only checks above were insufficient:
they verified virtual console characters, not the physical framebuffer pixels.
The initial console workaround is superseded; its deployment scripts and installed
printk/ExecStartPre changes are removed with the client-owned background fix.

## Physical framebuffer finding

A capture of `/dev/fb0` contained DietPi startup text and its login invitation.
The RGB565 image was 1920 × 1080, 4,147,200 bytes, with 12,564 nonzero bytes.
The active DRM primary plane was owned by `fbcon`; camera video occupied separate
overlay planes. Thus uncovered screen areas displayed the console framebuffer.

DietPi postboot ran from 02:14:15 to 02:14:19 EDT with `StandardOutput=tty`.
The wall client started at 02:14:17. `/boot/dietpi/postboot` prints its banner and
login invitation even with getty masked. Its late output could repaint the console
after the startup clear. Clearing VCS and suppressing kernel warnings did not
isolate the background from other console writers.

## Client-owned static background

The planes client now allocates its own mode-sized black image and assigns it to
the selected output primary plane. Hardware-decoded camera overlays remain above
it. The image is filled once and retained for the client lifetime; no background
video pipeline or repeated drawing is required. RGB565 is preferred when the
primary plane supports it, with XRGB8888 as the format fallback.

Connector, active CRTC and primary-plane IDs are discovered rather than hardcoded.
On clean shutdown, video subprocesses stop before the previous primary image is
restored and the client image released. Console output continues to its own
framebuffer, which is no longer the visible primary image while the client runs.

### Installation and console-write test

An ARMv6 candidate was installed on GaragePi. Its SHA-256 was
`4a1c3bd4e53256e4ee2a536f8617fe59c9026a6b4ffc4a58b9ba212535849832`.
DRM state showed primary plane 86 framebuffer 673 allocated by `eufy-wall`,
RGB565 1920 × 1080, pitch 3840, behind V4L2 hardware-decoded planes 98 and 109.
A privileged GETFB2 / MAP_DUMB capture of that active primary image contained
4,147,200 bytes, all zero.

The earlier console helper, ExecStartPre drop-in and persistent printk sysctl were
removed. Kernel console printing was restored to `4 4 1 7`; the existing HDMI-wait
drop-in was retained. After writing a test message to `/dev/tty1`, the console
framebuffer had 1,204 nonzero bytes while the application primary image remained
byte-identical and all black. This verifies isolation from console writes using
actual displayed framebuffer pixels, rather than virtual console characters.

`go test -race ./...`, `go vet ./...`, and CGO-disabled Linux ARMv6 and amd64 builds
passed. Tests exercise connector/primary selection, pixel format preference,
UAPI byte layouts, full black fill including padding, failure cleanup, previous
plane restoration, avoiding replacement of a newer owner, and waiting for video
subprocesses before display cleanup.

A reboot check was attempted after installation. DietPi reported that
`dbus-org.freedesktop.login1.service` failed to load, and SSH stopped responding.
Boot persistence and physical display confirmation remain pending recovery; the
console-write isolation result above was obtained before that attempt.

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
| `fb-before.raw` | `0dae93ee6dd1fe69c619f9e820255ca0078bd5248b8a27551b29b8950d3bbfe6` |
| `owned-black-primary.raw` | `06b2c5a0c01e515d009c0bfbe0e61fafb105a54da5ec621104915cd5949849e8` |
| `black-after-console-write.raw` | `06b2c5a0c01e515d009c0bfbe0e61fafb105a54da5ec621104915cd5949849e8` |
| `hidden-console-after-write.raw` | `0028fb9968592567f3fbee5ba54894c2e26b100d3473479959829ad4896fce0f` |
