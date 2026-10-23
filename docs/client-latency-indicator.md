# Camera latency dots

Each camera tile has a small dot at its bottom-right corner. It updates without
changing video buffering, decoding, quality or reconnect policy.
Status is refreshed four times per second.

| Color | Meaning |
| --- | --- |
| Green | The latest output frame has a measured bridge-ingress age of at most 500 ms. |
| Orange | Measured age is over 500 ms and at most 1,500 ms, or timing is unavailable, starting or idle. |
| Red | Measured age is over 1,500 ms, or a requested live feed has produced no output for over 1,500 ms. |

Age includes time since the last output frame, so a frozen picture cannot stay
green. A camera change clears the previous frame measurement. Retrying the same
live feed does not reset its stale-output timer.
Missing frame timestamps, expired clock mappings and streams without timing
metadata cannot produce green. Orange on an idle battery camera does not mean
the client will wake it.

The measurement starts when the bridge receives a frame. It does not measure
the camera's sensor, HomeBase forwarding delay or the TV panel's physical
display delay. Camera capture time remains unverified.

## Android TV and Fire TV

The dot uses the timestamp belonging to the latest frame reported by the codec's
render callback. Strict freshness rendering instead uses its existing GPU-swap
admission callback. Neither callback proves physical screen scanout. The dot is
also present on motion-following tiles and fullscreen camera views.

## Linux

Direct DRM planes use the latest frame returned by the video sink and the
existing bridge clock mapping. A small bottom gutter keeps the dot visible
above the black backdrop without copying or converting hardware video frames.

Compositor and window modes show orange when per-camera output timing cannot be
verified after composition. They do not substitute incoming packets or decoder
input timestamps for displayed-frame timing.
