# Broadcom RX reorder warning: source audit and offline invariant proof

## Observed boundary

The separate device agent reported kernel `6.18.50+rpt-rpi-v6`, Debian package
`linux-image-6.18.50+rpt-rpi-v6`, warning build `1:6.18.50-1+rpt1`, BCM43143 USB,
and `brcmfmac` module srcversion `94DA3F3023073BA79C36394`. Its driver-reported
firmware version was `01-32bd010e`. The agent observed 59 warning events over
7.6505 seconds, yielding 1,794 journal entries. The callsite was
`fwsignal.c:1731 brcmf_fws_rxreorder`. A different 5.084-second CPU census was near
100% of the single core: two players about 66%, journald about 18.7%, kernel workers
about 10%, and the controller about 2%. These are separate observation windows;
this source audit does not retrospectively assign exact CPU cost to each warning.
No device/module/firmware/kernel changes were made for this audit.

## Exact source condition

The [official Raspberry Pi Linux 6.18.50 source](https://github.com/raspberrypi/linux/blob/7cfc41f8e80f11ffa8382ed1a505154ceffb79c7/drivers/net/wireless/broadcom/brcm80211/brcmfmac/fwsignal.c#L1643-L1755)
is byte-identical to official stable `v6.18.50` fwsignal.c (SHA-256
`e7de3c68cc4af9b4f26cb48dcd326403be77bf2fb1f2ba7d49ddc6c9791e32cd`).
The exact installed binary-to-source patch manifest was not reconstructed; the
version and warning line match this audited source.

NEW_HOLE firmware metadata requests a full ring flush. The driver removes every
occupied slot and subtracts the number actually removed from `pend_pkts`, then
warns if that count is still nonzero. Therefore the observed callsite establishes
an inconsistent pending count after a full ring flush. It does not itself establish
RF loss, bad firmware, or a codec/parser defect.

Two adjacent paths replace an already occupied slot: they free the old skb,
install the replacement, and increment `pend_pkts` without first decrementing for
the freed packet. One occupied slot can consequently count as two. A later full
flush removes one skb and leaves a phantom pending count, triggering that warning.
This is a reproducible driver accounting defect. Whether those replacement paths
caused the particular live warning storm remains unmeasured; no live reorder
metadata was captured.

## Actual-function regression

`server/tools/diagnostics/test-brcmf-rxreorder.py FILE` verifies the entire official
source SHA before extracting the unchanged two reorder functions. It compiles and
executes those functions with synthetic skb/queue/firmware-header fixtures. It does
not reimplement the reorder algorithm. For each of the two occupied-slot branches,
the base reports one warning, delivers three packets and frees one replaced packet.
An offline candidate adding the missing count decrement reports zero warnings
while preserving those delivered/freed counts. A nonduplicate control has zero
warnings, four delivered packets and no replaced packet for both variants.
A 16-replacement burst reproduces the same warning and preserves three delivered
packets / 16 freed replacements after the accounting change. All cases passed
with `cc -std=c11 -Wall -Wextra -Werror`.

This is offline algorithm proof, not a deployed kernel repair, firmware validation,
or demonstrated improvement in playback. The harness mocks networking and memory
allocation but executes the official condition and counter logic unchanged.

## Upgrade and firmware limits

Current [Raspberry Pi 6.18.56 branch checkpoint](https://github.com/raspberrypi/linux/blob/80d4218cb877ccb07d09a9f21f0678e78d8b36f5/drivers/net/wireless/broadcom/brcm80211/brcmfmac/fwsignal.c)
has the same entire file SHA. The [audited upstream checkpoint](https://github.com/torvalds/linux/blob/24a19b953e2a3d0ea5a8c4951a0ab8f32720c907/drivers/net/wireless/broadcom/brcm80211/brcmfmac/fwsignal.c)
still contains both increment paths. Recent file history includes an allocation
layout cleanup, not a fix for this pending-count behavior. No existing primary-source
fix or verified compatible firmware update was found in this focused audit; this
is not proof none exists. A blanket newer-kernel or firmware recommendation is
unsupported by these results.

The firmware supplies flow/window/slot metadata, while the host owns the packet
count. Duplicate/reused slot metadata can expose the host accounting defect;
that does not imply the firmware's metadata is invalid. A root-cause repair should
preserve that count invariant and actual packet handling, rather than merely rate
limit or suppress the warning. Any live kernel trial needs an isolated build,
rollback and matched warning/CPU/playback proof, separately authorized.

## Unapplied candidate and trial prerequisites

`server/patches/brcmfmac-v6.18.50-rxreorder-count.patch` adds only the two missing
pending-count decrements. It is unapplied to any running kernel or installed
module. The candidate assumes a clean initial invariant; it does not repair an
already-corrupt ring in memory. The harness starts with clean driver state.
A complete driver audit must separately cover other slot writes, bounds, window
changes and concurrency before treating this as a production-ready repair.

A network-independent rollback should be armed locally before any authorized
module trial, since unloading USB Wi-Fi removes the SSH path. Keep the packaged
module file unchanged; store a separately built candidate and the exact original
module hash. Require matching architecture, kernel release/vermagic, modversions
and build provenance; do not force-load a mismatched module. A local timer worker
can unload the candidate and load the unchanged packaged module without network
access, with a bounded reboot fallback if module restoration fails. Confirm the
worker is armed and preserve normal boot's original module path. The parent owns
any actual build, trial and rollback; no such operation was performed here.
