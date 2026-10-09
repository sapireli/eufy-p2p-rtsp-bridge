# Linux playback audit — 2026-10-08

## Scope

Check whether the Linux client shares the input/output scheduling defect corrected in the
[direct RTSP TV player](tv-low-latency-rtsp-2026-10-08.md). This audit does not change Linux
decoder selection, buffer settings, watchdog thresholds, or DRM handling.

## Live Pi observations before bridge migration

Read-only capture on the Pi at LAN address ending **.73** ran from
**2026-10-09 02:48:39–02:48:59 UTC** (October 8, 22:48:39–22:48:59 EDT).
The installed GStreamer version was **1.26.2**. The client was still consuming the old bridge
at LAN address ending **.199**; these observations do not verify playback after migration to .158.
No extra RTSP connections, service restarts, network interruptions, or configuration changes
were introduced for this audit.

The supervisor was active/running, PID **14275**, started at 18:16:10 EDT. Its live child
processes were Garage **23588**, started 22:40:37, and Front Door **23600**, started 22:40:39.
Both command lines used `v4l2h264dec`, `latency=200`, TCP, the post-decoder 15-second watchdog,
and independent shared-DRM `kmssink` planes with `sync=false fd=3 skip-vsync=true`.

Sixty snapshots of `/sys/kernel/debug/dri/0/state` were collected. The first and last sample
timestamps were **02:48:41.211951801** and **02:48:59.090356801 UTC**, about **17.88 seconds**
apart. Each iteration requested a 200 ms sleep; actual sample spacing includes command and
driver-state-read overhead. Adjacent-sample framebuffer comparisons gave:

| Camera | Plane | Decoded buffer | Render rectangle | Framebuffer-ID changes / 59 comparisons |
|---|---:|---|---|---:|
| Front Door | 98 | YU12, 524×720 | 786×1080+87+0 | 38 |
| Garage | 109 | YU12, 640×720 | 960×1080+960+0 | 41 |

Both planes used imported V4L2 decoder buffers on `pixelvalve-2`. Changes prove that both
planes advanced during this sample. They are not a delivered-FPS measurement: the decoder
reuses a pool of framebuffer IDs, and several frame changes can occur between snapshots.
An unchanged ID in adjacent samples also cannot establish a freeze.

A post-capture check at **02:49:38 UTC** found the same supervisor and both child PIDs still
active. `journalctl -u eufy-wall --since '2026-10-09 02:48:39 UTC'` returned no entries,
so no pipeline exit, watchdog error, or other new service log message was recorded in that window.

The preceding 100 journal entries did contain startup warnings: `VIDIOC_G_SELECTION` and
`VIDIOC_G_PARM` failures, decreasing output timestamps, and the decoder's warnings that **25**
initial Garage frames and **one** initial Front Door frame were not dequeued. Garage's decreases
were logged around 22:40:46 against a previous timestamp of 7.538665222 seconds; Front Door's
was logged at 22:40:52 against 14.303981334 seconds. These are observed startup anomalies, not
a measured steady-playback freeze. Their cause was not established by this audit; subsequent
framebuffer changes and surviving processes do not erase them.

This short capture establishes active hardware-decoded output on both planes. It does not
measure physical motion-to-screen latency, visually certify smoothness, establish long-term
stability, or test the recovery threshold. All eight Linux packages passed `go test -race ./...`
and `go vet ./...` in this session; those checks cover software behavior, not display timing.

## Verification after bridge migration to .158

The migration changed the Pi's configured bridge address from **.199** to **.158** and restarted
the existing client service at **22:56:29 EDT**. This verification introduced no additional
service restart, configuration change, or camera connection.

The new supervisor PID was **24414**. Front Door and Garage child PIDs **24425** and **24426**
started at 22:56:31, and both pipelines reached PLAYING at 22:56:34. Their recorded command
lines used `rtsp://192.168.23.158:8554/front_door_cle` and
`rtsp://192.168.23.158:8554/garage_cle`; the client also followed bridge events at
`ws://192.168.23.158:3000/ws`. Hardware decoder, jitter allowance, watchdog and DRM settings
were unchanged.

A second read-only capture ran **02:56:56–02:57:17 UTC**. Sixty DRM snapshots spanned
**02:56:58.781601801–02:57:17.241334801 UTC**, about **18.46 seconds**:

| Camera | Plane | Decoded buffer | Render rectangle | Framebuffer-ID changes / 59 comparisons |
|---|---:|---|---|---:|
| Front Door | 98 | YU12, 524×720 | 786×1080+87+0 | 41 |
| Garage | 109 | YU12, 640×720 | 960×1080+960+0 | 44 |

Both planes retained those dimensions/positions throughout and used imported V4L2 buffers.
The supervisor and both child PIDs were unchanged at capture completion and at a follow-up
check at **02:57:51 UTC**. The journal query covering **02:56:56–02:57:51 UTC** returned no
entries: no new pipeline exit, watchdog error, or other service log message appeared in that
steady-playback window.

Startup warnings were still present on the new bridge path: `VIDIOC_G_SELECTION`,
`VIDIOC_G_PARM`, decreasing timestamps, and **11** initial Front Door frames / **eight** initial
Garage frames not dequeued, logged at 22:56:41. Those warnings were not fixed or attributed
to a root cause by this migration. The preceding journal also contained old-address connection
failures during cutover; they belong to the old supervisor, before the .158 service restart.

This verifies both hardware planes advancing from the migrated bridge. As in the first sample,
framebuffer changes do not establish exact FPS, physical delay, or visually smooth playback.

## Recovery after the final bridge launcher restart

After the bridge service restarted at **03:07:48 UTC**, the Pi's existing supervisor PID
**24414** remained active. Its replacement Front Door/Garage child PIDs **25192/25193** started
at **03:08:13 UTC** and both pipelines reached PLAYING at **03:08:16 UTC**, still consuming
the .158 RTSP endpoints through `v4l2h264dec`. This check did not restart the Pi service.

A final read-only capture ran **03:10:18–03:10:25 UTC**. Twenty DRM snapshots spanned
**03:10:19.621123801–03:10:25.242012801 UTC**, about **5.62 seconds**. Front Door plane 98
changed framebuffer ID **18 times in 19 adjacent comparisons**; Garage plane 109 changed
**14 times**. Both retained the same imported YU12 buffers, 524×720 / 640×720 dimensions,
and render rectangles recorded above. These observations demonstrate advancing hardware
planes after reconnection, without establishing exact FPS or physical latency.

At **03:11:02 UTC**, the supervisor and both child PIDs were unchanged. The journal query
covering **03:10:18–03:11:02 UTC** returned no entries. Startup warnings were again present
in the preceding log: decreasing timestamps and **23** initial Front Door frames / **eight**
initial Garage frames not dequeued at **03:08:23 UTC**, along with the same V4L2 capability
query warnings. The final check does not establish that those startup anomalies are fixed.

## Decoder scheduling: the Android defect does not apply

The affected TV dependency read compressed input before draining decoded output; an empty input
queue could delay output by its one-second input wait. GStreamer's V4L2 decoder already separates
those operations. The exact **GStreamer 1.26.2** source shows:

- [`gst_v4l2_video_dec_handle_frame`, lines 1051–1059](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c#L1051)
  starts a source-pad task running `gst_v4l2_video_dec_loop`.
- [Lines 1062–1069](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c#L1062)
  submit compressed input, releasing the decoder stream lock while processing it.
- [`gst_v4l2_video_dec_loop`, lines 800–830](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c#L800)
  releases that lock, acquires an output buffer, and processes the capture queue independently.
- [Lines 900–903](https://github.com/GStreamer/gstreamer/blob/1.26.2/subprojects/gst-plugins-good/sys/v4l2/gstv4l2videodec.c#L900)
  deliver the decoded frame through `gst_video_decoder_finish_frame`.

This establishes that the V4L2 output task does not first wait for another compressed input.
It does not establish zero buffering inside the camera, hardware decoder/driver, RTP source,
or display sink. There is no source evidence supporting a Linux decoder rewrite for the
Android defect.

## Existing client policy and limits

`client/internal/pipeline/pipeline.go` builds TCP RTSP input with the configured `latency_ms`
(default **200 ms**), codec-specific depayloader/parser/decoder, and a post-decoder watchdog
of **15,000 ms**. Output sinks use `sync=false`; shared DRM planes also use `skip-vsync=true`.
The live pipeline contains no explicit extra `queue` element.

The watchdog detects absence of decoded buffers and makes the process fail so the supervisor
can restart it. It does not measure glass-to-glass delay, detect a camera repeatedly encoding
the same image, or independently prove that a display sink is presenting frames. Fifteen seconds
is a recovery threshold, not an intentional fifteen-second playback buffer. Unlike the TV
player, this GStreamer watchdog does not implement separate startup and steady-playback deadlines.
[GStreamer watchdog documentation](https://gstreamer.freedesktop.org/documentation/debugutilsbad/watchdog.html)

`sink: planes` creates one process per tile; errors restart that tile. `sink: compositor` and
`sink: window` create one process for the wall, so an error restarts all tiles.
`decoder: auto` continues to select available V4L2, VA-API, or software elements. This is
installation-time selection; it does not implement the TV player's runtime codec fallback.

## Buffer changes require separate evidence

No change to `latency_ms`, sink synchronization, or packet dropping follows from the TV
decoder correction alone. The client keeps the existing Linux settings.

- `drop-on-latency` drops old RTP packets before decoding when the jitterbuffer fills.
  Dropping compressed reference data can affect later decoded frames, so this is not equivalent
  to discarding an already decoded image. [GStreamer jitterbuffer documentation](https://gstreamer.freedesktop.org/documentation/rtpmanager/rtpjitterbuffer.html)
- `tcp-timestamp` is available starting with **GStreamer 1.24.10**. Its documentation describes
  TCP timestamp drift as a possible cause of growing delay, and also warns that receive-time
  timestamping can cause problems with bursty servers. That is a documented possibility,
  not a measured diagnosis for this installation. [GStreamer RTSP documentation](https://gstreamer.freedesktop.org/documentation/rtsp/rtspsrc.html)

The earlier [Pi HDMI deployment evidence](pi-hdmi-2026-10-08.md) records the hardware-plane,
profile, and shared-DRM fixes separately. Those measurements should not be presented as a new
end-to-end latency or long-term stability result from this audit.

## Preserved artifacts

Private raw capture and derived sample analysis are under
`.evidence/linux-playback-audit-2026-10-08/` (gitignored). The derived JSON retains each sample's
timestamp, plane ID, framebuffer ID, dimensions, format, and render position. Artifact SHA-256:

```text
1205efc78c005dca17da45012682bfd3e70c2334d0c0f24dc1e691a26d1f6057  pi-initial.txt
4f5ff9b6cfa91316ae2381302e7c1ffdeab8b131670955c8c33a3b7b1a98ee0e  pi-post.txt
998cae85c4cb30f501e0e507b87ec7cf1765285425c27f17723f6636b1de8a90  pi-sample-analysis.json
331d2096092b9e645fbd6406411f318d0575bc2af11657d07bef087c21d9f518  pi-migrated.txt
b2bd9d124caadee68ae3080dbf832c140511df8abd49e422b13a39df8e1c3774  pi-migrated-post.txt
02b24027a3c308cd8ae10f50374514326b14cf01b9c095db82bf0131158b67de  pi-migrated-analysis.json
87df876e14c47cb362c8f4a922040298a2830c0b0ee2c6944929f95259b6f73e  pi-final-bridge-restart.txt
b91e3eebeb2c1580674d82ad474208a6d4e8fb0c156d14f7f10fdfe7d012eae9  pi-final-bridge-restart-post.txt
bad7a9756821448f8bc917ee6f0a6a557f0b4302fa1ee05b9ec92892d6aad916  pi-final-bridge-restart-analysis.json
```
