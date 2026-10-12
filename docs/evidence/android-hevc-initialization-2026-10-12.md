# Android HEVC initialization and remaining transport limits — 2026-10-12

## Standards correction

[Android MediaCodec documentation](https://developer.android.com/reference/android/media/MediaCodec)
specifies HEVC `csd-0` as Annex B VPS, SPS, PPS. Codec configuration supplied in the
MediaFormat is automatically submitted when the codec starts.

The actual shipped rtsp-client-android 5.6.3 AAR was inspected with `javap`.
Its `RtspProcessor$proxyClientListener$1` queues SDP configuration as SPS, PPS, VPS,
tagged H264. The previous decoder adapter submitted this byte array as an ordinary
keyframe, with neither format `csd-0` nor `BUFFER_FLAG_CODEC_CONFIG`.

The correction extracts the complete initial HEVC parameter sets, orders them as
VPS/SPS/PPS in `csd-0`, and consumes only a configuration frame without VCL. A first
frame containing a picture remains queued. AVC playback is unchanged.

The actual native SDP contained VPS24/SPS45/PPS7 bytes. Canonical Annex B CSD is
88 bytes, SHA256 `d04a5e2bb2ac51e60a5d1713cf967b6053aec7beb30d8ff681907a81dd725d42`.
The unit fixture checks these exact bytes, canonical ordering, incomplete rejection,
and preservation of a first picture.

## Physical experiment and limits

Both Fire TVs were tested with their existing two AVC cameras plus one temporary
native HEVC 1920x1080 tile. Full preferences were backed up and restored exactly.

- Before CSD correction, actual hardware `OMX.amlogic.hevc.decoder.awesome` started
  but decoded/rendered zero while compressed input increased. The 15-second
  startup watchdog correctly switched the camera to compatibility. A third AVC
  instance then required software decoding; this was not a successful capacity test.
- CSD candidate APK SHA256:
  `1e07d595893e25adf5af2c66509d4f072829732a599120d0844b614cf2c5c7b6`.
- Candidate launch epochs: 1791826090811 / 1791826091198 ms.
- Actual HEVC hardware output format became 1920x1080. First render took
  11.939 / 11.522 seconds. Both reached 69 decoded and 64 informational rendered
  frames by approximately 31 seconds, without native fallback.
- Playback remained bursty: longest reported gaps approximately 3.8 seconds,
  pending-media estimate 7–8 seconds, and 24 output stamps missing. This is
  initialization evidence, **not proof of smooth or low-latency playback**.
- Exact original two-camera preferences restored and apps relaunched at epochs
  1791826126632 / 1791826127321 ms. All temporary third tiles were removed.

Two separate incompatibilities remain:

1. Actual Fire TV HEVC capabilities advertise Main/High Tier Level4.1
   (Android constants4096/8192). The native source SPS advertises Main profile, High Tier Level5.0
   (HEVC level_idc150). Dimensions and rate alone do not establish eligibility.
2. A bounded five-second native RTSP metadata observation found 126 marked frames
   at approximately25fps: 120 aggregation packets (RTP HEVC type48), 148 FU packets
   (141 IDR/type19 and7 delta/type1),6 SEI packets and3 single delta packets.
   Actual5.6.3 `RtpH265Parser` bytecode contains the unimplemented aggregation-packet
   branch. Those aggregation payloads are not delivered as decoded NALs. This
   explains sparse transport delivery independently of codec initialization.

No camera headers or levels were rewritten. No SDK or server change was required
for the CSD correction. Native HEVC auto-selection must remain conservative and
must not be described as successfully validated on these TVs.

Private logs: `/tmp/eufy-hevc-csd-tv85.log`, `/tmp/eufy-hevc-csd-tv58.log`.
Private RTP metadata: `/tmp/eufy-native-rtp-types.json`; no image payload retained.
