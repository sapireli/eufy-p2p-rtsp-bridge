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
