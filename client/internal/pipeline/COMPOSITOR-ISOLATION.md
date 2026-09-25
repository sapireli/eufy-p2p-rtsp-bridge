# Compositor tile isolation: feasibility record

Status: **not implemented**. The current `sink: compositor` and `sink: window` paths run one `gst-launch-1.0` process for every source. Changing any source changes that process's arguments, so the supervisor replaces the entire wall. `sink: planes` already has one process per tile.

## Why the process split is not a production fix

The [GStreamer `gst-launch-1.0` documentation](https://gstreamer.freedesktop.org/documentation/tools/gst-launch.html) describes it as a pipeline construction and debugging tool and recommends an application using the GStreamer API for application behavior. There is no supported command in the current client that can replace one branch inside an already-running `gst-launch-1.0` process. [GStreamer's live-pipeline guide](https://gstreamer.freedesktop.org/documentation/application-development/advanced/pipeline-manipulation.html) requires blocking the branch's data flow, unlinking pads, draining or flushing the old elements, linking new elements, and synchronizing their state with the pipeline. A different command line starts a different pipeline and display sink.

An attempted shared-memory split was rejected in a local probe: the `shmsrc` consumer exited when its producer died, so the compositor still died when a tile producer failed. A separate local UDP/JPEG probe did not establish a working alternative: the receiver reported repeated `Impossible to configure latency` warnings and produced no measured frames, including after a producer connected. That probe is evidence against adopting its exact pipeline, **not** proof that all UDP designs are impossible. [GStreamer's `udpsrc` documentation](https://gstreamer.freedesktop.org/documentation/udp/udpsrc.html) also describes timeout as a bus message, so a static `gst-launch` pipeline would still need an application to handle recovery. A robust receiver would need packet framing, timestamps, codec changes, fallback frames, socket lifecycle, and measured copy/decode cost. `fallbackswitch` offers automatic timeout switching, but it is a separate [GStreamer Rust plugin](https://gstreamer.freedesktop.org/documentation/fallbackswitch/fallbackswitch.html), absent from the current Debian installer package list.

The existing client builds with `CGO_ENABLED=0` for armv6, armv7, arm64, and amd64 in `client/Makefile`; `.github/workflows/release.yml` packages those outputs. The installer installs GStreamer runtime tools and plugins, but no native development libraries. A native GStreamer renderer therefore changes the build and release contract. A pure-Go relay of decoded frames would copy full raw frames between processes and needs a measured Pi memory/CPU budget before it can be considered.

## Production implementation design

Implement a Go-owned GStreamer application using native bindings, while keeping `eufy-wall` as the sole client command and the YAML format unchanged. The renderer should own one long-running `GstPipeline` containing the compositor, a fixed output caps filter, and the selected display sink. A permanent live black frame source keeps the compositor clock and output alive when every camera tile is blank.

For each stable tile ID, create an independently managed source bin. A live bin contains RTSP source, codec-specific depayloader/parser/decoder, watchdog, converter, and bounded queue; a still bin contains HTTP JPEG source, decoder, and freeze element. Link each bin to a request pad on the compositor. Keep one request pad per configured tile, with explicit geometry. When a tile changes source, codec, or content:

1. Prepare and validate the replacement bin without touching the other tile bins.
2. Block the old bin's output pad, drain or flush it, unlink it, and set it to `NULL`.
3. Link the new bin to the same compositor pad, synchronize its state with the parent, then unblock.
4. If the replacement cannot produce frames within a bounded deadline, show black or the retained still in that rectangle and report a tile-scoped error. Keep the other bins and display sink running.

GStreamer bus errors must be attributed to their tile bin. A tile source or watchdog error should rebuild only that bin; compositor or display-sink errors may require a full pipeline restart. All native mutations should run on one GLib main context, with Go requests serialized onto it. The supervisor should expose tile-scoped state and frame progress to `doctor`, setup, health checks, and logs. Mixed H.264/H.265 transitions rebuild the affected bin with the correct decoder. Initial loading, blanking, recovery, and shutdown need bounded timeouts.

The release work must add native Linux builds for every supported architecture, pin GStreamer ABI and Go binding versions, package or declare the correct runtime libraries, and verify startup on Debian and Pi OS. The current static cross-build cannot be treated as proof for a native renderer. Preserve the existing `gst-launch` path for diagnostics during migration, then remove it from the supported compositor runtime once the native path passes hardware qualification.

## Verification required before enabling it

- On each supported Pi and Debian x86 profile, run two active tiles; switch one camera and confirm the other tile's frame counter continues without reset or gap beyond the display frame budget.
- Kill one RTSP source, its station, and its tile decoder; confirm only that tile goes blank/reconnects while the other remains live. Repeat with the source frozen but the process alive.
- Switch a motion tile between H.264 and H.265, then between live and a retained still. Check that the selected decoder and tile-scoped recovery remain correct.
- Start with zero, one, and all cameras idle; verify a black output continues at the chosen mode without EOS or stale last frame.
- Restart the bridge, unplug network, restart the compositor, and interrupt config apply. Check bounded recovery and rollback, plus 30-minute soak measurements of frame progress, memory, CPU, decoder utilization, and dropped frames.

Until these checks pass, `sink: compositor` cannot be advertised as supporting independent tile changes. The current setup and runbook should keep warning that a source change restarts the compositor.
