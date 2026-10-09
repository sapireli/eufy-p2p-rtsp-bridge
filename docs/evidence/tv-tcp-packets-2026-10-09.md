# Android TCP packet capability — 2026-10-09

## Change and boundary

Version 0.4/code 4 reads the optional per-camera `rtspTcpPacketSize` capability from `/api/cameras`. A hint in the range 256–65535 enables the go2rtc `pkt_size` URL query. Existing query parameters and an explicit `pkt_size` remain intact. Without a valid hint the URL is unchanged. TCP is already the player's transport; UDP behavior is not changed.

This reduces RTP fragmentation. It adds no playout margin, compressed-frame queue, codec tuning, image scaling, or render scheduling. Hardware-first decoding and software startup fallback remain unchanged. A larger packet array can hold one RTP packet; it is not a queue of future pictures.

The actual Gradle dependency `rtsp-client-android:5.6.3` bytecode was inspected: `readRtpData` grows its byte array to the received interleaved payload size and reads that exact size. It has no fixed 1500-byte receive limit. Independent concurrent RTSP clients verify maximum packets 1472 versus 8192 bytes, and 226 matching H.264 access-unit hashes at shared RTP timestamps, with zero coded-picture differences (repeated SPS/PPS excluded). See the Pi packet-size evidence for scripts and packet-level files.

## Fire TV .85 live before/after

Two 60-second process-CPU windows use `/proc/PID/stat` and the device's `AT_CLKTCK=100`. DEBUG per-frame timing is enabled for both tests and restored to INFO afterward. The new-app test excludes 12 seconds of startup from the CPU window.

| Metric | 0.3 default packets | 0.4 advertised 8192 |
| --- | ---: | ---: |
| App CPU, percent of one core | 23.27% | 19.92% |
| Garage received / rendered frames | 899 / 900 | 1020 / 1020 |
| Door received / rendered frames | 922 / 922 | 891 / 891 |
| Garage receive → decoded, median | 11.32 ms | 11.52 ms |
| Garage receive → rendered, median | 74.07 ms | 68.02 ms |
| Door receive → decoded, median | 11.20 ms | 11.48 ms |
| Door receive → rendered, median | 73.62 ms | 68.99 ms |

Observed CPU falls about 14.4% relative to the first window. Camera traffic and scenes are not held constant; Garage's second window includes catch-up bursts and more frames. This is a measured reduction, not a controlled estimate of a universal percentage. Decode time remains similar. No seconds of existing TV backlog were demonstrated, so this is not presented as a TV latency repair.

Both hardware output formats remain Door 1222×1680 and Garage 1280×1440, using `OMX.amlogic.avc.decoder.awesome`. Both cameras log the requested TCP packet size 8192. There are no fatal codec/crop errors in the measured window.

A Garage pause of 4.483 seconds is already present between receive callbacks and matches the 4.483-second render gap. Door's corresponding maximum receive gap is 1.476 seconds versus a 1.467-second render gap. This change does not remove upstream delivery pauses. The separate raw/encoded source investigation remains active.

## Deployment evidence

The tested APK SHA-256 is `b3b972af7cdcf887c16f0534fc8239fdf5908ced5888a674fa926d6f5ed50bda`. Version 0.4 is installed on both Fire TVs .58/.85, and installed bytes are verified against that hash. Both retain Front Door and Garage selections. A 30-second .58 startup check confirms the same hardware output sizes and the requested 8192-byte packet setting. Its Garage feed then hits an existing TCP `Read timed out` after five seconds without input at 18:03:22 UTC and reconnects with a new first hardware frame at 18:03:28. Queued units remain zero; this is not an uninterrupted source-availability pass. No decoder/crop failure was observed. The distributable file is `tv/dist/eufy-wall-tv-debug.apk`.

Private logs, CPU records, exact dependency bytecode, preferences and installed-package checks are under `.evidence/firetv-packetsize-2026-10-09/`.
