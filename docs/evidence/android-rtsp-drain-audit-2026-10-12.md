# Android RTSP drain / decoder interaction audit — 2026-10-12

No playback, buffering, transport, priority or device policy changed. This audit
uses the actual vendored library and current direct-decoder adapter, plus two
bounded local negative controls. It is not a new live latency fix.

## Actual steady-state path

Each camera owns a separate RTSP IO thread. `RtspClient.readRtpData` reads a header
and payload, assembles an AVC NAL, and calls the listener synchronously.
`RtspProcessor.receiveVideo` checks keyframe status over at most the first1 KB,
constructs a VideoFrame, offers it to `VideoFrameQueue(60)`, then calls the app's
data listener. The app listener performs two AtomicLong updates; DEBUG formatting
is off. Full-NAL debug scanning and experimental SPS rewriting are also off.

`FrameQueue.push` uses `ArrayBlockingQueue.offer(...,5ms)`. A full queue can wait
nominally5 ms, plus OS scheduling, then returns false. The caller currently does
not report that rejection. At15 fps capacity60 could hold aboutfour seconds of
compressed input; capacity is not observed occupancy or proof of a live backlog.

The consumer's `pop` releases the queue lock before `DrainingVideoDecoder` calls
MediaCodec input/output, copies bytes into the codec input, parses the SEI stamp,
or schedules surface output. Codec configuration/start and CSD parsing run on the
video decoder thread after its start, not in the steady RTP callback. The stamp
map lock is shared by decoder/UI, not acquired by the RTSP IO callback. There is
no decoder pool wait or codec operation under the producer queue lock on this path.

Hardware surface/output blocking could eventually fill the input queue; this is
a possible indirect relationship, not the observed cause of the documented late
frames. Those real outlier records showed listener elapsed at mostabout1.5 ms,
small input queues0–3, parser elapsed at most10.5 ms, and hundreds of milliseconds
in header/payload read calls. These measurements do not match a queue-full or
expensive synchronous decoder callback causing those particular stalls.

## Concrete synchronous output-write path

RTSP keepalive runs on the same IO thread after payload read. It calls
`sendGetParameterCommand` or `sendOptionsCommand`, which writes to a
BufferedOutputStream and flushes it synchronously. The configured Socket timeout
is a read timeout; it does not bound that output write. A blocked transport write
can therefore stop this thread's next read. The code does not synchronously wait
for a keepalive response; the header resynchronizer handles interleaved responses.

This is a genuine code path worth distinguishing, not proof it happened live.
The existing per-RTP parser elapsed includes time from payload completion through
keepalive and dispatch. Its small value on the long-read witnesses argues against
hundreds of milliseconds in keepalive writes for those frames. Previously sampled
client TX queues were also generally zero. No keepalive-thread rewrite was made.

## Bounded tests and their limits

`RtspBackpressureAuditTest` adds two controls:

- After a consumer pops a frame and pauses in simulated codec work, the producer
  can fill the queue, then complete a full-queue offer without waiting for that
  consumer work to finish. The worker calls have a one-second test deadline.
  This proves queue handoff independence, not real MediaCodec timing.
- The actual private RTSP command sender is invoked with a BufferedOutputStream
  whose underlying transport write is deliberately gated. The calling thread
  cannot complete until the gate opens; exact OPTIONS request bytes are verified
  afterward. This proves synchronous transport-write behavior without claiming
  real socket congestion or a live blocked keepalive.

All44 app+14 library tests passed (58 total); the new library tests completed in a
short local run. Runtime source and APK bytes did not change, so no device install
or additional live trace was needed for this audit.

## Boundary

The audit found no observed application lock/codec path that explains the measured
500–800 ms complete-AU receipt delay. The remaining live distinction is transport
read blocking versus receiver descheduling, with keepalive output writes as a
separate measurable possibility. Changing queue size, thread priority, or protocol
without a corresponding late-event witness would not be a demonstrated root fix.
See `fire-tv-rtp-read-stage-2026-10-12.md` for exact-frame/TCP evidence and
`fire-tv-network-scheduler-2026-10-12.md` for the quiet scheduler-trace limitations.
