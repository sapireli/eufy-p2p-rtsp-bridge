# Three-camera output interruption on October 20

The bridge service PID remained 135966, started at 22:41:34 UTC. At
23:50:10 UTC Balcony hit the pending-frame bound; Garage Doorbell followed at
23:50:12. At 23:50:19 Front Door hit the same bound, followed by Garage
Doorbell again at 23:50:20. Each closes its timed HTTP producer, causing go2rtc
EOF/reconnect. This is a demonstrated mechanism for clustered interruptions
across the three Eufy feeds. It does not establish the initiating encoder or
host scheduling failure. The Wyze passthrough stream continued advancing in
the retained .85 decoder records.

The TV process remained PID 8951. Its retained logs show a later Balcony
hardware reassignment to the pinned Front Door decoder and software fallback;
this is another tile-level interruption. Retention was too short to establish
a separate earlier control-channel timeout. The current Android recovery path
preserves RTSP players on control loss and only invalidates changed playback
contracts for the corresponding camera. No new global-release defect was found.

A subsequent 30-second API/kernel sample (23:52:22.209–23:52:52.101 UTC) had
55 samples and 54 intervals with the same three FFmpeg DRM clients. Combined
video-engine utilization was median 34.708%, maximum 54.836%. Across intervals
without counter resets, Garage Doorbell accepted/output 396/395 frames, Front
Door 445/446, Balcony 741/741. An output count can exceed the interval input
when an earlier accepted frame completes. No counter reset occurred. These
healthy intervals do not explain or exclude intermittent hardware, filter or
host scheduling stalls; constant video-engine saturation was not demonstrated.
The counters do not measure sensor capture-to-screen latency.

The existing backlog-triggered hardware/mixed/software fallback experiment in
`balcony-recovery-2026-10-16.md` failed its sustained latency gate and was
reverted. Reintroducing it, raising the bound or reducing quality is not
justified by this check. No runtime policy changed in this investigation.

Private artifacts: `.evidence/garage-doorbell-2026-10-20/bridge-three-encoder-load.json`
and `/tmp/tv85-bridge-timeout-20261020.log`.

## Kernel evidence: shared Intel video-engine hangs

A subsequent kernel-log check found repeated i915 GPU HANG reports and vcs0
heartbeat resets at 23:24:04, 23:36:08, 23:38:21, 23:48:50, 23:49:33 and
23:50:21 UTC, among earlier events since October 18. The last three coincide
with the clusters of egress failures. All three VA-API transcoders share this
video engine. A shared engine hang/reset is a demonstrated host failure that
can interrupt all three outputs; the exact submitted operation that hung is
not attributed to a camera by the available error dump.

Hardware/driver: Cherryview PCI 8086:22b0 revision 36; kernel
6.12.113+deb13-amd64; i965-va-driver-shaders 2.4.1-2; libva 2.22.0-3; FFmpeg
7.1.5-0+deb13u1. The kernel error code `8:4:8cffbffd` is an approximate state
hash, not a semantic diagnosis of HEVC decode or H.264 encode. The retained
first error dump names no FFmpeg PID. Intel's iHD supported-platform list does
not include Cherryview, so swapping to iHD was not a supported intervention.

## Reversible software-decode trial

A trial changed only Garage Doorbell to software HEVC decoding with the same
hardware H.264 encoder, 750×1080 output geometry, camera quality and receipt
stamps. Other cameras retained full hardware decode/encode. The configuration
had a 150-second automatic rollback guard; it was restored manually and the
guard canceled. The trial's service started at 23:57:29 UTC.

In the subsequent 30-second steady sample, all three encoder clients remained
stable and outputs progressed. Combined video-engine median/max utilization
was 31.365%/44.069%. Garage Doorbell used roughly 203% process CPU in the
startup process sample, retained median eight pending source frame counts,
and only 62% of its output in the steady window met the existing 500 ms
bridge-receipt freshness budget. Front Door and Balcony each had approximately
100% fresh-output ratios over their corresponding steady windows. No GPU-hang
cure is claimed from this short interval. This software-decode configuration
failed the latency gate and was restored to the original hardware-first policy.

During the bridge restart, Fire TV .85 also crashed at 23:57:19 UTC in the
vendor Android RenderThread: RefBase::decStrong -> SurfaceControl.destroy ->
SurfaceView.surfacePositionLost_uiRtSync. This is a separate confirmed client
crash. The app removes/recreates its SurfaceView on every RTSP retry; a stable
surface lifetime is being investigated. The exact vendor-framework internal
race is not established by this stack. The app was relaunched and all three
Eufy cameras were verified decoding on .58, .85 and .100 at 23:59:56 UTC.

Private logs and samples: `.evidence/three-camera-gpu-fault-2026-10-20/`.

## Slice-threading retest and final policy

Real off-device FFmpeg tests exposed one second of future-frame hold in the
software decoder's automatic frame-threading mode; slice-only threading removed
that hold while preserving automatic within-frame parallelism. See
`software-decoder-frame-hold-2026-10-20.md`. This decoder fallback fix passed
255 server tests and is deployed. Hardware-first ordering and quality are
unchanged. A per-camera override provides the scoped trial path; it does not
change the global default.

The live slice-only retest again changed only Garage Doorbell to software
decode/hardware encode. Its first sampled interval included an upstream silence
with a retained source receipt over 23 seconds old, so that startup was not a
valid decoder performance comparison. In the next 30-second sample, Garage
Doorbell delivered 388 encoded outputs, 353 within the existing 500 ms receipt
budget (91%); Front Door 448/448 fresh, Balcony 729/729 fresh. Garage Doorbell
reported roughly 136% process CPU in a subsequent process sample. Combined
video-engine median/max utilization was 29.068%/43.902%. No sustained GPU-hang
cure was established. The earlier healthy full-hardware sample achieved
387/395 fresh Garage Doorbell outputs (98%). These are different sessions and
windows, not an isolated causal benchmark of source behavior.

Both temporary decode overrides were removed. The bridge restarted with its
original hardware-first policy at 00:07:16 UTC on October 21. Both rollback
timers were canceled. No camera resolution, encoder quality or source view
was lowered. The output-crash cause is the observed shared GPU hang/reset;
the specific provoking operation and a sustained driver remedy remain open.

## Controlled encoder retirement, October 21

At 00:12:20.931 UTC, a single SIGTERM was sent to Garage Doorbell's uniquely
identified full-hardware FFmpeg PID 140648. The observation covered
00:12:15.786–00:12:55.536 UTC, approximately five seconds before and 35 seconds
after the intervention. No bridge service restart or camera-source change was
made. The encoder exited with code 255 in the bridge journal and was replaced
by PID 140826. Garage Doorbell resumed output. No GPU-hang/reset event occurred
in this window. Front Door PID 140620 and Balcony PID 140619 remained unchanged;
their output counters advanced by 601 and 984 frames respectively. Their fresh
output counters advanced by the same amounts.

This weakens a deterministic healthy-context-retirement hypothesis. It does
not reproduce teardown of an already stalled GPU request and does not prove
that all teardown paths are safe. No runtime change was retained from this
test. The original hardware-first policy remains active.

Before this test, the old retained GPU error was backed up privately and the
retained error state cleared to allow the next fault to be captured. This did
not reset the GPU. The original dump is retained at
`/var/lib/eufy-wall-bridge/gpu-context-stop-20261021/before.error` on the bridge.
The metadata-only trial JSON has SHA-256
`959bfa3d98985dbeb320384fafef6c9f72ed0941db5fe834905820c40cd868c4`.

A native HEVC header check of Garage Doorbell used the existing bridge source,
without opening another camera session or saving pictures. The SPS reports
1600×2304 coded luma samples with four display lines cropped from the bottom,
a minimum coding block of eight, and a two-picture decoded-picture buffer.
Observed slices use one active reference picture. No parser error occurred in
30 copied pictures. These values do not demonstrate an unsupported geometry
or reference count. Driver dimension checks apply to the coded SPS dimensions,
not the cropped 2300-line display height. Reducing resolution or padding the
750-pixel compatible output is not justified by these results.

An independent 15-second observation of .85 at 00:16:03–00:16:16 UTC found all
four configured streams advancing, with no timeout, control-loss, player-release
or reconnect event. Front Door and Garage Doorbell used hardware decoding;
Balcony and Wyze used software decoding. Reported input queues were zero at the
sampled instants. This verifies current playback progress, not the absence of
historical outages: retained logs were sparse and did not establish the user's
original timeout timestamp. The separate stable-surface fix remains deployed
as app 0.6.9 on all three TVs.

## Retained batch operation

Offline decoding of the original October 18 error dump identified an HEVC
decode batch attached to the hung `vcs0` engine. It did not identify a camera,
exact faulting opcode or the origin of every October 20 hang. See
[the command metadata and attribution limits](cherryview-hevc-hang-batch-2026-10-21.md).
This narrows the implicated operation in one retained fault; it does not turn
the failed software-decode trials into a verified remedy.

## Rejected hardware-scaling fallback trial

The fresh 00:18:38 GPU reset yielded a new HEVC decode batch matching Garage
Doorbell's current coded dimensions; see the linked batch analysis above.
A guarded trial at 00:21:31 changed only that camera to software decoding with
slice-only threading, retaining VA-API scaling and H.264 encoding. The previous
mixed path had scaled on the CPU. Output remained 750×1080 with the same encoder
options. Front Door and Balcony remained full hardware decode/encode.

The first sample included startup and a go2rtc restart, so it is not a steady
performance benchmark. In the next 30-second sample (00:22:31.644–00:23:01.316),
Garage Doorbell's producer reset once. Summing only adjacent intervals without
counter regression gives 372 accepted inputs, 366 outputs, and 249 outputs
within the existing 500 ms receipt budget (68%). Median pending count was 4.5.
Intervals spanning the counter reset were excluded, so these are partial
window counts, not totals for all accepted pictures. Front Door output/fresh
output advanced 474/474 and Balcony 734/734. No new GPU reset occurred during
this short trial; the lack of one is insufficient to establish a hang cure.

A separate .85 observation at 00:21:58.616–00:22:28.879 found an unchanged app
PID, no common timeout/reconnect/release and all feeds decoding. Its sampled
Garage Doorbell decode-age upper bounds were 105–499 ms with zero queued units.
That observation preceded the bridge counter reset and cannot establish the
whole trial was uninterrupted.

The candidate failed the freshness gate. The original module and YAML were
restored at **00:23:29 UTC**, the rollback timer was stopped, and the local
candidate code/tests were reverted. No hardware-scaling candidate was retained
or claimed as a fix. Hardware-first policy is restored on all three cameras.
The candidate passed all 255 server tests; live verification rejected it.
Private artifacts include the rejected patch, both bridge samples and .85 logs
under `.evidence/three-camera-gpu-fault-2026-10-20/`.
