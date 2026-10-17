# GaragePi three-stream backlog, 2026-10-17

## Result and limits

The installed Linux client accumulates old Garage pictures under the combined
Front Door, Garage and Wyze workload. Removing only Wyze's processing lets the
same Garage decoder and RTSP session catch up. Reintroducing Wyze consumes that
headroom again. This establishes a load-induced client backlog; it does not
establish an immutable hardware decoding limit or prove that further software
optimization is impossible.

No quality-preserving playback fix was deployed during this investigation. The
existing decoded-picture queue cannot bound work retained before decoding, and
the freeze watchdog does not detect continuously arriving old pictures.

## Deployed path

GaragePi is a Raspberry Pi Model B Rev 2, ARMv6, one CPU at 900 MHz. The live
client uses three independent `v4l2h264dec` hardware decoders and KMS planes.
Front Door is 784×1080; Garage and Wyze are 1920×1080. The Eufy bridge counters
measured approximately 15 fps for Front Door and Garage. Wyze previously
advertised 15 fps. Display rectangles do not reduce the resolution decoded.

The installed client embeds revision
`b228f2214e08f3a2597fe6c5241c6e6665e3d2b0`, with `vcs.modified=false`.
Its client source matches `main` at `feba909`; newer server/documentation commits
do not imply this client binary is stale. Binary SHA-256:
`374911d69f5385ce61c6ceece7694ce2eba872e36d2eb1eb32d1c4455a4d0112`.

The pipeline is RTSP/TCP, depayloading, parsing, stamp observation, hardware
decoding, a one-picture leaky queue, pacing and KMS. Freshness observation is
enabled, enforcement is not. Rebased presentation timestamps and KMS lateness
checks therefore do not discard all content that became old upstream.

## Controlled comparison on the installed client

Times below are GaragePi's local log time. Front Door PID 7188 and Garage PID
7190 remained unchanged throughout. Neither their sessions nor the bridge was
restarted. Wyze PID 7233 was stopped with SIGSTOP at 14:19:00, terminated and
continued at 14:19:22 so the supervisor could reconnect it with a fresh session.
The returning Wyze process was PID 7571. Configuration, quality and decoders
were unchanged.

| Phase | Five-second CPU sample | Garage decoded frame upper age |
| --- | --- | --- |
| Three streams, before intervention | 0 idle ticks / 501 total | 15.661 s at 14:18:54; 17.878 s at 14:18:59 |
| Wyze processing paused | 119 idle ticks / 500 total, 23.8% idle | 6.518 s at 14:19:09; 0.263 s at 14:19:14; 0.218 s at 14:19:20 |
| Wyze reconnected | 21 idle ticks / 500 total, 4.2% idle | 0.089 s at 14:19:45; 1.923 s at 14:19:50 |

The return phase is short and includes Wyze startup; it is not a sustained
playback pass. The recovery while Garage's own process remains intact is the
strongest evidence against a restart alone explaining the improvement.
CPU census observer cost was 0.81–0.91% of one core. Both endpoint inventories
were complete. CPU categories overlap process accounting; do not add them twice.

These ages measure the same frame from bridge ingress to the decoded observer,
with clock-mapping bounds. They do not verify sensor capture time or physical
scanout, and do not compare the independent cameras' burned-in clocks.

## Direct profiling and competing work

A separate five-second sample of the actual unmodified processes captured 256
userspace instruction-pointer samples, zero lost records, and a validated
5.000634-second CPU-clock event count. It showed distributed GStreamer/RTP,
buffer/object management and copying costs. It did not identify one runaway
function. Most sampled Garage copies ran on its RTP/depay/parser/input thread,
not its decoded-output thread. Sampling is not a byte-volume measurement.

Avahi is a variable competing load, not a proven sole cause. An earlier
pause/resume experiment, with all three playback processes retained, reduced
Garage's age from about five seconds to 0.084 seconds; after resuming Avahi it
grew to 1.570 seconds. However, another actual-client CPU census measured Avahi
at only about 2.2% while the host was saturated. Disabling discovery permanently
is not justified by these measurements.

## Rejected or excluded experiments

- Plain fakesink tests lacked KMS allocation requirements and could introduce
  full-picture copies. They are excluded from platform capacity conclusions.
- Cross-compiled allocation helpers initially exported Zig's generic `memcpy`
  and could interpose it over optimized platform functions. Their CPU and
  capacity measurements are excluded. Replacement helpers have zero defined
  dynamic exports, but were not used for the production comparison above.
- Removing `h264parse` failed negotiation for all three sources. It was not
  applied to the client.
- Shared-producer HTTP-FLV decoded Wyze but produced no decoded Eufy pictures;
  lower CPU during that failed run is not an improvement.
- A 32768-byte RTP trial did not provide a playback improvement. No packet-size
  default or deployed override was changed.
- An unconditional unused-audio sink fixed a Wyze startup test but prevented
  clean EOS on a video-only camera. That implementation was reverted.

## Evidence and next decision

Private artifacts are in `.evidence/garagepi-backlog-2026-10-17/`, with hashes in
`verified-live-manifest.json`. The production comparison is reproduced by
`garagepi-real-third-trial.py`, with its phase times, playback log and CPU
censuses. The direct profile and symbolized addresses are retained separately.

The current stack has insufficient processing headroom under the tested
three-stream workload. A proposed remedy must demonstrate lower processing
cost at equivalent delivered video, then pass sustained three-stream playback.
Restarting old sessions, hiding stale video, or increasing queues does not
establish such an improvement. No SDK change follows from this evidence.
