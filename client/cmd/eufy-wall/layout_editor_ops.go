package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

func editorMutation(c *config.Config, args []string) error {
	switch args[0] {
	case "template":
		if len(args) != 2 {
			return errors.New("usage: template one|split|four|one-plus-five|motion")
		}
		return editorTemplate(c, args[1])
	case "add":
		if len(args) != 7 {
			return errors.New("usage: add <id> <camera> <x> <y> <w> <h>")
		}
		r, err := rectArgs(args[3:])
		if err != nil {
			return err
		}
		t := config.Tile{ID: args[1], Camera: args[2], Rect: &r}
		if args[2] == "latest" {
			t.Camera, t.Motion = "", "latest"
		}
		c.Tiles = append(c.Tiles, t)
		return nil
	case "delete":
		if len(args) != 2 {
			return errors.New("usage: delete <id>")
		}
		i, err := tileIndex(c, args[1])
		if err != nil {
			return err
		}
		c.Tiles = append(c.Tiles[:i], c.Tiles[i+1:]...)
		return nil
	case "rect", "move", "resize":
		if len(args) != 6 && args[0] == "rect" || len(args) != 4 && args[0] != "rect" {
			return fmt.Errorf("usage: %s <id> %s", args[0], map[string]string{"rect": "<x> <y> <w> <h>", "move": "<dx> <dy>", "resize": "<dw> <dh>"}[args[0]])
		}
		i, err := tileIndex(c, args[1])
		if err != nil {
			return err
		}
		if c.Tiles[i].Rect == nil {
			return fmt.Errorf("tile %q has no rectangle", args[1])
		}
		r := *c.Tiles[i].Rect
		if args[0] == "rect" {
			r, err = rectArgs(args[2:])
		} else {
			dx, e1 := strconv.Atoi(args[2])
			dy, e2 := strconv.Atoi(args[3])
			if e1 != nil || e2 != nil {
				return errors.New("offsets must be integers")
			}
			if args[0] == "move" {
				r.X += dx
				r.Y += dy
			} else {
				r.W += dx
				r.H += dy
			}
		}
		if err != nil {
			return err
		}
		c.Tiles[i].Rect = &r
		return nil
	case "camera":
		if len(args) != 3 {
			return errors.New("usage: camera <id> <serial>")
		}
		i, err := tileIndex(c, args[1])
		if err != nil {
			return err
		}
		if args[2] == "" {
			return errors.New("camera serial is required")
		}
		t := &c.Tiles[i]
		t.Camera, t.Motion, t.URL, t.Watch = args[2], "", "", nil
		t.BlankAfterSeconds, t.DwellSeconds = 0, 0
		return nil
	case "motion":
		if len(args) < 3 || len(args) > 5 {
			return errors.New("usage: motion <id> <serial,...|all> [blank_seconds] [dwell_seconds]")
		}
		i, err := tileIndex(c, args[1])
		if err != nil {
			return err
		}
		t := &c.Tiles[i]
		t.Camera, t.Motion, t.URL = "", "latest", ""
		if args[2] == "all" {
			t.Watch = nil
		} else {
			t.Watch = strings.Split(args[2], ",")
			for _, sn := range t.Watch {
				if sn == "" {
					return errors.New("watch serials cannot be empty")
				}
			}
		}
		t.BlankAfterSeconds, t.DwellSeconds = 0, 0
		if len(args) >= 4 {
			n, err := strconv.Atoi(args[3])
			if err != nil || n < 0 {
				return errors.New("blank_seconds must be a nonnegative integer")
			}
			t.BlankAfterSeconds = n
		}
		if len(args) == 5 {
			n, err := strconv.Atoi(args[4])
			if err != nil || n < 0 {
				return errors.New("dwell_seconds must be a nonnegative integer")
			}
			t.DwellSeconds = n
		}
		return nil
	}
	return fmt.Errorf("unknown edit command %q", args[0])
}

func tileIndex(c *config.Config, id string) (int, error) {
	for i, t := range c.Tiles {
		if t.ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("tile %q not found", id)
}

func rectArgs(args []string) (config.Rect, error) {
	if len(args) != 4 {
		return config.Rect{}, errors.New("rectangle needs x y w h")
	}
	n := [4]int{}
	for i, a := range args {
		v, err := strconv.Atoi(a)
		if err != nil {
			return config.Rect{}, fmt.Errorf("rectangle %q is not an integer", a)
		}
		n[i] = v
	}
	return config.Rect{X: n[0], Y: n[1], W: n[2], H: n[3]}, nil
}

// Templates always write explicit rectangles on a 32x32 canvas. Existing fixed camera serials are
// reused in order; missing sources get visible placeholders that must be replaced before deployment.
func editorTemplate(c *config.Config, name string) error {
	sources := []string{}
	for _, t := range c.Tiles {
		if t.Camera != "" {
			sources = append(sources, t.Camera)
		}
	}
	source := func(i int) string {
		if i < len(sources) {
			return sources[i]
		}
		return fmt.Sprintf("CAMERA_%d", i+1)
	}
	geometry, err := layout.StarterTemplate(name)
	if err != nil {
		return err
	}
	c.SchemaVersion, c.Layout, c.Canvas = 2, "custom", config.Canvas{Cols: 32, Rows: 32}
	c.Tiles = make([]config.Tile, 0, len(geometry))
	for i, entry := range geometry {
		rect := entry.Rect
		tile := config.Tile{ID: entry.ID, Camera: source(i), Rect: &rect}
		if name == "motion" {
			tile.Camera, tile.Motion, tile.BlankAfterSeconds = "", "latest", 90
		}
		c.Tiles = append(c.Tiles, tile)
	}
	return nil
}
