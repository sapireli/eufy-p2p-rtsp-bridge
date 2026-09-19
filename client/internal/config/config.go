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
