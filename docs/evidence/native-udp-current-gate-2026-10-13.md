# Current HomeBase UDP metadata gate — 2026-10-13

## Scope and provenance

One authorized five-second overhead/validity gate and **one** twenty-second trigger capture ran on bridge 192.168.23.158, interface eth0, peer HomeBase 192.168.23.233. No source, SDK session, player, camera, interface, service, or configuration changes were made. Current diagnostic source/wrapper were staged in a private temporary directory; the native `-O2 -std=c11 -Wall -Wextra -Werror` build and parser selftest completed before capture.

Read-only process checks retained Node 32681. After capture, go2rtc PID 32731 had the expected binary SHA `7f0858a30dfd8d5450b78458f1abb3dc3240f91165191646b23d65cbf785c585`. Two FFmpeg children 33257/33258 used `-hwaccel vaapi`, VA-API surfaces, and `h264_vaapi`; their existing scale expression capped height at 1680. No encoder settings were changed. SDK D5 was parent-reported unchanged. Post-capture health had all three sources streaming, auth OK, stalls 0, and unchanged epoch `7795771fb7475382`.

The current Node listeners included ports 32863/46707/47546. The actual five-second packet metadata identified **32863 and 46707** as the two current HomeBase incoming VIDEO-data ports. Previous capture ports were not reused. Header channels were 2 and 1 respectively; no camera name was inferred from a port/channel or geometry.

## Strict gate and artifact preservation

| Phase | Actual native UTC interval | Native elapsed | Native process CPU, one core | Fixed 56B records | Drops / missing timestamp / data truncation / control truncation / IO error |
| --- | --- | --- | --- | --- | --- |
| Gate5 | 00:55:36.853648–00:55:41.854772 | 5.001124 s | 3.1924% | 1,679 | 0 / 0 / 0 / 0 / 0 |
| Trigger20 | 00:56:37.013084–00:56:57.023890 | 20.010805 s | 3.8658% | 9,591 | 0 / 0 / 0 / 0 / 0 |

The trigger capture targeted the freshly observed port 32863, selected because it had the larger gate gap. No >500-ms trigger fired (`triggerWallNs=0`); the native polling deadline completed about11 ms after its 20-second target. There was no repeat after the quiet result. Native process CPU includes its syscall work; this is not a direct measurement of all additional kernel receive-side work.

The updated wrapper completed with exit0 in both phases. Fixed metadata, stderr statistics, stdout, separate exit status, and manifest were all copied to the Mac and independently SHA-verified **before interpreting the exit status**. Remote artifacts remain private; no packet snapshot or media payload was persisted. Maximum returned data/control sizes 128/32 matched the supplied 128/32-byte capacities. Distinct truncation counts were 0, so this run does not explain the previous failed run's merged truncation count.

The twenty-second capture reported 9,592 received messages and 9,591 mapped metadata records. One received message did not produce a record under the existing protocol/header filters; its reason cannot be recovered from the retained scalar metadata. No data/control truncation, missing timestamp, packet-socket drop, or missing VIDEO sequence interval was reported. The unclassified message is not claimed to be proven harmless or a lost camera frame.

## Physical arrival and logical metadata

After deduplicating retransmissions:

| Local port | Incoming VIDEO-data records | Duplicate sequences | Unique sequences | Missing sequence intervals | Unknown interior-header intervals | Maximum kernel-timestamp VIDEO-data quiet | Maximum accepted picture-header arrival gap |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 32863 | 2,118 | 39 | 2,079 | 0 | 2 | 222.235 ms | 353.648 ms |
| 46707 | 2,641 | 26 | 2,615 | 0 | 0 | 141.917 ms | 182.689 ms |

No >500-ms arrival gap was exercised in either current VIDEO-data stream. These VIDEO transport units include camera audio/other logical commands as well as command 1300 video. They must not be equated with decoded-picture arrivals.

Sequence-ordered declared-size reconstruction accepted 292 command 1300 picture-header labels on 32863, geometry 1600×2200, picture sequence 22654→22948; and 300 labels on 46707, geometry 1280×1440, picture sequence 34433→34732. Geometry here is a fixed source-header field, not independently parsed SPS or client output resolution. Retransmitted sequences were consumed only once. Header-like bytes inside an unfinished logical body did not establish a new picture start.

Port 32863 has three skipped accepted picture labels. Reconstruction also has two unknown interior-header intervals and incomplete visibility of headers coalesced inside datagrams. The capture cannot distinguish an unobserved picture header from a picture omitted before this receive boundary; it does not establish picture loss. Its observed source-word advances were 60–74 or 131/133/135 units. Port 46707's were 62–71 units, with no skipped picture-sequence labels. The source words progressed from 851637335→851656934 and 851631231→851651163 respectively. Their time domain is not sensor-validated; the cross-port offset was not interpreted as latency. Header flag values 0/1 were recorded without imposing a new canonical-flags rule.

## Result and next boundary

This proves the current collector's bounded CPU/strict-validity path and failure-preserving export work on the actual host. The short interval **did not exercise the intermittent source pause**, so it does not establish that pauses are fixed or identify sender production, forwarding, retransmission windows, SDK delivery, GPU processing, or physical display timing as the cause. No absence claim extends beyond these exact intervals. No playback/source code change was justified by this quiet sample.

Both collector handles terminated, and a read-only `pgrep -x udp-headers` found no remaining observer. The sibling bridge agent was told the capture window was clear. Private metadata and derived summaries are retained at `.evidence/frame-freshness-2026-10-12/native-source-current-20261013/{gate5,trigger20}/`.

Metadata SHA-256: Gate5 `5546b14f41966516320f813d2dadeb1d7ae7cb207dbf2025bbd1dd431e6f52c8`; Trigger20 `44c9859f5266232d3435fdb588e140eb740b2dae96243a457f39da1618f45257`.
