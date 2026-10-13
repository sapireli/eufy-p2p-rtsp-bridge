# TV recovery gate after bounded egress-stage instrumentation

Root performed one intentional SDK/bridge process restart on 2026-10-13 at 00:03:26.335 UTC to load the diagnostic initializer and stage hooks. The SDK bundle remained D5; the candidate transport code and camera configuration were unchanged. Root reported Main PID32681 and all three sources streaming. The optional stage capture was expected to run around 00:03:41–00:04:11; its data and lifecycle are a separate root-owned observation.

## Read-only TV window

Both Fire TVs were observed at INFO from **00:04:22.427 to 00:05:07.436 UTC**, 45.008 seconds. No app launch, install, debug change, preference change, Inspector action, source reset, or new RTSP viewer was performed. The two owned log collectors exited normally.

| Device | Camera | First→last decoded output count | Stats subwindow | Latest decode age lower–upper bound |
| --- | --- | --- | --- | --- |
| .85, PID20526 | Front Door | 685→1285 (+600) | approximately40s | 70–75 ms |
| .85, PID20526 | Garage | 696→1293 (+597) | approximately40s | 52–58 ms |
| .58, PID19463 | Front Door | 727→1327 (+600) | approximately40s | 50–57 ms |
| .58, PID19463 | Garage | 682→1357 (+675) | approximately45s | 68–72 ms |

Every sampled decoder was `OMX.amlogic.avc.decoder.awesome`. Input queue depth was zero in all samples. No EufyWallTV warning/error, new first-frame/recreation event, retry, sparse decode-age outlier, or increment in definitely-late frame counts occurred during this window. Missing stamps and unknown clocks remained zero. The observation establishes continued hardware decoding and recovery after the intentional restart; it does not establish physical smoothness.

## Timing limits and unresolved pauses

Garage's cumulative definitely-late count remained9 on each TV. Its cumulative maximum decode ages755/.85 and790/.58 ms originated before this window, including startup-era exact stamped outliers around 00:03:39. They are not current-window latency measurements.

The informational render-callback longest-gap maximum grew from483/.85 and500/.58 ms to1016 ms on both TVs. Front Door's corresponding maximum grew550→649/.85 and533→650/.58 ms. Android API28 render callbacks may be delayed, batched, or omitted; these counters alone do not establish a physical display freeze or identify a source, network, decoder, or scheduling cause. The source-arrival pause investigation remains open.

The read-only gate begins after the scheduled diagnostic expiry, so it cannot by itself verify enabled-collector overhead or classify any stage-capture rows. Root independently verified zero aged rows, file mode0600, FD closed at expiry around00:04:11, rollback timer and service inactive, and the temporary runtime environment override removed with daemon-reload and no further restart. Main PID32681 retained the latest instrumentation code. Root records these activation/API checks separately; zero rows means this short enabled window did not exercise an aged egress frame.

Private artifacts: `.evidence/frame-freshness-2026-10-12/tv-egress-stage-postdeploy45/manifest.json`, `summary.json`, `tv85.txt`, `tv58.txt`, and bounded prewindow logs. No image/video payload is stored or committed.
