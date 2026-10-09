# Pi RTSP processing backlog: packet-size diagnosis, 2026-10-09

## Scope and unchanged video path

Target: Raspberry Pi Model B Rev 2, ARMv6, DietPi/Raspbian, GStreamer 1.26.2.
The bridge is an Atom x5-Z8350 running go2rtc 1.9.14. Front Door and Garage use
independent hardware V4L2 H.264 decoders and KMS overlays. The displayed streams
remain Front Door 1222×1680 and Garage 1280×1440. RTSP is TCP with `latency=200`;
the sinks remain `sync=false`, using the shared DRM descriptor and `skip-vsync=true`.
The static black primary layer is unchanged.

The user reported Garage 15–20 seconds behind the Fire TV. This comparison is
between **the same camera** on different clients, not the different cameras'
independent clock overlays.

## Baseline and stage isolation

Before tuning, the bridge served the same Garage producer to the TVs and Pi.
An independent consumer measured 181 Garage RTP marker frames in 11.931 seconds,
about 15.09 fps. The Pi's own connection had TCP backpressure. Root's matched
same-camera captures at 17:43:50 UTC showed Pi Garage OSD `01:43:25PM` versus
reference Garage OSD `01:43:43PM`: the Pi was **18 seconds behind that observer**.
The PNG reference initially used output frame threading and could itself retain
sampled frames; this baseline is therefore a conservative relative-delay result,
not an exact measurement of the Pi's total backlog.
The separate same-camera comparison artifacts are retained privately by the root agent.

Tests replaced only the Pi Garage pipeline temporarily; Front Door remained active
except for the explicitly bounded single-camera comparison. No extra hardware
decoder or camera connection was added. The trace used identities before/after
decoding, with logs written to a private file rather than the journal.

| Test | Observation | Interpretation boundary |
| --- | --- | --- |
| Normal Garage, encoded and decoded identities | Warm intervals delivered roughly 5–13 fps; encoded→decoded median 243 ms, maximum 743 ms | This is not a 15–20 second queue inside the hardware decoder itself. Diagnostic logging adds some CPU cost. |
| Front Door process paused, 17:38:56–17:39:21 UTC | Garage decoded 16.21 and 17.36 fps in two consecutive warm intervals, catching up; after Door resumed, initial Garage output was about 12.12 fps | Competition on the Pi matters. This does not by itself identify CPU versus hardware capacity. |
| Actual native Garage KMS path | 548 logged DMA-BUF import calls, including per-frame cached imports; zero `frame copy` messages with `GST_PERFORMANCE` INFO enabled | The tested path imports the decoder buffers rather than copying each full frame to a KMS dumb buffer. |
| V4L2 decoder → fakesink, Door still active | 124 encoded and 124 decoded frames over about 23.2 seconds after startup, about 5.3 fps | Removing display submission did not recover the source's 15 fps. |
| Depay/parser → fakesink, no Garage decoder or display | 512 frames over 47.484 seconds after startup, 10.762 fps; process CPU about 46.1% | Receiving/RTP/depay/parser work alone is substantial. A VPU-only capacity explanation does not account for this result. |

These diagnostic intervals are stage-isolation results, not clean CPU benchmarks.
A preliminary raw packet sampler consumed about 18% additional CPU and is excluded
from performance claims. All temporary launch wrappers were removed and normal
camera playback restored before the clean A/B below.

## Clean CPU A/B

The baseline sampled `/proc/stat` and both player processes with no per-frame
diagnostics. The candidate changed only the Pi's two RTSP URLs by requesting
`?pkt_size=8192`, then restarted the wall once at **17:45:39 UTC**. The early
17:45:23 restart preceded the successful URL edit and is not the candidate boundary.

| Metric | Original packetization, 8-second sample | 8192-byte packet setting, 17:46:08–17:46:20 UTC |
| --- | ---: | ---: |
| CPU user | 63.2% | 36.6% |
| CPU system | 25.3% | 27.8% |
| CPU softirq | 11.5% | 12.7% |
| CPU idle | 0.0% | 22.8% |
| Front Door player CPU | 41.5% | 24.1% |
| Garage player CPU | 41.6% | 28.1% |

The reduction is predominantly userspace work, consistent with processing fewer
RTP packets. Network interface/IP/TCP segmentation still uses the existing MTU;
an 8192-byte RTP packet does not require Ethernet jumbo frames.

An independent wire client verifies actual maximum RTP packet length 8192 bytes
for the tuned connection, versus 1472 normally. More importantly, simultaneous
connections reconstructed 226 complete H.264 access units at shared RTP
timestamps: all 226 coded-picture hashes matched, with zero differences after
excluding repeated per-connection SPS/PPS configuration. This demonstrates that
the change alters fragmentation, not encoded pictures. See
[the wire and matched-frame evidence](pi-garage-delay-2026-10-09.md).

Root's later comparison at 17:47:13 UTC showed Pi Garage OSD `01:47:08PM` against
reference `01:47:06PM`. The Pi was no longer 18 seconds behind; the reference
decoder can itself hold frames, so this is not a precise zero-latency claim.
At more than four minutes, the bridge's same Pi Garage consumer remained connected
with no reported sender drops. Longer continuous and final-binary verification
are recorded below once completed.

At the ten-minute checkpoint, the original tuned player PIDs 13384 and 13385 were
still running. Root's matched reference captures showed Garage Pi at
17:55:55.840 UTC displaying `01:55:36PM`, versus reference at 17:55:55.971 UTC
displaying `01:55:35PM`. Front Door Pi at 17:55:57.290 UTC and reference at
17:55:57.628 UTC both displayed `01:55:43PM`. These observations exclude a renewed
18-second lag behind that observer, but do not establish zero Pi-specific backlog:
the reference's output PNG frame threading was subsequently found to retain
sampled frames. The final gate uses single-threaded PNG output with a short
input decoder path. The camera overlay's difference from UTC had changed, so
subtracting its text from wall time would have produced a false local-delay claim.
The bridge reported no sender drops for the same Garage consumer at ten minutes.

A later clean CPU sample, 17:56:55 UTC onward, measured user 42.0%, system 28.9%,
softirq 22.9%, and idle 6.1%; Front Door used 27.7% and Garage 38.5%. Thus the
initial 22.8% idle figure is a bounded sample, not a guaranteed constant margin
under all scene bitrates. Some TCP receive queue bursts remained. These counters
are considered with matched video evidence, not used alone as a latency estimate.

## Mechanism and implementation

The original Pi receiving path exhausted the single CPU. Slow consumption created
backpressure in its TCP connection and go2rtc's per-consumer sender queue. The TVs
could consume the same producer promptly. This is a client-specific processing
backlog, not shared camera ingress delay or full-frame software copying to KMS.

go2rtc supports `pkt_size` per RTSP consumer: [the server reads the query option](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/internal/rtsp/rtsp.go#L207-L209),
and [the consumer depacketizes/repacketizes H.264 or H.265 when requested](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/rtsp/consumer.go#L164-L173).
The existing [sender queue drops new packets when full](https://github.com/AlexxIT/go2rtc/blob/v1.9.14/pkg/core/track.go);
the fix addresses the processing pressure rather than flushing that queue periodically.

The bridge advertises `rtspTcpPacketSize` in `/api/cameras` and each WebSocket
`hello` camera. The Linux client consumes the hint for generated URLs. Camera
serial identity stays separate from the slug used in the URL, so reconnects and
motion selections retain the correct capability. WebSocket snapshots also handle
a bridge that was unavailable during the initial HTTP request. The config uses
immutable hint snapshots during event updates.

Cold startup needs more than one initial snapshot: the bridge begins listening
before SDK login populates its camera registry. An early `/api/cameras` response
and WebSocket `hello` can both be empty. The bridge therefore broadcasts a fresh
full snapshot once registry/go2rtc setup is complete, before starting its warm
streams. This supplies the stream keys and capability on the existing connection.
Snapshot thumbnail reads complete before all camera states are read synchronously,
so an asynchronous thumbnail operation does not send a stale idle state after a
live event. This recovery uses the event protocol rather than client polling.
Live cold-start deployment verification is recorded below when completed.

`rtsp_packet_size` is an optional operator override: omitted means automatic,
zero disables automatic tuning, and 256–65535 explicitly requests a packet size.
Automatic hints do not change explicit tile URLs. A positive override applies
only to RTSP URLs and preserves other query options. Older bridges and invalid
hints leave URLs unchanged. Video resolution, frame rate, codec, decoder,
presentation scheduling and the 200 ms jitter allowance are not changed.

This does not prove camera ingress pauses have disappeared. Those remain a
separate measured limit; see [the earlier cadence evidence](linux-cadence-2026-10-09.md).

## Build and evidence

- `go test -race ./...`: all nine client packages passed.
- `go vet ./...`: passed.
- ARMv6 build with `GOOS=linux GOARCH=arm GOARM=6 CGO_ENABLED=0`: passed.
- Candidate SHA-256: `126c8cafa5d8b7cade31b31b4e203a1e77e85b9879595f7c93991c3541c7795e`.
- Private artifacts under `.evidence/linux-cadence-2026-10-09/`:
  `garage-lag-12275.log`, `garage-isolation-12722.log`, `garage-fakesink.log`,
  `garage-parseronly.log`, `normal-cpu-after-isolation.txt`,
  `pkt8192-corrected-cpu.txt`. Temporary Pi wrappers and packet-sampling executables
  were removed. Capture logs remain private diagnostic artifacts.

## Continuous and final deployment verification

The explicit-query A/B players stayed uninterrupted for ten minutes, as recorded
above; its latency comparison has the reference-buffering qualification stated
there. The final binary was installed at **17:59:36 UTC**, with SHA-256 matching
the candidate above. The Pi's source mirror at `/root/eufy-wall-source/client`
was updated as well. Both explicit tile `url:` lines were removed, and there is
no `rtsp_packet_size` override: the final test exercises discovery, not manual
query parameters. Parent PID 14490 started players 14508 and 14509 around
17:59:38–17:59:39 UTC; both resolved their slug URLs with `?pkt_size=8192`.

At 17:59:57 UTC the service was active, with Front Door and Garage on their
original overlays at 1222×1680 and 1280×1440; the black primary layer remained
1920×1080. Initial decoder negotiation/timestamp warnings matched the previously
documented startup behavior. A clean warm CPU sample, 18:00:16–18:00:26 UTC,
measured user 31.3%, system 24.3%, softirq 12.8%, and idle 31.7%.
The Pi subsequently rebooted independently. Current boot logs begin at a restored
fake-hardware-clock timestamp of 18:00:12 UTC, before NTP synchronization at
18:01:48; that first entry is not an exact externally observed reboot time.
At 18:09:35 UTC the parent was PID
507, with players 589 and 590 started around 18:02:03 UTC. Both automatically
recovered the 8192-byte hint after boot, kept the same hardware decoder path and
displayed dimensions, and retained the client-owned black primary layer. This
interrupts the original final-binary continuous gate; it is not evidence that
players 14508 and 14509 survived ten minutes.
The reboot cause could not be established: the Pi retained only the volatile
current-boot journal, had no `last` utility or persistent wtmp/syslog/auth log,
and its pstore was empty. The current-boot kernel log did not report undervoltage,
thermal reset or a panic. This does not exclude an event in the lost prior boot.

A clean post-reboot sample at 18:10:03–18:10:15 UTC measured user 34.5%, system
24.7%, softirq 12.5%, and idle 28.3%; Front Door used 18.8% and Garage 34.0%.
The installed SHA-256 still matched the candidate. There were no service journal
entries since 18:03 UTC. TCP receive queue bursts of about 172 KB and 510 KB
were observed, so queues alone are not treated as a latency measurement.
The final binary's uninterrupted ten-minute gate and the fresh-ready-snapshot
bridge cold-start test remain pending; the earlier tuning run does not replace
those checks. Private captures: `auto-final-tenmin.txt` (records the interrupted
gate) and `auto-final-reboot-cpu.txt`.

At **18:12:14 UTC**, Garage PID 590 triggered the existing 15-second decoded-frame
watchdog after about ten minutes of runtime. It restarted as PID 1275 at
18:12:15; Front Door PID 589 remained running. At 18:13:10 both hardware overlays
were active again at the intended dimensions and both URLs retained the automatic
8192-byte option. This is a concrete stability failure in the final observation
window; it cannot be presented as uninterrupted successful Garage playback.
Correlation with the contemporaneous source/reference capture is required to
distinguish a shared source pause from a Pi-specific decode/receive stall.
Private journal evidence: `auto-garage-restart.txt`; status:
`auto-reboot-tenmin-status.txt`.

The next bounded check at 18:16:06–18:16:17 UTC found both players unchanged and
both TCP receive queues empty at the sample endpoint. CPU idle was 37.7%
(user 26.4%, system 24.7%, softirq 11.2%). Front Door's journal also recorded
server backward-timestamp resynchronizations at 18:14:09 and 18:14:17, decoder
caps renegotiation, and one KMS buffer-pool size warning. No Pi mutation produced
these events. They require correlation with bridge/source activity and are not
silently treated as normal continuous playback. Private capture:
`post-garage-watchdog-cpu.txt`.

## Remaining acceptance checks

The measured CPU reduction and bit-exact repacketization are verified. Final
low-latency, uninterrupted playback is **not fully verified**: the corrected
single-threaded reference comparison, correlation of the Garage watchdog and
Front Door source transitions, and the deployed fresh-ready-snapshot cold-start
recovery followed by another continuous soak remain open. The current client
has no diagnostic wrappers or packet probes installed, and its source and binary
are ready for those checks without further Linux changes.
