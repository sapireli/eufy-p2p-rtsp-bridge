# Client configuration

`eufy-wall` reads `/etc/eufy-wall.yaml` by default on Linux and `~/Library/Application Support/eufy-wall/config.yaml` on macOS. Its setup wizard, editor, renderer, validation, and apply commands use the same Go config parser. A hand-written file is fully supported:

```sh
eufy-wall config example > wall.yaml
eufy-wall config validate wall.yaml
eufy-wall layout preview wall.yaml --png preview.png
eufy-wall probe wall.yaml T8214XXXXXXXXXXX
sudo eufy-wall config apply wall.yaml
# Or transfer a reviewed file over SSH without a config API:
cat wall.yaml | ssh pi 'sudo eufy-wall config apply -'
```

The `-` input means stdin. `config apply` stages validated YAML, checks bridge health, camera inventory, RTSP socket reachability, installed decoder elements, and the local layout, then backs up the active file. It restarts the wall, checks that the service stays active, and restores the previous config on failure. `eufy-wall probe <file|-> <camera-serial>` is an optional stronger check: it uses this host's decoder to receive two actual frames without taking over HDMI. A sleeping camera gets a hold capped at 20 seconds and the command releases it afterward. Run it for each camera whose live path must be proven before apply; a socket check alone cannot prove video. `config recover` is run by systemd before the service starts after an interrupted apply. `eufy-wall status --json` reports the active hash, most recent backup, and last rollback reason. Keep the backup until the new layout has run on the intended hardware. The client file contains no Eufy password or token.

## Complete custom layout

```yaml
schema_version: 2
bridge_url: http://192.168.1.10:3000
rtsp_base: rtsp://192.168.1.10:8554
output: HDMI-A-1
screen: {width: 1920, height: 1080}
decoder: auto
sink: auto
layout: custom
canvas: {cols: 32, rows: 32}
tiles:
  - id: front-door
    camera: T8214XXXXXXXXXXX
    rect: {x: 0, y: 0, w: 20, h: 32}
  - id: recent-motion
    motion: latest
    watch: [T81A0XXXXXXXXXXX, T8425XXXXXXXXXXX]
    blank_after_seconds: 90
    dwell_seconds: 10
    rect: {x: 20, y: 0, w: 12, h: 32}
```

Use a camera **serial** in `camera` and `watch`. The bridge reports its current stream key and codec at runtime. A fixed `on_demand` camera is held while visible and released when the wall stops showing it. A fixed `on_motion` camera sleeps between events. If the bridge has a retained thumbnail, a sleeping motion tile shows it until live frames arrive; otherwise the tile is dark. The GStreamer watchdog restarts a live tile whose decoded frames stall for 15 seconds. A camera's power mode is configured on the bridge; this file only chooses how to display it.

## Fields and defaults

| Field | Values and behavior |
| --- | --- |
| `schema_version` | `2` enables strict unknown-field errors and stable tile IDs. Omit for an existing legacy file. Other versions fail. |
| `bridge_url` | HTTP(S) origin for health, camera state, WebSocket events, stills, and holds. Required in v2. No path, credentials, query, or fragment. Legacy files derive the conventional port `3000` from `rtsp_base`. |
| `rtsp_base` | RTSP origin for video, such as `rtsp://bridge:8554`. Required in v2. A tile `url` overrides it for that tile. |
| `output` | DRM connector name, such as `HDMI-A-1` or `DP-1`. Empty selects the first connected output. Run one wall instance per monitor. |
| `screen` | Optional pixel `width` and `height`; specify both or neither. When omitted the renderer reads the selected DRM mode. Preview falls back to 1920×1080 when no mode is available. |
| `decoder` | `auto` (default), `v4l2`, `va`, or `software`. Auto prefers a Pi V4L2 decoder, then VA, then software according to installed elements. The actual codec must be supported by the chosen decoder. |
| `sink` | `auto` (default), `planes`, `compositor`, or `window`. Auto chooses planes when a V4L2 decoder and plane IDs are supplied, otherwise compositor when installed. `window` is useful on desktop hosts. |
| `planes` | DRM overlay plane IDs, one per tile index, used with `sink: planes`. Discover IDs with `modetest` and verify they work on the chosen connector. An explicit `output` is passed to kmssink when its connector ID is available. |
| `latency_ms` | RTSP latency; default `200`. Increase for a noisy link, decrease only after measuring drops. |
| `layout` | `custom`, `1`, `1+5`, or a preset grid such as `2x1` or `2x2` (each side 1–6). `custom` needs v2 and `canvas`. |
| `canvas` | `{cols, rows}` with each dimension 1–32, only for `custom`. Cells are placement units, not decoder slots. |
| `primary_position` | `left` (default) or `right` in the `1+5` preset. |
| `restart` | Optional `min_seconds`, `max_seconds`, and `stable_seconds`; defaults 1, 30, and 60 for pipeline backoff. |

Each tile must have a stable, unique `id` in v2 (1–64 ASCII letters, digits, `_`, or `-`). A fixed tile has `camera` or an explicit `url`. `motion: latest` selects the most recently moving camera in `watch`; an empty watch set follows all enabled cameras. `blank_after_seconds: 0` keeps the last selection, while a positive value blanks after inactivity. `dwell_seconds` limits rapid switching. The optional `codec` is `h264` or `h265`, used as an offline fallback until the bridge reports the active codec. For presets, `span: {cols, rows}`, `aspect: wide|tall`, and `role: primary` affect placement. These preset fields cannot be mixed with a custom `rect`.

With `layout: custom`, every tile needs `rect: {x, y, w, h}`. Coordinates start at zero. The rectangle must fit the canvas and must not overlap another tile; gaps are permitted and appear black. The renderer converts each rectangle edge to pixels independently, so 32×32 arrangements cover odd-size displays without cumulative rounding gaps. See [layouts.md](layouts.md) for editor commands and previews.

## Setup and offline inventory

`sudo eufy-wall setup` asks for the bridge HTTP and RTSP addresses, display output, camera serials, and a starter template. It shows a summary before applying. To prepare a draft without restarting the service, pass `--output wall.draft.yaml`. On a headless host, use an answer file:

```yaml
bridge_url: http://192.168.1.10:3000
rtsp_base: rtsp://192.168.1.10:8554
output: HDMI-A-1
cameras: [T8214XXXXXXXXXXX, T8425XXXXXXXXXXX]
template: split
probe_streams: true  # optional: decode two frames from each selected camera before apply
# inventory_file: /tmp/cameras.json  # exported by eufy-bridge for offline setup
```

```sh
eufy-wall setup --answers answers.yaml --output wall.draft.yaml
eufy-wall config validate wall.draft.yaml
```

Without `inventory_file`, setup reads `/api/cameras` from the bridge. It sends no config over HTTP. Interactive setup asks whether to run decoded-frame probes; answer files opt in with `probe_streams: true`. A failed probe leaves the active config unchanged. An offline inventory cannot run a live probe until the bridge is reachable. The `one`, `split`, `four`, and `motion` templates write ordinary v2 YAML that can be edited afterward. For a layout outside these templates, use `layout edit` or hand-write rectangles. If inventory codec data is missing or stale, the bounded probe tries H.264 and H.265 with the selected decoder. A successful probe proves two decoded frames; test the actual display before relying on the wall.

## Diagnostics and recovery

`eufy-wall doctor --json` reports the detected screen, decoder, sink, watchdog element, renderer binary, bridge health, and config errors; it exits nonzero when a check fails. `eufy-wall -config wall.yaml -dry-run` prints the resolved GStreamer plans. A config can pass schema checks but still fail on a host with missing DRM planes, unsupported H.265 decoding, or a disconnected display. Run `probe` for stream and decoder progress, then use the target display for final checks. `journalctl -u eufy-wall -f` shows stream and pipeline recovery. If an apply fails, the command restores the prior bytes and restarts the old service; inspect the dated `.bak.*` files before deleting them.

On a network outage, the wall reconnects its WebSocket and refreshes the camera snapshot. Pipeline restarts use bounded backoff. Fixed on-demand holds are bounded by the bridge and expire even if the display disappears. A compositor currently remains one process, so changing a tile restarts the entire compositor; use tested DRM planes when unaffected tiles must remain continuous. This limitation remains a release gate in the setup plan.
