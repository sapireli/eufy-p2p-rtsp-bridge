package layout

import (
	"fmt"

	"eufy-wall/internal/config"
)

// StarterTile is one named rectangle in the editor and setup wizard's shared layouts.
type StarterTile struct {
	ID   string
	Rect config.Rect
}

func StarterTemplate(name string) ([]StarterTile, error) {
	switch name {
	case "one", "motion":
		id := "camera-1"
		if name == "motion" {
			id = "recent-motion"
		}
		return []StarterTile{{id, config.Rect{X: 0, Y: 0, W: 32, H: 32}}}, nil
	case "split":
		return []StarterTile{
			{"camera-1", config.Rect{X: 0, Y: 0, W: 16, H: 32}},
			{"camera-2", config.Rect{X: 16, Y: 0, W: 16, H: 32}},
		}, nil
	case "four":
		return []StarterTile{
			{"camera-1", config.Rect{X: 0, Y: 0, W: 16, H: 16}},
			{"camera-2", config.Rect{X: 16, Y: 0, W: 16, H: 16}},
			{"camera-3", config.Rect{X: 0, Y: 16, W: 16, H: 16}},
			{"camera-4", config.Rect{X: 16, Y: 16, W: 16, H: 16}},
		}, nil
	case "1+5", "one-plus-five":
		return []StarterTile{
			{"primary", config.Rect{X: 0, Y: 0, W: 21, H: 21}},
			{"side-1", config.Rect{X: 21, Y: 0, W: 11, H: 11}},
			{"side-2", config.Rect{X: 21, Y: 11, W: 11, H: 10}},
			{"bottom-1", config.Rect{X: 0, Y: 21, W: 11, H: 11}},
			{"bottom-2", config.Rect{X: 11, Y: 21, W: 10, H: 11}},
			{"bottom-3", config.Rect{X: 21, Y: 21, W: 11, H: 11}},
		}, nil
	default:
		return nil, fmt.Errorf("unknown template %q (one, split, four, 1+5, motion)", name)
	}
}
