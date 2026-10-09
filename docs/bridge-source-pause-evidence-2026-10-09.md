# Source pauses and downstream failures: live evidence, 2026-10-09

This separates the observed failure stages. It does **not** claim that one fix solves every pause. Device identities, network addresses, credentials and full media captures remain in ignored private evidence.

## Observation boundaries

- Camera A: H.264 source, 1600×2200. Camera B: HEVC source, 1280×1440. They share a station and use independent P2P UDP sessions.
- Camera C: an independent HEVC camera, changing between 1920×1080 and 1280×720.
- Wire capture used AF_PACKET, a 160-byte metadata snapshot and kernel receive timestamps. The 18:27:11–18:37:11 capture recorded 536,458 packets and **zero observer drops**.
- A separate direct raw HTTP observer recorded byte-chunk arrival and Annex-B first-slice NAL arrival. These are delivered compressed units, **not decoded or displayed frames**.
- SDK baseline bundle SHA-256: `5ae12418fb1a7048bdff04c55d44d647009dcd57274897f990bf63453023f193`, corresponding to source `df535e2`.
- Restart/inspection windows are excluded: 18:25:24–18:26:10; 18:36:21–18:36:37; 18:41:34–18:42:08; GPU isolation 18:46:09.877–18:48:16.796; cached reads 18:49:02–18:49:17 and 19:01:03–19:01:55. The later buffer-only SDK restart begins 19:03:42.955.

## Genuine upstream media pauses

| Camera B media-wire silence | Duration | Adjacent sequence advance | Previous datagram ACK delay | Raw NAL gap |
| --- | ---: | ---: | ---: | ---: |
| 18:29:42.303–18:29:46.155 UTC | 3.852 s | 1 | 529.19 ms | 3.333 s |
| 18:31:15.095–18:31:18.223 UTC | 3.128 s | 1 | 27.98 ms | 3.118 s |
| 18:32:38.447–18:32:45.808 UTC | 7.361 s | 1 | 14.41 ms | 7.458 s |
| 18:34:38.107–18:34:52.564 UTC | 14.457 s | 1 | 67.17 ms | 14.509 s |

During the last pause, that session's PING/PONG traffic continued while Camera A's station media and independent Camera C media continued. The next B frame header advanced its source clock by only 72 ms despite the 14.457 s wall gap. This places missing media availability before the bridge raw feed; it does not identify the camera-to-station wireless link, encoder, station scheduler, or a source-side queue as the final cause.

No outgoing media-start/control command preceded the two longest pauses. The only four outgoing DATA commands in B's entire ten-minute trace occurred at 18:32:44.450, 18:34:44.173, 18:34:47.192 and 18:34:50.205. These match the existing attached-stream six-second silence watch and subsequent three-second restart nudge. They are responses to media silence. The two starts approximately 120 seconds apart are insufficient to establish a station lease expiry: later post-restart pauses did not follow that exact period.

Replay of deployed sequence handling found zero holds, abandonments or sequence restarts for A/B over 599.632 seconds, including natural sequence wrap. Both sockets also recorded zero kernel drops. Camera C's replay is **wire-only** and precedes its measured host-socket drops, so it cannot bound actual SDK loss/gating on C.

## Live source-link telemetry

Existing cached session identifiers were read privately, without refresh or camera writes. Level-1 control frames were then decrypted offline; no keys or identifiers were printed or committed. The accepted JSON notification decryption validates the key used for the binary controls.

Command 1032's signed first integer is interpreted as camera-channel Wi-Fi RSSI by the [primary implementation](https://github.com/bropat/eufy-security-client/blob/master/src/p2p/session.ts). This is provenance for the label; the numeric values below are from the actual capture.

For B's channel: -66 at 18:34:37.372, followed by -100 at 18:34:57.741 and 18:35:01.206/.277/.308, then -70 at 18:35:01.419, -62 at 18:35:01.747, and -59 at 18:35:02.545. A's channel generally reported -35 through -46. The -100 value correlates with the longest source outage, but its meaning as a disconnected/unavailable sentinel versus an actual signal measurement is unverified. This is a link-quality lead, **not proof that changing Wi-Fi will fix the issue**.

The 1,799 decoded 1351/1188 notifications carried an empty position array and count zero. The other decoded notifications were zoom 6203 and livestream status 6246; all recorded status counts were one, with no stop notification preceding the pause. No device-ping request 1152 was observed. Cached B Wi-Fi parameter 1142 was -60 from 11:40:08 UTC, several hours old, so it is not used as a live signal measurement.

There is no evidence supporting a new attached-camera keepalive default yet. The other implementation's command 1139 timer is gated to standalone battery devices, and its 1152 response mechanism requires an incoming request. Neither establishes an app-matching renewal requirement for this powered station. No maintenance-command change was deployed.

## Distinct downstream GPU failure

At approximately 18:35:25 the displays failed while both raw feeds continued. In 18:35:10–18:36:05, A delivered 820 first-slice NALs/27 IDRs and B delivered 960/35. Largest raw gaps were 1.219 s and 0.925 s; media-wire gaps were at most 0.827 s and 0.524 s.

The kernel recorded a shared video-engine hang in the B transcoder at 18:35:29 and another reset at 18:35:44. Existing error state captured both transcoder contexts. This identifies the GPU producer failure separately from camera media silence. Earlier producer failures at 17:59, 18:01 and 18:12 had no matching kernel hang record and cannot all be attributed to this event.

A fresh valid 631-frame clip passed three serial, real-time-paced isolated paths: hardware decode, software decode/hardware encode, and full hardware processing. Those short single-workload tests do **not** reproduce or eliminate the concurrent live GPU failure.

## Later display-only failures

The 18:56:22 parse failure and 18:58:44/45 decoded-frame watchdogs had continuous B raw delivery: 308 first-slice NALs in 18:56:07–24 and 587 in 18:58:29–18:59:00. There were no corresponding GPU hangs or producer read-timeout records. The final ring's complete VPS/SPS/PPS/IDR clip, from 18:57:59.193 through 18:59:23.396, decoded fully in software without warnings or errors. An untrimmed ring begins mid-frame and gives expected missing-parameter warnings; those are a diagnostic truncation artifact.

The subsequent Pi journal audit found DietPi's Wi-Fi monitor forcibly withdrawing the client's address and reconnecting Wi-Fi before these failures. See [the network teardown evidence](evidence/pi-wifi-reset-2026-10-09.md), including the interrupted diagnostic and remaining verification. They are not evidence of an SDK source pause or malformed delivered B bitstream.

## Private artifact fingerprints

| Artifact | SHA-256 |
| --- | --- |
| Ten-minute passive wire metadata | `45d38a7b4099251de8e1700aeb2be4a7af40596eed9c51ae050136baa8fce2bb` |
| Ordinary UDP/socket counters | `9237a003dec7dc7aef82e3622bc636e2e8212e7069f8966a1b3198ebb518c7c8` |
| Raw HTTP/NAL timeline | `a2cd55b712eea9badba329239edde8ce5d372244b6d067e47053b2fa0aa5a361` |
| Kernel GPU error state | `d54f6931179144388345ca7e752479bbaf2a755253196f3bc6ac100b74938e26` |
| Later watchdog interval, trimmed valid HEVC source | `5a7002dc00c293138d5750b10d3806be1f113940b0ad51316a23ee9354ce8015` |

Private ignored evidence resides under `.evidence/bridge-capacity-2026-10-09/`; publication must retain these boundaries and omit raw identities, keys and media.
