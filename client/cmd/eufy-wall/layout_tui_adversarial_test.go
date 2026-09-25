package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
)

func TestFullScreenRenderResizeHelpPickerAndControlText(t *testing.T) {
	s := screenFixture(t)
	s.editor.inventory = map[string]setupCamera{"A": {SN: "A", Name: "\x1b]0;bad\aCamera", Codec: "h264", Mode: "always"}}
	s.status = "unsafe \x1b]0;bad\a"
	for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 20}} {
		s.width, s.height = size[0], size[1]
		var out bytes.Buffer
		if err := s.render(&out); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if !strings.HasPrefix(text, "\x1b[H\x1b[2J") || strings.Contains(text, "\x1b]0;bad") {
			t.Fatalf("unsafe terminal frame: %q", text)
		}
		if size[0] < 80 {
			if !strings.Contains(text, "Resize terminal to at least 80x24") {
				t.Fatal("small terminal lost resize hint")
			}
			s.prompt = "quit"
			out.Reset()
			if err := s.render(&out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "type DISCARD then Enter") {
				t.Fatal("small terminal hid quit confirmation")
			}
			s.prompt = ""
			continue
		}
		if lines := strings.Split(text, "\r\n"); len(lines) != size[1] {
			t.Fatalf("frame has %d lines, want %d", len(lines), size[1])
		}
		if !strings.Contains(text, "SELECTED 1  solo") || !strings.Contains(text, "Grid: x=0 y=0 w=8 h=8") || !strings.Contains(text, "@") {
			t.Fatalf("frame lacks selected tile: %q", text)
		}
	}
	s.width, s.height = 80, 24
	s.help = true
	var out bytes.Buffer
	if err := s.render(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "KEYBOARD HELP") {
		t.Fatal("help overlay missing")
	}
	s.help = false
	tile, _ := s.tile()
	if err := s.openPicker("watch", tile); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := s.render(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CHOOSE WATCH") || !strings.Contains(out.String(), "Watching all") {
		t.Fatal("watch picker missing")
	}
	s.picker = nil
	s.prompt = "rect"
	out.Reset()
	if err := s.render(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Exact rectangle x y w h") {
		t.Fatal("exact prompt missing")
	}
}

func TestFullScreenInventorySelectionTemplatesAndAdversarialPrompts(t *testing.T) {
	s := screenFixture(t)
	if err := s.executePrompt("inventory", contractInventoryPath("inventory-v1.json")); err != nil {
		t.Fatal(err)
	}
	if len(s.editor.inventory) != 3 {
		t.Fatal("offline inventory did not load")
	}
	if err := s.executePrompt("camera", "UNKNOWN"); err == nil {
		t.Fatal("unknown camera accepted")
	}
	if err := s.executePrompt("watch", "UNKNOWN"); err == nil {
		t.Fatal("unknown watch camera accepted")
	}
	if err := s.executePrompt("watch", "all"); err != nil {
		t.Fatal(err)
	}
	if err := s.executePrompt("template", "split"); err != nil {
		t.Fatal(err)
	}
	if s.selected != 0 {
		t.Fatal("template did not select first tile")
	}
	press(t, s, screenKey{name: "tab"})
	if s.selected != 1 {
		t.Fatal("Tab did not select next tile")
	}
	press(t, s, screenKey{name: "backtab"})
	if s.selected != 0 {
		t.Fatal("Shift-Tab did not select previous tile")
	}
	if err := s.executePrompt("template", "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.executePrompt("rect", "0 0 16 32"); err != nil {
		t.Fatal(err)
	}
	if err := s.executePrompt("add", "new BATTERY-DEMAND-H265 16 0 16 32"); err != nil {
		t.Fatal(err)
	}
	if s.selected != 1 {
		t.Fatal("new tile was not selected")
	}
	if err := s.executePrompt("delete", "wrong"); err == nil {
		t.Fatal("deletion without confirmation accepted")
	}
	if err := s.executePrompt("delete", "DELETE"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.tile(); err != nil || s.selected != 0 {
		t.Fatalf("selection after delete: %d %v", s.selected, err)
	}
	for _, tc := range []struct{ kind, value string }{{"rect", "1 2 bad 4"}, {"add", "bad"}, {"template", "unknown"}, {"camera", "two words"}, {"watch", ""}, {"png", ""}, {"apply", "no"}, {"bogus", ""}} {
		if err := s.executePrompt(tc.kind, tc.value); err == nil {
			t.Fatalf("accepted %s %q", tc.kind, tc.value)
		}
	}
}

func TestFullScreenSavePNGAndPromptErrorRecovery(t *testing.T) {
	s := screenFixture(t)
	if err := s.editor.change(func(c *config.Config) error { c.Screen = config.Screen{Width: 64, Height: 64}; return nil }); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(t.TempDir(), "preview.png")
	if err := s.executePrompt("png", png); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(png); err != nil || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("PNG missing: %v", err)
	}
	s.prompt = "rect"
	s.input = "bad"
	if _, err := s.handle(screenKey{name: "enter"}); err == nil || s.prompt != "rect" {
		t.Fatal("bad prompt was not retained for correction")
	}
	press(t, s, screenKey{name: "cancel"})
	if s.prompt != "" {
		t.Fatal("Ctrl-G did not cancel prompt")
	}
	if _, err := s.handle(screenKey{r: 'u'}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handle(screenKey{r: 'y'}); err != nil {
		t.Fatal(err)
	}
	if err := s.executePrompt("apply", "APPLY"); err == nil {
		t.Fatal("missing apply function accepted")
	}
	s.apply = func() error { return errors.New("service unavailable") }
	if err := s.executePrompt("apply", "APPLY"); err == nil || !strings.Contains(err.Error(), "service unavailable") {
		t.Fatalf("apply error=%v", err)
	}
	draft := filepath.Join(t.TempDir(), "saved.yaml")
	if err := s.executePrompt("save", draft); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(draft); err != nil {
		t.Fatal(err)
	}
}

func TestFullScreenLabelsSelectionAndAdditionalKeys(t *testing.T) {
	s := screenFixture(t)
	for _, tc := range []struct{ kind, want string }{
		{"rect", "x y w h"}, {"camera", "Camera serial"}, {"watch", "Watch serials"},
		{"delete", "DELETE"}, {"save", "Draft path"}, {"apply", "APPLY"},
		{"inventory", "Inventory file"}, {"template", "Template"}, {"add", "Add id"},
		{"png", "PNG path"}, {"quit", "DISCARD"}, {"custom", "custom"},
	} {
		s.prompt = tc.kind
		if got := s.promptLabel(); !strings.Contains(got, tc.want) {
			t.Fatalf("%s label=%q", tc.kind, got)
		}
	}
	s.prompt = ""
	press(t, s, screenKey{r: '?'})
	if !s.help {
		t.Fatal("help did not open")
	}
	press(t, s, screenKey{r: '?'})
	if s.help {
		t.Fatal("help did not close")
	}
	press(t, s, screenKey{r: 'r'})
	if !s.resize {
		t.Fatal("resize mode did not open")
	}
	press(t, s, screenKey{r: 'r'})
	if s.resize {
		t.Fatal("resize mode did not close")
	}
	press(t, s, screenKey{r: 't'})
	if s.prompt != "template" {
		t.Fatal("template prompt missing")
	}
	press(t, s, screenKey{name: "cancel"})
	press(t, s, screenKey{r: 'a'})
	if s.prompt != "add" {
		t.Fatal("add prompt missing")
	}
	press(t, s, screenKey{name: "cancel"})
	press(t, s, screenKey{r: 'P'})
	if s.prompt != "png" {
		t.Fatal("PNG prompt missing")
	}
	press(t, s, screenKey{name: "cancel"})
	press(t, s, screenKey{r: 'i'})
	if s.prompt != "inventory" {
		t.Fatal("inventory prompt missing")
	}
	press(t, s, screenKey{name: "cancel"})
	if err := s.executePrompt("template", "four"); err != nil {
		t.Fatal(err)
	}
	press(t, s, screenKey{name: "backtab"})
	if s.selected != 3 {
		t.Fatalf("selection did not wrap backward: %d", s.selected)
	}
	press(t, s, screenKey{name: "tab"})
	if s.selected != 0 {
		t.Fatalf("selection did not wrap forward: %d", s.selected)
	}
	for i := 0; i < 2; i++ {
		press(t, s, screenKey{r: 'u'})
	}
	if _, err := s.handle(screenKey{r: 'u'}); err == nil {
		t.Fatal("undo below first history entry accepted")
	}
	for i := 0; i < 2; i++ {
		press(t, s, screenKey{r: 'y'})
	}
	if _, err := s.handle(screenKey{r: 'y'}); err == nil {
		t.Fatal("redo beyond latest history accepted")
	}
}

func TestFullScreenPickerRejectsEmptyWatchAndShowsMotion(t *testing.T) {
	s := screenFixture(t)
	s.editor.inventory = map[string]setupCamera{"A": {SN: "A"}, "B": {SN: "B"}}
	press(t, s, screenKey{r: 'w'})
	press(t, s, screenKey{r: ' '})
	press(t, s, screenKey{r: ' '})
	if _, err := s.handle(screenKey{name: "enter"}); err == nil {
		t.Fatal("empty watch subset accepted")
	}
	press(t, s, screenKey{r: 'a'})
	press(t, s, screenKey{name: "enter"})
	var out bytes.Buffer
	if err := s.render(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Motion: latest") || !strings.Contains(out.String(), "Watch: all") {
		t.Fatal("motion detail missing")
	}
}
