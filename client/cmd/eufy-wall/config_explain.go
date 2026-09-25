package main

import "strings"

// Keep these short descriptions next to the CLI; docs/config-client.md has the full examples.
var clientConfigFields = map[string]string{
	"schema_version":              "2 enables strict unknown-field checks and stable tile IDs; omit for legacy YAML",
	"bridge_url":                  "required v2 HTTP(S) control origin for inventory, events, stills, and holds; no path or credentials",
	"rtsp_base":                   "required v2 RTSP origin for camera video; tiles[].url overrides it for that tile",
	"output":                      "Linux DRM connector name such as HDMI-A-1; leave empty for the first connected output or a macOS window",
	"screen":                      "optional display pixel dimensions; set both width and height or neither",
	"screen.width":                "positive display width in pixels; requires screen.height",
	"screen.height":               "positive display height in pixels; requires screen.width",
	"decoder":                     "auto (default), v4l2, va, or software; the chosen decoder must support each source codec",
	"sink":                        "auto (default), planes, compositor, or window; macOS uses a window",
	"planes":                      "Linux DRM plane IDs, one per tile, used with sink: planes; verify connector routing with doctor",
	"latency_ms":                  "RTSP receive latency in milliseconds; default 200",
	"layout":                      "custom needs explicit rectangles; legacy presets are 1, 1+5, or a 1..6 by 1..6 grid",
	"canvas":                      "logical placement grid for layout: custom, with 1..32 columns and rows",
	"canvas.cols":                 "logical canvas width in cells, 1..32; does not set stream capacity",
	"canvas.rows":                 "logical canvas height in cells, 1..32; does not set stream capacity",
	"primary_position":            "left (default) or right for the legacy 1+5 preset",
	"restart":                     "pipeline retry settings; defaults min_seconds: 1, max_seconds: 30, stable_seconds: 60",
	"restart.min_seconds":         "minimum pipeline retry delay in seconds; default 1",
	"restart.max_seconds":         "maximum pipeline retry delay in seconds; default 30, at least min_seconds",
	"restart.stable_seconds":      "healthy runtime before retry backoff resets; default 60 seconds",
	"tiles":                       "one or more source tiles with a stable id; v2 permits up to 32 definitions, subject to host capacity",
	"tiles[].id":                  "stable unique tile ID in v2, using 1..64 ASCII letters, digits, underscore, or hyphen",
	"tiles[].camera":              "camera serial for a fixed tile; the bridge supplies its current stream key and codec",
	"tiles[].url":                 "explicit RTSP URL for a fixed tile; overrides rtsp_base and camera stream lookup",
	"tiles[].motion":              "latest selects the most recently moving watched camera for this tile",
	"tiles[].watch":               "camera serials followed by a motion tile; empty watches all enabled cameras",
	"tiles[].blank_after_seconds": "seconds without motion before the tile blanks; 0 keeps the last selection",
	"tiles[].dwell_seconds":       "minimum seconds before a motion tile switches to another camera",
	"tiles[].codec":               "h264 or h265 offline fallback; live bridge inventory takes precedence",
	"tiles[].rect":                "required custom-layout rectangle with x, y, w, h grid cells; overlaps are invalid",
	"tiles[].rect.x":              "zero-based horizontal cell position on the logical canvas",
	"tiles[].rect.y":              "zero-based vertical cell position on the logical canvas",
	"tiles[].rect.w":              "positive rectangle width in cells; x+w must fit canvas.cols",
	"tiles[].rect.h":              "positive rectangle height in cells; y+h must fit canvas.rows",
	"tiles[].span":                "legacy preset tile span in columns and rows; cannot be used with a custom rectangle",
	"tiles[].span.cols":           "legacy preset tile width in grid cells",
	"tiles[].span.rows":           "legacy preset tile height in grid cells",
	"tiles[].aspect":              "legacy preset placement hint: wide or tall; cannot be used with a custom rectangle",
	"tiles[].role":                "legacy 1+5 primary tile marker; cannot be used with a custom rectangle",
}

var clientConfigFieldOrder = []string{
	"schema_version", "bridge_url", "rtsp_base", "output", "screen", "screen.width", "screen.height",
	"decoder", "sink", "planes", "latency_ms", "layout", "canvas", "canvas.cols", "canvas.rows",
	"primary_position", "restart", "restart.min_seconds", "restart.max_seconds", "restart.stable_seconds",
	"tiles", "tiles[].id", "tiles[].camera", "tiles[].url", "tiles[].motion", "tiles[].watch",
	"tiles[].blank_after_seconds", "tiles[].dwell_seconds", "tiles[].codec", "tiles[].rect",
	"tiles[].rect.x", "tiles[].rect.y", "tiles[].rect.w", "tiles[].rect.h", "tiles[].span",
	"tiles[].span.cols", "tiles[].span.rows", "tiles[].aspect", "tiles[].role",
}

func explainClientField(path string) (string, bool) {
	if value, ok := clientConfigFields[path]; ok {
		return value, true
	}
	if !strings.HasPrefix(path, "tiles[") {
		return "", false
	}
	end := strings.IndexByte(path, ']')
	if end <= len("tiles[") {
		return "", false
	}
	for _, digit := range path[len("tiles["):end] {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	value, ok := clientConfigFields["tiles[]"+path[end+1:]]
	return value, ok
}
