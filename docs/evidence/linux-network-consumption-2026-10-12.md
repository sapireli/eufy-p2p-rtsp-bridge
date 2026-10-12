# GaragePi receive backlog and CPU boundary — 2026-10-12

## Read-only 30-second observation

The Pi was externally ON with `eufy-wall` active, MainPID 23349, and disabled for
automatic boot. These states remained unchanged. No service, power, network,
camera quality, decoder or renderer configuration changed. Seven sparse samples
covered 29.737 seconds between their monotonic timestamps; controller completion
took 32.364 seconds including the final request and cleanup.

| Measurement | Observed result |
| --- | --- |
| CPU user / system / softIRQ | 32.98% / 17.15% / 8.22% |
| CPU idle / iowait | 41.54% / 0.10% |
| Five-second busy CPU | 41.8%, 78.0%, 81.9%, 54.3%, 50.6%, 41.1% |
| WLAN receive | 17,321,072 bytes; 4.660 Mbps over the sampled span |
| WLAN transmit | 165,388 bytes |
| WLAN interface drop-counter changes | zero RX and TX |
| Largest simultaneous RTSP socket receive queues | 534,827 and 80,646 bytes |
| Final second-socket receive queue | 151,202 bytes |
| Clock API requests | 6 succeeded; 1 failed |
| Successful clock-request elapsed time | 65.0–3138.1 ms |

`CLK_TCK` was verified as 100. CPU percentages use the first eight `/proc/stat`
fields, avoiding double-counting guest time. One persistent GStreamer process
used 19.57% of one core and Go used 1.31%; another GStreamer process joined during
the window, so its complete-window CPU comparison is not meaningful. This was a
natural changing live workload, not a steady two-camera capacity experiment.

The observer itself used 1.091 seconds CPU plus 1.081 seconds child CPU: **6.71%
of one core over controller elapsed time**. It was not neutral. Process and
network sampling were sparse, but Python parsing and incremental journal reads
still have material cost on this Pi. No per-frame logs or raw pixel copies were
enabled. The WLAN count includes other interface traffic and observation traffic;
it is not an isolated camera bitrate or maximum WiFi capacity measurement.

The clock-request measurement includes DNS, Python processing and scheduling as
well as HTTP transport. A one-second socket timeout did not bound total elapsed
request time. Therefore 3138 ms must not be described as a pure network RTT.

## Exact picture identity and age

All listed valid identities belong to bridge epoch `0cc9875de09a3126`.
KMS-return scope is output-call completion, **not physical scanout** or calibrated
camera-capture age.

| Camera / frame ID | KMS-return ingress-age lower–upper bound |
| --- | --- |
| Front Door 426113 | 161.7–265.4 ms |
| Front Door 426601 | 1220.6–1243.0 ms |
| Front Door 427038 | 147.4–175.3 ms |
| Front Door 427172 | 1978.6–2445.1 ms |
| Garage 426891 | 1504.4–1571.1 ms |
| Garage 427046 | 667.4–696.0 ms |

Garage frame 427076 had an invalid clock mapping; zero age fields are not proof
of freshness. No pipeline ERROR appeared in the incremental observation window.
Private metadata is retained in
`.evidence/frame-freshness-2026-10-12/linux-network-cpu-readonly30.jsonl`.

## What the socket observation establishes

Positive kernel receive-queue sizes prove some TCP bytes had reached the Pi and
had not yet been consumed by the application at those snapshots. They do not
prove that every delayed frame was already present, or identify the blocking
thread. Queues later drained to zero, so this is not proof of continuously held
data. Average CPU below saturation does not exclude shorter scheduling stalls;
interface drops of zero do not rule out WiFi retransmissions or slow delivery.

## Pinned GStreamer threading audit

In GStreamer 1.26.2 the RTSP interleaved receive loop handles each TCP message and
synchronously chains its RTP payload into `rtpbin`. The RTP jitterbuffer already
has a separate output task. It releases its internal lock before pushing packets
toward depayloading, parsing and hardware decoder input. Thus an ordinary KMS
wait does not immediately imply the TCP reader is blocked in the same call.

Jitterbuffer input can wait on a full queue: its full condition requires a
sequence span of at least 32765 **and more than 10000 queued packets**. Serialized
queries can also wait for the output task. Neither condition was measured in
this observation. `buffer-mode=none` does not remove the jitterbuffer task.

The V4L2 decoder separately queues compressed input and runs a capture-output
task. Buffer-pool operations can block. Its output task finishes decoded frames
into the installed raw admission/pacing/render graph. Holding decoder buffers
downstream can therefore affect decoder progress; that is a possible path, not
the measured cause of these receive queues.

Primary sources:
[RTSP source](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-good/gst/rtsp/gstrtspsrc.c),
[jitterbuffer element](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-good/gst/rtpmanager/gstrtpjitterbuffer.c),
[jitterbuffer queue](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-good/gst/rtpmanager/rtpjitterbuffer.c),
[V4L2 decoder](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c).

## Minimum next discrimination

Before changing buffering, join sparse named-thread CPU/state/wait-channel
samples to socket receive queues. Determine whether the TCP reader is runnable,
waiting for the socket, or waiting on pipeline synchronization while bytes are
queued. This requires no media-policy change. It avoids assuming that an extra
queue before the decoder repairs a reader that is already decoupled by the
jitterbuffer task.

If downstream blocking is observed, a bounded same-source reader-only comparison
can temporarily replace the Garage decoded/render branch with a nonwaiting sink,
keeping its RTSP/depay/parser/stamp input unchanged. This separates contribution
from that downstream workload, but removes its CPU and buffer-pool demand too;
it cannot alone distinguish lock blocking from scheduling capacity. No such
experiment has been run, and it is not a production fix.
