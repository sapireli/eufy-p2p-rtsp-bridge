package main

import (
	"bytes"
	"errors"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

func TestEditorCommandMatrix(t *testing.T) {
	e, err := newLayoutEditor("")
	if err != nil {
		t.Fatal(err)
	}
	valid := [][]string{
		{"resize", "garage", "-8", "0"},
		{"move", "garage", "1", "0"},
		{"rect", "garage", "0", "0", "8", "32"},
		{"add", "third", "C", "8", "0", "8", "32"},
		{"motion", "third", "C,D", "90", "5"},
		{"camera", "third", "D"},
		{"delete", "third"},
	}
	for _, cmd := range valid {
		if err := e.change(func(c *config.Config) error { return editorMutation(c, cmd) }); err != nil {
			t.Fatalf("%v: %v", cmd, err)
		}
	}
	for _, cmd := range [][]string{
		{"template"}, {"template", "unknown"}, {"add", "x"}, {"add", "bad id", "C", "0", "0", "1", "1"},
		{"add", "x", "C", "q", "0", "1", "1"}, {"add", "x", "C", "0", "0", "0", "1"},
		{"delete"}, {"delete", "missing"},
		{"rect", "garage", "0"}, {"rect", "garage", "0", "0", "99", "1"},
		{"move", "garage", "NaN", "0"}, {"resize", "garage", "-99", "0"},
		{"camera", "garage"}, {"camera", "missing", "A"},
		{"motion", "garage"}, {"motion", "garage", "A,,B"}, {"motion", "garage", "all", "-1"}, {"motion", "garage", "all", "90", "bad"},
		{"unknown"},
	} {
		if err := e.change(func(c *config.Config) error { return editorMutation(c, cmd) }); err == nil {
			t.Errorf("invalid command accepted: %v", cmd)
		}
	}
	if _, err := rectArgs([]string{"0", "0", "1"}); err == nil {
		t.Fatal("short rect accepted")
	}
	if _, err := rectArgs([]string{"0", "0", "oops", "1"}); err == nil {
		t.Fatal("nonnumeric rect accepted")
	}
	if err := e.change(func(c *config.Config) error { return editorTemplate(c, "one") }); err != nil {
		t.Fatal(err)
	}
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"delete", "camera-1"}) }); err == nil {
		t.Fatal("last tile deletion accepted")
	}
}

func TestEditorScriptExercisesDraftAndPreviewCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.png")
	draft := filepath.Join(t.TempDir(), "draft.yaml")
	input := "help\nshow\nundo\nredo\npng " + path + "\ntemplate four\nundo\nredo\nsave " + draft + "\nunknown\nquit\n"
	var out bytes.Buffer
	if err := editLayout("", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("PNG command: %v", err)
	}
	c, err := config.Load(draft)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tiles) != 4 {
		t.Fatalf("redo should restore four-tile template, got %d", len(c.Tiles))
	}
	if !strings.Contains(out.String(), "nothing to undo") || !strings.Contains(out.String(), "nothing to redo") || !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("missing diagnostics: %s", out.String())
	}
}

func TestEditorRejectsUnsupportedInputFiles(t *testing.T) {
	if _, err := newLayoutEditor("-"); err == nil {
		t.Fatal("stdin editor accepted")
	}
	if _, err := newLayoutEditor(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
	for name, data := range map[string]string{
		"bad.yaml":    "schema_version: [\n",
		"preset.yaml": "schema_version: 2\nbridge_url: http://x:3000\nrtsp_base: rtsp://x\nlayout: 1\ntiles: [{id: one, camera: A}]\n",
		"legacy.yaml": "rtsp_base: rtsp://x\nlayout: 1\ntiles: [{camera: A}]\n",
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newLayoutEditor(path); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPreviewFromStdinPNGAndOddDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd.png")
	var out bytes.Buffer
	if err := previewLayout("-", bytes.NewReader(config.Example()), &out, PreviewOptions{PNGPath: path, Width: 1919, Height: 1079}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "screen 1919x1079") || !strings.Contains(out.String(), "PNG saved") {
		t.Fatalf("preview output: %s", out.String())
	}
	if info, err := os.Stat(path); err != nil || info.Size() < 100 {
		t.Fatalf("PNG missing or empty: %v %+v", err, info)
	}
	if err := previewLayout("-", bytes.NewReader(config.Example()), &out, PreviewOptions{Width: 100}); err == nil {
		t.Fatal("partial dimensions accepted")
	}
	if err := previewLayout("-", strings.NewReader("garbage"), &out, PreviewOptions{}); err == nil {
		t.Fatal("malformed config accepted")
	}
	if err := previewLayout(filepath.Join(t.TempDir(), "missing.yaml"), nil, &out, PreviewOptions{}); err == nil {
		t.Fatal("missing config accepted")
	}
}

func TestPreviewDisplayUsesBoundedExternalCommand(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gst-launch-1.0")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var out bytes.Buffer
	if err := previewLayout("-", bytes.NewReader(config.Example()), &out, PreviewOptions{Display: true, Width: 64, Height: 36}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "HDMI preview is running") {
		t.Fatalf("missing preview message: %s", out.String())
	}
	if err := displayLayoutPNG("/unused.png", "NONEXISTENT-DRM-OUTPUT", &out); err == nil {
		t.Fatal("invalid output accepted")
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := displayLayoutPNG("/unused.png", "", &out); err == nil {
		t.Fatal("GStreamer failure hidden")
	}
}

func TestNumberGlyphIsRenderedForLargeTile(t *testing.T) {
	imgPath := filepath.Join(t.TempDir(), "number.png")
	s := config.Screen{Width: 100, Height: 60}
	if err := writeLayoutPNGForScreen(imgPath, s, []layout.Placed{{ID: "x", W: 100, H: 60}}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	white := color.RGBA{255, 255, 255, 255}
	count := 0
	for y := 22; y < 38; y++ {
		for x := 40; x < 60; x++ {
			if img.At(x, y) == white {
				count++
			}
		}
	}
	if count == 0 {
		t.Fatal("tile number was not drawn")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

func TestTextPreviewReturnsWriterError(t *testing.T) {
	c, err := config.Parse(config.Example())
	if err != nil {
		t.Fatal(err)
	}
	p, err := placeForValidation(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := textLayout(failingWriter{}, c, p, config.Screen{Width: 1920, Height: 1080}); err == nil {
		t.Fatal("write error lost")
	}
}
