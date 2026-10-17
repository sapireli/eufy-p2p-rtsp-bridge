# Cherryview encoder backlog: explicit driver-default preset

## Finding and change

The installed Intel Atom x5-Z8350 can sustain the observed three-camera wall.
The bridge was omitting VA-API's encoder performance setting, leaving this
Cherryview/i965 driver on its expensive initial AVC motion-search preset.
Explicit `-quality:v 0` requests the driver's default preset and approximately
doubled steady throughput on identical recorded input. This is a bridge
argument change; no SDK, Android, Linux client, FFmpeg, or driver source changed.

`server/src/ffmpeg-hardware.mjs` now supplies that option to both hardware
encoding attempts. Hardware-first ordering and software fallback remain intact.
Resolution, frame rate, GOP, B-frame policy, timestamps, and quantization are
unchanged. This setting changes motion-search effort and compression efficiency;
it is not a request for lossless encoding or a quantizer of zero.

## Actual driver path

Host: `homebridge.lan`, Atom x5-Z8350, Debian kernel 6.12.111, FFmpeg
7.1.5-0+deb13u1, Intel i965 CherryView driver 2.4.1 with shaders, libva 2.22.0.
Runtime debug showed H.264 Main, CQP20 for IDR/P pictures, and no B pictures.

FFmpeg's omitted performance option is -1: it sends no VA quality parameter.
i965 initially selects preset 1. Cherryview redirects AVC to the shared Gen9
AVC implementation, where preset 1 is best-quality motion search. Explicit
zero is accepted by the stock API and resolved to default AVC preset 4,
real-time speed. The previously tested value 2 still uses the quality kernel,
which explains why it offered no useful improvement.

This follows the actual Cherryview AVC redirect, not the older standalone
Gen8 VME quality branch. Source audit used i965 tag 2.4.1, commit
`9a1f0c64174f970a26380d4957583c71372fbb7c`.

Primary sources:

- [VA quality level zero requests the default](https://intel.github.io/libva/structVAEncMiscParameterBufferQualityLevel.html).
- [Initial preset and range](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/i965_encoder.c#L1568-L1602).
- [Explicit-zero handling and validation](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/i965_encoder.c#L838-L870).
- [Gen8 AVC redirect](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/gen8_vme.c#L1334-L1348).
- [AVC default resolution](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/i965_avc_encoder.c#L10520-L10544).
- [Motion-search preset tables](https://github.com/intel/intel-vaapi-driver/blob/2.4.1/src/i965_avc_const_def.c#L993-L1044).
- [FFmpeg performance-setting submission](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/libavcodec/vaapi_encode.c#L2148-L2152).

## Controlled throughput comparison

A stable 1920x1080 HEVC fixture was made from 400 consecutive source pictures,
then repeated with regenerated 40 ms timestamps. Each test stopped after its
first 400 pictures. There were no resolution changes. Both runs used the same
stock FFmpeg executable and production decode/scale/encode arguments, wrote
encoded output to `/dev/null`, and wrote packet statistics to regular files.
Only `-quality:v 0` differed. The bridge was stopped for both runs; a process
census confirmed no competing FFmpeg. A `finally` block restarted the service.

| Setting | 400-picture wall time | Steady rate, excluding startup |
| --- | ---: | ---: |
| Omitted performance setting | 16.930 seconds | 24.490 fps |
| Explicit driver default, quality 0 | 8.538 seconds | 49.783 fps |

Steady rate uses monotonic timestamps on FFmpeg progress records between
pictures 50 and 350. Both runs produced all 400 pictures, exited zero, and
reported no errors. An earlier independent default run measured 24.657 fps.
The source supplies 25 fps; the corrected setting provides substantial headroom.
This rules out network/player buffering and an unavoidable 25-fps hardware
ceiling as explanations for this measured throughput deficit.

## Live comparison and its limits

The initial 32-second baseline reached 120 outstanding Balcony pictures and
restarted. Its source was 1080p. The first 90-second candidate observation kept
up at 25 fps with 1–5 outstanding pictures and no backlog restart, but its source
was 720p. A subsequent 60-second old-setting reversion reached only 17
outstanding pictures and did not reproduce the full backlog; its source changed
from 720p to 1080p. Therefore that live A/B/reversion sequence alone is not
conclusive attribution. The identical-input comparison above supplies the
controlled evidence for the performance-setting effect.

The five-minute corrected live run covered all three Eufy streams. Excluding
initial startup, its 282-second measurement window showed:

| Camera | Source dimensions | Input/output fps | Outstanding pictures | Resets |
| --- | --- | --- | --- | ---: |
| Front Door | 1600x2200 | 14.999 / 14.999 | 0–1 | 0 |
| Balcony | 1080p in 139/141 samples; 720p in two | 24.982 / 24.982 | 1–7 | 0 |
| Garage | 1920x1080 | 15.003 / 14.999 | 0–2 | 0 |

All three remained hardware decode/encode. No packet/source identity mapping
failures occurred. Maximum egress ages included isolated excursions of
684–798 ms; this is not a guarantee that every frame stays below 500 ms.
There was no accumulating multi-second queue in this window.

Both Fire TVs independently decoded Balcony at about 25 fps over 90 seconds,
with hardware decoding and zero input queue. Bridge-ingress-to-decoder age was
91–96 ms median and 150–151 ms maximum, versus the earlier 4.6–4.7 seconds.
There were no Balcony decoder watchdogs, metadata releases, recoveries, or
disconnects. These timestamps start at bridge ingress, not camera capture.

## Output fidelity

Independent software decoding compared the same 114-picture production
excerpt with each setting. Both produced 114 pictures with no decode errors.
A same-index CPU-scaled 1080p reference gave:

| Metric | Omitted setting | Quality 0 |
| --- | ---: | ---: |
| SSIM | 0.986949 | 0.986906 |
| PSNR | 39.618 dB | 39.511 dB |
| Encoded bytes | 3,948,864 | 4,064,872 |

The measured change was -0.000043 SSIM, -0.107 dB PSNR, and +2.94% bytes.
Absolute scores include CPU-versus-VAAPI scaling differences in the 720p
section. This establishes a small fidelity/efficiency difference on this
excerpt, not bit-identical or guaranteed perceptually identical output.
No source or compatibility resolution, frame rate, or QP was reduced.

## Other findings and rejected changes

The old driver also returns UNIMPLEMENTED for `vaSyncBuffer`. FFmpeg therefore
uses blocking surface synchronization and disables its asynchronous encoder
FIFO. That explains why increasing async depth did nothing. It is a separate
software/API constraint; no async override, driver patch, or custom FFmpeg was
needed or deployed for this fix. The bridge still uses supported `vaSyncSurface`.

Removing scaling, software decoding with hardware encoding, and async depths
4/8 failed the earlier throughput comparisons and were not retained. Raising
quantization, dropping pictures, lowering resolution, and enlarging buffers
were not used as remedies. The earlier conclusion that this hardware simply
could not support the wall was premature; it described the old preset's
performance, not the hardware's best demonstrated supported path.

The reported green-frame symptom was not reproduced in this investigation and
is not declared resolved. Unrelated source pauses and all future workloads are
outside the scope of this encoder-backlog verification.

## Verification and records

The FFmpeg argument and production fresh-egress suites passed 33/33 tests.
The regression verifies the option applies to both hardware encoding paths,
never to software encoding, without changing quantization or fallback policy.
Independent review found no blocking issue.

Private records are retained under `.evidence/balcony-root-2026-10-16/`:
`encoder-preset-final.tgz`, source audit, fidelity metrics, and TV observations.
The archive includes exact arguments, packet statistics, per-progress monotonic
samples, live API samples, and derived summaries. Camera video and device logs
are not committed.

Evidence archive SHA-256: `070fc15117c3c659293a4911bc53edff5a2a3adaf8413336d8219c8fd092847c`.
