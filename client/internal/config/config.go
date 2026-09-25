// Package config loads the wall's YAML config and applies defaults + validation.
package config

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Span struct {
	Cols int `yaml:"cols"`
	Rows int `yaml:"rows"`
}

// Rect is a half-open rectangle on the logical canvas.
type Rect struct {
	X int `yaml:"x"`
	Y int `yaml:"y"`
	W int `yaml:"w"`
	H int `yaml:"h"`
}

type Canvas struct {
	Cols int `yaml:"cols"`
	Rows int `yaml:"rows"`
}

type Tile struct {
	ID     string `yaml:"id"`
	Rect   *Rect  `yaml:"rect"`
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
	SchemaVersion int    `yaml:"schema_version"`
	BridgeURL     string `yaml:"bridge_url"`
	RTSPBase      string `yaml:"rtsp_base"`
	Layout        string `yaml:"layout"`
	Canvas        Canvas `yaml:"canvas"`
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
	if c.SchemaVersion != 0 && c.SchemaVersion != 2 {
		return nil, fmt.Errorf("config: schema_version %d is unsupported; this client supports version 2 and legacy files without a version", c.SchemaVersion)
	}
	// v2 cannot silently drop an editor's unknown setting on import/export. Every version rejects
	// multiple documents so an operator cannot think a second document overrode the first.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(c.SchemaVersion == 2)
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("config: only one YAML document is allowed")
	}
	if c.BridgeURL != "" {
		u, err := url.Parse(c.BridgeURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("config: bridge_url must be an http(s) origin without credentials or a path")
		}
	}
	if c.SchemaVersion == 2 && c.BridgeURL == "" {
		return nil, fmt.Errorf("config: bridge_url is required in schema_version 2")
	}
	if c.SchemaVersion == 2 && c.RTSPBase == "" {
		return nil, fmt.Errorf("config: rtsp_base is required in schema_version 2")
	}
	if c.Layout == "" {
		if c.SchemaVersion == 2 && c.Canvas != (Canvas{}) {
			c.Layout = "custom"
		} else {
			c.Layout = "1"
		}
	}
	if c.PrimaryPosition == "" {
		c.PrimaryPosition = "left"
	}
	if c.Layout == "custom" && c.SchemaVersion != 2 {
		return nil, fmt.Errorf("config: layout custom requires schema_version: 2")
	}
	if c.Layout != "custom" {
		if c.Canvas != (Canvas{}) {
			return nil, fmt.Errorf("config: canvas is only valid with layout: custom")
		}
	}
	if c.Layout != "custom" {
		if _, _, ok := gridFor(c.Layout); !ok {
			return nil, fmt.Errorf("config: layout must be 1, 1+5, or <cols>x<rows> with each side 1-%d, e.g. 2x1 (two side by side), 2x2, 3x1 (got %q)", maxGridSide, c.Layout)
		}
	} else if c.Canvas.Cols < 1 || c.Canvas.Rows < 1 || c.Canvas.Cols > 32 || c.Canvas.Rows > 32 {
		return nil, fmt.Errorf("config: canvas.cols and canvas.rows must each be 1-32")
	}
	if c.Screen.Width < 0 || c.Screen.Height < 0 || (c.Screen.Width == 0) != (c.Screen.Height == 0) {
		return nil, fmt.Errorf("config: screen.width and screen.height must both be positive or both omitted")
	}
	if c.Output != "" && !validOutputName(c.Output) {
		return nil, fmt.Errorf("config: output must be a DRM connector name using ASCII letters, digits, or hyphens (got %q)", c.Output)
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
	if c.SchemaVersion == 2 && len(c.Tiles) > 32 {
		return nil, fmt.Errorf("config: at most 32 tile definitions are supported; host stream capacity may be lower")
	}
	if c.Layout != "custom" && len(c.Tiles) > cols*rows {
		return nil, fmt.Errorf("config: %d tiles do not fit layout %s (%d cells)", len(c.Tiles), c.Layout, cols*rows)
	}
	ids := map[string]bool{}
	for i, t := range c.Tiles {
		if c.SchemaVersion == 2 {
			if t.ID == "" || !validTileID(t.ID) {
				return nil, fmt.Errorf("config: tiles[%d].id must use 1-64 ASCII letters, digits, hyphens, or underscores", i)
			}
			if ids[t.ID] {
				return nil, fmt.Errorf("config: tiles[%d].id %q is duplicated", i, t.ID)
			}
			ids[t.ID] = true
		} else {
			// Legacy identities are positional because the old format had no persistent IDs.
			// Ignore an incidental id field so duplicate legacy camera tiles remain independent.
			c.Tiles[i].ID = fmt.Sprintf("legacy-%d", i)
		}
		if c.Layout == "custom" {
			if t.Rect == nil {
				return nil, fmt.Errorf("config: tiles[%d].rect is required for custom layout", i)
			}
			r := *t.Rect
			if r.X < 0 || r.Y < 0 || r.W < 1 || r.H < 1 || r.X+r.W > cols || r.Y+r.H > rows {
				return nil, fmt.Errorf("config: tiles[%d].rect is outside the %dx%d canvas", i, cols, rows)
			}
			if t.Span != nil || t.Aspect != "" || t.Role != "" {
				return nil, fmt.Errorf("config: tiles[%d] cannot use span, aspect, or role with a custom rect", i)
			}
			for j := 0; j < i; j++ {
				if rectanglesOverlap(r, *c.Tiles[j].Rect) {
					return nil, fmt.Errorf("config: tiles[%d].rect overlaps tiles[%d].rect", i, j)
				}
			}
		} else if t.Rect != nil {
			return nil, fmt.Errorf("config: tiles[%d].rect requires layout: custom", i)
		}
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
	if t.Camera == "" {
		return ""
	}
	return strings.TrimRight(c.RTSPBase, "/") + "/" + url.PathEscape(t.Camera)
}

// GridDims is the cell grid behind a layout.
func (c *Config) GridDims() (cols, rows int) {
	if c.Layout == "custom" {
		return c.Canvas.Cols, c.Canvas.Rows
	}
	cols, rows, _ = gridFor(c.Layout)
	return cols, rows
}

func validTileID(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validOutputName(s string) bool {
	if len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

func rectanglesOverlap(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
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
