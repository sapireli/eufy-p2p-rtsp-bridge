# Compositor tile isolation and qualification

Status: **implemented in the Go client; hardware qualification remains open.** `sink: compositor` and `sink: window` use `client/internal/gstnative`, which owns one GStreamer pipeline with stable compositor pads and replaces individual source bins. `sink: planes` uses one supervised `gst-launch-1.0` process per tile. `pipeline.Plans` still builds a single-process compositor command for dry-run diagnostics; that command is not the compositor runtime.

## Runtime behavior

The Go renderer keeps a live black output source and one stable entry queue per configured tile. A tile source can be RTSP video, a retained JPEG, or black. A source or codec change parses and installs a replacement bin on that tile's entry pad; other source bins and the output sink remain in place. Live bins use a decoded-buffer watchdog. The monitor checks per-tile decoded frame counters and retries stalled sources after a bounded delay; it also reloads retained JPEGs every 30 seconds because `imagefreeze` otherwise repeats its first image forever. A fatal output stall causes the wall process to exit so its service manager can restart it.

The renderer writes local status with the active config hash, process ID, sink, output frame counter, and per-tile generation, state, expected-live flag, and decoded frame counter. Failed source transitions report `retrying` and do not credit old-source frames to the new expected live tile. Config apply checks fresh progress samples; a counter before the display sink is evidence of pipeline progress, not proof that pixels reached HDMI or a physical window.

This implementation serializes native mutations with a Go mutex. It sets the old source to `NULL`, unlinks it, links the replacement, and synchronizes the new source with the parent pipeline. It does not implement a separate GLib main-context queue or pad-block/drain protocol. Those choices need device-specific interruption and resource measurements before calling every target production-qualified.

## Evidence and remaining gates

Synthetic GStreamer tests verify that a peer tile's frame counter and generation continue through source switches, same-URL H.264-to-H.265 selection, failed source creation, retry backoff, retained-still refresh, and source recovery. Bounded local RTSP tests on an Intel Mac exercised H.264/H.265 switching and source loss/restart while a peer kept decoding. A short actual macOS window run showed output and decoded frame progress and exited cleanly. These tests do not establish a 30-minute soak or Apple Silicon, Pi, or Debian display performance.

Before qualifying a hardware profile, run the recovery and soak matrix in `docs/plug-and-play-setup-plan.md` on the physical output: source freeze, publisher/station/network failure, bridge restart, codec change, all tiles idle, compositor restart, and interrupted config apply. Record output mode, tile geometry, decoded/output frame rates, displayed frame continuity, CPU, memory, decoder load, dropped frames, and recovery time. Verify plane/CRTC reachability on the chosen connector. Keep profiles without these measurements marked unverified in the client runbook.
