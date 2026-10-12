# Linux H.264 parser audit — 2026-10-12

## Result and limits

The corrected real parser-only bypass **failed caps negotiation** on both
camera pipelines and was rolled back. Removing `h264parse` is not deployed.
A temporary two-mode test
aborted during the baseline metadata gate because its CAPS selector expected
`GstV4l2H264Dec`; the deployed dynamically registered class prints
`v4l2h264dec`. The bypass mode never ran. This was an observer bug, not a decoder
negotiation failure.

A subsequent bypass-only attempt also produced no candidate hardware output.
The diagnostic wrapper replaced the parser with an AU caps token beside the
existing AU caps token. GStreamer rejected those consecutive constraints during
pipeline construction (`no element "video"`). This is a test construction
failure, not evidence that the decoder requires the parser. The corrected
construction removes the parser and its preceding separator, retaining the
existing AU constraint before `eufystamp`. No CPU or freshness benefit can be
claimed from these attempts.

## Corrected live bypass: concrete rejection

The corrected wrapper removed `h264parse` and its preceding separator, retaining
the existing byte-stream/AU caps before `eufystamp`. An actual Pi GStreamer
zero-buffer grammar fixture passed before testing. The installed binary, plugin,
all other arguments and camera settings remained unchanged. The candidate had
no repeated baseline window; comparison to the earlier baseline was explicitly
not paired.

Candidate hardware pipeline PIDs 23249 and 23250 both reached RTSP setup. Neither
produced raw decoder CAPS or a candidate decoded picture. Both logged:

```text
eufystamp0: transform could not transform video/x-h264,
  stream-format=byte-stream, alignment=au in anything we support
rtspsrc: streaming stopped, reason not-negotiated (-4)
ERROR: Internal data stream error.
```

The `eufystamp` warning may reflect its downstream caps negotiation; it does not
by itself identify which downstream requirement rejects the bare caps.
Therefore the proven result is that the **current complete installed chain**
cannot use this simple bypass. It is not proof that stateful H.264 hardware
always requires a parser, or that the stamp element intrinsically requires
`parsed=true`. Parser-added fixed metadata is a hypothesis to isolate offline.

The controller aborted at the first sampled pipeline error and restored the
original active service, MainPID 23349, disabled. Total setup, failure detection
and cleanup took 37.09 seconds; self plus child CPU was 3.24 seconds. This is
observer/setup CPU, not a parser CPU saving. The override was absent and the
100-second fallback timer stopped. There was no 30-second candidate steady
window, no candidate CPU-capacity comparison and no candidate freshness result.

Private proof: `linux-parser-bypass-corrected-results.jsonl` and
`linux-parser-bypass-corrected-journal.txt` under the same private evidence
directory. The short controller kept 5-second process ticks and WLAN-byte
snapshots; verbose output was startup CAPS, not per-picture tracing.

The installed binary, plugin, camera configuration, quality and geometry were
preserved. The test added `gst-launch -v` CAPS output through a temporary runtime
PATH wrapper. Its finally block removed the unit override and restarted the
original service. Cleanup confirmed MainPID 22454, active and disabled; the
independent rollback timer was stopped. The user's existing ON state was kept.
After the second attempt, cleanup again confirmed the original service active
and disabled, MainPID 22941, with the runtime override removed and guard stopped.

## Actual baseline CAPS

Both depayloader outputs were `video/x-h264,stream-format=byte-stream,alignment=au`.
The parser added SPS-derived metadata. Hardware output was:

| Camera | Visible dimensions | Memory / format | Pixel aspect | Framerate |
| --- | --- | --- | --- | --- |
| Front Door | 1222 × 1680 | DMABuf / DMA_DRM / YU12 | 1/1 | 30/1 |
| Garage | 1280 × 1440 | DMABuf / DMA_DRM / YU12 | 1/1 | 61/4 |

Both hardware outputs first advertised colorimetry `1:3:5:1`, then changed to
`1:4:7:1`. Front Door parser output had pixel aspect `6720/6721`, while hardware
output normalized it to `1/1`. These are real comparison requirements, not
assumed equivalence. Framerate caps alone do not prove actual source cadence.

The controller ran 53.74 seconds including setup and restoration, with measured
self plus child CPU of 4.76 seconds. No steady A/B comparison was completed.
Clock API timeouts persisted. A baseline Garage frame had KMS-return age bounds
10.110–10.463 seconds; many other records had invalid clocks and cannot support
an age claim. Hardware output return is not physical scanout proof.

Private metadata artifacts are
`.evidence/frame-freshness-2026-10-12/linux-parser-ab-baseline-failed.jsonl` and
`linux-parser-ab-baseline-caps.txt`. No image payload was recorded.

## Why a narrow experiment is technically justified

Pinned GStreamer 1.26.2 `GstV4l2VideoDec` explicitly enables packetized input.
The deployed decoder accepts byte-stream AU input without requiring
`parsed=true`. `rtph264depay` assembles marker-terminated AUs and retains SEI and
in-band parameter NALs in byte-stream mode. However, `h264parse` also supplies
timing, aspect and color metadata; its automatic passthrough is disabled.
These facts justify testing removal, not assuming safe removal.

Primary sources:
[depayloader](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-good/gst/rtp/gstrtph264depay.c),
[stateful decoder](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c),
[parser](https://raw.githubusercontent.com/GStreamer/gstreamer/1.26.2/subprojects/gst-plugins-bad/gst/videoparsers/gsth264parse.c).

## Remaining gate

A later experiment first needs an isolated, reproducible caps-negotiation fix.
It must remove only `h264parse`, retain the existing explicit byte-stream/AU
constraint, preserve all compressed NALs and stamps, and compare decoded
dimensions, crop, color, aspect, PTS, frame ages and targeted process CPU.
Unknown clocks fail the age-comparison gate. SPS changes and reference-picture
continuity require verification before a general default change. The historical
39% receive/depay/parse CPU figure does not attribute CPU cost to the parser.
