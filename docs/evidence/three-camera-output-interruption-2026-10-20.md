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
