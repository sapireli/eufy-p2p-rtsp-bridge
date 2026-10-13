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
