package config

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestRTSPPacketSizeConfigBounds(t *testing.T) {
	for _, value := range []int{0, 256, 8192, 65535, -1, 1, 255, 65536} {
		c, err := Parse([]byte(fmt.Sprintf("rtsp_base: rtsp://bridge:8554\nrtsp_packet_size: %d\ntiles: [{camera: FRONT}]\n", value)))
		valid := value == 0 || value >= 256 && value <= 65535
		if (err == nil) != valid {
			t.Fatalf("value %d: config=%v error=%v", value, c, err)
		}
		if valid && (c.RTSPPacketSize == nil || *c.RTSPPacketSize != value) {
			t.Fatalf("value %d was not retained", value)
		}
	}
}

func TestRTSPPacketSizeHintsPreserveExplicitURLsAndIdentity(t *testing.T) {
	base := &Config{RTSPBase: "rtsp://bridge:8554"}
	tuned := base.WithRTSPPacketSizeHints(map[string]int{"FRONT": 8192, "BAD": 65536})
	if got := tuned.TileURLForStream(Tile{Camera: "FRONT"}, "front_door"); got != "rtsp://bridge:8554/front_door?pkt_size=8192" {
		t.Fatalf("camera hint lost with stream alias: %q", got)
	}
	for _, tile := range []Tile{{Camera: "FRONT", URL: "rtsp://manual:8554/custom?video=1"}, {Camera: "BAD"}, {Camera: "UNKNOWN"}} {
		if got, want := tuned.TileURL(tile), base.TileURL(tile); got != want {
			t.Fatalf("unexpected URL tuning: got %q want %q", got, want)
		}
	}
	if got := base.TileURL(Tile{Camera: "FRONT"}); got != "rtsp://bridge:8554/FRONT" {
		t.Fatalf("snapshot mutated original config: %q", got)
	}
	older := tuned.WithRTSPPacketSizeHints(map[string]int{"FRONT": 0})
	if got := older.TileURL(Tile{Camera: "FRONT"}); got != "rtsp://bridge:8554/FRONT" {
		t.Fatalf("older bridge retained stale hint: %q", got)
	}
	if got := tuned.TileURL(Tile{Camera: "FRONT"}); !strings.Contains(got, "pkt_size=8192") {
		t.Fatalf("snapshot mutated previous hint: %q", got)
	}
}

func TestRTSPPacketSizeOverrideAndDisable(t *testing.T) {
	c := (&Config{RTSPBase: "rtsp://bridge:8554"}).WithRTSPPacketSizeHints(map[string]int{"FRONT": 8192})
	zero := 0
	c.RTSPPacketSize = &zero
	if got := c.TileURL(Tile{Camera: "FRONT"}); strings.Contains(got, "pkt_size") {
		t.Fatalf("explicit zero did not disable automatic hint: %q", got)
	}
	override := 4096
	c.RTSPPacketSize = &override
	u, err := url.Parse(c.TileURL(Tile{Camera: "FRONT", URL: "rtsps://manual:8554/custom?video=1&pkt_size=512#camera"}))
	if err != nil || u.Query().Get("pkt_size") != "4096" || u.Query().Get("video") != "1" || u.Fragment != "camera" {
		t.Fatalf("override lost URL options: url=%v err=%v", u, err)
	}
	if got := c.TileURL(Tile{URL: "http://bridge/snapshot/FRONT"}); got != "http://bridge/snapshot/FRONT" {
		t.Fatalf("non-RTSP URL changed: %q", got)
	}
}

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
