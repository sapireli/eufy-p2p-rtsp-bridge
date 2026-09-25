# Client layouts and terminal editor

The client accepts hand-written YAML and a keyboard-only editor in the `eufy-wall` binary. A layout is a set of video tiles on one display output. The logical canvas can have 1–32 columns and 1–32 rows; this controls placement precision, not the number of simultaneous streams. A v2 file can define up to 32 tiles, but the display's practical stream count depends on its decoder, DRM planes, codec mix, and measured performance.

## Start with an editable configuration

```sh
eufy-wall config example > eufy-wall.yaml
eufy-wall config validate eufy-wall.yaml
eufy-wall layout edit eufy-wall.yaml
```

The example has two side-by-side tiles. Replace the placeholder camera serials and bridge address. `bridge_url` is the bridge's HTTP control origin (usually port 3000); `rtsp_base` is its RTSP origin (usually port 8554). Set `output` when more than one display is connected. `screen` can be omitted for automatic mode detection. Each tile `id` is stable across edits and must be unique; a camera may appear in several tiles with different IDs.

```yaml
schema_version: 2
bridge_url: http://bridge.local:3000
rtsp_base: rtsp://bridge.local:8554
layout: custom
canvas: {cols: 32, rows: 32}
tiles:
  - id: front-door
    camera: T8214XXXXXXXXXXX
    rect: {x: 0, y: 0, w: 20, h: 32}
  - id: recent-motion
    motion: latest
    watch: [T81A0XXXXXXXXXXX]
    blank_after_seconds: 90
    rect: {x: 20, y: 0, w: 12, h: 32}
```

Rectangles use integer half-open bounds. `x` and `y` start at zero; `w` and `h` must be positive; the rectangle must fit the canvas. Tiles may touch edges and leave black gaps. They cannot overlap. A custom tile cannot also use legacy `span`, `aspect`, or `role`. Every edge is converted independently to pixels with `floor(edge × screen size ÷ canvas size)`, which keeps adjacent edges aligned on odd-sized outputs. A tile that becomes zero pixels wide or high at the selected mode is rejected.

## Edit with the terminal

`eufy-wall layout edit [file]` uses a line menu that works in monochrome terminals and through SSH. Omit the file to start from the built-in v2 example. Type `help` to see commands. The canvas uses one character per tile and `.` for empty space, followed by exact grid and pixel coordinates for every tile. The text view is scaled to the configured display aspect ratio; it does not require an 80×32 terminal or a mouse.

Useful commands:

```text
template split                     # one | split | four | one-plus-five | motion
add driveway T8425XXXXXXXXXXX 0 0 16 32
rect driveway 0 0 20 32
move driveway 1 0                 # signed offset in grid cells
resize driveway -1 0              # signed change in width and height
camera driveway T8425XXXXXXXXXXX
motion driveway T81A0XXXXXXXXXXX,T8425XXXXXXXXXXX 90 5
motion driveway all 90 5          # watch every camera
delete driveway
undo
redo
png driveway-preview.png
inventory                         # or inventory exported.json
save driveway.draft.yaml
apply
quit
```

Every edit is validated before it enters history. An invalid edit reports the reason and leaves the current draft intact. `undo` and `redo` operate on validated states. `save` writes a draft with mode `0600`; without a path it writes `eufy-wall.draft.yaml` in the current directory. It refuses the active `/etc/eufy-wall.yaml` path. `apply` uses the same validation, backup, service check, and rollback path as `eufy-wall config apply`; quitting or losing the SSH session does not change the active config. Template slots beyond the current camera list use `CAMERA_N` placeholders, which must be replaced before applying a real wall.

Type `inventory` in the editor to fetch camera serials, names, codecs, and modes from the configured bridge, or `inventory exported.json` to load a sanitized inventory exported on the server. Unknown fixed cameras and unknown motion watch serials then appear as warnings next to the canvas. The editor checks inventory before `apply` and refuses unknown serials. A reachable bridge or a loaded export is required for the editor's apply command. An offline draft can still be saved and checked later.

If an imported file is unversioned, the editor shows a migration summary with the derived `bridge_url`, a custom canvas matching the old grid, tile IDs, and explicit rectangles. Type `MIGRATE` to accept an in-memory v2 draft, then `save` or `apply`; any other answer leaves the original file untouched. Migration rejects unknown fields that it cannot preserve. Verify the derived bridge URL before applying. A v2 preset such as `layout: 2x2` is valid to run, but the editor requires `layout: custom` so every edit has an explicit rectangle.

## Preview on SSH or HDMI

```sh
eufy-wall layout preview eufy-wall.yaml
eufy-wall layout preview eufy-wall.yaml --png preview.png
eufy-wall layout preview eufy-wall.yaml --width 1919 --height 1079 --png odd-size.png
eufy-wall layout preview eufy-wall.yaml --display
```

The text and PNG previews use the same placement engine as the runtime. The PNG shows colored, numbered rectangles, gaps in dark gray, and an inset border on legacy letterboxed tiles. `--display` shows the same PNG on the selected DRM connector through GStreamer until Ctrl-C; it opens no camera streams. This verifies geometry and output selection, not codec or RTSP reachability. Run `eufy-wall doctor` and check the live display before trusting an unmeasured hardware profile.

For hand-written YAML, run `eufy-wall config validate file.yaml`, then `eufy-wall config apply file.yaml`. To send a file over SSH without an HTTP endpoint, use `ssh host 'sudo eufy-wall config apply -' < file.yaml`. The CLI reads stdin once and uses the same safe apply transaction as an editor-generated file.

## Legacy preset layouts

Unversioned configs keep `layout: 1`, `1+5`, and `<cols>x<rows>` with sides 1–6. Tiles are placed by first fit in row order; `1+5` reserves a 2×2 primary tile, and `span` or `aspect: tall` can change a tile's cell use. The legacy format remains valid for runtime and `config validate`. New designs should use v2 custom rectangles so a saved placement is explicit. A layout is per display: run a separate client instance and config per output rather than spanning DRM planes across monitors.
