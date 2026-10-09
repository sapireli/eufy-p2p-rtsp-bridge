# Cherryview shared video outage: GPU evidence

## Confirmed event

At 2026-10-09 18:35 UTC, both RTSP references and the Pi lost video. This event
was downstream of SDK delivery: the concurrent raw HTTP observer continued to
receive complete first-slice NALs for Front Door and Garage throughout 18:35:10–
18:36:05. Maximum observed raw gaps were 1.219s and 0.925s, respectively.
Garage keyframes continued during the outage. This is not a claim about every
previous pause; other captures did show upstream pauses.

The kernel records at 18:35:29 report `GPU HANG: ecode 8:4:8cffbffd` in
`ffmpeg[30611]`, a stopped-heartbeat reset of `vcs0`, and that process's context
reset. A second GPU hang/reset appears at 18:35:44. Process 30611 was Garage's
HEVC-decode/H.264-encode producer; 30613 was Front Door's producer. Kernel records
are in `.evidence/shared-outage-go2rtc/kernel-outage.txt`; the complete first
error capture is `.evidence/bridge-capacity-2026-10-09/i915-error-183529.txt`.
Bridge PID 30559 and go2rtc PID 30575 remained running. Ethernet negotiated 1Gb/s
full duplex. The ordinary raw transport capture had no observed sequence holes.

## Where the GPU stopped

The first i915 error record identifies the active video-engine context as
Garage's ffmpeg 30611, with Front Door 30613 also queued. Its compressed batch was
decoded by ASCII85-decoding each captured dword, converting big-endian decoded
words to little-endian byte order, then zlib-decompressing. The resulting 524288
bytes are retained privately as `hung-batch.bin`.

The batch ends at offset 0x838 with command 0x73a00001, followed by bitstream
length 0x649 and offset 0. Intel's primary driver defines this as
`HCP_BSD_OBJECT`: the HEVC bitstream decode submission. At 0x748,
`HCP_PIC_STATE` DW1 is 0x00b3009f. With the submitted 8-pixel minimum coding block,
this is 1280×1440, the actual Garage source size. `HCP_PIPE_MODE_SELECT` DW1 is 0,
the decode path. These are HCP decode commands, rather than the MFX AVC encoder
commands. This locates the captured hung workload in Garage HEVC decode; it does
not yet prove which particular bitstream or driver defect triggered the hang.

Primary definitions and command emission:

- [Intel driver command definitions](https://github.com/intel/intel-vaapi-driver/blob/master/src/i965_defines.h).
- [Intel HEVC HCP decoder](https://github.com/intel/intel-vaapi-driver/blob/master/src/gen9_mfd.c).
- [Intel CHV HEVC support commit](https://chromium.googlesource.com/chromiumos/third_party/libva-intel-driver/+/refs/tags/upstream/skl-beta), which explicitly reuses the SKL HEVC pipeline.

## Downstream timeout and recovery effects

Exact deployed go2rtc version: 1.9.14, b5948cf. Its passive FFmpeg publisher has a
15-second incoming-data deadline. Producer read timeouts at 18:35:29.593 and
18:35:29.627 are consistent with last complete publisher data near 18:35:14.6,
matching reference video stopping around 18:35:15.

Its ordinary RTSP consumer fanout enqueues packets nonblockingly into independent
4096-packet sender queues and increments a drop counter when full; each consumer
writes in its own goroutine. Ordinary slow reference consumers therefore do not
synchronously block producer fanout through this path. This does not exclude an
unidentified go2rtc bug, but no such bug is established by this outage.

- [Version 1.9.14 sender implementation](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/core/track.go).
- [Version 1.9.14 RTSP deadlines](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/rtsp/conn.go).

After recovery, existing reference connections saw discontinuous timestamps:
Garage 4.563856→16049.434178 seconds, Front Door 5.051311→23564.002378 seconds.
Their duration-limited observers consequently exited. These jumps are a secondary
recovery boundary; they are not measurements of hours of buffered video and do
not explain the initial GPU starvation. Pi parser/watchdog recovery is recorded
in `linux-rtp-packet-size-2026-10-09.md`.

## Exact platform and remaining isolation

Host 192.168.23.158: Atom x5-Z8350, Cherryview PCI 22b0 revision 36;
kernel 6.12.111+deb13-amd64; FFmpeg 7.1.5; libva 2.22.0-3;
`i965-va-driver-shaders` 2.4.1-2, reporting driver 2.4.1. `vainfo` advertises
HEVCMain VLD and H.264 decode/encode. Intel's driver implements CHV HEVC decode;
its existence must not be dismissed as universally unsupported hybrid decoding.
[Intel's platform support description](https://community.intel.com/t5/Graphics/Problem-with-HEVC-4K-8-bit-playback-with-Atom-x7-Z8700-Cherry/td-p/407470/highlight/true/page/2)
distinguishes hybrid A-step from fixed-function B-step HEVC. The newer
[iHD driver platform list](https://github.com/intel/media-driver) does not list
Cherryview, so switching to iHD is not a supported remedy for this host.

No full raw payload of the exact failing 18:35 frame was retained. Driver/bitstream
root-cause isolation still requires a reproducible captured stream and controlled
HEVC-decode-only versus encode-only versus combined/concurrent tests. Additional
GPU work alongside production would risk another shared reset and is not yet
performed. Hardware-first/software-fallback policy remains unchanged; no added
playout buffer, blanket hardware disable, or driver replacement was deployed.

## Controlled isolation, no speculative repair

With approval, the exact original bridge configuration and SDK were left intact
while the bridge service was stopped at 18:46:09.877 UTC and started again at
18:48:16.796 UTC. An independent 210-second service-start guard was armed before
stopping and cancelled after normal restoration. No extra GPU workload ran
alongside the production transcoders. Both Fire TV sessions recovered automatically and resumed hardware rendering,
with zero queued encoded units in the captured counters. The parent separately
checks Linux recovery.

A fresh, independently software-decoded Garage capture was replayed at 15 fps:
631 frames, 6,229,394 bytes, SHA-256
`6449c1ab6f35ebf788ab351b978e441edcdfc8c42b68e8adfe2b00cc01cee121`.
This is an 18:43 capture, **not the failing 18:35 input**. Three serial paths used
that identical input, with a 55-second timeout per test:

| Path | Frames | Wall time | Result |
| --- | ---: | ---: | --- |
| VAAPI HEVC decode → hwdownload → null | 631/631 | 42.30 s | exit 0 |
| CPU HEVC decode → hwupload → VAAPI H.264 encode → null | 631/631 | 42.38 s | exit 0 |
| VAAPI HEVC decode → scale_vaapi → VAAPI H.264 encode → null | 631/631 | 42.15 s | exit 0 |

All reported zero duplicated/dropped frames. Kernel journal contained no new
entries during the test. Full commands, stderr, progress/frame counts, and exact
intervals are archived under `.evidence/shared-outage-go2rtc/`, especially
`isolate.py` and `results.json`. The replay's nominal 15 fps pacing is diagnostic
only; no pacing or waiting was introduced into production.

These tests establish that the host can run all three paths on this valid sample.
They do **not** reproduce or fix the hang, exclude longer concurrent workload
failures, or establish that the missing failing frame was valid. Capturing the
actual next event's full raw input is the next necessary evidence step.

The [FFmpeg 7.1.5 HEVC decoder](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavcodec/hevc/hevcdec.c)
returns to its hardware decoder callback before the software WPP/slice-thread
executor. Therefore the current slice-only setting does not run that software
slice executor concurrently against VAAPI. This source boundary weakens an
unsupported theory that the earlier slice-thread option alone causes the hang;
it does not prove all VAAPI threading code safe.
