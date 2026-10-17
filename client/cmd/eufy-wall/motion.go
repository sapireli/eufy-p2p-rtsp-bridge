package main

import "eufy-wall/internal/layout"

// Use resolved camera identities, including motion-following tiles. Never flash
// a different camera merely because it occupies the tile's configured position.
func motionTileIndices(tiles []layout.Placed, showing map[int]string, camera string) []int {
	if camera == "" {
		return nil
	}
	var out []int
	for _, tile := range tiles {
		current, resolved := showing[tile.Index]
		if !resolved {
			current = tile.Camera
		}
		if current == camera {
			out = append(out, tile.Index)
		}
	}
	return out
}
