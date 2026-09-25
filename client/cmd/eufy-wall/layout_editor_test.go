package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

func TestEditorRejectsBadEditsWithoutChangingHistory(t *testing.T) {
	e, err := newLayoutEditor("")
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), e.history[0]...)
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"move", "garage", "1", "0"}) }); err == nil {
		t.Fatal("overlap accepted")
	}
	if e.at != 0 || len(e.history) != 1 || !bytes.Equal(before, e.history[0]) {
		t.Fatal("rejected edit changed history")
	}
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"resize", "garage", "-1", "0"}) }); err != nil {
		t.Fatal(err)
	}
	c, err := e.config()
	if err != nil {
		t.Fatal(err)
	}
	if c.Tiles[0].Rect.W != 15 {
		t.Fatalf("resize missing: %+v", c.Tiles[0].Rect)
	}
	if !e.undo() {
		t.Fatal("undo failed")
	}
	c, _ = e.config()
	if c.Tiles[0].Rect.W != 16 {
		t.Fatal("undo failed to restore width")
	}
	if !e.redo() {
		t.Fatal("redo failed")
	}
	c, _ = e.config()
	if c.Tiles[0].Rect.W != 15 {
		t.Fatal("redo failed to restore width")
	}
}

func TestEditorTemplatesAndMotion(t *testing.T) {
	e, err := newLayoutEditor("")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "split", "four", "one-plus-five", "motion"} {
		if err := e.change(func(c *config.Config) error { return editorTemplate(c, name) }); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	c, _ := e.config()
	if len(c.Tiles) != 1 || c.Tiles[0].Motion != "latest" || c.Tiles[0].Rect.W != 32 {
		t.Fatalf("motion template: %+v", c.Tiles)
	}
	if err := e.change(func(c *config.Config) error {
		return editorMutation(c, []string{"motion", "recent-motion", "BAT1,BAT2", "90", "5"})
	}); err != nil {
		t.Fatal(err)
	}
	c, _ = e.config()
	if len(c.Tiles[0].Watch) != 2 || c.Tiles[0].DwellSeconds != 5 {
		t.Fatalf("motion edit: %+v", c.Tiles[0])
	}
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"camera", "recent-motion", "BAT1"}) }); err != nil {
		t.Fatal(err)
	}
	c, _ = e.config()
	if c.Tiles[0].Motion != "" || len(c.Tiles[0].Watch) != 0 || c.Tiles[0].Camera != "BAT1" {
		t.Fatalf("fixed edit: %+v", c.Tiles[0])
	}
}

func TestEditorSaveIsDraftAndValid(t *testing.T) {
	e, err := newLayoutEditor("")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.save(clientConfigPath); err == nil {
		t.Fatal("direct active config overwrite accepted")
	}
	path := filepath.Join(t.TempDir(), "draft.yaml")
	if err := e.save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("draft mode %o", info.Mode().Perm())
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("saved draft invalid: %v", err)
	}
}

func TestEditorRejectsOversizedImportedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.yaml")
	if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, (4<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newLayoutEditor(path); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversized editor import was accepted: %v", err)
	}
}

func TestEditorScriptKeepsRunningAfterRejectedCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.yaml")
	var output bytes.Buffer
	if err := editLayout("", strings.NewReader("move garage 1 0\nsave "+path+"\nquit\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "error: ") || !strings.Contains(output.String(), "draft saved") {
		t.Fatalf("script output: %s", output.String())
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Tiles[0].Rect.X != 0 {
		t.Fatal("rejected edit leaked into draft")
	}
}

func TestPreviewPNGUsesExactLayoutAndBlackGaps(t *testing.T) {
	c := &config.Config{SchemaVersion: 2, BridgeURL: "http://s:3000", RTSPBase: "rtsp://s", Layout: "custom", Canvas: config.Canvas{Cols: 4, Rows: 1}, Screen: config.Screen{Width: 41, Height: 20}, Tiles: []config.Tile{
		{ID: "left", Camera: "A", Rect: &config.Rect{X: 0, Y: 0, W: 1, H: 1}},
		{ID: "right", Camera: "B", Rect: &config.Rect{X: 3, Y: 0, W: 1, H: 1}},
	}}
	placed, err := layout.Place(c, c.Screen)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preview.png")
	if err := writeLayoutPNG(path, c, placed); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 41 || img.Bounds().Dy() != 20 {
		t.Fatalf("size %v", img.Bounds())
	}
	r, g, b, _ := img.At(20, 10).RGBA()
	if r != 0x0c0c || g != 0x1010 || b != 0x1616 {
		t.Fatalf("gap not dark: %x %x %x", r, g, b)
	}
	if img.At(2, 2) == img.At(39, 2) {
		t.Fatal("separate tile colors indistinguishable")
	}
}

func TestPreviewRejectsOversizedImage(t *testing.T) {
	var out bytes.Buffer
	if err := previewLayout("-", bytes.NewReader(config.Example()), &out, PreviewOptions{Width: 9000, Height: 100}); err == nil {
		t.Fatal("oversized preview accepted")
	}
}

func TestLegacyMigrationRequiresExplicitAcceptanceAndPreservesPlacement(t *testing.T) {
	legacy := "rtsp_base: rtsp://bridge:8554\nlayout: 2x1\ntiles: [{camera: A}, {camera: B}]\n"
	path := filepath.Join(t.TempDir(), "legacy.yaml")
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := editLayout(path, strings.NewReader("no\n"), &output); err == nil {
		t.Fatal("migration accepted without MIGRATE")
	}
	if !strings.Contains(output.String(), "layout: 2x1 -> custom") {
		t.Fatalf("migration diff absent: %s", output.String())
	}
	output.Reset()
	draft := filepath.Join(t.TempDir(), "migrated.yaml")
	if err := editLayout(path, strings.NewReader("MIGRATE\nsave "+draft+"\nquit\n"), &output); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(draft)
	if err != nil {
		t.Fatal(err)
	}
	if c.SchemaVersion != 2 || c.BridgeURL != "http://bridge:3000" || c.Canvas.Cols != 2 || c.Tiles[1].Rect.X != 1 || c.Tiles[1].ID != "tile-2" {
		t.Fatalf("migrated draft %+v", c)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != legacy {
		t.Fatal("source changed during migration")
	}
}

func TestLegacyMigrationRejectsUnknownFields(t *testing.T) {
	y := "rtsp_base: rtsp://bridge:8554\nlayout: 1\ncustom_option: preserve-me\ntiles: [{camera: A}]\n"
	if _, _, err := migrateLegacyForEditor([]byte(y)); err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("silent loss of unknown field: %v", err)
	}
}

func TestLegacyMigrationKeepsExplicitBridgeURL(t *testing.T) {
	y := "bridge_url: https://bridge.example:443\nrtsp_base: rtsp://bridge:8554\nlayout: 1\ntiles: [{camera: A}]\n"
	b, _, err := migrateLegacyForEditor([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if c.BridgeURL != "https://bridge.example:443" {
		t.Fatalf("explicit control origin lost: %q", c.BridgeURL)
	}
}
