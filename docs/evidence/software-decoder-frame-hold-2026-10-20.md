# Software decoder threading holds future pictures

## Experiment

A real FFmpeg 9.0.1 process on macOS decoded sixty synthetic HEVC pictures at
1600×2300 and 15 fps. The fixture used `testsrc2`, libx265 ultrafast, no B frames,
one encoder frame thread, and keyframes every thirty pictures. Complete access
units were passed through the production `newVideoMatroskaMuxer` and written
at 15 fps with their source presentation timestamps. No live camera, SDK
session, GPU, or bridge deployment was involved.

Each decoder emitted `wrapped_avframe` packets to the null muxer with packet
PTS statistics. The measurement joined each output packet to its exact input
write by PTS. This measures input-write to decoded-output availability, not
camera capture or physical display latency. All variants decoded all sixty
pictures. The steady comparison uses pictures 10–39, excluding startup and EOF
drain. Before closing stdin, the producer waited an additional 300 ms and
counted pictures still held.

| Input decoder threading | Median / maximum steady delay | Pictures held before EOF | Unpaced throughput |
| --- | --- | --- | --- |
| Default | 1000.332 / 1003.156 ms | 15 | 238.02 fps |
| `-threads:v 2` | 66.949 / 68.948 ms | 1 | 168.21 fps |
| `-threads:v 1` | 11.082 / 14.923 ms | 0 | 95.19 fps |
| `-thread_type:v slice`, automatic thread count | 4.473 / 6.692 ms | 0 | 218.87 fps |

Unpaced throughput includes process startup and shutdown for sixty pictures;
it is a short comparison, not a sustained capacity benchmark. The slice variant
used 1.28 CPU seconds during the paced run versus 0.77 for default threading.
Its reduced delay does not imply lower CPU utilization.

## Change

Both software-decode/hardware-encode and software-decode/software-encode
attempts now set `-thread_type:v slice` before the input. The hardware decode
attempt already used this mode. Thread count remains automatic, preserving
available intra-frame parallelism. Hardware-first attempt order, software
fallback, resolution, and encoder settings are unchanged.

Argument regressions verify the setting belongs to the decoder input in both
fallback branches, including explicit per-camera software-decode preference.
They also verify automatic worker count and existing encoder priority remain
intact. The focused FFmpeg/fresh-egress/real Matroska gate passed 39 tests;
the full server suite passed 255 tests on Node 26.8.1. Logs are
`/tmp/software-slice-focused.log` and `/tmp/software-slice-full.log`.

## Scope and artifacts

This proves future-frame holding in the tested software decoder and its removal
with slice threading on this fixture. It does not establish real-camera slice/WPP
layout, Atom throughput, GPU-reset resolution, or smooth live playback on the
bridge. Those require the controlled live trial.

Local artifacts are in `/tmp/hevc-thread-hold-trial/`: `run.mjs`,
`run-slice.mjs`, `input.hevc`, `input-stats.txt`, and `result.json`. The input is
synthetic video. The scripts include the exact FFmpeg arguments, production
muxer import, per-frame timestamps, and output joins. No image of a live camera
is retained in this experiment.
