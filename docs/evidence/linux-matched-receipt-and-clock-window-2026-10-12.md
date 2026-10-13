# Linux matched receipt and clock window, 2026-10-12

## Scope

Offline analysis of the existing private `linux-native-sdk-matched-info.txt` and its collection status in `.evidence/frame-freshness-2026-10-12`. The journal labels use local EDT. This is a bounded output-counter analysis, not a new camera measurement. The collection status records 249.860 seconds and exit 124, the collection timeout; it does not establish a player failure.

## Counter changes

| Camera | First–last report | Picture timestamp span | Observed/accepted delta | Approximate output rate | Matched delta | Unknown clock delta | Receipt upper bound over 500 ms delta |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Front Door | 22:42:19–22:46:24 | 244.865 s | 3,075 | 12.558 fps | 2,885 | 190 | 751 |
| Garage | 22:42:19–22:46:22 | 243.129 s | 3,104 | 12.767 fps | 2,899 | 205 | 939 |

Each camera has 48 reports. Neither observed counter resets. Both retain server epoch `c88dfdc72b434ad0`. Missing-stamp, reordered-picture and `pacingDropped` counters increase by zero. These counters are measured at the pacer, after raw admission; they do not count pictures discarded earlier by admission. `accepted` denotes successful sink-chain return, not verified physical scanout.

There are 47 HTTP clock timeouts between 22:42:34 and 22:44:25. The unchanged pipeline subsequently improves:

| Camera | Degraded early interval | Later interval |
| --- | --- | --- |
| Front Door | 22:42:19–22:44:22: 1,329 pictures / 123.531 s = 10.758 fps; 664 late-upper and 157 unknown-clock increments | 22:44:33–22:46:24: 1,603 / 111.150 s = 14.422 fps; 15 late-upper and zero unknown-clock increments |
| Garage | 22:42:19–22:44:25: 1,428 / 126.761 s = 11.265 fps; 787 late-upper and 204 unknown-clock increments | 22:44:30–22:46:22: 1,598 / 111.339 s = 14.353 fps; 79 late-upper and zero unknown-clock increments |

Final individual decoded receipt upper bounds are 147.641 ms and 196.208 ms; final sink-return bounds are 166.736 ms and 215.546 ms. The multi-second cumulative maxima already include earlier activity and cannot be presented as current delay. Front Door's cumulative 8.600-second decoded gap is already present in the first report, so this window did not measure its origin. Histogram overflow returns the accumulated maximum, so its displayed high percentile is not an exact tail percentile above the histogram range.

## Separate path evidence

The parent agent's matched bridge-local API collector reports zero errors and a maximum request duration of 125 ms. Its passive UDP tap reports 3.48% of one core and zero socket drops. These exclude a global bridge API hang in that sample; they do not prove delivery or scheduling of requests on the Pi path. A separately classified physical source VIDEO gap remains an upstream issue and must not be attributed to Linux pacing from these counters.

One authorized later read-only Pi snapshot found:

- `homebridge.lan` resolves to IPv4 `192.168.23.158`; routing to that address is direct through `wlan0`, source `192.168.23.73`.
- The existing 22:42–22:47 kernel journal filter has no matching wireless, USB, reorder, reset or timeout entries.
- Current RTSP receive queues contain 1,024,361 and 502,654 bytes. Bytes alone do not measure picture delay. Socket RTT estimates are 493/608 ms versus minimum RTT 3.52/2.05 ms; cumulative outgoing retransmissions are 31/51 and incoming out-of-order packet counts 1,034/496.
- An existing HTTP socket has RTT 172 ms versus minimum 2.43 ms and ten cumulative outgoing retransmissions; a fresh HTTP socket has RTT 25.96 ms. An existing SSH socket also has elevated RTT and historical retransmissions.

These are later cumulative/current socket observations, not deltas joined to the earlier timeouts. They differ from the earlier five-second reader gate, which found empty sockets while polling. They support investigating intermittent Pi-path delivery/service and current unread backlog; they do not distinguish RF loss, host receive scheduling or media CPU competition. No network setting, service, decoder, packet size or camera quality was changed for this snapshot.

## Decision

There is no justified pacing-delay or DNS correction from this record. Both the generation-fix playback trial and the 16 KiB packet-size trial failed their bounded comparisons and were reverted by the parent. A conditional full compressed-AU memory merge in the stamp reader is a source-level optimization candidate, not a demonstrated cause of this window's poor motion. No split-memory fixture or new device diagnostic was run for this audit.

The next causal comparison must keep shared upstream source gaps separate from Pi-path interruptions. A successful bridge-local request does not validate the Pi request path; a shared physical VIDEO quiet interval cannot be repaired by changing Linux presentation timing. Existing matched per-picture evidence should decide which boundary to address before another code or network change.

## Proposed capacity isolation: decision only

A whole-process Garage `SIGSTOP` is not a clean CPU-versus-RF experiment. It removes media CPU work and, after its TCP receive queue fills, reduces incoming wireless traffic through backpressure. Improvement on Front Door would establish a shared Pi resource interaction, not distinguish those two mechanisms.

The Go supervisor restarts only after process exit or context cancellation; it has no separate decoded-frame timer. Each Gst pipeline does have a 15-second watchdog. Its thread stops with the process, but an overdue timer can fire immediately on resume, so a 15-second pause is unsafe. The RTSP sender also has a five-second write deadline after socket backpressure develops. A bounded shorter pause can therefore still close the paused consumer and change its recovery path.

The parent rejected this comparison because it confounds CPU and wireless capacity and can disrupt RTSP. No pause was executed.

## Verified parser-to-decoder handoff trial

The parent staged an ARMv6 launcher wrapper without rebuilding the Go client or installed `0b54` plugin. For hardware H.264 only it inserts `queue max-size-buffers=2 max-size-bytes=0 max-size-time=0 min-threshold-buffers=0` immediately before `v4l2h264dec`. The queue is nonleaky, retains compressed references, has no minimum fill threshold, and changes neither picture resolution nor decoder selection. This separates depay/parser execution from decoder-input work. It does not newly separate the TCP reader: GStreamer 1.26.2 already uses an independent rtpjitterbuffer source task.

An initial purported candidate was **invalid**: the service user could not traverse the private root-only wrapper directory, so PATH fell back to the original launcher. Its poor `candidate-60` output is not evidence against the queue. The parent corrected directory traversal and verified both actual hardware child command lines contain the queue before authorizing the valid measurement. Private verification: `hw-input-handoff/candidate-verified.json` (parent-owned).

The valid INFO-only collector ran 03:34:03.003–03:35:04.027 UTC on October 13. No additional media observer or per-frame probe was added. Its timeout exit 124 is expected. The parent verified main PID 11402 and hardware children 11450/11451 before this capture; this recorder did not independently poll child identities. Reports retain the same source epoch `ef8d6ac33b3483d9` as the prior baseline.

| Metric | Original baseline, bounded sampled span | Verified candidate, first 10 seconds conservatively excluded |
| --- | --- | --- |
| Front Door output | 147 / 10.952 s = 13.422 fps | 583 / 40.282 s = 14.473 fps |
| Garage output | 184 / 15.505 s = 11.867 fps | 671 / 45.583 s = 14.720 fps |
| Clock timeouts | 5 during approximately 20-second collection | 0 during complete 60-second collection |
| Front Door sampled sink-return upper age | 2,981.858; 2,071.463; 378.212 ms | Nine valid samples, 130.390–201.615 ms; last 139.028 ms |
| Garage sampled sink-return upper age | 1,017.372; 3,682.567; 2,631.340; 159.436 ms | Ten valid samples, 150.226–320.808 ms; last 170.984 ms |
| Unknown-clock increments | Door 6; Garage 3 | Zero both cameras |
| Receipt upper bound over 500 ms increments | Door 118; Garage 160 | Door 11; Garage 0 |

No counter resets, missing stamps, reordered pictures, pacer drops or decoder errors were recorded in the valid candidate capture. It passes the agreed short decision gate of at least 14 fps on both cameras and predominantly current sampled receipt upper bounds below 500 ms. Front Door's 11 late-picture increments show that not every picture met that bound. Physical motion and scanout were not observed by this collector.

The baseline was already recovering during its short window and the intervals are sequential, so this is not conclusive attribution of prior stutter to the handoff boundary. It is a positive short candidate result that warrants the parent's sustained verification, not an overall playback completion claim. The invalid run remains separately preserved; it must never be included as a genuine queue A/B. Private raw files and statuses: `hw-input-handoff/baseline-20.*`, `candidate-60.*` (invalid), `candidate-valid-60.*`, and `candidate-valid-exclude-first10.txt`.

### Planned sustained gate stopped early

The planned 300-second INFO-only collection began 03:36:25.498 UTC. One HTTP clock timeout and a shared sampled age outlier occurred during the first minute, violating the agreed zero-timeout criterion. The owned SSH collector was stopped at 03:38:17.958 UTC; its actual recorded duration is 112.460 seconds, exit 255. The private `candidate-valid-300` filename describes the plan, not a completed five-minute observation.

The final partial window contains 22 reports per camera, no counter resets/errors/missing stamps/reordered pictures/pacer drops or unknown-clock increments, and the same source epoch. Front Door delivers 1,548 pictures across 106.862 seconds (14.486 fps), with 41 receipt-upper-over-500-ms increments (2.65% of matched pictures). Garage delivers 1,542 across 106.214 seconds (14.518 fps), with 56 increments (3.63%). Both pass throughput and the five-percent incidence threshold over this partial window. Each has one sampled sink-return outlier (Front Door 1,691.947 ms; Garage 1,324.352 ms), with the other 21 samples below 500 ms. Final bounds are 308.507 and 388.644 ms.

There is exactly one clock timeout. Therefore the original zero-timeout gate fails and the planned five-minute gate was not completed, despite recovered throughput. The parent owns any revised retention criterion, rollback or subsequent verification. This collector changed no player, service, network setting or camera source.

The parent subsequently reverted the parser-to-decoder queue candidate after the agreed sustained gate failed. No permanent queue patch or wrapper was retained. The successful short window is preserved as scoped evidence, not a released fix.
