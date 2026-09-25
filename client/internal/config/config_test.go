package config

import (
	"bytes"
	"os"
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
	if got := c.TileURL(Tile{Camera: "Front Door/1"}); got != "rtsp://192.168.1.10:8554/Front%20Door%2F1" {
		t.Fatalf("stream key was not escaped as one RTSP path segment: %q", got)
	}
	if c.Tiles[3].Span == nil || *c.Tiles[3].Span != (Span{1, 1}) {
		t.Fatalf("span %+v", c.Tiles[3].Span)
	}
	cols, rows := c.GridDims()
	if cols != 3 || rows != 3 {
		t.Fatalf("dims %dx%d", cols, rows)
	}
}

func TestRestartBounds(t *testing.T) {
	c, err := Parse([]byte("rtsp_base: rtsp://x\nlayout: 1\nrestart: {min_seconds: 2, max_seconds: 2}\ntiles: [{camera: A}]\n"))
	if err != nil {
		t.Fatalf("max == min must be accepted: %v", err)
	}
	if c.Restart != (Restart{MinSeconds: 2, MaxSeconds: 2, StableSeconds: 60}) {
		t.Fatalf("restart %+v", c.Restart)
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
		"no tiles":        "rtsp_base: rtsp://x\nlayout: 1\n",
		"bad layout":      "rtsp_base: rtsp://x\nlayout: banana\ntiles: [{camera: A}]\n",
		"grid too big":    "rtsp_base: rtsp://x\nlayout: 7x7\ntiles: [{camera: A}]\n",
		"grid zero side":  "rtsp_base: rtsp://x\nlayout: 0x2\ntiles: [{camera: A}]\n",
		"grid no rows":    "rtsp_base: rtsp://x\nlayout: 2x\ntiles: [{camera: A}]\n",
		"center":          "rtsp_base: rtsp://x\nlayout: 1+5\nprimary_position: center\ntiles: [{camera: A}]\n",
		"no base no url":  "layout: 1\ntiles: [{camera: A}]\n",
		"bad aspect":      "rtsp_base: rtsp://x\nlayout: 1\ntiles: [{camera: A, aspect: square}]\n",
		"bad sink":        "rtsp_base: rtsp://x\nlayout: 1\nsink: magic\ntiles: [{camera: A}]\n",
		"unsafe output":   "rtsp_base: rtsp://x\nlayout: 1\noutput: ../HDMI-A-1\ntiles: [{camera: A}]\n",
		"too many tiles":  "rtsp_base: rtsp://x\nlayout: 2x2\ntiles: [{camera: A},{camera: B},{camera: C},{camera: D},{camera: E}]\n",
		"restart min 0":   "rtsp_base: rtsp://x\nlayout: 1\nrestart: {min_seconds: 0}\ntiles: [{camera: A}]\n",
		"restart min<0":   "rtsp_base: rtsp://x\nlayout: 1\nrestart: {min_seconds: -5}\ntiles: [{camera: A}]\n",
		"restart max<min": "rtsp_base: rtsp://x\nlayout: 1\nrestart: {min_seconds: 10, max_seconds: 5}\ntiles: [{camera: A}]\n",
	}
	for name, y := range cases {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected error", name)
		} else if name == "center" && !strings.Contains(err.Error(), "3-column") {
			t.Errorf("center error should explain: %v", err)
		}
	}
}

// A wall is not limited to the grid shapes that happen to have a name: any <cols>x<rows> is a layout.
// 2x1 is the one this wall is built around — two portrait dual-lens cameras side by side at full height.
func TestGridLayouts(t *testing.T) {
	for _, tc := range []struct {
		layout     string
		cols, rows int
	}{
		{"2x1", 2, 1}, {"1x2", 1, 2}, {"3x1", 3, 1}, {"2x2", 2, 2}, {"3x3", 3, 3}, {"6x6", 6, 6},
		{"1", 1, 1}, {"1+5", 3, 3},
	} {
		y := "rtsp_base: rtsp://x\nlayout: " + tc.layout + "\ntiles: [{camera: A}]\n"
		c, err := Parse([]byte(y))
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.layout, err)
			continue
		}
		if cols, rows := c.GridDims(); cols != tc.cols || rows != tc.rows {
			t.Errorf("%s: got %dx%d, want %dx%d", tc.layout, cols, rows, tc.cols, tc.rows)
		}
	}
}

func TestV2CustomRectanglesAndStableIDs(t *testing.T) {
	y := `schema_version: 2
bridge_url: http://bridge.local:3000
rtsp_base: rtsp://bridge.local:8554
layout: custom
canvas: {cols: 32, rows: 32}
tiles:
  - {id: front, camera: A, rect: {x: 0, y: 0, w: 20, h: 32}}
  - {id: motion, motion: latest, watch: [B], rect: {x: 20, y: 0, w: 12, h: 32}}
`
	c, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if c.SchemaVersion != 2 || c.BridgeURL != "http://bridge.local:3000" || c.Tiles[1].ID != "motion" {
		t.Fatalf("%+v", c)
	}
	if cols, rows := c.GridDims(); cols != 32 || rows != 32 {
		t.Fatalf("%dx%d", cols, rows)
	}
}

func TestV2RejectsUnsafeGeometryAndUnknownFields(t *testing.T) {
	base := "schema_version: 2\nbridge_url: http://x:3000\nrtsp_base: rtsp://x\nlayout: custom\ncanvas: {cols: 32, rows: 32}\n"
	cases := map[string]string{
		"overlap":            "tiles: [{id: a, camera: A, rect: {x: 0, y: 0, w: 20, h: 32}}, {id: b, camera: B, rect: {x: 19, y: 0, w: 13, h: 32}}]",
		"out of bounds":      "tiles: [{id: a, camera: A, rect: {x: 31, y: 0, w: 2, h: 1}}]",
		"zero size":          "tiles: [{id: a, camera: A, rect: {x: 0, y: 0, w: 0, h: 1}}]",
		"duplicate id":       "tiles: [{id: a, camera: A, rect: {x: 0, y: 0, w: 1, h: 1}}, {id: a, camera: B, rect: {x: 1, y: 0, w: 1, h: 1}}]",
		"missing id":         "tiles: [{camera: A, rect: {x: 0, y: 0, w: 1, h: 1}}]",
		"missing rect":       "tiles: [{id: a, camera: A}]",
		"unknown field":      "tiles: [{id: a, camera: A, rect: {x: 0, y: 0, w: 1, h: 1}, mystery: 1}]",
		"ambiguous geometry": "tiles: [{id: a, camera: A, span: {cols: 1, rows: 1}, rect: {x: 0, y: 0, w: 1, h: 1}}]",
	}
	for name, tail := range cases {
		if _, err := Parse([]byte(base + tail + "\n")); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestV2RejectsFutureVersionAndInvalidBridgeOrigin(t *testing.T) {
	for _, y := range []string{
		"schema_version: 3\nlayout: 1\nrtsp_base: rtsp://x\ntiles: [{camera: A}]",
		"schema_version: 2\nbridge_url: http://user:pass@x:3000\nlayout: 1\nrtsp_base: rtsp://x\ntiles: [{id: a, camera: A}]",
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("expected rejection: %s", y)
		}
	}
}

func TestEmbeddedExampleMatchesCheckedInTemplate(t *testing.T) {
	if _, err := Parse(Example()); err != nil {
		t.Fatalf("embedded example must parse: %v", err)
	}
	checkedIn, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(Example(), checkedIn) {
		t.Fatal("embedded and checked-in examples differ")
	}
}

func TestLegacyMaximumGridCapacityRemainsValid(t *testing.T) {
	y := "rtsp_base: rtsp://x\nlayout: 6x6\ntiles:\n"
	for i := 0; i < 36; i++ {
		y += "  - {camera: A}\n"
	}
	c, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tiles) != 36 || c.Tiles[35].ID != "legacy-35" {
		t.Fatalf("legacy ids: %+v", c.Tiles[35])
	}
}
