# Saved Android wall recovery during bridge startup

## Root cause and change

The activity initially displays Setup before requesting `/api/cameras`. Its old
error path scheduled the WebSocket reconnect only when the wall was already
visible. An unreachable bridge therefore stranded a saved wall in Setup. An
empty startup camera list did the same, despite a successful HTTP response.

Version 0.5/code5 retains the saved auto-open intent and retries camera discovery
every five seconds after these two transient outcomes. The timer exists only
before the saved wall opens. Manual Connect, Discover, Back, and destruction
cancel it. Backgrounding invalidates outstanding responses and suspends retry;
returning resumes the saved attempt. A request generation prevents a late result
from replacing a newer setup choice. Active-wall playback, hardware selection,
packet sizing, decoder output draining, and playback buffering are unchanged.

## Actual device check

On Fire TV 192.168.23.85, a temporary Mac HTTP proxy was used for the saved
bridge address. Production bridge/service/camera sessions were not stopped.
The proxy initially did not listen, then returned one `200 []`, then forwarded
the real API and WebSocket. Its HTTP Host was translated to the real bridge so
both camera RTSP URLs remained `192.168.23.158:8554`; no extra camera pull was
opened. Original bridge, camera selection, and owner preferences were restored
byte for byte after the test.

The successful run logged retries at Unix 1791570861.778 and 1791570866.847;
real API discovery at 1791570872.208 started both RTSP players. Front Door first
rendered at 1791570873.158 and Garage at 1791570874.101, using
`OMX.amlogic.avc.decoder.awesome` hardware decoding. Actual visible formats were
1222×1680 and 1280×1440. The wall opened without remote interaction.

A HOME/resume check made no proxy API request during the eight-second background
interval and resumed discovery on return. This is a background suspension check;
we did not obtain a clean conclusive delayed-response cancellation trace. That
boundary is protected by the request-generation checks in the implementation.
Initial unsuccessful diagnostic runs used incorrect proxy Host routing or reused
test activity state; their RTSP404s are not claimed as production failures.

## Delivery and limits

Build: `assembleDebug` with JDK17/Gradle8.13. APK 0.5/code5 SHA256:
`ad82c14769938cd5083134f9c25708920f9868202ed960f1ed0dff69eec67e35`.
Installed on both 192.168.23.85 and 192.168.23.58, retaining their actual saved
bridge/camera choices. Installed APK hashes matched the delivered artifact.
In a final 20-second real-bridge run, both cameras rendered on both TVs using
the hardware decoder, with empty pending encoded-unit queues. The final sample
reported Door/Garage rendered counts 221/200 on .85 and 213/200 on .58. These are
short continuity checks rather than physical smoothness confirmation.
Delivered artifact: `tv/dist/eufy-wall-tv-debug.apk`.

Private reproducibility artifacts are under
`.evidence/tv-coldlaunch-2026-10-09/`: proxy/test scripts, request times,
`recovery.log`, restored preference copies, and final real-bridge device logs.
This fixes setup recovery; it does not resolve upstream source pauses or prove
physical display smoothness/brightness behavior.
