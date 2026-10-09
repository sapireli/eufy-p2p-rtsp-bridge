# Pi Garage delay: independent source/TV reference

The user reports Garage approximately 15–20 seconds behind on GaragePi while the Fire TVs remain current. This is separate from the unresolved Front Door brightness flashing report.

## Read-only reference capture

A 15-second capture compares a new Mac TCP RTSP consumer with Fire TV .58's existing live session. Frames are matched using the same RTP media timestamp, not the camera's printed clock. The Android player was not restarted. DEBUG timing was enabled only for the bounded measurement and restored to INFO in `finally`.

| Camera | Matched received timestamps | Mac marker → TV receive log, median | TV receive → decoded, median | TV receive → actual render, median / max |
| --- | ---: | ---: | ---: | ---: |
| Front Door | 171 | 34.02 ms | 11.15 ms | 72.42 / 175.81 ms |
| Garage | 169 | 33.87 ms | 11.49 ms | 71.75 / 247.67 ms |

The third column includes cross-host clock offset. Three contemporaneous clock checks use ADB round trips of 73–78 ms and bound the TV clock approximately 60–70 ms ahead of the Mac. The two consumers therefore receive matched stream frames within roughly a tenth of a second; the TV has no 15-second queue in this window. The RTSP marker and Android complete-access-unit callback are different packet boundaries, so their tens-of-milliseconds ordering is not a precise one-way network latency measurement.

The rendered listener runs on the UI thread and logs events later than their physical render timestamp. The final column uses the hardware callback's `renderNs` value, not the time when its log message appears.

## Pi observation and limits

A simultaneous read-only `ss -tnp` sample finds Pi RTSP receive queues of 413,829 and 82,192 bytes. Five additional samples at approximately two-second intervals show one socket varying from zero to 452,229 bytes and back down. This demonstrates local socket backpressure/bursty consumption, but does not alone quantify 15–20 seconds of queued video or identify the decoding bottleneck. One other decoder process restarted during that later window as the separate Linux diagnostic ran; the window cannot be called an unchanged steady-state experiment.

These results exclude a shared bridge delay explaining the TV/Pi difference in this sampled window. They do not measure absolute physical-camera-to-screen latency. The Linux investigation must match Pi ingress and rendered timestamps and establish why its consumption falls behind before selecting a fix.

Private captures and scripts: `.evidence/pi-delay-2026-10-09/` (`bridge-reference.jsonl`, `tv58.log`, `matched-reference.json`, `clock-check.json`). No server or Linux configuration was changed by this reference capture.

A later bridge-side snapshot confirms that the Garage producer's receiver feeds all three consumers: Fire TVs .85/.58 and GaragePi .73. The Pi connection alone has a large server TCP send queue (465,808 bytes in the initial read), alongside its local receive queue. This identifies per-client backpressure rather than a distinct delayed bridge producer. The amount of queued data alone still does not establish the reported total delay. Snapshot: `server-socket-stream-snapshot.txt`.

## Contemporaneous throughput cross-check

A second independent 15-second reference capture during the Linux encoded/decoded trace receives 223 Garage frame markers. After startup, 181 markers over 11.931 wall seconds give 15.09 frames/s; the matching RTP clock spans 11.973 seconds, giving 15.03 frames/s. Thus the producer continues delivering approximately 15 frames/s to an unconstrained consumer in the window where the Linux agent measures approximately 9.3 parsed frames/s and 9.5 decoded frames/s. This establishes a per-client consumption discrepancy. It does not alone decide whether packets are queued, dropped by QoS, or blocked by a particular stage. Artifact: `reference-during-pi-trace.jsonl`.

## Same-camera displayed-image comparison: 18 seconds behind

A warmed continuous Mac decoder sampled one original Garage frame every 15 frames, retaining top 100 image rows with the camera's visible timestamp. The analysis sampling does not alter client playback or add a production buffer. Files use VFR output; no duplicate frames are synthesized.

| Measurement | Capture wall time (UTC) | Same Garage image timestamp |
| --- | --- | --- |
| Warm Mac reference PNG `warm-reference-png/frame-006.png` | 17:43:50.187343 | 01:43:43 PM |
| Pi active Garage DRM buffer, `pi-garage-top-174350.pgm` | 17:43:50.318 | 01:43:25 PM |

The captures are approximately 131 ms apart and show **18 seconds of Pi display delay** relative to the same current Garage source. This comparison uses the same camera in both images, eliminating differences between camera clocks. The Pi image filename was corrected to its actual capture time. Recorded stderr timestamps in `pi-snapshot-times.json` supply authoritative capture times. The reference is its sixth sampled frame after decoder startup, avoiding the earlier single-image probe/startup uncertainty.

A concurrent 60-second independent RTP reference from 17:43:00.224 through 17:44:00.386 receives 893 Garage markers. After startup, 851 markers over 56.910 wall seconds give 14.94 frames/s; the same RTP media spans 57.049 seconds, giving 14.90 frames/s. Door is also approximately 14.87 frames/s. The source remains healthy while the Pi picture is stale. Reference file: `reference-quiet-60.jsonl`; image timing index: `warm-reference-png-times.json`; corresponding decode log: `warm-reference-png.log`.

This proves the local delay, not yet its blocking stage or a fix. The separate Linux investigation owns changes and before/after testing.

## Packet-size candidate: paired displayed-picture check

The root agent applied `pkt_size=8192` to both Pi RTSP URLs at 17:45:39, retaining the full coded resolutions and existing playback buffering. A 60-second warmed Mac reference repeats the same image-sampling method used in the baseline.

| Measurement | Capture wall time (UTC) | Same Garage image timestamp |
| --- | --- | --- |
| Reference `post-packetsize-reference-png/frame-037.png` | 17:47:13.093807 | 01:47:06 PM |
| Pi `pi-garage-pkt8192-second.pgm` | 17:47:13.124323 | 01:47:08 PM |

At approximately 31 ms wall-time separation, the Pi picture is two seconds newer than the warmed Mac reference, rather than 18 seconds older. The Mac reference uses an ordinary FFmpeg decoder and can retain future frames through frame threading; it is not a zero-delay absolute clock. The repeat uses the same reference pipeline, so the large before/after difference is meaningful without claiming subframe latency or measuring physical motion. A longer sustained check is still required to exclude renewed backlog.

An independent RTSP wire client verifies that `?pkt_size=8192` yields actual 8192-byte RTP packets over TCP, with mean packet length 7370 bytes and 1581 packets larger than 1500 bytes in the 15-second sample. It receives approximately 15.14 frames/s, matching the RTP clock at 15.06 frames/s. These are wire packet lengths, rather than producer-input packet counters. File: `check-packetsize.jsonl`.

A concurrent normal-versus-8192 test reconstructs complete H.264 access units and compares SHA-256 hashes at the same RTP timestamps. All 226 shared timestamps match, with zero differences; repeated per-connection SPS/PPS configuration is excluded from the hash. Normal maximum RTP packet length is 1472 bytes versus 8192 for the enlarged query. This directly verifies that the candidate changes fragmentation, not coded pictures. Files: `check-packetsize-bitexact.json` and its reproducing Python script.

## Observer correction and failed final continuity gate

FFmpeg's PNG encoder supports frame threading. Earlier references disabled decoder frame threading only; output PNG encoding still used its default thread policy. Those samples establish the baseline stale-picture discrepancy and a candidate improvement, but they cannot certify a subsecond current-source age. The corrected method uses both input `-thread_type:v slice` and output `-threads:v 1`, preserving original decoded frames and retaining exact output-image wall times. These flags apply only to the diagnostic Mac reference, not production playback.

The first ten-minute candidate pair at 17:55:55 shows Garage's Pi picture timestamp 01:55:36 versus the reference 01:55:35 approximately 131 ms later. Door also matches its reference timestamp 01:55:43 within 338 ms. This indicates agreement with the sampled current bridge feed; the camera clock's growing offset from wall time is not proof of renewed Pi-only lag. The PNG observer limitation still applies to that earlier pair.

The Pi independently rebooted during later testing and current decoder processes restarted around 18:02:03. Therefore the later automatic-mode ten-minute gate starts from that process boundary, not the older packet-size deployment time.

A corrected two-camera reference at 18:11:50–18:12:25 cannot pass the uninterrupted Garage gate: Garage produces only one sampled decoded PNG at 18:11:58.432039, while Door produces 33 through 18:12:24.936. The Pi Garage watchdog detects 15 seconds without decoded frames at 18:12:14 and restarts its pipeline. The independent raw/encoded audit also observes a raw Garage source pause of 10.007 seconds (18:11:46.257–18:11:56.263), an encoded pause of 8.451 seconds, and then a producer read timeout at 18:12:13.384. This proves an availability interruption upstream of the Pi decoder. It does not establish a packet-size regression or a complete smoothness fix. The upstream source investigation is separate and remains active.

## Final corrected recovery sample

The final reference at 18:15:15–18:16:05 uses decoder slice threading and single-thread PNG encoding on both cameras. The Garage comparison is bounded by adjacent reference pictures:

| Measurement | Capture UTC | Same Garage timestamp |
| --- | --- | --- |
| `settled-ten-minute-garage_cle-png/frame-037.png` | 18:16:01.959874 | 02:16:00 PM |
| Pi `pi-auto-coherent-garage.pgm` | 18:16:02.902 | 02:16:00 PM |
| `settled-ten-minute-garage_cle-png/frame-038.png` | 18:16:03.324174 | 02:16:01 PM |

The Pi agrees with current-source pictures within the approximately one-second precision of the visible timestamp. There is no 18-second local backlog in this recovery sample. This is not an uninterrupted ten-minute success: the source outage and Garage pipeline restart at 18:12:14/15 remain part of the record. The comparison demonstrates recovery and current-source agreement after that event; it does not repair or hide the upstream outage.

Front Door's timestamp is absent in both the corrected reference and Pi picture, and the user also reports its absence in the Eufy app. Its image clock therefore cannot establish a Door latency bound in this window. The missing timestamp and the earlier brightness report remain separate investigations.
