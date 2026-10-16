# Balcony reconnect and decoding investigation, 2026-10-16

## Confirmed defects

### Bridge: dynamic-size VA-API filter failure

The live bridge repeatedly logged `encoder frame backlog exceeded bound` and
FFmpeg exit 218 with `Impossible to convert between ... scale_vaapi ... auto_scale_0`.
Each exit closed the HTTP producer and caused an RTSP producer reconnect.

A 35-second capture of the existing SDK source produced 775 independently decoded
HEVC pictures without decoder errors. Dimensions changed at complete keyframes:
1920×1080 → 1280×720 at picture 404 → 1920×1080 at picture 475. This was a real
source change, not an inference from bridge metadata.

A 114-picture excerpt (source pictures 401–514) reproduced exit 218 on the actual
bridge GPU, with both `reinit_filter=0` and `1`. An explicit fixed GPU canvas alone
also failed. Debug logs identified FFmpeg inserting a software `auto_scale_0`
after the VA-API filter during hardware-frame-pool reconfiguration.

An explicit fixed canvas, filter reinitialization and `-noautoscale` together
encoded all 114 pictures successfully at 1920×1080. The production Matroska input
and exact production argument builder also emitted 114 packets, with PTS
0, 3600, …, 406800 at time base 1/90000, exit 0 and no errors.

Commit `c040517` applies that correction. The configured compatibility height
defines the stable canvas; aspect ratio comes from the initial source picture.
A smaller temporary source is fitted into that canvas. This retains 1080p
detail when supplied and does not claim to recover detail from 720p. The
hardware-first, mixed, and software attempts use the same output geometry.

### Android: unused native metadata restarted compatibility playback

Both `hello` and `streamState` handlers released players when `nativeRendition`
changed, even when native playback was unsupported and compatibility playback
was selected. Live `.85` logs showed `hello-metadata-changed` releases at
17:01:22.541, 17:01:30.163 and 17:01:32.066 EDT, followed by compatibility starts.

Commit `1497474` stores the selected playback contract and reevaluates selection
when metadata changes. Only a changed selected contract releases the player.
Native eligibility changes still take effect. Six regression tests cover this.

### Android: AVC slices were submitted as separate pictures

The software encoder emitted four VCL slices for every picture in a 205-picture
live capture. The AVC receiver previously delivered each VCL NAL separately to
MediaCodec. At 17:01:04.052 EDT `.85` reported 1,048 received units but zero
decoded pictures and its 15-second no-output watchdog fired.

Commit `799249a` assembles bounded complete AVC pictures using the RTP timestamp
and marker, handles FU-A and STAP-A, retains parameter sets, and rejects incomplete
or sequence-loss pictures. Ten regression tests cover packetization and loss.
It adds no multi-picture playout buffer.

## Hardware/software allocation

Both tested AFTALMO firmware codec configurations advertise two simultaneous
hardware AVC decoder instances. Android's resource manager confirmed two active
Amlogic instances per TV. `.58` had three live tiles; `.85` had four. Balcony's
natural hardware allocation failure on `.85` at 16:54:25.306 EDT reported
`internalError -12`, followed by software startup at 16:54:25.355. Allocation
order explains which camera falls back; this is not evidence of different TVs.

Native HEVC selection was correctly rejected: Balcony advertised Main, High Tier,
Level 5.0, while these TVs advertised through Level 4.1. Passing the size/rate
test alone does not establish support for the declared stream level.

## Live results and remaining failures

TV 0.6.2/code 11 was installed on both devices with preferences unchanged.
Across the 125-second post-install observation, Balcony had zero metadata-triggered
releases and zero no-output watchdogs on both devices. `.58` decoded 163→2839 and
rendered 158→2829 with queue zero. `.85` decoded 128→2661 and rendered 124→2635,
but queued up to 60 pictures; its median pending media was 2484 ms. Both used
software AVC in that observation. Three Wyze no-output recoveries on `.85` were
separate failures, not attributed to Balcony.

The bridge still cannot sustain this simultaneous transcode workload. A two-minute
post-client sample measured source input 24.91 fps and encoded output 22.49 fps
between resets, with egress age reaching 4.75 seconds. Source receipt age stayed
under 41 ms, with the same source session and no source stalls. Thus this drift
was after source receipt. This does not establish a single-stream hardware limit.

A trial advancing hardware→mixed→software after a backlog failure did not pass
the sustained latency gate. That policy was reverted, not shipped. The format
correction and the two demonstrated client correctness fixes remain deployed.
Low-latency playback, including `.85` software decode backlog, remains unresolved.

The reported green occurrence was not captured. Independent analysis of 1,073
captured H.264 output pictures found no corruption errors or whole-picture green
excursion; representative source/output images looked normal. The corrected
format and access-unit defects are relevant candidates, but green-frame recovery
is **not verified** by this capture.

## Verification limits

All 93 app and 24 RTSP module tests passed, including the new regressions. The
final server run passed 241 of 242 tests. The unchanged observer clock-stall
fixture emitted 240 pictures in 6.2 seconds and failed its `>250` count guard;
it reported zero invalid transport pictures. The fixture and observer script
were unchanged from the pre-investigation revision. Repeating that test in
a focused group and in a serial suite still failed the count guard. Server test output is retained in
`server-tests-serial.log` with the private records.

The final live bridge configuration restores hardware-first attempts after the
rejected throughput-fallback trial. Subsequent logs still show Balcony encoder
backlog restarts, approximately every 24 seconds. The bridge format error has
not recurred in that observed post-rollback window, but the user's overall
frequent-interruption problem is **not resolved**.

## Records

Private records: `.evidence/balcony-root-2026-10-16/`, including bridge journals,
raw source/output captures, GPU replay commands, API comparison samples and TV
decoder-allocation/post-install logs. No SDK source was changed.

Source SHA-256: `d16c9f15f1d087a4b980f7e36b223ad5e9c68636ef3b56ac6fe98138906afd10`.
114-picture excerpt: `db93bc1787d925da27f2c9fcd26b2dc1eb17dec7c0d687e8066f2d72cb9295af`.
Production Matroska: `eafa893b91611cf60e2d704c5d1db878486a8198de33b80a09bdb1d7890894a5`.
TV APK: `50978453185fbc2327fb8cc3133b2f1da120cab91b0e0abe1270599edb38c3c4`.
