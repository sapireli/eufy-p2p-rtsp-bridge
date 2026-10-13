# TCP packet-size trial: rejected and reverted

## Suspected cause, change and decision

Hypothesis: additional RTP packet parsing/dispatch contributes to the Pi's media
CPU cost. Existing configuration can request larger RTP payloads over TCP without
changing picture bytes, source timing, resolution, decoding or frame hold time.
The bounded trial added only `rtsp_packet_size: 16384`. Both actual hardware
pipelines (PIDs 4327/4328) requested `pkt_size=16384`; the existing `0b54` plugin
remained installed. Only the client restarted; no bridge, SDK or codec change.

The short live trial did not improve playback, so the setting was reverted.
Original configuration SHA-256, confirmed after rollback:
`54faa03adbc5f4bbdacd0ee2f2e7f6c1b60fcbc57cc3dd0acac76eb1137678b6`.
Trial SHA-256:
`32a20476a53d4fcac4f37b0320fa5ec21d35c65c4ae27425d91e24e072f7dc62`.
Backup: `/var/backups/eufy-wall/rtsp-packet-size-20261013/eufy-wall.yaml`.
Restore/restart completed with exit 0. A 32 KB trial was not performed after this
negative result. No project default was changed.

## Measured live result

From each camera's actual aggregate subwindow:

| Camera | Observed delta / elapsed | Frames/s | Decoded upper-age misses | Latest sampled KMS upper-age max |
| --- | --- | ---: | ---: | ---: |
| Front Door CLE, T8214510242321E6 | 602 / 56.963978 s | 10.57 | +281 | 2,297 ms |
| Garage CLE, T8425T1124130A0C | 673 / 59.204991 s | 11.37 | +365 | 3,636 ms |

The earlier unchanged-plugin baseline yielded 13.27/14.74 frames/s and +29/+11
misses. These sequential windows have different source/network conditions and
include a planned client restart, so they do not prove packet size caused the
regression. They provide no reason to keep the override. Fourteen HTTP clock
timeouts and unknown-clock increments +4/+7 further invalidate any sustained
freshness claim. Sparse KMS age means SDK ingress to sink return, not verified
sensor capture or physical display age.

The five-second CPU sample was valid (observer 0.803%, complete kernel counter,
zero warnings). It showed 139 idle ticks of 504 total and 255 media-process ticks
over 5.054802 s, about 50.4% of one core. The preceding stock sample showed about
72.6% media CPU and 9.3% idle. Lower CPU while frame delivery deteriorates is
**not evidence of an efficiency improvement**; the windows are not controlled
throughput comparisons. No Wi-Fi warning storm or maintenance job was observed
in these samples.

## Next decision

The existing five-second reader probe passed its observer gate (0.993%, both
RTSP sockets proven). Polling samples had empty receive queues; runnable samples
had only roughly 3–7 KB unread. It did not observe a reader blocked downstream
with sustained socket backlog, and does not exclude an intermittent backlog
outside its samples. Do not attribute all delay to decoder load or the Wi-Fi card
from average CPU alone.

The independent TV sustained gate then captured both Garage players timing out
at the same last RTP timestamp while Front Door continued. Investigate that
shared delivery event at its source/encoder/RTSP boundary before further tuning.
The separate WebSocket media-isolation repair remains deployed after its
controlled live pass; it is not claimed to fix this shared media timeout.

Private artifacts in `.evidence/frame-freshness-2026-10-12/`:
`linux-packet16384-deployment.json`, `linux-packet16384-reverted.json`,
`linux-packet16384-info65{.txt,.status.json}`,
`linux-packet16384-census5{.txt,.status.json}`,
`linux-postrevert-reader5{.txt,.status.json}`,
`linux-postrevert-census5{.txt,.status.json}`.
