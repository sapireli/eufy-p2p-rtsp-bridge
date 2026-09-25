package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/wsclient"
	"gopkg.in/yaml.v3"
)

// migrateLegacyForEditor prepares a v2 draft in memory. The caller shows the diff and requires an
// explicit MIGRATE answer before entering the editor. Strict decode prevents unknown legacy fields
// from vanishing in the round trip.
func migrateLegacyForEditor(data []byte) ([]byte, string, error) {
	c, err := config.Parse(data)
	if err != nil {
		return nil, "", err
	}
	if c.SchemaVersion != 0 {
		return nil, "", errors.New("only unversioned legacy config can be migrated")
	}
	strict := &config.Config{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(strict); err != nil {
		return nil, "", fmt.Errorf("legacy migration would lose an unsupported field: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, "", errors.New("legacy migration requires one YAML document")
	}
	if c.RTSPBase == "" {
		return nil, "", errors.New("legacy migration needs rtsp_base so the bridge control URL can be derived; add it to the source file")
	}
	bridge := c.BridgeURL
	if bridge == "" {
		ws := wsclient.EventURL(c.RTSPBase)
		if ws == "" {
			return nil, "", errors.New("cannot derive bridge_url from legacy rtsp_base")
		}
		u, err := url.Parse(ws)
		if err != nil {
			return nil, "", err
		}
		if u.Scheme == "wss" {
			u.Scheme = "https"
		} else {
			u.Scheme = "http"
		}
		u.Path = ""
		u.RawPath = ""
		bridge = u.String()
	}
	cols, rows := c.GridDims()
	screen := c.Screen
	if screen.Width == 0 || screen.Height == 0 {
		screen = config.Screen{Width: 1920, Height: 1080}
	}
	placed, err := layout.Place(c, screen)
	if err != nil {
		return nil, "", fmt.Errorf("legacy layout does not fit: %w", err)
	}
	oldLayout := c.Layout
	c.SchemaVersion = 2
	c.BridgeURL = bridge
	c.Layout = "custom"
	c.Canvas = config.Canvas{Cols: cols, Rows: rows}
	c.PrimaryPosition = ""
	var diff strings.Builder
	fmt.Fprintf(&diff, "  schema_version: omitted -> 2\n  layout: %s -> custom, canvas %dx%d\n  bridge_url: %s (verify before apply)\n", oldLayout, cols, rows, bridge)
	for i := range c.Tiles {
		p := placed[i]
		t := &c.Tiles[i]
		t.ID = fmt.Sprintf("tile-%d", i+1)
		t.Rect = &config.Rect{X: p.Col, Y: p.Row, W: p.Cols, H: p.Rows}
		t.Span = nil
		t.Role = ""
		t.Aspect = ""
		fmt.Fprintf(&diff, "  tiles[%d]: id=%s rect=(%d,%d %dx%d)\n", i, t.ID, p.Col, p.Row, p.Cols, p.Rows)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return nil, "", err
	}
	parsed, err := config.Parse(b)
	if err != nil {
		return nil, "", err
	}
	if _, err := layout.Place(parsed, screen); err != nil {
		return nil, "", err
	}
	return b, diff.String(), nil
}
