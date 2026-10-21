# Retained Cherryview GPU batch: HEVC decode operation

## Result and scope

The retained `vcs0` dump contains a captured batch configured for **HEVC decode**.
It is not an AVC encoder batch. This identifies the operation in that submitted
batch, not the faulty instruction, camera, or exact driver defect. It is the
retained October 18 error, not a newly captured October 21 request.

Source artifact on bridge:
`/var/lib/eufy-wall-bridge/gpu-context-stop-20261021/before.error`.
Private local copy: `/tmp/i915-before-private.error`.
SHA-256: `78c1c3c0abdcc69f53814a29b0b3015b15adfa34ba2c4e84ea001d23c0ae6d7f`.

The dump reports `Time: 1792353868 s 220831 us`, or
**2026-10-18 20:04:28.220831 UTC**, kernel `6.12.113+deb13-amd64`, platform
CHERRYVIEW, PCI `0x22b0`, and error `8:4:8cffbffd`.

## Attribution to the captured engine batch

The `vcs0 command stream` reports `hung: 1` and a batch range
`[0x01d5b000, 0x01ddb000)`. Its dumped `vcs0 --- batch` starts at the same
`0x01d5b000` address. `BBADDR` is `0x01d5b848`, inside that allocation.
Thus the operation was found in the batch attached to the reported hung engine,
rather than in an unrelated process buffer.

The process name is empty and PID is zero. ELSP lists requests
`11f:00000008+` and `104:000c1f1c+`; the context timeline sequence is 5. The dump
also labels its active-context metadata `guilty 0 active 0`. These fields do not
identify a camera or permit confident attribution to one request among the
listed execution ports.

At byte offset `0x10`, the decoded batch contains:

| Field | Value | Interpretation from Intel driver source |
| --- | --- | --- |
| Command | `0x73800002` | `HCP_PIPE_MODE_SELECT`, four dwords |
| Following mode word | `0x00000000` | HEVC codec selector zero, decode selector zero |

The 524,288-byte allocation contains one HCP pipe-mode command and no
`MFX_PIPE_MODE_SELECT` candidate for the AVC encoder. A bounded opcode scan found
no `MI_BATCH_BUFFER_START` candidates in this allocation. This is not a full
command disassembly or a proof that no external/chained work existed. The word
at `BBADDR - batch_base = 0x848` is zero; the dump therefore does not establish
the exact faulting decode command. `IPEHR: 0x13004002` is recorded separately
and must not be called the HCP pipe-mode instruction.

## Reproduction without exposing raw buffers

Metadata-only extraction script: [i915-extract-batch-metadata.py](scripts/i915-extract-batch-metadata.py).
SHA-256: `498d29dbc3a22062ccdc5053d99fcf44490c9a30a0b35a2c6a1307ee36836309`.

```sh
rtk proxy python3 docs/evidence/scripts/i915-extract-batch-metadata.py /tmp/i915-before-private.error
```

The script decodes the kernel's ASCII85 words, reverses each decoded four-byte
word to restore little-endian storage, then decompresses the zlib stream. It
prints section sizes and command metadata only, and does not persist raw buffers.
Decoded section sizes are status 4096, ring 16384, context 8192, and batch 524288
bytes. The earlier exploratory raw extracts remain private and must not be
committed.

Summary metadata: `/tmp/i915-before-batch-operation-analysis.json`, SHA-256
`2c1f026032e5180d7dbb9073ecdc892694c150f8a7d36ca5359ba9c984c0e603`.

## Primary source interpretation

- [Intel i965 2.4.1 command definitions](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/i965_defines.h): `HCP_PIPE_MODE_SELECT`, `HCP_CODEC_HEVC = 0`, and `HCP_CODEC_SELECT_DECODE = 0`.
- [Intel HEVC decode pipe setup](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/gen9_mfd.c#L190-L215): emits the observed four-dword mode selection with codec and decode bits.
- [Intel AVC encoder pipe setup](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/i965_avc_encoder.c#L9275-L9314): uses the distinct MFX pipe command with encoding mode enabled.
- [Linux error dump emission](https://github.com/torvalds/linux/blob/v6.12/drivers/gpu/drm/i915/i915_gpu_error.c#L497-L534): prints the engine's named batch range and BBADDR; its VMA printer encodes 32-bit words with ASCII85.

This evidence strengthens the case for isolating hardware HEVC decoding while
retaining hardware AVC encoding. It does not establish that every later hang has
the same request origin or that a mixed path will meet the sustained latency gate.

## Fresh fault, October 21

After backing up and clearing the retained October 18 error, the kernel captured
a new fault at **2026-10-21 00:18:37.713214 UTC**, logged as a GPU hang/reset at
00:18:38. The new dump SHA-256 is
`446a0a842bd046d70eee6e5643ca1e4fc45ccfed149c73221361c800aa482b71`.
The same metadata-only script reproduces this dump's HEVC decode mode and
picture state:

| Field | Value |
| --- | --- |
| Batch base / BBADDR | `0x0132c000` / `0x0132c848` |
| HCP pipe-mode command / mode word | `0x73800002` / `0x00000000` |
| HCP_PIC_STATE offset | `0x748` |
| Picture dimension word | `0x011f00c7` |
| Coding-block word | `0x000008c8` |
| Derived coded dimensions | **1600×2304**, minimum coding block 8 |
| Depth word | `0x00772400`, 8-bit luma/chroma |

The current native streams are Garage Doorbell HEVC 1600×2304 coded (1600×2300
display), Balcony HEVC 1920×1080/1280×720, and Front Door H.264. Thus this new
captured decode batch matches Garage Doorbell's unique current codec and shape.
The dump still reports PID zero and does not identify the exact faulty opcode.
This is stronger camera-path attribution than the earlier error code alone;
it does not establish whether malformed input, driver command generation,
hardware execution or cancellation triggered the hang.

The HCP picture dimension/coding-block/depth interpretation follows
[Intel's picture-state emitter](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/gen9_mfd.c#L531-L575).
The older dump's same fields decode to 1920×2160, 8-bit: Garage Doorbell is not
necessary for this host's historical HEVC-decode hangs.

In this current window the bridge remained the same process, PID 140491.
Garage Doorbell's upstream six-second silence triggered a feed restart at
00:18:20; Balcony's encoder reached its backlog bound at 00:18:27, Garage
Doorbell at 00:18:29 and Front Door at 00:18:37. All are consistent with shared
engine starvation before the driver's eventual heartbeat reset. The timing
alone does not prove that the upstream silence or the restart caused the hang.
The earlier healthy SIGTERM experiment did not reproduce it.
