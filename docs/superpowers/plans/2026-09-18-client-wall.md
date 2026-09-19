# eufy-wall (display client) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single static Go binary that turns a layout config into one GStreamer pipeline showing N RTSP camera tiles on a headless Pi/x86 HDMI screen, and keeps it running.

**Architecture:** `eufy-wall` = config → layout engine (cell grid with spans; tall tiles 1×2) → pipeline generator (per-tile `rtspsrc → depay → parse → HW decoder → sink`, either N `kmssink`s on DRM planes or one `compositor → kmssink`) → supervisor that execs `gst-launch-1.0` and restarts it with backoff. No cgo, no WebSocket in Phase 1 (the server keeps wired cameras always-on, so tiles are static). Cross-compiled for Pi 1 (`GOARM=6`), Pi 3 (`GOARM=7`), arm64 and amd64.

**Tech Stack:** Go 1.23 (`go test`), `gopkg.in/yaml.v3`; on the target: GStreamer 1.22+ (`gstreamer1.0-tools`, `-plugins-base/good/bad`, `-libav` for software fallback), Raspberry Pi OS Lite Bookworm/Trixie with `vc4-kms-v3d`.

**Spec:** `docs/superpowers/specs/2026-09-18-eufy-wall-design.md`

## Global Constraints

- Pure Go, `CGO_ENABLED=0`; only external runtime dependency is `gst-launch-1.0` (+ `gst-inspect-1.0` for auto-detect).
- Layouts: `1`, `2x2`, `3x3`, `1+5`. `1+5` primary spans 2×2 in a 3×3 grid at `left` (cols 0–1) or `right` (cols 1–2). A horizontally centred 2×2 is geometrically impossible in a 3-column grid; `primary_position: center` is rejected with an explanatory error.
- Tall (split-view dual-lens) tiles get span 1 col × 2 rows; if that doesn't fit, they fall back to 1×1 and letterbox. Every sink keeps aspect ratio.
- The pipeline is one process; any tile failure restarts all tiles (accepted for Phase 1).
- Sink mode (`planes` vs `compositor`) and decoder are config-selectable; defaults chosen by Spike B on real hardware (Task 6).
- No Docker. Deployment = `deploy/install-client.sh` + systemd unit.

## File structure

```
client/
  go.mod                         module eufy-wall, go 1.23, require gopkg.in/yaml.v3
  Makefile                       build targets pi1/pi3/pi64/amd64/host, test
  cmd/eufy-wall/main.go          flags (-config, -dry-run, -print-layout), wiring, signal handling
  internal/config/config.go      YAML → Config (+validation, defaults)
  internal/config/config_test.go
  internal/layout/layout.go      Place(cfg) → []Placed (pure)
  internal/layout/layout_test.go
  internal/pipeline/pipeline.go  Build(cfg, placed, caps) → []string args for gst-launch-1.0 (pure)
  internal/pipeline/pipeline_test.go
  internal/detect/detect.go      screen size from /sys/class/drm, decoder from gst-inspect (overridable)
  internal/detect/detect_test.go
  internal/supervisor/supervisor.go   run+restart loop with backoff; log forwarding
  internal/supervisor/supervisor_test.go
  config.example.yaml
  spikes/spike-b.sh              throwaway gst-launch experiments for the Pi (planes vs compositor)
deploy/
  eufy-wall.service
  install-client.sh
docs/
  runbook-client.md              Pi setup, config, troubleshooting, Spike B results
```

Shared types (defined in Task 1/2, used everywhere):

```go
// internal/config
type Span struct{ Cols, Rows int }
type Tile struct {
    Camera string  `yaml:"camera"`
    Role   string  `yaml:"role"`   // "" | "primary"
    Aspect string  `yaml:"aspect"` // "" | "wide" | "tall"
    Span   *Span   `yaml:"span"`   // explicit override
    URL    string  `yaml:"url"`    // optional full RTSP url; default RTSPBase + "/" + Camera
}
type Screen struct{ Width, Height int }
type Config struct {
    RTSPBase        string   `yaml:"rtsp_base"`
    Layout          string   `yaml:"layout"`           // 1 | 2x2 | 3x3 | 1+5
    PrimaryPosition string   `yaml:"primary_position"` // left | right (1+5)
    Screen          Screen   `yaml:"screen"`           // 0,0 = auto-detect
    Decoder         string   `yaml:"decoder"`          // auto | v4l2 | va | software
    Sink            string   `yaml:"sink"`             // auto | planes | compositor | window
    Planes          []int    `yaml:"planes"`           // DRM plane ids for sink=planes (from modetest)
    Latency         int      `yaml:"latency_ms"`       // rtspsrc latency, default 200
    Tiles           []Tile   `yaml:"tiles"`
    Restart         Restart  `yaml:"restart"`
}
type Restart struct{ MinSeconds, MaxSeconds, StableSeconds int } // 1, 30, 60

// internal/layout
type Placed struct {
    Index     int    // tile index in cfg.Tiles
    Camera    string
    URL       string
    Col, Row  int
    Cols, Rows int
    X, Y, W, H int   // pixels
    Letterbox bool   // tall tile that had to fall back to 1×1
}
```

---

### Task 1: Module skeleton + config loader

**Files:**
- Create: `client/go.mod`, `client/internal/config/config.go`, `client/internal/config/config_test.go`, `client/config.example.yaml`, `client/Makefile`

**Interfaces:**
- Produces: `config.Load(path string) (*Config, error)`, `config.Parse(data []byte) (*Config, error)`, `(*Config).TileURL(t Tile) string`, `(*Config).GridDims() (cols, rows int)`.

- [ ] **Step 1: `client/go.mod`**

```
module eufy-wall

go 1.23

require gopkg.in/yaml.v3 v3.0.1
```
Run: `cd client && go mod tidy` (creates go.sum once a file imports yaml).

- [ ] **Step 2: Write the failing test `client/internal/config/config_test.go`**

```go
package config

import (
	"strings"
	"testing"
)

const good = `
rtsp_base: rtsp://192.168.1.10:8554
layout: 1+5
primary_position: left
decoder: auto
tiles:
  - { camera: A, role: primary }
  - { camera: B, aspect: tall }
  - { camera: C }
  - { camera: D, span: { cols: 1, rows: 1 } }
  - { camera: E, url: rtsp://other:8554/E }
`

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if c.Layout != "1+5" || c.PrimaryPosition != "left" {
		t.Fatalf("layout %q pos %q", c.Layout, c.PrimaryPosition)
	}
	if c.Sink != "auto" || c.Decoder != "auto" || c.Latency != 200 {
		t.Fatalf("defaults: sink=%q decoder=%q latency=%d", c.Sink, c.Decoder, c.Latency)
	}
	if c.Restart != (Restart{MinSeconds: 1, MaxSeconds: 30, StableSeconds: 60}) {
		t.Fatalf("restart defaults %+v", c.Restart)
	}
	if got := c.TileURL(c.Tiles[0]); got != "rtsp://192.168.1.10:8554/A" {
		t.Fatalf("url %q", got)
	}
	if got := c.TileURL(c.Tiles[4]); got != "rtsp://other:8554/E" {
		t.Fatalf("explicit url %q", got)
	}
	if c.Tiles[3].Span == nil || *c.Tiles[3].Span != (Span{1, 1}) {
		t.Fatalf("span %+v", c.Tiles[3].Span)
	}
	cols, rows := c.GridDims()
	if cols != 3 || rows != 3 {
		t.Fatalf("dims %dx%d", cols, rows)
	}
}

func TestGridDims(t *testing.T) {
	for _, tc := range []struct {
		layout     string
		cols, rows int
	}{{"1", 1, 1}, {"2x2", 2, 2}, {"3x3", 3, 3}, {"1+5", 3, 3}} {
		c := &Config{Layout: tc.layout}
		if a, b := c.GridDims(); a != tc.cols || b != tc.rows {
			t.Errorf("%s → %dx%d", tc.layout, a, b)
		}
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]string{
		"no tiles":       "rtsp_base: rtsp://x\nlayout: 1\n",
		"bad layout":     "rtsp_base: rtsp://x\nlayout: 4x4\ntiles: [{camera: A}]\n",
		"center":         "rtsp_base: rtsp://x\nlayout: 1+5\nprimary_position: center\ntiles: [{camera: A}]\n",
		"no base no url": "layout: 1\ntiles: [{camera: A}]\n",
		"bad aspect":     "rtsp_base: rtsp://x\nlayout: 1\ntiles: [{camera: A, aspect: square}]\n",
		"bad sink":       "rtsp_base: rtsp://x\nlayout: 1\nsink: magic\ntiles: [{camera: A}]\n",
		"too many tiles": "rtsp_base: rtsp://x\nlayout: 2x2\ntiles: [{camera: A},{camera: B},{camera: C},{camera: D},{camera: E}]\n",
	}
	for name, y := range cases {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected error", name)
		} else if name == "center" && !strings.Contains(err.Error(), "3-column") {
			t.Errorf("center error should explain: %v", err)
		}
	}
}
```

- [ ] **Step 3: Run to verify failure** — `cd client && go test ./internal/config/` → FAIL (undefined: Parse …).

- [ ] **Step 4: Implement `client/internal/config/config.go`**

```go
// Package config loads the wall's YAML config and applies defaults + validation.
package config

import (
	"fmt"
	"os"
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
	Span   *Span  `yaml:"span"`
	URL    string `yaml:"url"`
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
	RTSPBase        string  `yaml:"rtsp_base"`
	Layout          string  `yaml:"layout"`
	PrimaryPosition string  `yaml:"primary_position"`
	Screen          Screen  `yaml:"screen"`
	Decoder         string  `yaml:"decoder"`
	Sink            string  `yaml:"sink"`
	Planes          []int   `yaml:"planes"`
	Latency         int     `yaml:"latency_ms"`
	Tiles           []Tile  `yaml:"tiles"`
	Restart         Restart `yaml:"restart"`
}

var layouts = map[string][2]int{"1": {1, 1}, "2x2": {2, 2}, "3x3": {3, 3}, "1+5": {3, 3}}

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
	if _, ok := layouts[c.Layout]; !ok {
		return nil, fmt.Errorf("config: layout must be one of 1, 2x2, 3x3, 1+5 (got %q)", c.Layout)
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
	if len(c.Tiles) == 0 {
		return nil, fmt.Errorf("config: at least one tile is required")
	}
	cols, rows := c.GridDims()
	if len(c.Tiles) > cols*rows {
		return nil, fmt.Errorf("config: %d tiles do not fit layout %s (%d cells)", len(c.Tiles), c.Layout, cols*rows)
	}
	for i, t := range c.Tiles {
		if t.Camera == "" && t.URL == "" {
			return nil, fmt.Errorf("config: tiles[%d] needs camera or url", i)
		}
		if t.URL == "" && c.RTSPBase == "" {
			return nil, fmt.Errorf("config: rtsp_base is required when a tile has no url (tiles[%d])", i)
		}
		switch t.Aspect {
		case "", "wide", "tall":
		default:
			return nil, fmt.Errorf("config: tiles[%d].aspect must be wide or tall", i)
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
	d := layouts[c.Layout]
	return d[0], d[1]
}
```

- [ ] **Step 5: Run tests** — `cd client && go mod tidy && go test ./internal/config/` → PASS.

- [ ] **Step 6: `client/config.example.yaml` and `client/Makefile`**

```yaml
# eufy-wall display client configuration (/etc/eufy-wall.yaml)
rtsp_base: rtsp://192.168.1.10:8554   # the eufy-wall-bridge server (go2rtc RTSP)
layout: 1+5                            # 1 | 2x2 | 3x3 | 1+5
primary_position: left                 # 1+5 only: left | right
# screen: { width: 1920, height: 1080 }  # omit → auto-detect from /sys/class/drm
decoder: auto                          # auto | v4l2 (Pi) | va (x86 VAAPI) | software
sink: auto                             # auto | planes (Pi, one kmssink per tile) | compositor | window (dev)
# planes: [31, 32, 33, 34, 35, 36]     # DRM overlay plane ids for sink=planes → `modetest -M vc4 -p`
latency_ms: 200
tiles:                                 # order = placement order (row-major after the primary)
  - { camera: T8410XXXXXXXXXXX, role: primary }
  - { camera: T8214XXXXXXXXXXX, aspect: tall }     # E340 split-view → 1x2 tall tile
  - { camera: T8420XXXXXXXXXXX }
  - { camera: T8411XXXXXXXXXXX }
  - { camera: T8412XXXXXXXXXXX }
restart: { min_seconds: 1, max_seconds: 30, stable_seconds: 60 }
```

```makefile
BIN=bin/eufy-wall
PKG=./cmd/eufy-wall
LDFLAGS=-s -w

.PHONY: test host pi1 pi3 pi64 amd64 all clean
test:
	go test ./...
host:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)
pi1:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "$(LDFLAGS)" -o $(BIN)-armv6 $(PKG)
pi3:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o $(BIN)-armv7 $(PKG)
pi64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)-arm64 $(PKG)
amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)-amd64 $(PKG)
all: pi1 pi3 pi64 amd64
clean:
	rm -rf bin
```

- [ ] **Step 7: Commit**

```bash
git add client/go.mod client/go.sum client/internal/config client/config.example.yaml client/Makefile
git commit -m "feat(client): go module, config loader with validation"
```

---

### Task 2: Layout engine

**Files:**
- Create: `client/internal/layout/layout.go`, `client/internal/layout/layout_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.Tile`, `config.Screen`.
- Produces: `layout.Place(c *config.Config, screen config.Screen) ([]Placed, error)` and `type Placed struct{ Index int; Camera, URL string; Col, Row, Cols, Rows, X, Y, W, H int; Letterbox bool }`.
- Rules: primary = tile with `role: primary`, else tile 0 when layout is `1+5`; it spans 2×2 at `left` (col 0) or `right` (col 1), row 0. Other tiles in config order: span = explicit `Span`, else `1×2` if `aspect: tall`, else `1×1`. First-fit scan row-major; a tall tile that doesn't fit retries as 1×1 with `Letterbox=true`; anything that still doesn't fit → error naming the tile. Pixel rects: `cellW = W/cols`, `cellH = H/rows`; the last column/row absorbs the remainder.

- [ ] **Step 1: Write the failing test `client/internal/layout/layout_test.go`**

```go
package layout

import (
	"testing"

	"eufy-wall/internal/config"
)

func cfg(layout, pos string, tiles ...config.Tile) *config.Config {
	return &config.Config{RTSPBase: "rtsp://s:8554", Layout: layout, PrimaryPosition: pos, Tiles: tiles}
}

func rect(p Placed) [4]int { return [4]int{p.X, p.Y, p.W, p.H} }

func TestSingle(t *testing.T) {
	p, err := Place(cfg("1", "", config.Tile{Camera: "A"}), config.Screen{1920, 1080})
	if err != nil || len(p) != 1 || rect(p[0]) != [4]int{0, 0, 1920, 1080} || p[0].URL != "rtsp://s:8554/A" {
		t.Fatalf("%v %+v", err, p)
	}
}

func Test2x2RowMajorWithRemainder(t *testing.T) {
	p, err := Place(cfg("2x2", "", config.Tile{Camera: "A"}, config.Tile{Camera: "B"}, config.Tile{Camera: "C"}), config.Screen{1919, 1079})
	if err != nil {
		t.Fatal(err)
	}
	want := [][4]int{{0, 0, 959, 539}, {959, 0, 960, 539}, {0, 539, 959, 540}}
	for i, w := range want {
		if rect(p[i]) != w {
			t.Errorf("tile %d: got %v want %v", i, rect(p[i]), w)
		}
	}
}

func Test1Plus5LeftAndRight(t *testing.T) {
	tiles := []config.Tile{{Camera: "P", Role: "primary"}, {Camera: "B"}, {Camera: "C"}, {Camera: "D"}, {Camera: "E"}, {Camera: "F"}}
	p, err := Place(cfg("1+5", "left", tiles...), config.Screen{1920, 1080})
	if err != nil {
		t.Fatal(err)
	}
	if rect(p[0]) != [4]int{0, 0, 1280, 720} {
		t.Errorf("primary left %v", rect(p[0]))
	}
	// remaining cells row-major: (2,0) (2,1) (0,2) (1,2) (2,2)
	want := [][4]int{{1280, 0, 640, 360}, {1280, 360, 640, 360}, {0, 720, 640, 360}, {640, 720, 640, 360}, {1280, 720, 640, 360}}
	for i, w := range want {
		if rect(p[i+1]) != w {
			t.Errorf("secondary %d: got %v want %v", i, rect(p[i+1]), w)
		}
	}
	p, _ = Place(cfg("1+5", "right", tiles...), config.Screen{1920, 1080})
	if rect(p[0]) != [4]int{640, 0, 1280, 720} || rect(p[1]) != [4]int{0, 0, 640, 360} {
		t.Errorf("primary right %v first secondary %v", rect(p[0]), rect(p[1]))
	}
}

func Test1Plus5PrimaryDefaultsToFirstTile(t *testing.T) {
	p, err := Place(cfg("1+5", "left", config.Tile{Camera: "A"}, config.Tile{Camera: "B"}), config.Screen{1920, 1080})
	if err != nil || p[0].Cols != 2 || p[0].Rows != 2 || p[0].Camera != "A" {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestTallTileGets1x2InSideColumn(t *testing.T) {
	tiles := []config.Tile{{Camera: "P", Role: "primary"}, {Camera: "T", Aspect: "tall"}, {Camera: "C"}, {Camera: "D"}, {Camera: "E"}}
	p, err := Place(cfg("1+5", "left", tiles...), config.Screen{1920, 1080})
	if err != nil {
		t.Fatal(err)
	}
	if rect(p[1]) != [4]int{1280, 0, 640, 720} || p[1].Letterbox {
		t.Errorf("tall: %v letterbox=%v", rect(p[1]), p[1].Letterbox)
	}
	if rect(p[2]) != [4]int{0, 720, 640, 360} {
		t.Errorf("next tile after tall: %v", rect(p[2]))
	}
}

func TestTallFallsBackToLetterboxWhenNoRoomForTwoRows(t *testing.T) {
	// 2x2: A takes (0,0), B takes (1,0); tall C can only fit 1x1 at (0,1).
	tiles := []config.Tile{{Camera: "A"}, {Camera: "B"}, {Camera: "C", Aspect: "tall"}}
	p, err := Place(cfg("2x2", "", tiles...), config.Screen{1920, 1080})
	if err != nil {
		t.Fatal(err)
	}
	if !p[2].Letterbox || p[2].Cols != 1 || p[2].Rows != 1 || rect(p[2]) != [4]int{0, 540, 960, 540} {
		t.Errorf("%+v", p[2])
	}
}

func TestExplicitSpanWinsAndOverflowErrors(t *testing.T) {
	two := config.Span{Cols: 2, Rows: 1}
	p, err := Place(cfg("3x3", "", config.Tile{Camera: "A", Span: &two}, config.Tile{Camera: "B"}), config.Screen{1920, 1080})
	if err != nil || rect(p[0]) != [4]int{0, 0, 1280, 360} || rect(p[1]) != [4]int{1280, 0, 640, 360} {
		t.Fatalf("%v %+v", err, p)
	}
	big := config.Span{Cols: 2, Rows: 2}
	_, err = Place(cfg("2x2", "", config.Tile{Camera: "A", Span: &big}, config.Tile{Camera: "B"}), config.Screen{1920, 1080})
	if err == nil {
		t.Fatal("expected overflow error")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd client && go test ./internal/layout/` → FAIL.

- [ ] **Step 3: Implement `client/internal/layout/layout.go`**

```go
// Package layout places tiles on a cell grid and converts cells to pixel rectangles.
package layout

import (
	"fmt"

	"eufy-wall/internal/config"
)

type Placed struct {
	Index      int
	Camera     string
	URL        string
	Col, Row   int
	Cols, Rows int
	X, Y, W, H int
	Letterbox  bool
}

type grid struct {
	cols, rows int
	used       []bool
}

func (g *grid) fits(col, row, cols, rows int) bool {
	if col+cols > g.cols || row+rows > g.rows {
		return false
	}
	for r := row; r < row+rows; r++ {
		for c := col; c < col+cols; c++ {
			if g.used[r*g.cols+c] {
				return false
			}
		}
	}
	return true
}

func (g *grid) take(col, row, cols, rows int) {
	for r := row; r < row+rows; r++ {
		for c := col; c < col+cols; c++ {
			g.used[r*g.cols+c] = true
		}
	}
}

// firstFit scans row-major for the first free block of cols×rows.
func (g *grid) firstFit(cols, rows int) (col, row int, ok bool) {
	for r := 0; r < g.rows; r++ {
		for c := 0; c < g.cols; c++ {
			if g.fits(c, r, cols, rows) {
				return c, r, true
			}
		}
	}
	return 0, 0, false
}

// Place assigns every tile a cell block and a pixel rectangle on `screen`.
func Place(c *config.Config, screen config.Screen) ([]Placed, error) {
	cols, rows := c.GridDims()
	g := &grid{cols: cols, rows: rows, used: make([]bool, cols*rows)}
	out := make([]Placed, len(c.Tiles))
	placed := make([]bool, len(c.Tiles))

	toPixels := func(p *Placed) {
		cellW, cellH := screen.Width/cols, screen.Height/rows
		p.X, p.Y = p.Col*cellW, p.Row*cellH
		p.W, p.H = p.Cols*cellW, p.Rows*cellH
		if p.Col+p.Cols == cols { // last column absorbs rounding
			p.W = screen.Width - p.X
		}
		if p.Row+p.Rows == rows {
			p.H = screen.Height - p.Y
		}
	}

	// Primary for 1+5: explicit role, else the first tile. Fixed position, 2x2.
	if c.Layout == "1+5" {
		pi := 0
		for i, t := range c.Tiles {
			if t.Role == "primary" {
				pi = i
				break
			}
		}
		col := 0
		if c.PrimaryPosition == "right" {
			col = 1
		}
		p := Placed{Index: pi, Camera: c.Tiles[pi].Camera, URL: c.TileURL(c.Tiles[pi]), Col: col, Row: 0, Cols: 2, Rows: 2}
		g.take(col, 0, 2, 2)
		toPixels(&p)
		out[pi] = p
		placed[pi] = true
	}

	for i, t := range c.Tiles {
		if placed[i] {
			continue
		}
		want := config.Span{Cols: 1, Rows: 1}
		if t.Aspect == "tall" {
			want = config.Span{Cols: 1, Rows: 2}
		}
		if t.Span != nil {
			want = *t.Span
		}
		p := Placed{Index: i, Camera: t.Camera, URL: c.TileURL(t)}
		col, row, ok := g.firstFit(want.Cols, want.Rows)
		if !ok && t.Aspect == "tall" && t.Span == nil {
			want = config.Span{Cols: 1, Rows: 1}
			col, row, ok = g.firstFit(1, 1)
			p.Letterbox = true
		}
		if !ok {
			return nil, fmt.Errorf("layout %s: tile %d (%s) with span %dx%d does not fit", c.Layout, i, t.Camera, want.Cols, want.Rows)
		}
		p.Col, p.Row, p.Cols, p.Rows = col, row, want.Cols, want.Rows
		g.take(col, row, want.Cols, want.Rows)
		toPixels(&p)
		out[i] = p
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests** — `cd client && go test ./internal/layout/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add client/internal/layout
git commit -m "feat(client): span-based layout engine (1, 2x2, 3x3, 1+5, tall tiles)"
```

---

### Task 3: Pipeline generator

**Files:**
- Create: `client/internal/pipeline/pipeline.go`, `client/internal/pipeline/pipeline_test.go`

**Interfaces:**
- Consumes: `[]layout.Placed`, `config.Config`, and a resolved `Caps{Decoder, Sink string; Screen config.Screen}` (decoder/sink already resolved from `auto` by `detect`, Task 4).
- Produces: `pipeline.Build(c *config.Config, tiles []layout.Placed, caps Caps) ([]string, error)` — argv for `gst-launch-1.0` (each element/property is a separate arg; no shell quoting), and `pipeline.String(args []string) string` for logs/dry-run.
- Element choices:
  - decoder `v4l2` → `v4l2h264dec`; `va` → `vah264dec`; `software` → `avdec_h264`.
  - source per tile: `rtspsrc location=<url> latency=<ms> protocols=tcp name=src<i> ! rtph264depay ! h264parse ! <dec>`
  - sink `planes`: each tile `… ! kmssink name=sink<i> plane-id=<planes[i]> render-rectangle=<x,y,w,h> force-aspect-ratio=true sync=false`; requires `len(c.Planes) >= len(tiles)` else error.
  - sink `compositor`: each tile `… ! videoconvert ! mix.sink_<i>`; then `compositor name=mix background=black sink_<i>::xpos=X sink_<i>::ypos=Y sink_<i>::width=W sink_<i>::height=H sink_<i>::sizing-policy=keep-aspect-ratio ! video/x-raw,width=<screen.W>,height=<screen.H> ! kmssink sync=false`.
  - sink `window` (dev on a desktop): same as compositor but `autovideosink sync=false`.
  - `-e` (EOS on Ctrl-C) always first.

- [ ] **Step 1: Write the failing test `client/internal/pipeline/pipeline_test.go`**

```go
package pipeline

import (
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

func two() (*config.Config, []layout.Placed) {
	c := &config.Config{Latency: 200, Planes: []int{31, 32}}
	tiles := []layout.Placed{
		{Index: 0, Camera: "A", URL: "rtsp://s/A", X: 0, Y: 0, W: 960, H: 1080},
		{Index: 1, Camera: "B", URL: "rtsp://s/B", X: 960, Y: 0, W: 960, H: 1080, Letterbox: true},
	}
	return c, tiles
}

func TestPlanesPipeline(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", Screen: config.Screen{1920, 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	want := "-e " +
		"rtspsrc location=rtsp://s/A latency=200 protocols=tcp name=src0 ! rtph264depay ! h264parse ! v4l2h264dec ! kmssink name=sink0 plane-id=31 render-rectangle=<0,0,960,1080> force-aspect-ratio=true sync=false " +
		"rtspsrc location=rtsp://s/B latency=200 protocols=tcp name=src1 ! rtph264depay ! h264parse ! v4l2h264dec ! kmssink name=sink1 plane-id=32 render-rectangle=<960,0,960,1080> force-aspect-ratio=true sync=false"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestPlanesNeedsPlaneIDs(t *testing.T) {
	c, tiles := two()
	c.Planes = []int{31}
	if _, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes"}); err == nil || !strings.Contains(err.Error(), "planes") {
		t.Fatalf("expected plane error, got %v", err)
	}
}

func TestCompositorPipeline(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "va", Sink: "compositor", Screen: config.Screen{1920, 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	want := "-e " +
		"rtspsrc location=rtsp://s/A latency=200 protocols=tcp name=src0 ! rtph264depay ! h264parse ! vah264dec ! videoconvert ! mix.sink_0 " +
		"rtspsrc location=rtsp://s/B latency=200 protocols=tcp name=src1 ! rtph264depay ! h264parse ! vah264dec ! videoconvert ! mix.sink_1 " +
		"compositor name=mix background=black sink_0::xpos=0 sink_0::ypos=0 sink_0::width=960 sink_0::height=1080 sink_0::sizing-policy=keep-aspect-ratio " +
		"sink_1::xpos=960 sink_1::ypos=0 sink_1::width=960 sink_1::height=1080 sink_1::sizing-policy=keep-aspect-ratio " +
		"! video/x-raw,width=1920,height=1080 ! kmssink sync=false"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestWindowSoftware(t *testing.T) {
	c, tiles := two()
	args, _ := Build(c, tiles, Caps{Decoder: "software", Sink: "window", Screen: config.Screen{1920, 1080}})
	s := String(args)
	if !strings.Contains(s, "avdec_h264") || !strings.HasSuffix(s, "! autovideosink sync=false") {
		t.Fatalf("%s", s)
	}
}

func TestArgsAreNotShellJoined(t *testing.T) {
	c, tiles := two()
	args, _ := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes"})
	for _, a := range args {
		if strings.ContainsAny(a, " \t\n") {
			t.Fatalf("arg with whitespace: %q", a)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd client && go test ./internal/pipeline/` → FAIL.

- [ ] **Step 3: Implement `client/internal/pipeline/pipeline.go`**

```go
// Package pipeline renders the wall as gst-launch-1.0 arguments. Two sink strategies:
//   planes:     one kmssink per tile on its own DRM overlay plane (the Pi's HVS composites for free)
//   compositor: N decoders → compositor → one kmssink (CPU/GPU composite; works everywhere)
package pipeline

import (
	"fmt"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

type Caps struct {
	Decoder string // v4l2 | va | software
	Sink    string // planes | compositor | window
	Screen  config.Screen
}

var decoders = map[string]string{"v4l2": "v4l2h264dec", "va": "vah264dec", "software": "avdec_h264"}

func Build(c *config.Config, tiles []layout.Placed, caps Caps) ([]string, error) {
	dec, ok := decoders[caps.Decoder]
	if !ok {
		return nil, fmt.Errorf("pipeline: unknown decoder %q", caps.Decoder)
	}
	if caps.Sink == "planes" && len(c.Planes) < len(tiles) {
		return nil, fmt.Errorf("pipeline: sink=planes needs %d plane ids in `planes:` (have %d) — list overlay planes with `modetest -M vc4 -p`", len(tiles), len(c.Planes))
	}
	args := []string{"-e"}
	src := func(i int, t layout.Placed) []string {
		return []string{
			"rtspsrc", "location=" + t.URL, fmt.Sprintf("latency=%d", c.Latency), "protocols=tcp", fmt.Sprintf("name=src%d", i),
			"!", "rtph264depay", "!", "h264parse", "!", dec,
		}
	}
	switch caps.Sink {
	case "planes":
		for i, t := range tiles {
			args = append(args, src(i, t)...)
			args = append(args, "!", "kmssink", fmt.Sprintf("name=sink%d", i), fmt.Sprintf("plane-id=%d", c.Planes[i]),
				fmt.Sprintf("render-rectangle=<%d,%d,%d,%d>", t.X, t.Y, t.W, t.H), "force-aspect-ratio=true", "sync=false")
		}
	case "compositor", "window":
		for i, t := range tiles {
			args = append(args, src(i, t)...)
			args = append(args, "!", "videoconvert", "!", fmt.Sprintf("mix.sink_%d", i))
		}
		args = append(args, "compositor", "name=mix", "background=black")
		for i, t := range tiles {
			args = append(args,
				fmt.Sprintf("sink_%d::xpos=%d", i, t.X), fmt.Sprintf("sink_%d::ypos=%d", i, t.Y),
				fmt.Sprintf("sink_%d::width=%d", i, t.W), fmt.Sprintf("sink_%d::height=%d", i, t.H),
				fmt.Sprintf("sink_%d::sizing-policy=keep-aspect-ratio", i))
		}
		args = append(args, "!", fmt.Sprintf("video/x-raw,width=%d,height=%d", caps.Screen.Width, caps.Screen.Height))
		if caps.Sink == "window" {
			args = append(args, "!", "autovideosink", "sync=false")
		} else {
			args = append(args, "!", "kmssink", "sync=false")
		}
	default:
		return nil, fmt.Errorf("pipeline: unknown sink %q", caps.Sink)
	}
	return args, nil
}

// String joins args for logging; the supervisor execs the slice directly (no shell).
func String(args []string) string { return strings.Join(args, " ") }
```

- [ ] **Step 4: Run tests** — `cd client && go test ./internal/pipeline/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add client/internal/pipeline
git commit -m "feat(client): gst-launch pipeline generator (planes and compositor sinks)"
```

---

### Task 4: Detection (screen size, decoder, sink)

**Files:**
- Create: `client/internal/detect/detect.go`, `client/internal/detect/detect_test.go`

**Interfaces:**
- Produces:
  - `detect.Screen(fsRoot string) (config.Screen, bool)` — reads the first line of the first `<fsRoot>/sys/class/drm/card*-HDMI-A-*/modes` file (`1920x1080`); `ok=false` if none.
  - `detect.Resolve(c *config.Config, has func(element string) bool, fileExists func(string) bool) (pipeline.Caps, error)` — decoder `auto`: `v4l2` if `fileExists("/dev/video10") && has("v4l2h264dec")`, else `va` if `has("vah264dec")`, else `software` if `has("avdec_h264")`, else error; sink `auto`: `planes` if decoder is `v4l2` and `len(c.Planes) > 0`, else `compositor` if `has("compositor")`, else error.
  - `detect.HasElement(name string) bool` — runs `gst-inspect-1.0 --exists <name>` (exit 0 = present).

- [ ] **Step 1: Write the failing test `client/internal/detect/detect_test.go`**

```go
package detect

import (
	"os"
	"path/filepath"
	"testing"

	"eufy-wall/internal/config"
)

func TestScreenFromSysfs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sys/class/drm/card1-HDMI-A-1")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "modes"), []byte("1920x1080\n1280x720\n"), 0o644)
	s, ok := Screen(root)
	if !ok || s != (config.Screen{1920, 1080}) {
		t.Fatalf("%v %+v", ok, s)
	}
	if _, ok := Screen(t.TempDir()); ok {
		t.Fatal("expected not ok without drm")
	}
}

func TestResolve(t *testing.T) {
	has := func(set ...string) func(string) bool {
		m := map[string]bool{}
		for _, s := range set {
			m[s] = true
		}
		return func(e string) bool { return m[e] }
	}
	exists := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range paths {
				if x == p {
					return true
				}
			}
			return false
		}
	}
	pi := &config.Config{Decoder: "auto", Sink: "auto", Planes: []int{31}, Screen: config.Screen{1920, 1080}}
	caps, err := Resolve(pi, has("v4l2h264dec", "compositor"), exists("/dev/video10"))
	if err != nil || caps.Decoder != "v4l2" || caps.Sink != "planes" {
		t.Fatalf("pi: %v %+v", err, caps)
	}
	pi.Planes = nil
	caps, _ = Resolve(pi, has("v4l2h264dec", "compositor"), exists("/dev/video10"))
	if caps.Sink != "compositor" {
		t.Fatalf("pi no planes: %+v", caps)
	}
	x86 := &config.Config{Decoder: "auto", Sink: "auto", Screen: config.Screen{1920, 1080}}
	caps, _ = Resolve(x86, has("vah264dec", "avdec_h264", "compositor"), exists())
	if caps.Decoder != "va" || caps.Sink != "compositor" {
		t.Fatalf("x86: %+v", caps)
	}
	caps, _ = Resolve(x86, has("avdec_h264", "compositor"), exists())
	if caps.Decoder != "software" {
		t.Fatalf("sw: %+v", caps)
	}
	if _, err := Resolve(x86, has(), exists()); err == nil {
		t.Fatal("expected error with no decoders")
	}
	forced := &config.Config{Decoder: "software", Sink: "window", Screen: config.Screen{800, 600}}
	caps, err = Resolve(forced, has(), exists())
	if err != nil || caps.Decoder != "software" || caps.Sink != "window" || caps.Screen.Width != 800 {
		t.Fatalf("forced: %v %+v", err, caps)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd client && go test ./internal/detect/` → FAIL.

- [ ] **Step 3: Implement `client/internal/detect/detect.go`**

```go
// Package detect resolves "auto" settings against the machine: screen mode from DRM sysfs, decoder and
// sink from the GStreamer elements actually installed.
package detect

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// Screen reads the preferred mode of the first HDMI connector (first line of .../modes).
func Screen(fsRoot string) (config.Screen, bool) {
	matches, _ := filepath.Glob(filepath.Join(fsRoot, "sys/class/drm/card*-HDMI-A-*/modes"))
	for _, m := range matches {
		f, err := os.Open(m)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		if sc.Scan() {
			var w, h int
			if _, err := fmt.Sscanf(strings.TrimSpace(sc.Text()), "%dx%d", &w, &h); err == nil && w > 0 && h > 0 {
				f.Close()
				return config.Screen{Width: w, Height: h}, true
			}
		}
		f.Close()
	}
	return config.Screen{}, false
}

// HasElement asks gst-inspect whether an element exists on this machine.
func HasElement(name string) bool {
	return exec.Command("gst-inspect-1.0", "--exists", name).Run() == nil
}

func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Resolve turns auto decoder/sink into concrete choices. `has`/`fileExists` are injectable for tests.
func Resolve(c *config.Config, has func(string) bool, fileExists func(string) bool) (pipeline.Caps, error) {
	caps := pipeline.Caps{Decoder: c.Decoder, Sink: c.Sink, Screen: c.Screen}
	if caps.Decoder == "auto" {
		switch {
		case fileExists("/dev/video10") && has("v4l2h264dec"):
			caps.Decoder = "v4l2"
		case has("vah264dec"):
			caps.Decoder = "va"
		case has("avdec_h264"):
			caps.Decoder = "software"
		default:
			return caps, fmt.Errorf("detect: no H.264 decoder found (install gstreamer1.0-plugins-good/bad/libav)")
		}
	}
	if caps.Sink == "auto" {
		switch {
		case caps.Decoder == "v4l2" && len(c.Planes) > 0:
			caps.Sink = "planes"
		case has("compositor"):
			caps.Sink = "compositor"
		default:
			return caps, fmt.Errorf("detect: no sink strategy: set `planes:` for kmssink planes or install gstreamer1.0-plugins-base (compositor)")
		}
	}
	return caps, nil
}
```

- [ ] **Step 4: Run tests** — `cd client && go test ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
git add client/internal/detect
git commit -m "feat(client): auto-detect screen mode, decoder and sink strategy"
```

---

### Task 5: Supervisor + main

**Files:**
- Create: `client/internal/supervisor/supervisor.go`, `client/internal/supervisor/supervisor_test.go`, `client/cmd/eufy-wall/main.go`

**Interfaces:**
- Produces: `supervisor.Run(ctx context.Context, bin string, args []string, r config.Restart, log func(string)) error` — loops: start process (stdout/stderr → `log` line by line), wait; on exit (not ctx cancel) sleep backoff (min, doubling to max; reset to min if the run lasted ≥ StableSeconds), restart. Returns when ctx is cancelled (kills the child with SIGTERM, waits ≤ 5 s, then SIGKILL).
- `main`: flags `-config /etc/eufy-wall.yaml`, `-dry-run` (print resolved caps, layout table and pipeline, exit 0), `-print-layout`. Sets `GST_DEBUG` from env or `2`. SIGINT/SIGTERM cancel ctx.

- [ ] **Step 1: Write the failing test `client/internal/supervisor/supervisor_test.go`**

```go
package supervisor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func TestRestartsWithBackoffAndStopsOnCancel(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	log := func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Child prints and exits 1 immediately; backoff min=max=0s via 0 → we use a tiny custom unit.
	go func() {
		done <- Run(ctx, "sh", []string{"-c", "echo hello; exit 1"}, config.Restart{MinSeconds: 0, MaxSeconds: 0, StableSeconds: 60}, log)
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	hello, restarts := 0, 0
	for _, l := range lines {
		if strings.Contains(l, "hello") {
			hello++
		}
		if strings.Contains(l, "restarting in") {
			restarts++
		}
	}
	if hello < 2 || restarts < 1 {
		t.Fatalf("expected repeated runs, got hello=%d restarts=%d lines=%v", hello, restarts, lines)
	}
}

func TestBackoffSchedule(t *testing.T) {
	r := config.Restart{MinSeconds: 1, MaxSeconds: 8, StableSeconds: 60}
	b := newBackoff(r)
	got := []time.Duration{b.next(0), b.next(0), b.next(0), b.next(0), b.next(0)}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: %v want %v", i, got[i], want[i])
		}
	}
	if d := b.next(61 * time.Second); d != 1*time.Second {
		t.Fatalf("stable run should reset: %v", d)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd client && go test ./internal/supervisor/` → FAIL.

- [ ] **Step 3: Implement `client/internal/supervisor/supervisor.go`**

```go
// Package supervisor keeps one gst-launch process alive: restart on exit with exponential backoff,
// forward its output to the log, and shut it down cleanly on cancel.
package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"eufy-wall/internal/config"
)

type backoff struct {
	r   config.Restart
	cur time.Duration
}

func newBackoff(r config.Restart) *backoff { return &backoff{r: r} }

// next returns the delay before the next start, given how long the previous run lasted.
func (b *backoff) next(ran time.Duration) time.Duration {
	min := time.Duration(b.r.MinSeconds) * time.Second
	max := time.Duration(b.r.MaxSeconds) * time.Second
	if ran >= time.Duration(b.r.StableSeconds)*time.Second || b.cur == 0 {
		b.cur = min
	} else {
		b.cur *= 2
		if b.cur > max {
			b.cur = max
		}
	}
	return b.cur
}

func pump(r io.Reader, log func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		log(sc.Text())
	}
}

// Run blocks until ctx is cancelled. It never returns a process error; failures are logged and retried.
func Run(ctx context.Context, bin string, args []string, r config.Restart, log func(string)) error {
	b := newBackoff(r)
	for {
		if ctx.Err() != nil {
			return nil
		}
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "GST_DEBUG_NO_COLOR=1")
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		start := time.Now()
		if err := cmd.Start(); err != nil {
			log(fmt.Sprintf("start failed: %v", err))
		} else {
			log(fmt.Sprintf("started pid %d", cmd.Process.Pid))
			go pump(stdout, log)
			go pump(stderr, log)
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			select {
			case err := <-waited:
				log(fmt.Sprintf("exited after %s: %v", time.Since(start).Round(time.Second), err))
			case <-ctx.Done():
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-waited:
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
					<-waited
				}
				return nil
			}
		}
		d := b.next(time.Since(start))
		log(fmt.Sprintf("restarting in %s", d))
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil
		}
	}
}
```

- [ ] **Step 4: Run tests** — `cd client && go test ./internal/supervisor/` → PASS.

- [ ] **Step 5: Write `client/cmd/eufy-wall/main.go`**

```go
// eufy-wall: render N RTSP camera tiles on the HDMI output with one supervised GStreamer pipeline.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/supervisor"
)

func main() {
	cfgPath := flag.String("config", "/etc/eufy-wall.yaml", "config file")
	dryRun := flag.Bool("dry-run", false, "print the resolved layout and pipeline, then exit")
	flag.Parse()
	log.SetFlags(log.Ltime)

	c, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	if c.Screen.Width == 0 || c.Screen.Height == 0 {
		if s, ok := detect.Screen("/"); ok {
			c.Screen = s
		} else {
			c.Screen = config.Screen{Width: 1920, Height: 1080}
			log.Printf("[wall] no HDMI mode found in sysfs — assuming 1920x1080 (set screen: in config)")
		}
	}
	caps, err := detect.Resolve(c, detect.HasElement, detect.FileExists)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	tiles, err := layout.Place(c, c.Screen)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	args, err := pipeline.Build(c, tiles, caps)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}

	log.Printf("[wall] screen %dx%d layout %s decoder %s sink %s", c.Screen.Width, c.Screen.Height, c.Layout, caps.Decoder, caps.Sink)
	for _, t := range tiles {
		lb := ""
		if t.Letterbox {
			lb = " (letterbox)"
		}
		log.Printf("[wall] tile %d %-18s cell %d,%d span %dx%d px %d,%d %dx%d%s", t.Index, t.Camera, t.Col, t.Row, t.Cols, t.Rows, t.X, t.Y, t.W, t.H, lb)
	}
	if *dryRun {
		fmt.Println("gst-launch-1.0 " + pipeline.String(args))
		return
	}
	if os.Getenv("GST_DEBUG") == "" {
		os.Setenv("GST_DEBUG", "2")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	_ = supervisor.Run(ctx, "gst-launch-1.0", args, c.Restart, func(s string) { log.Printf("[gst] %s", s) })
	log.Printf("[wall] stopped")
}
```

- [ ] **Step 6: Build + dry-run**

Run: `cd client && make host && cat > /tmp/w.yaml <<'EOF'
rtsp_base: rtsp://127.0.0.1:8554
layout: 1+5
decoder: software
sink: window
screen: { width: 1920, height: 1080 }
tiles: [{camera: A, role: primary}, {camera: B, aspect: tall}, {camera: C}, {camera: D}]
EOF
./bin/eufy-wall -config /tmp/w.yaml -dry-run`
Expected: layout table (primary 0,0 2x2; B tall at cell 2,0 span 1x2) and a `gst-launch-1.0 -e rtspsrc … compositor … autovideosink` line.

- [ ] **Step 7: Commit**

```bash
git add client/internal/supervisor client/cmd
git commit -m "feat(client): process supervisor with backoff and eufy-wall main"
```

---

### Task 6: Spike B — validate sink strategy on the Pi (throwaway)

**Files:**
- Create: `client/spikes/spike-b.sh`, results into `docs/runbook-client.md` (Task 7)

Purpose: decide `sink: planes` vs `compositor` on Pi 3 and Pi 1, find plane ids, measure CPU with 4–6 720p streams. Needs the server (or any RTSP source — go2rtc can serve a looping file: add `test: ffmpeg:/path/sample.mp4#video=h264#loop` streams to a scratch go2rtc.yaml).

- [ ] **Step 1: Write `client/spikes/spike-b.sh`**

```bash
#!/usr/bin/env bash
# Throwaway experiments for the Pi display backend. Run ON the Pi as a user in the video+render groups,
# from a text console (no X/Wayland). Usage: spike-b.sh <rtsp-url-1> [<rtsp-url-2> ...]
set -u
URLS=("$@"); N=${#URLS[@]}
[[ $N -ge 1 ]] || { echo "usage: $0 rtsp://... [rtsp://...]"; exit 1; }
echo "== environment"; uname -r; cat /proc/device-tree/model 2>/dev/null; echo; vcgencmd get_mem gpu 2>/dev/null
echo "== decoder + sinks"; for e in v4l2h264dec kmssink compositor glvideomixer; do printf '%-14s %s\n' "$e" "$(gst-inspect-1.0 --exists $e && echo yes || echo NO)"; done
echo "== overlay planes (need ids for sink=planes)"; modetest -M vc4 -p 2>/dev/null | awk '/^Planes/,/^$/' | head -40

W=1920; H=1080; COLS=3; ROWS=2; CW=$((W/COLS)); CH=$((H/ROWS))
src() { echo "rtspsrc location=$1 latency=200 protocols=tcp ! rtph264depay ! h264parse ! v4l2h264dec"; }

echo; echo "== A) one kmssink per tile on separate planes (edit PLANES to match modetest)"; PLANES=(${PLANES:-})
if [[ ${#PLANES[@]} -ge $N ]]; then
  P=""; for i in $(seq 0 $((N-1))); do x=$(( (i%COLS)*CW )); y=$(( (i/COLS)*CH )); P+="$(src "${URLS[$i]}") ! kmssink plane-id=${PLANES[$i]} render-rectangle=<$x,$y,$CW,$CH> force-aspect-ratio=true sync=false "; done
  echo "gst-launch-1.0 -e $P"; timeout 60 gst-launch-1.0 -e $P & sleep 45; top -bn1 | head -12; wait
else echo "skip: export PLANES='31 32 33 ...' first"; fi

echo; echo "== B) compositor → one kmssink"
P=""; MIX="compositor name=mix background=black "
for i in $(seq 0 $((N-1))); do x=$(( (i%COLS)*CW )); y=$(( (i/COLS)*CH )); P+="$(src "${URLS[$i]}") ! videoconvert ! mix.sink_$i "; MIX+="sink_$i::xpos=$x sink_$i::ypos=$y sink_$i::width=$CW sink_$i::height=$CH sink_$i::sizing-policy=keep-aspect-ratio "; done
echo "gst-launch-1.0 -e $P $MIX ! video/x-raw,width=$W,height=$H ! kmssink sync=false"
timeout 60 gst-launch-1.0 -e $P $MIX ! video/x-raw,width=$W,height=$H ! kmssink sync=false & sleep 45; top -bn1 | head -12; wait

echo; echo "== C) glvideomixer → kmssink (GPU composite)"
P=""; MIX="glvideomixer name=mix "
for i in $(seq 0 $((N-1))); do x=$(( (i%COLS)*CW )); y=$(( (i/COLS)*CH )); P+="$(src "${URLS[$i]}") ! glupload ! mix.sink_$i "; MIX+="sink_$i::xpos=$x sink_$i::ypos=$y sink_$i::width=$CW sink_$i::height=$CH "; done
echo "gst-launch-1.0 -e $P $MIX ! gldownload ! kmssink sync=false"
timeout 60 gst-launch-1.0 -e $P $MIX ! gldownload ! kmssink sync=false & sleep 45; top -bn1 | head -12; wait
echo "== done. Record: which variants rendered, CPU%, dropped frames (GST_DEBUG=2 warnings), kernel."
```
Run: `chmod +x client/spikes/spike-b.sh`.

- [ ] **Step 2: Run on the Pi 3** (with the user): `sudo apt install -y gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad libdrm-tests` then `PLANES="…" ./spike-b.sh rtsp://server:8554/<sn1> … ×6`. Record per variant: rendered yes/no, CPU %, warnings. Repeat with 1/4/6 streams on the **Pi 1** (only variant A/B).

- [ ] **Step 3: Decide defaults** — if variant A works (multiple kmssinks on planes in one process), keep `sink: auto → planes when planes: set`; else make `auto` prefer `compositor` (or `glvideomixer` if C clearly wins — then add `gl` as a third sink strategy in `pipeline.go`: branches `! glupload ! mix.sink_i`, mixer `glvideomixer`, tail `! gldownload ! kmssink sync=false`, with a golden test like Task 3). Document the numbers in the runbook.

- [ ] **Step 4: Commit**

```bash
git add client/spikes/spike-b.sh
git commit -m "chore(client): spike B display backend experiments"
```

---

### Task 7: Deployment + runbook

**Files:**
- Create: `deploy/eufy-wall.service`, `deploy/install-client.sh`, `docs/runbook-client.md`

- [ ] **Step 1: `deploy/eufy-wall.service`**

```ini
[Unit]
Description=eufy-wall camera grid on HDMI
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=wall
Group=wall
SupplementaryGroups=video render
Environment=GST_DEBUG=2
ExecStart=/usr/local/bin/eufy-wall -config /etc/eufy-wall.yaml
Restart=always
RestartSec=3
StartLimitIntervalSec=0

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: `deploy/install-client.sh`**

```bash
#!/usr/bin/env bash
# Install eufy-wall on a Raspberry Pi (OS Lite, Bookworm/Trixie) or Debian x86 box. Run as root from the
# repo root after building the binary (make pi1|pi3|pi64|amd64) or with a downloaded release binary.
#   sudo deploy/install-client.sh client/bin/eufy-wall-armv7
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root"; exit 1; }
BIN=${1:?path to eufy-wall binary}
REPO=$(cd "$(dirname "$0")/.." && pwd)

apt-get update
apt-get install -y gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad libdrm-tests
# x86 VAAPI / software fallback (harmless on the Pi if unavailable)
apt-get install -y gstreamer1.0-vaapi gstreamer1.0-libav 2>/dev/null || true

install -m 755 "$BIN" /usr/local/bin/eufy-wall
id -u wall >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin --groups video,render wall
[[ -f /etc/eufy-wall.yaml ]] || { cp "$REPO/client/config.example.yaml" /etc/eufy-wall.yaml; echo "edit /etc/eufy-wall.yaml"; }
cp "$REPO/deploy/eufy-wall.service" /etc/systemd/system/
systemctl daemon-reload && systemctl enable eufy-wall

if [[ -f /boot/firmware/config.txt ]]; then
  CFG=/boot/firmware/config.txt
  grep -q '^dtoverlay=vc4-kms-v3d' "$CFG" || echo 'dtoverlay=vc4-kms-v3d' >> "$CFG"
  grep -q '^gpu_mem=' "$CFG" || echo 'gpu_mem=128' >> "$CFG"
  grep -q '^hdmi_blanking=' "$CFG" || echo 'hdmi_blanking=0' >> "$CFG"
  echo "Pi config.txt updated (vc4-kms-v3d, gpu_mem=128, hdmi_blanking=0) — reboot required"
fi
echo "installed. next: edit /etc/eufy-wall.yaml (rtsp_base, tiles, planes); eufy-wall -config /etc/eufy-wall.yaml -dry-run; systemctl start eufy-wall; journalctl -fu eufy-wall"
```
Run: `chmod +x deploy/install-client.sh && bash -n deploy/install-client.sh`.

- [ ] **Step 3: `docs/runbook-client.md`**

```markdown
# Runbook — eufy-wall (display client)

## Hardware / OS
- Raspberry Pi 3 (recommended), Pi 1/Zero (H.264 only, ≤ 4×720p — see limits), or Debian x86 (VAAPI).
- Raspberry Pi OS **Lite** (Bookworm or Trixie), no desktop. Ethernet preferred.
- `/boot/firmware/config.txt`: `dtoverlay=vc4-kms-v3d`, `gpu_mem=128`, `hdmi_blanking=0` (install script adds them).
- Cameras must stream **H.264** (the bridge's /api/cameras shows `codec`). The Pi has no HEVC decoder.

## Install
    make pi3            # on your workstation (or pi1 / pi64 / amd64) → client/bin/eufy-wall-armv7
    scp client/bin/eufy-wall-armv7 pi:/tmp/ ; scp -r deploy client/config.example.yaml pi:/tmp/
    sudo deploy/install-client.sh /tmp/eufy-wall-armv7
    sudo nano /etc/eufy-wall.yaml      # rtsp_base → your server, tiles, layout
    eufy-wall -config /etc/eufy-wall.yaml -dry-run   # shows the tile table + the gst-launch line
    sudo systemctl start eufy-wall && journalctl -fu eufy-wall

## Layouts
`1`, `2x2`, `3x3`, `1+5` (primary 2×2 at `left` or `right` of a 3×3 grid; `center` is not possible with 3
columns). A tile with `aspect: tall` (an E340 in split-view) takes 1 column × 2 rows — in `1+5` that is the
side column next to the primary; if there is no room it is letterboxed in one cell.

## Sink strategy (from Spike B — fill in measured numbers)
| Pi | streams | sink=planes | sink=compositor | notes |
|----|---------|-------------|-----------------|-------|
| Pi 3 | 6×720p15 | | | |
| Pi 1 | 4×720p15 | | | |
- `sink: planes` needs the DRM overlay plane ids: `modetest -M vc4 -p` → "Planes" table → ids whose
  `type` is Overlay. Put ≥ N ids in `planes:`.
- `sink: compositor` needs no ids and works on x86 too.

## Troubleshooting
- Black screen, logs say `Could not open DRM`/`Permission denied` → user `wall` must be in `video`+`render`
  and nothing else (X/Wayland/getty splash) may own the display; `systemctl stop getty@tty1` if needed.
- `v4l2h264dec` missing → `apt install gstreamer1.0-plugins-good`; `/dev/video10` missing → kernel/firmware
  without `bcm2835-codec` — check `dmesg | grep codec`.
- Decoder hangs after a while on kernel 6.6.x (`h264_v4l2m2m` regression) → `journalctl` shows no frames;
  the supervisor restarts the pipeline; upgrade the kernel (`sudo apt full-upgrade`).
- One camera down restarts the whole wall (single pipeline) — expected in Phase 1; the bridge keeps the
  others warm so they return in ~2 s.
- Too slow (dropped frames, CPU > 80 %) → lower secondaries' quality on the server (`cameras.<sn>.quality:
  "HD (720P)"`) or use a smaller layout.
```

- [ ] **Step 4: Commit**

```bash
git add deploy/eufy-wall.service deploy/install-client.sh docs/runbook-client.md
git commit -m "feat(client): systemd unit, Pi install script and runbook"
```

---

### Task 8: End-to-end on the Pi 3 (and Pi 1)

- [ ] **Step 1:** Build (`make pi3`), install on the Pi 3 per runbook, point `rtsp_base` at the server, configure `1+5` with the real serials (E340 as `aspect: tall`).
- [ ] **Step 2:** `-dry-run` shows the expected table; `systemctl start eufy-wall` renders all tiles within ~10 s; `top` CPU < 60 %.
- [ ] **Step 3:** `sudo kill -9 $(pgrep gst-launch)` → tiles back within 5 s (journal shows `restarting in 1s`).
- [ ] **Step 4:** Unplug the Pi's Ethernet 30 s → after reconnect, pipeline restarts on its own (rtspsrc errors → exit → backoff → restart) and all tiles return.
- [ ] **Step 5:** Pi 1: `make pi1`, same install; try `2x2` at 720p, then `1+5` with secondaries at `HD (720P)`; record what holds in the runbook table.
- [ ] **Step 6:** Commit runbook numbers: `git commit -am "docs: spike B / e2e results on Pi 3 and Pi 1"`.

---

## Self-review notes

- Spec coverage: config + layouts incl. spans/tall/letterbox (T1–T2), pipeline for Pi planes / compositor / x86 / software (T3), auto-detect decoder+sink+screen (T4), supervisor with backoff + systemd (T5, T7), Spike B (T6), Pi 1 as a client target via `make pi1` and runbook limits (T1, T7, T8), no Docker (T7). Phase 2 (WS events, per-tile pipelines, placeholders) intentionally absent.
- `primary_position: center` is rejected with an explanation rather than silently mapped — the spec's "left or center" wording is not realizable in a 3-column grid; flagged for the user.
- Type consistency: `layout.Placed` fields used identically in T2/T3/T5; `pipeline.Caps{Decoder,Sink,Screen}` in T3/T4/T5; `config.Restart{MinSeconds,MaxSeconds,StableSeconds}` in T1/T5.
