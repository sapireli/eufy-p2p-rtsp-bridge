package layout

import (
	"testing"

	"eufy-wall/internal/config"
)

func TestCustomOneCellTilesPartitionEveryPixel(t *testing.T) {
	c := &config.Config{Layout: "custom", Canvas: config.Canvas{Cols: 32, Rows: 1}, RTSPBase: "rtsp://s"}
	for x := 0; x < 32; x++ {
		c.Tiles = append(c.Tiles, config.Tile{ID: "tile", Camera: "A", Rect: &config.Rect{X: x, Y: 0, W: 1, H: 1}})
	}
	for _, width := range []int{32, 33, 1919, 1920, 1921} {
		placed, err := Place(c, config.Screen{Width: width, Height: 1080})
		if err != nil {
			t.Fatal(err)
		}
		edge := 0
		for i, tile := range placed {
			if tile.X != edge || tile.W < 1 {
				t.Fatalf("width=%d tile=%d has gap or zero width: %+v", width, i, tile)
			}
			edge = tile.X + tile.W
		}
		if edge != width {
			t.Fatalf("width=%d ends at %d", width, edge)
		}
	}
}

func TestPlaceRejectsInvalidDirectConfig(t *testing.T) {
	c := &config.Config{Layout: "custom", Canvas: config.Canvas{Cols: 2, Rows: 1}, RTSPBase: "rtsp://s", Tiles: []config.Tile{
		{Camera: "A", Rect: &config.Rect{X: 0, Y: 0, W: 2, H: 1}},
		{Camera: "B", Rect: &config.Rect{X: 1, Y: 0, W: 1, H: 1}},
	}}
	if _, err := Place(c, config.Screen{Width: 1920, Height: 1080}); err == nil {
		t.Fatal("overlap bypassed by direct config")
	}
	c.Tiles[1].Rect = nil
	if _, err := Place(c, config.Screen{Width: 1920, Height: 1080}); err == nil {
		t.Fatal("missing rect bypassed by direct config")
	}
	if _, err := Place(c, config.Screen{}); err == nil {
		t.Fatal("zero screen accepted")
	}
}
