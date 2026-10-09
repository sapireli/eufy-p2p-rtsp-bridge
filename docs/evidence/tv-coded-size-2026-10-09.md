# TV coded-size allocation and measured resolution boundary

## Root cause in the client

The direct decoder used the SurfaceView tile viewport as its initial video
allocation. A Garage 1280×1440 stream produced the native codec error
`Wrong cropped rect (0, 0, 1280, 1440) vs. frame (960, 1076)`.
The coded picture and its display viewport are different sizes. SDP already
supplies SPS/PPS before the decoder starts, so the H.264 adapter now parses
that queued SPS, configures the coded picture dimensions, and retains the
initial configuration packet for decoding. No compressed bytes are rewritten,
resized, or discarded, and no new playout delay is introduced.

The change is restricted to H.264 SPS initialization. HEVC and streams lacking
an SDP SPS retain the previous allocation path. A candidate HEVC parser
was removed because the attempted Balcony playback test used the bridge's
H.264 transcoded output, which does not verify the HEVC parser.

## Declared capabilities versus actual playback

Both Fire TVs advertise an Amlogic AVC hardware decoder with dimensions
64×64 through 1920×1088 and two concurrent instances. Runtime VideoCapabilities
reports portrait Garage and Door sizes unsupported. Nevertheless, configuring
the actual Garage dimensions produces visible output 1280×1440, stride1280,
slice-height1440, and crop0..1279/0..1439 in the hardware output format.
Declared dimensions alone therefore cannot establish the physical ceiling.

With two camera tiles on Fire TV .85, the Door boundary tests are:

| Door coded dimensions | Result |
|---|---|
| 1600×2200 native | Hardware error; zero rendered frames |
| 1396×1920 | Inputs arrive; zero rendered frames over15 seconds |
| 1232×1694 | 211 input units; zero rendered frames over15 seconds |
| 1224×1682 | 222 input units; zero rendered frames over15 seconds |
| 1222×1680 | Hardware decoded format and sustained rendered frames |

Thus 1680 is the highest tested working even height for this fixed aspect ratio
on the tested two-camera wall; the adjacent1682 fails. The successful output
has stride1248 and slice-height1680. A padded-area limit is a plausible explanation
for the boundary, but this is not a proven firmware specification or a universal
height limit for other streams. Garage remains at its native1280×1440. The
bridge clamps height to the source size, avoiding upscaling Garage.

## Final installed artifact and live verification

Version0.3/code3 is installed on both Fire TVs .58/.85, with matching APK SHA-256
`6cf021a011f60f22c747bb0309bfe037b055e505ea75c8ddf7b272a33dc36c55`.
A paired30-second capture verifies actual Amlogic hardware output1222×1680
for Door and1280×1440 for Garage on both devices. No fatal decoder error or
compressed queue backlog occurs in that window. Excluding startup in a common
approximately22-second window:

| TV | Camera | Received / decoded / rendered frames | Receive→decode median | Receive→render median |
|---|---|---:|---:|---:|
| .58 | Door | 313 /313 /313 |11.6 ms |73.6 ms |
| .85 | Door |315 /315 /315 |11.4 ms |73.8 ms |
| .58 | Garage |360 /361 /360 |11.7 ms |72.4 ms |
| .85 | Garage |360 /360 /360 |11.6 ms |72.5 ms |

Counts are callbacks within the same time window; a frame crossing its boundary
can differ by one. Door cadence is approximately14.1fps in this short window;
Garage's17.3fps reflects burst delivery, not a changed nominal source frame rate.
Door still has a1.13-second incoming gap which propagates to rendering. Garage's
439ms incoming gap also propagates as a433ms render gap. This verifies the
allocation/resolution repair, **not elimination of upstream choppiness**.

The user subsequently reports that playback looks sharper and less choppy,
but Front Door has a brightness-flashing effect absent from Garage and the
phone app. That image-quality issue remains under investigation; the successful
frame counts above do not establish that it is repaired. After the user selected
Medium in the phone app and the bridge restarted its source session, the bridge
still reported native Front Door input at 1600×2200 H.264.

Underlying private evidence is in `.evidence/firetv-latency-2026-10-09/`:
`noscale-coded-tv85.log`, `noscale-outputformat-tv85.log`, the `height*` trial
logs, `final1680-tv58.log`, `final1680-tv85.log`, `final1680-summary.json`, and
`final1680-installed.json`. The hash manifest records the files. Camera choices
were restored byte-for-byte after the temporary Balcony test; diagnostic timing
logging was restored to INFO on both TVs. Build and physical playback are
verified on Fire TV; Google TV remains physically unverified.
