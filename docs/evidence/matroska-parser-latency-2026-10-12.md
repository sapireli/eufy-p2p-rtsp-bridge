# Complete access-unit transport removes the bridge parser hold

## Root cause

Finite-fragment MPEG-TS fixed the extra wait caused by oversized length-zero
PES packets, but FFmpeg's MPEG-TS demuxer still selects full elementary-stream
parsing. That parser cannot establish the final Annex-B access-unit boundary
until it receives the following unit. The
[October 11 controlled experiment](source-timestamp-transport-2026-10-11.md)
measured an approximately one-frame hold remaining after the finite-PES repair.

This hold is avoidable because the SDK already supplies complete access units.
The bridge should preserve that known boundary instead of throwing it away and
asking FFmpeg to rediscover it from an unframed Annex-B byte stream.

## Change

Only the **SDK-to-FFmpeg input** uses streaming Matroska:

- One finite SimpleBlock contains exactly one complete source access unit.
- CodecPrivate uses proper AVCDecoderConfigurationRecord (`avcC`) or
  HEVCDecoderConfigurationRecord (`hvcC`), derived from source SPS/PPS/VPS.
- Packet NAL units use four-byte big-endian lengths, matching the declared
  configuration, rather than pretending Annex-B bytes follow the Matroska
  AVC/HEVC codec mapping.
- TimecodeScale is one millisecond, preserving this host's validated source
  millisecond increments exactly, including uint32 wrap. `-copyts` and encoder
  time base `1:90000` retain exact input/output PTS association.
- Source resolution changes remain in-band; supported chroma and bit depth stay
  constant. Container dimensions describe the initial track, while actual SPS
  updates determine decoded picture dimensions.
- Complete source units and the encoder input queue retain a 4 MiB bound.

Final output remains the existing timestamped MPEG-TS/go2rtc/RTSP path. Hardware
decode and encode remain first, with the existing software fallback. No output
resolution, freshness deadline or playback buffer was increased.

The installed FFmpeg source establishes the relevant distinction:
[MPEG-TS selects full parsing](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavformat/mpegts.c),
whereas [Matroska video uses header parsing](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavformat/matroskadec.c),
and [the generic demuxer marks header-parsed packets complete](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavformat/demux.c).
The container codec configuration follows the
[Matroska AVC/HEVC codec mappings](https://www.matroska.org/technical/codec_specs.html).

## Controlled local validation

Real libx264 and libx265 fixtures decoded with identical per-picture hashes
before and after the input-container conversion. Packet timestamps preserved
the original irregular increments and wrap: source words
`0xfffffff0,51,217` produced container PTS `0,67,233` milliseconds.

Separate in-band resolution-change checks decoded three 128×96 pictures followed
by a 160×120 picture without recreating the Matroska stream, for both codecs.

The open-input parser experiment sent a valid 300 KB filler-bearing IDR followed
by two P pictures and then withheld both another picture and EOF:

| Input transport | Decoded pictures before another AU or EOF |
| --- | ---: |
| Finite-fragment MPEG-TS | 2 |
| Complete-AU Matroska | **3** |

The large first packet makes startup probing complete in this small synthetic
fixture. It does not justify changing production probe limits. The result
directly verifies removal of the final-unit parser hold.

## Native hardware validation

An isolated test ran on the deployed Atom bridge with its installed FFmpeg
7.1.5/i965 VA-API hardware decoder and H.264 encoder. It did not change production
configuration. Each transport received the same 30-picture synthetic sequence
at approximately 67 ms intervals, with a valid 300 KB filler-bearing first IDR.
The source write time was joined to the output packet by exact PTS. The first
six and final output picture were excluded from latency statistics.

| Input codec / transport | Median source-write→encoded packet | P95 | Packets before EOF / total |
| --- | ---: | ---: | ---: |
| H.264 / MPEG-TS | 103.920 ms | 203.995 ms | 29 / 30 |
| H.264 / Matroska | **32.171 ms** | **94.814 ms** | **30 / 30** |
| HEVC / MPEG-TS | 81.029 ms | 119.111 ms | 29 / 30 |
| HEVC / Matroska | **35.527 ms** | 178.442 ms | **30 / 30** |

All four runs emitted exactly 30 packets, with zero unmatched PTS and no FFmpeg
errors. Matroska emitted the final packet while its input remained open, for
both source codecs. The HEVC P95 worsened in the separate window; this result
does **not** establish improved tail latency or absence of source/driver stalls.
It establishes removal of the deterministic next-unit hold and lower median
stage delay in the observed fixture runs. It is not a camera-to-screen latency
measurement or proof of production client smoothness.

Private artifacts:

- `/tmp/ewb-native-mkv-comparison.jsonl`: complete per-packet native results.
- `/tmp/ewb-mkv-native-probe.mjs`: native fixture runner.
- `/tmp/ewb-native-fixtures.json`: synthetic encoded source pictures.
- `server/test/video-matroska.test.mjs`: checked-in local reproduction.

## Rejected encoder tuning hypothesis

The installed `h264_vaapi` encoder reports `async_depth=2` by default. A live
Garage stage comparison tried 2→1→2. Medians were 362.8→274.3→267.4 ms, so the
reversed baseline eliminated the apparent benefit. This agrees with the
[earlier stage study](firetv-latency-investigation-2026-10-09.md), which measured
encoder-submission→output medians 19.5 ms at baseline versus 20.7 ms with depth 1.
No async-depth change was accepted from these results.

## Codec dependencies and the freshness deadline

An expired compressed reference packet retains its original ingress stamp and
must still reach the decoder. Strict clients suppress its **decoded output**;
they must not display the expired picture. Dropping that compressed P picture
at the bridge would corrupt its dependents and manufacture a hole until the
next IDR, even if later pictures are fresh.

The bridge reports `expiredReferenceFrames` separately. It does not restamp old
packets as fresh, widen the 500 ms deadline, or claim all transmitted packets
are fresh pictures. Initial stream entry still requires a valid keyframe.
Missing/unmatched metadata, impossible receipt clocks, hard queue bounds and
stopped downstream drainage fail the timed stream.

The local egress tests verify that a stale reference packet retains its exact
expired stamp and a following fresh P picture resumes immediately, without
waiting for another IDR. Real continuous-transcoder tests verify stamped output
before EOF and large-IDR backpressure behavior. Physical Android/Linux
presentation, expiry masking and live smoothness remain separate deployment
gates. Capture-clock calibration remains unverified; the enforced scope is
bridge ingress, not sensor capture.
