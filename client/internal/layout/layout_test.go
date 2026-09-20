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
	p, err := Place(cfg("1", "", config.Tile{Camera: "A"}), config.Screen{Width: 1920, Height: 1080})
	if err != nil || len(p) != 1 || rect(p[0]) != [4]int{0, 0, 1920, 1080} || p[0].URL != "rtsp://s:8554/A" {
		t.Fatalf("%v %+v", err, p)
	}
}

func Test2x2RowMajorWithRemainder(t *testing.T) {
	p, err := Place(cfg("2x2", "", config.Tile{Camera: "A"}, config.Tile{Camera: "B"}, config.Tile{Camera: "C"}), config.Screen{Width: 1919, Height: 1079})
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
	p, err := Place(cfg("1+5", "left", tiles...), config.Screen{Width: 1920, Height: 1080})
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
	p, _ = Place(cfg("1+5", "right", tiles...), config.Screen{Width: 1920, Height: 1080})
	if rect(p[0]) != [4]int{640, 0, 1280, 720} || rect(p[1]) != [4]int{0, 0, 640, 360} {
		t.Errorf("primary right %v first secondary %v", rect(p[0]), rect(p[1]))
	}
}

func Test1Plus5PrimaryDefaultsToFirstTile(t *testing.T) {
	p, err := Place(cfg("1+5", "left", config.Tile{Camera: "A"}, config.Tile{Camera: "B"}), config.Screen{Width: 1920, Height: 1080})
	if err != nil || p[0].Cols != 2 || p[0].Rows != 2 || p[0].Camera != "A" {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestTallTileGets1x2InSideColumn(t *testing.T) {
	tiles := []config.Tile{{Camera: "P", Role: "primary"}, {Camera: "T", Aspect: "tall"}, {Camera: "C"}, {Camera: "D"}, {Camera: "E"}}
	p, err := Place(cfg("1+5", "left", tiles...), config.Screen{Width: 1920, Height: 1080})
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
	p, err := Place(cfg("2x2", "", tiles...), config.Screen{Width: 1920, Height: 1080})
	if err != nil {
		t.Fatal(err)
	}
	if !p[2].Letterbox || p[2].Cols != 1 || p[2].Rows != 1 || rect(p[2]) != [4]int{0, 540, 960, 540} {
		t.Errorf("%+v", p[2])
	}
}

func TestExplicitSpanWinsAndOverflowErrors(t *testing.T) {
	two := config.Span{Cols: 2, Rows: 1}
	p, err := Place(cfg("3x3", "", config.Tile{Camera: "A", Span: &two}, config.Tile{Camera: "B"}), config.Screen{Width: 1920, Height: 1080})
	if err != nil || rect(p[0]) != [4]int{0, 0, 1280, 360} || rect(p[1]) != [4]int{1280, 0, 640, 360} {
		t.Fatalf("%v %+v", err, p)
	}
	big := config.Span{Cols: 2, Rows: 2}
	_, err = Place(cfg("2x2", "", config.Tile{Camera: "A", Span: &big}, config.Tile{Camera: "B"}), config.Screen{Width: 1920, Height: 1080})
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

// Two-up: the shape this wall is built around. Both HomeBase cameras are dual-lens and compose a
// PORTRAIT stream (Front Door 1600x2200, Garage 1920x2160), and two of those side by side at full
// height fill a 16:9 screen almost exactly — 91% of the width, which is why a third tile does not fit
// beside them. Each tile takes a full-height half of the screen; neither is letterboxed into a cell.
func TestTwoUpFillsTheScreen(t *testing.T) {
	c := cfg("2x1", "", config.Tile{Camera: "GARAGE"}, config.Tile{Camera: "FRONTDOOR"})
	got, err := Place(c, config.Screen{Width: 1920, Height: 1080})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tiles, want 2", len(got))
	}
	left, right := got[0], got[1]
	if left.X != 0 || left.Y != 0 || left.W != 960 || left.H != 1080 {
		t.Errorf("left tile = %dx%d at %d,%d; want 960x1080 at 0,0", left.W, left.H, left.X, left.Y)
	}
	if right.X != 960 || right.Y != 0 || right.W != 960 || right.H != 1080 {
		t.Errorf("right tile = %dx%d at %d,%d; want 960x1080 at 960,0", right.W, right.H, right.X, right.Y)
	}
	for _, p := range got {
		if p.Letterbox {
			t.Errorf("%s: tile should own a full-height half, not be letterboxed into a cell", p.Camera)
		}
	}
}
