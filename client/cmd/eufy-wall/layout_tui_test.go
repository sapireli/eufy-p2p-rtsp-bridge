package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
)

func screenFixture(t *testing.T) *layoutScreen {
	t.Helper()
	e, err := newLayoutEditor("")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.change(func(c *config.Config) error {
		c.Tiles = []config.Tile{{ID: "solo", Camera: "A", Rect: &config.Rect{X: 0, Y: 0, W: 8, H: 8}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return &layoutScreen{editor: e, step: 1, width: 80, height: 24, saved: bytes.Clone(e.history[e.at])}
}

func TestFullScreenCanvasKeepsDisplayAspectAndShowsPixelEdges(t *testing.T) {
	s := screenFixture(t)
	if err := s.editor.change(func(c *config.Config) error {
		c.Screen = config.Screen{Width: 1919, Height: 1079}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := s.render(&out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\r\n")
	if !strings.HasPrefix(lines[10], "|................................|") || !strings.HasPrefix(lines[11], "|                                |") {
		t.Fatalf("16:9 canvas should use nine terminal rows: %q / %q", lines[10], lines[11])
	}
	if !strings.Contains(out.String(), "Pixels: x=0 y=0 w=479 h=269") {
		t.Fatal("panel omits renderer's odd-screen pixel bounds")
	}
	if err := s.editor.change(func(c *config.Config) error {
		c.Screen = config.Screen{Width: 1080, Height: 1920}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := s.render(&out); err != nil {
		t.Fatal(err)
	}
	lines = strings.Split(out.String(), "\r\n")
	if !strings.HasPrefix(lines[2], "|@@@@@...............|") || !strings.HasPrefix(lines[19], "|....................|") {
		t.Fatalf("portrait canvas should fit 20 columns by 18 rows: %q / %q", lines[2], lines[19])
	}
}

func press(t *testing.T, s *layoutScreen, key screenKey) bool {
	t.Helper()
	quit, err := s.handle(key)
	if err != nil {
		t.Fatal(err)
	}
	return quit
}

func typePrompt(t *testing.T, s *layoutScreen, text string) {
	t.Helper()
	for _, r := range text {
		press(t, s, screenKey{r: r})
	}
	press(t, s, screenKey{name: "enter"})
}

func TestFullScreenGeometryStepsUndoRedoAndInvalidEdit(t *testing.T) {
	s := screenFixture(t)
	press(t, s, screenKey{r: '8'})
	press(t, s, screenKey{name: "right"})
	press(t, s, screenKey{r: 'r'})
	press(t, s, screenKey{name: "right"})
	press(t, s, screenKey{r: '4'})
	press(t, s, screenKey{name: "down"})
	press(t, s, screenKey{r: '1'})
	press(t, s, screenKey{name: "left"})
	tile, err := s.tile()
	if err != nil {
		t.Fatal(err)
	}
	if *tile.Rect != (config.Rect{X: 8, Y: 0, W: 15, H: 12}) {
		t.Fatalf("rectangle=%+v", *tile.Rect)
	}
	press(t, s, screenKey{r: 'u'})
	tile, _ = s.tile()
	if tile.Rect.W != 16 {
		t.Fatalf("undo width=%d", tile.Rect.W)
	}
	press(t, s, screenKey{r: 'y'})
	tile, _ = s.tile()
	if tile.Rect.W != 15 {
		t.Fatalf("redo width=%d", tile.Rect.W)
	}
	s.step = 8
	before := len(s.editor.history)
	press(t, s, screenKey{name: "left"})
	if _, err := s.handle(screenKey{name: "left"}); err == nil {
		t.Fatal("invalid negative width accepted")
	}
	if len(s.editor.history) != before+1 {
		t.Fatal("invalid edit entered history")
	}
}

func TestFullScreenExactRectangleAndManualSources(t *testing.T) {
	s := screenFixture(t)
	press(t, s, screenKey{r: 'e'})
	if s.prompt != "rect" || s.input == "" {
		t.Fatal("exact editor did not prefill current rectangle")
	}
	press(t, s, screenKey{name: "clear"})
	typePrompt(t, s, "2 3 4 5")
	tile, _ := s.tile()
	if *tile.Rect != (config.Rect{X: 2, Y: 3, W: 4, H: 5}) {
		t.Fatalf("exact rectangle=%+v", *tile.Rect)
	}
	press(t, s, screenKey{r: 'c'})
	typePrompt(t, s, "SERIAL-B")
	tile, _ = s.tile()
	if tile.Camera != "SERIAL-B" {
		t.Fatalf("camera=%q", tile.Camera)
	}
	press(t, s, screenKey{r: 'w'})
	typePrompt(t, s, "SERIAL-B,SERIAL-C")
	tile, _ = s.tile()
	if tile.Motion != "latest" || strings.Join(tile.Watch, ",") != "SERIAL-B,SERIAL-C" || tile.BlankAfterSeconds != 90 {
		t.Fatalf("motion tile=%+v", tile)
	}
	press(t, s, screenKey{r: 'c'})
	typePrompt(t, s, "SERIAL-C")
	tile, _ = s.tile()
	if tile.Camera != "SERIAL-C" || tile.Motion != "" || len(tile.Watch) != 0 {
		t.Fatalf("camera conversion=%+v", tile)
	}
}

func TestFullScreenCameraAndWatchPicker(t *testing.T) {
	s := screenFixture(t)
	s.editor.inventory = map[string]setupCamera{"A": {SN: "A"}, "B": {SN: "B"}, "C": {SN: "C"}}
	press(t, s, screenKey{r: 'c'})
	if s.picker == nil || s.picker.kind != "camera" {
		t.Fatal("camera picker did not open")
	}
	press(t, s, screenKey{name: "down"})
	press(t, s, screenKey{name: "enter"})
	tile, _ := s.tile()
	if tile.Camera != "B" || s.picker != nil {
		t.Fatalf("camera picker=%+v tile=%+v", s.picker, tile)
	}
	press(t, s, screenKey{r: 'w'})
	press(t, s, screenKey{r: ' '})
	press(t, s, screenKey{name: "down"})
	press(t, s, screenKey{r: ' '})
	press(t, s, screenKey{name: "enter"})
	tile, _ = s.tile()
	if tile.Motion != "latest" || strings.Join(tile.Watch, ",") != "A,B" {
		t.Fatalf("watch picker=%+v", tile)
	}
	press(t, s, screenKey{r: 'w'})
	press(t, s, screenKey{r: 'a'})
	press(t, s, screenKey{name: "enter"})
	tile, _ = s.tile()
	if len(tile.Watch) != 0 {
		t.Fatalf("all watch=%+v", tile.Watch)
	}
	press(t, s, screenKey{r: 'w'})
	press(t, s, screenKey{name: "cancel"})
	if s.picker != nil {
		t.Fatal("picker did not cancel")
	}
}

func TestFullScreenDraftApplyConfirmationAndQuit(t *testing.T) {
	s := screenFixture(t)
	path := filepath.Join(t.TempDir(), "wall.draft.yaml")
	press(t, s, screenKey{r: 's'})
	press(t, s, screenKey{name: "clear"})
	typePrompt(t, s, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if !press(t, s, screenKey{r: 'q'}) {
		t.Fatal("saved draft did not quit")
	}
	press(t, s, screenKey{r: '8'})
	press(t, s, screenKey{name: "right"})
	if press(t, s, screenKey{r: 'q'}) || s.prompt != "quit" {
		t.Fatal("unsaved quit did not ask")
	}
	press(t, s, screenKey{name: "enter"})
	if s.prompt != "" {
		t.Fatal("quit cancellation stuck in prompt")
	}
	press(t, s, screenKey{r: 'A'})
	if _, err := s.handle(screenKey{name: "enter"}); err == nil {
		t.Fatal("apply accepted without confirmation")
	}
	press(t, s, screenKey{name: "clear"})
	called := 0
	s.apply = func() error { called++; return nil }
	typePrompt(t, s, "APPLY")
	if called != 1 {
		t.Fatalf("apply count=%d", called)
	}
	if !press(t, s, screenKey{r: 'q'}) {
		t.Fatal("applied draft did not quit")
	}
}

func TestFullScreenParserAndFallback(t *testing.T) {
	for _, tc := range []struct {
		bytes, name string
		r           rune
	}{
		{"\x1b[A", "up", 0}, {"\x1b[1;2D", "left", 0}, {"\x1b[Z", "backtab", 0},
		{"\x1bOX", "unknown", 0}, {"\x15", "clear", 0}, {"\x07", "cancel", 0},
		{"\x7f", "backspace", 0}, {"é", "", 'é'}, {"\r", "enter", 0},
	} {
		key, err := readScreenKey(bufio.NewReader(strings.NewReader(tc.bytes)))
		if err != nil || key.name != tc.name || key.r != tc.r {
			t.Fatalf("bytes=%q key=%+v err=%v", tc.bytes, key, err)
		}
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, _, ok := fullScreenTerminal(reader, writer); ok {
		t.Fatal("pipe selected full-screen terminal")
	}
}
