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

Two adjacent paths identified in the initial audit replace an already occupied slot: they free the old skb,
install the replacement, and increment `pend_pkts` without first decrementing for
the freed packet. One occupied slot can consequently count as two. A later full
flush removes one skb and leaves a phantom pending count, triggering that warning.
This is a reproducible driver accounting defect. Whether those replacement paths
caused the particular live warning storm remains unmeasured; no live reorder
metadata was captured.

## Initial actual-function regression

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

The original `server/patches/brcmfmac-v6.18.50-rxreorder-count.patch` added only the
two missing pending-count decrements. The independently reviewed expanded candidate
now also guards the third occupied-slot overwrite described below. It is unapplied
to any running kernel or installed module. The candidate assumes a clean initial invariant; it does not repair an
already-corrupt ring in memory. The harness starts with clean driver state.
The independent review below covers all slot writes and window/flush fixtures;
trusted firmware bounds and transport concurrency remain separate contracts.

A network-independent rollback should be armed locally before any authorized
module trial, since unloading USB Wi-Fi removes the SSH path. Keep the packaged
module file unchanged; store a separately built candidate and the exact original
module hash. Require matching architecture, kernel release/vermagic, modversions
and build provenance; do not force-load a mismatched module. A local timer worker
can unload the candidate and load the unchanged packaged module without network
access, with a bounded reboot fallback if module restoration fails. Confirm the
worker is armed and preserve normal boot's original module path. The parent owns
any actual build, trial and rollback; no such operation was performed here.


## Independent full-slot review and expanded candidate (2026-10-13)

The extended host-only actual-function harness applies the **committed patch file** to a private temporary copy of the exact SHA-verified source. It asserts the entire patched file contains exactly the two decrements plus the third occupied-slot replacement guard described below before extracting the official functions. This prevents a synthesized candidate from silently differing from the reviewed patch. No device, module, firmware, driver setting, or kernel build was changed for this review.

For baseline and candidate, eleven ordered fixtures exercise: expected-index delivery; NEW_HOLE full flush; both-moved direct delivery and storage; explicit window movement; explicit FLUSH_ALL; ring wrap; both duplicate replacement paths; invalid flags/no-flow deletion/window shrink; and both-moved FLUSH_ALL. A separate two-flow deletion fixture checks isolated flow counters and exact delivery order. A 320-replacement burst exceeds the u8 count range while repeatedly replacing one occupied slot.

After each synthetic packet, the harness counts occupied slots and compares that value with `pend_pkts`. Both patched duplicate paths maintain the invariant with no underflow, preserve exact delivered packet IDs/order and free the same replaced packet. All eleven regular fixture delivery orders match the baseline, and the candidate retains the invariant throughout the 320-replacement burst. These are metadata packet IDs, not camera pixels or payload output.

### Third overwrite found by review and repaired in the expanded candidate

The independent review found another `pktslots[cur_idx] = pkt; pend_pkts++` in the CURIDX_VALID **both moved** branch (official line1806). It has no occupied-slot guard. The actual-function counterexample is:

| Step | Firmware metadata (max_idx7) | Actual state/action |
| --- | --- | --- |
| 1 | NEW_HOLE, cur4, exp1 | Packet1 occupies slot4; pending1 |
| 2 | CURIDX_VALID, cur4, exp3 | Flushes slots1–2, not slot4; packet2 overwrites packet1 without freeing it; pending2 but occupancy1 |
| 3 | NEW_HOLE, cur6, exp6 | Flushes packet2, leaves a phantom count, triggers WARN_ON, stores packet3 |
| 4 | DEL_FLOW | Delivers packets3 and4; flow deleted |

Both baseline and the original two-line candidate reproduced this. Overall delivered packet order is2,3,4, with zero replaced-packet frees: packet1 was lost from the driver's only slot pointer. This is an inherited unguarded overwrite, not a regression introduced by the two decrements. Every numeric field is in range. Whether actual firmware is permitted to reuse an occupied `cur_idx` while advancing `exp_idx` has not been established; no live metadata witness demonstrates this sequence. The fixture proves the code's response, not that this was the observed warning-storm path.

The expanded patch gives this third storage path the same occupied-slot replacement semantics as the existing two paths: free the prior skb, decrement its count, clear its slot, then store/count the new skb. The actual patched function now frees packet1 exactly once, retains occupancy=count after every transition, triggers no warning, and delivers the same IDs2,3,4 in the same order. A partial-flush fixture with another occupied slot proves that range delivery remains ordered while only the overwritten destination is freed; a full-flush-to-storage fixture proves already-delivered packets are not freed twice. Neither test alters packet payloads or queue rules.

### Remaining boundaries

- **All slot writes:** NEW_HOLE first flushes the previous ring; the two guarded duplicate writes are repaired locally. Both-moved storage still has the overwrite above. Ring removal nulls slots and subtracts the actual number removed. DEL_FLOW uses a full ring flush before freeing the flow. The expanded patch changes no queue, index, flag branch, or delivery call; its added pointer/free action removes only the overwritten skb that was previously leaked.
- **Bounds/window changes:** The function trusts firmware indices. NEW_HOLE can change `max_idx` without resizing an existing allocation. The shrink fixture is safe because the prior ring is fully flushed and the original allocation is larger. Growth beyond original capacity and out-of-range indices are not made safe by this accounting patch and were not executed as undefined-memory tests. The u8 pending count cannot represent256 occupied slots; the firmware window contract is not established by this host-only harness.
- **Fresh initial state:** A decrement is safe when a nonnull slot is counted, as checked after every tested transition. The patch cannot heal an already-corrupt count (for example, occupied slot with count0); deployment must start with new driver state rather than patching an existing ring in place.
- **Teardown:** Firmware DEL_FLOW is tested, including deletion of an absent flow. In the reviewed official USB path, `brcmf_usb_down` cancels URBs before transport teardown. Core/protocol detach and `brcmf_fws_detach` do not directly iterate the RX reorder slots in the inspected files. Complete device/interface teardown leak safety is not proved by these function fixtures.
- **Concurrency:** USB completion calls `brcmf_rx_frame` then the protocol reorder function directly. The reviewed reorder code has no local lock around slot/count changes. A single-thread fixture does not prove completion serialization or SMP/teardown race safety. The patch introduces no lock or lifecycle change; any concurrent-state guarantee must come from the transport/kernel contract, not these tests.

The expanded candidate preserves the count/free invariant for all three occupied-slot replacement branches in clean serialized state, with exact packet-order tests and no new underflow witnessed. It is suitable to proceed to a **bounded, rollback-protected trial after the parent verifies exact build/module provenance**, within the existing trusted-firmware and transport-serialization contract. This is not a new bounds/concurrency hardening patch, a general driver memory-safety proof, or a production playback improvement claim. No device trial was performed here.

Reviewed expanded patch SHA-256: `944a5ad5b080f283999e20b564402d3c372991fc6eb2d05defb92a71ea1a1b04`. The patch's unified-diff context correctly contains a context-marker space followed by the original source tabs; applying it preserves source indentation.
