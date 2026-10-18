// Package layout places tiles on a cell grid and converts cells to pixel rectangles.
package layout

import (
	"fmt"

	"eufy-wall/internal/config"
)

type Placed struct {
	Index  int
	Camera string
	Codec  string // h264 | h265 — which decoder this tile needs
	URL    string
	// StillURL, when set, is an HTTP JPEG to display instead of URL — the camera's last retained
	// thumbnail, shown while it is asleep or waking. It needs no decoder and wakes nothing.
	StillURL   string
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
	if c.Layout == config.LayoutTwoTallThreeWide {
		return placeTwoTallThreeWide(c, screen)
	}
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
		p := Placed{Index: pi, Camera: c.Tiles[pi].Camera, Codec: c.Tiles[pi].Codec, URL: c.TileURL(c.Tiles[pi]), Col: col, Row: 0, Cols: 2, Rows: 2}
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
		p := Placed{Index: i, Camera: t.Camera, Codec: t.Codec, URL: c.TileURL(t)}
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

func placeTwoTallThreeWide(c *config.Config, screen config.Screen) ([]Placed, error) {
	count := len(c.Tiles)
	if count < 2 || count > 5 {
		return nil, fmt.Errorf("layout %s requires 2-5 tiles", c.Layout)
	}
	sideCount := count - 2
	if screen.Width < 3 || screen.Height < max(1, sideCount) {
		return nil, fmt.Errorf("layout %s: screen %dx%d is too small", c.Layout, screen.Width, screen.Height)
	}
	// Calculate shared boundaries rather than rounding each tile independently.
	// Division first also avoids overflowing an int on ARMv6.
	boundary := func(size, part, total int) int { return size/total*part + size%total*part/total }
	leftEnd, centerEnd := boundary(screen.Width, 35, 100), boundary(screen.Width, 70, 100)
	out := make([]Placed, count)
	for i, tile := range c.Tiles {
		if tile.Span != nil {
			return nil, fmt.Errorf("layout %s fixes tile positions; remove tile %d span", c.Layout, i)
		}
		p := Placed{Index: i, Camera: tile.Camera, Codec: tile.Codec, URL: c.TileURL(tile)}
		switch i {
		case 0:
			p.Cols, p.Rows, p.W, p.H = 7, 6, leftEnd, screen.Height
		case 1:
			p.Col, p.Cols, p.Rows = 7, 7, 6
			p.X, p.W, p.H = leftEnd, centerEnd-leftEnd, screen.Height
		default:
			row := i - 2
			p.Col, p.Row, p.Cols, p.Rows = 14, row*(6/sideCount), 6, 6/sideCount
			p.X, p.Y = centerEnd, boundary(screen.Height, row, sideCount)
			p.W, p.H = screen.Width-centerEnd, boundary(screen.Height, row+1, sideCount)-p.Y
		}
		out[i] = p
	}
	return out, nil
}
