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

## Reader-task follow-up: useful snapshots, not a causal gate

A later 30-second read-only inventory found the generic `task0` thread once in
each GStreamer process. An initial selected-name filter omitted that generic
name and therefore did not measure the reader candidate. Its 7.27% observer CPU
cost also rejects causal performance claims from that first follow-up.

A corrected task-only observation selected TIDs 25074 and 25510 in processes
25068 and 25500. PID/TID start times remained unchanged. Both processes owned
their RTSP sockets at FD 20, matched by socket inode. However, the sampled poll
syscall supplied a pointer to a two-entry descriptor array, rather than the
descriptor values themselves. **The specific `task0` → FD 20 association remains
an inference, not a directly inspected fact.**

The corrected window took 30.091 seconds, with sampling CPU 0.993 seconds
(**3.30% of one core**, excluding one-time inventory setup). This exceeds the
desired 2% probe budget. The observation does not justify a performance or
root-cause conclusion. No later five-second overhead gate was executed; that
requirement arrived after this window had completed.

The largest corrected-window receive queue was 823,615 bytes. For socket
snapshots exceeding 100 KB, the associated generic-task snapshots were runnable
with wait channel zero 20 times, and sleeping in a polling wait channel twice.
No downstream futex wait was observed for these candidate tasks. Their CPU
increments were approximately 5.6% and 8.1% of one core, respectively.

Those statements describe snapshots only. Task state, wait channel, syscall and
socket queue were not atomic observations. One task read spanned 99.8 ms,
showing that observation itself could be delayed substantially. `R` means
runnable or running; it does not prove the thread was continuously executing.
Polling with queued bytes at a later read is not proof of a stuck socket reader.
No service, power, network or playback policy changed; all observers terminated.

Private follow-up metadata:
`linux-reader-tasks30.jsonl` (wrong selected-name filter) and
`linux-reader-only-tasks30.jsonl` (corrected generic-task selection), under the
same private evidence directory. Further measurement must first establish the
reader's socket-descriptor identity and pass a short observer-overhead gate.

## Native five-second gates: one reader directly identified

The next preflight reported service active, MainPID 23349, disabled, with camera
processes 27848 and 27849. The existing power helper reported **OFF**. No wake,
service action or network change was issued. Physical output was unobservable.

The native [reader helper](../../client/tools/rtsp_reader_probe.c) reads precisely
16 bytes of poll-descriptor metadata only when the ARM32 poll syscall reports
exactly two descriptors. It validates the syscall number, count and userspace
pointer before reading, then verifies the same syscall arguments afterward.
It does not read arbitrary buffers, video data, decoded pixels or socket payload.
Socket descriptors are matched to `/proc/net/tcp` by inode and RTSP port.

This directly established process 27849, task 27861, polling FD 20, whose inode
595362 identifies its RTSP port-8554 connection. The second generic task was not
directly identified within the bounded attempts. Process and task start ticks
remained stable.

| Bounded attempt | Elapsed | Observer CPU | Single-core observer cost | Longer capture |
| --- | --- | --- | --- | --- |
| Initial native gate | 5.0066 s | 29.142 ms | 0.582% | none |
| Second identity gate | 5.0031 s | 20.968 ms | 0.419% | automatically rejected |

The second gate passed the CPU limit but lacked both direct reader identities,
so the helper rejected its requested 30-second extension. No long native reader
capture occurred and no repeated polling followed. Native cost includes helper
inventory/setup; compilation and transfer occurred before the measured gate.

In the initial gate, the proven reader was runnable with wait channel zero when
its receive queue showed 82,142 bytes. This is a positive reader/queued-data
snapshot, not a blocking-stack diagnosis. State, poll metadata and queue reads
remain non-atomic. Poll `revents` values were not interpreted: kernel poll
completion and userspace descriptor storage can change asynchronously.

Only combined user-plus-system task ticks were retained. Across the first gate,
generic tasks 27854 and 27861 advanced 35 and 22 ticks (approximately 7.0% and
4.4% of one core). Across the second gate they advanced 48 and 29 ticks
(approximately 9.6% and 5.8%). Separate user/system splits were not captured;
these short intervals do not establish a CPU-capacity limit.

The next alternative identity check is a read/recvfrom/recvmsg syscall whose
first argument directly identifies the RTSP descriptor; it needs no memory
read. It has not been performed. A strict poll-only check can miss a busy task
that does not enter a qualifying poll snapshot during the short window.

Private native artifacts: `linux-native-reader-gate5.txt` and
`linux-native-reader-proof-rejected-extension.txt`. The helper's focused
`--self-test` rejects unsupported syscalls, descriptor counts and pointer bounds;
it passed natively and on the local compiler. The committed helper additionally
guards established connection state and subsequent socket-inode changes; these
guards do not add any playback policy.
