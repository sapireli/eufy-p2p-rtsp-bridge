# Front Door brightness flashing investigation — 2026-10-09

## Report and capture

The user reports brightness flashing on the Fire TV Front Door tile, like a fluorescent light effect, rather than motion jumping. Garage does not show the effect. This investigation does not add playback waiting or change production settings.

A simultaneous capture started at Unix wall time 1791565266.06: 30 seconds of raw Front Door HTTP H.264, and 25 media seconds each of Front Door and Garage RTSP, copied without decoding. Private files are in `.evidence/firetv-brightness-2026-10-09/`. The raw HTTP timeout is expected and retained 3,748,938 bytes; RTSP copies exited successfully.

The source is H.264 1600×2200; the encoded Door rendition is H.264 1222×1680. Garage is 1280×1440. All inspected source and encoded frames consistently report full-range (`pc`) BT.709. The hardware Android decoder reports full range (`color-range=1`) and BT.709 for both feeds. No changing range or color-space metadata was observed.

## Decoded picture measurements

Independent FFmpeg software decoding produced 419 source frames, 361 encoded Door frames and 375 Garage frames. Luma measurements use analysis-only scaling to width 320; this does not change the production stream.

Matching source frame `n` to encoded frame `n+29` gives 331 matching temporal luma changes. Their mean absolute difference is 0.00060 on a 0–255 scale. The largest raw mean-luma change, 0.2775, corresponds to 0.2791 in the encoded stream. These keyframe-associated changes already exist in the raw source.

An 8×8 region analysis also finds the largest common scene changes in the same regions: an 8.392 raw luma increase matches an 8.361 encoded increase. A 332-frame grayscale picture comparison has pixel mean absolute error 0.725/255 and temporal-change mean absolute error 0.032/255. Temporal spatial-error spikes of approximately 0.70/255 occur at GOP boundaries versus approximately 0.02/255 for ordinary frames; this is being investigated as texture/contrast variation, and is not proof of the reported display flashing.

Garage has larger whole-frame luma changes than Door (maximum 0.811 versus 0.279), although the user only reports flashing on Door. Whole-frame mean luma alone therefore does not explain the reported symptom.

## Limits and next isolation

These measurements show that the sampled source luma variations are retained by the bridge; they do not establish the cause of the Fire TV symptom. They do not measure the physical screen. Fire TV `screencap` again returns black video planes, so a screenshot cannot prove rendered video brightness. No encoder, VPP, exposure, or Android fix has been selected from this evidence.

## Surface-copy diagnostic

A temporary diagnostic APK ran only on Fire TV .85. It requested sequential PixelCopy copies of each SurfaceView into a small 160×220 bitmap every 200 ms, bounded to 75 copies per camera. Each request completed successfully, but all pictures contained black video areas with a small fragment of overlaid text. Whole-bitmap averages around 0.3/255 are therefore not measurements of the live video. This device's hardware video plane is unavailable through both tested screenshot paths. The diagnostic cannot distinguish a hardware decoder picture fault from display postprocessing.

After the 25-second collection, the final version 0.3 APK was reinstalled and its installed bytes verified against SHA-256 `6cf021a011f60f22c747bb0309bfe037b055e505ea75c8ddf7b272a33dc36c55`. Preferences match the pre-diagnostic bytes exactly, and normal INFO timing logging was restored. Fire TV .58 was not modified. The temporary code exists only in `/tmp/eufy-pixelcopy-tv`, not in the repository or final APK.
