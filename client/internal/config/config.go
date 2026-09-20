// Package config loads the wall's YAML config and applies defaults + validation.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Span struct {
	Cols int `yaml:"cols"`
	Rows int `yaml:"rows"`
}

type Tile struct {
	Camera string `yaml:"camera"`
	Role   string `yaml:"role"`   // "" | "primary"
	Aspect string `yaml:"aspect"` // "" | "wide" | "tall"
	// Motion turns this into a dynamic tile: "latest" shows whichever watched camera moved most
	// recently. Empty is a normal fixed tile.
	Motion string `yaml:"motion"`
	// Watch is the set of camera serials a motion tile follows; empty means every enabled camera. Not
	// limited to battery cameras — a wired one costs nothing extra here, since it is already streaming
	// and the tile only switches URL.
	Watch []string `yaml:"watch"`
	// BlankAfterSeconds blanks a motion tile when nothing it watches has moved for this long. This is
	// what makes a screen that TURNS ON for motion, rather than one permanently showing the last thing
	// that moved. 0 means never blank.
	BlankAfterSeconds int `yaml:"blank_after_seconds"`
	// DwellSeconds is the minimum time a motion tile stays on a camera before it may switch again, so
	// two cameras firing together cannot make the tile strobe.
	DwellSeconds int `yaml:"dwell_seconds"`
	// Codec the camera actually sends, so the pipeline picks a matching decoder: "" (=h264) | h264 | h265.
	// Set h265 where the bridge passes the camera through untranscoded — several eufy models encode HEVC
	// at every quality tier, and decoding it here avoids paying for a transcode on the server.
	Codec string `yaml:"codec"`
	Span  *Span  `yaml:"span"`
	URL   string `yaml:"url"`
}

type Screen struct {
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}

type Restart struct {
	MinSeconds    int `yaml:"min_seconds"`
	MaxSeconds    int `yaml:"max_seconds"`
	StableSeconds int `yaml:"stable_seconds"`
}

type Config struct {
	RTSPBase string `yaml:"rtsp_base"`
	Layout   string `yaml:"layout"`
	// Output names the display this instance drives, as a DRM connector: "HDMI-A-1", "HDMI-A-2", "DP-1".
	// Empty means the first connected output, which is the single-screen case. Naming it is what lets one
	// instance per monitor each show its own cameras: each drives its own CRTC, so each keeps the cheap
	// hardware-plane path instead of compositing one framebuffer spanned across both.
	Output          string  `yaml:"output"`
	PrimaryPosition string  `yaml:"primary_position"`
	Screen          Screen  `yaml:"screen"`
	Decoder         string  `yaml:"decoder"`
	Sink            string  `yaml:"sink"`
	Planes          []int   `yaml:"planes"`
	Latency         int     `yaml:"latency_ms"`
	Tiles           []Tile  `yaml:"tiles"`
	Restart         Restart `yaml:"restart"`
}

// Named layouts. Everything else is a plain "<cols>x<rows>" grid parsed by gridFor, so a wall is not
// limited to the shapes someone thought to name: "2x1" is two tiles side by side at full height (the
// natural fit for two portrait dual-lens cameras on a 16:9 screen), "3x1" is three across, "1x2" stacks
// a pair for a rotated panel.
var layouts = map[string][2]int{"1": {1, 1}, "1+5": {3, 3}}

const maxGridSide = 6

// gridFor resolves a layout name to its cell grid: a named layout, else "<cols>x<rows>".
func gridFor(layout string) (cols, rows int, ok bool) {
	if d, named := layouts[layout]; named {
		return d[0], d[1], true
	}
	c, r, found := strings.Cut(layout, "x")
	cols, errC := strconv.Atoi(c)
	rows, errR := strconv.Atoi(r)
	if !found || errC != nil || errR != nil || cols < 1 || rows < 1 || cols > maxGridSide || rows > maxGridSide {
		return 0, 0, false
	}
	return cols, rows, true
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(data []byte) (*Config, error) {
	c := &Config{Decoder: "auto", Sink: "auto", Latency: 200, Restart: Restart{1, 30, 60}}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if c.Layout == "" {
		c.Layout = "1"
	}
	if c.PrimaryPosition == "" {
		c.PrimaryPosition = "left"
	}
	if _, _, ok := gridFor(c.Layout); !ok {
		return nil, fmt.Errorf("config: layout must be 1, 1+5, or <cols>x<rows> with each side 1-%d, e.g. 2x1 (two side by side), 2x2, 3x1 (got %q)", maxGridSide, c.Layout)
	}
	if c.Layout == "1+5" {
		switch c.PrimaryPosition {
		case "left", "right":
		case "center":
			return nil, fmt.Errorf("config: primary_position center is impossible in a 3-column grid (a 2x2 block cannot be centred); use left or right")
		default:
			return nil, fmt.Errorf("config: primary_position must be left or right (got %q)", c.PrimaryPosition)
		}
	}
	switch c.Decoder {
	case "auto", "v4l2", "va", "software":
	default:
		return nil, fmt.Errorf("config: decoder must be auto|v4l2|va|software (got %q)", c.Decoder)
	}
	switch c.Sink {
	case "auto", "planes", "compositor", "window":
	default:
		return nil, fmt.Errorf("config: sink must be auto|planes|compositor|window (got %q)", c.Sink)
	}
	if c.Restart.MinSeconds < 1 {
		return nil, fmt.Errorf("config: restart.min_seconds must be >= 1 (got %d)", c.Restart.MinSeconds)
	}
	if c.Restart.MaxSeconds < c.Restart.MinSeconds {
		return nil, fmt.Errorf("config: restart.max_seconds (%d) must be >= restart.min_seconds (%d)", c.Restart.MaxSeconds, c.Restart.MinSeconds)
	}
	if len(c.Tiles) == 0 {
		return nil, fmt.Errorf("config: at least one tile is required")
	}
	cols, rows := c.GridDims()
	if len(c.Tiles) > cols*rows {
		return nil, fmt.Errorf("config: %d tiles do not fit layout %s (%d cells)", len(c.Tiles), c.Layout, cols*rows)
	}
	for i, t := range c.Tiles {
		if t.Camera == "" && t.URL == "" && t.Motion == "" {
			return nil, fmt.Errorf("config: tiles[%d] needs camera, url, or motion: latest", i)
		}
		if t.URL == "" && c.RTSPBase == "" && t.Motion == "" {
			return nil, fmt.Errorf("config: rtsp_base is required when a tile has no url (tiles[%d])", i)
		}
		switch t.Aspect {
		case "", "wide", "tall":
		default:
			return nil, fmt.Errorf("config: tiles[%d].aspect must be wide or tall", i)
		}
		if t.Motion != "" && t.Motion != "latest" {
			return nil, fmt.Errorf("config: tiles[%d].motion must be latest (got %q)", i, t.Motion)
		}
		if t.Motion == "" && (len(t.Watch) > 0 || t.BlankAfterSeconds > 0 || t.DwellSeconds > 0) {
			return nil, fmt.Errorf("config: tiles[%d] sets watch/blank_after_seconds/dwell_seconds but is not a motion tile", i)
		}
		if t.Motion != "" && t.Camera != "" {
			return nil, fmt.Errorf("config: tiles[%d] is a motion tile, so it follows `watch` rather than a fixed camera", i)
		}
		switch t.Codec {
		case "", "h264", "h265":
		default:
			return nil, fmt.Errorf("config: tiles[%d].codec must be h264 or h265 (got %q)", i, t.Codec)
		}
		if t.Role != "" && t.Role != "primary" {
			return nil, fmt.Errorf("config: tiles[%d].role must be primary or empty", i)
		}
		if t.Span != nil && (t.Span.Cols < 1 || t.Span.Rows < 1 || t.Span.Cols > cols || t.Span.Rows > rows) {
			return nil, fmt.Errorf("config: tiles[%d].span out of range", i)
		}
	}
	return c, nil
}

// TileURL is the RTSP url for a tile: explicit url, else rtsp_base/camera.
func (c *Config) TileURL(t Tile) string {
	if t.URL != "" {
		return t.URL
	}
	return strings.TrimRight(c.RTSPBase, "/") + "/" + t.Camera
}

// GridDims is the cell grid behind a layout.
func (c *Config) GridDims() (cols, rows int) {
	cols, rows, _ = gridFor(c.Layout)
	return cols, rows
}

// TileFor finds the configured tile for a camera, so a dynamic tile that lands on it can inherit what
// the operator said about that camera — its codec above all, since decoding H.265 as H.264 fails.
func (c *Config) TileFor(camera string) *Tile {
	for i := range c.Tiles {
		if c.Tiles[i].Camera == camera {
			return &c.Tiles[i]
		}
	}
	return nil
}
