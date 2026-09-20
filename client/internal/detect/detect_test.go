package detect

import (
	"os"
	"path/filepath"
	"testing"

	"eufy-wall/internal/config"
)

func TestScreenFromSysfs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sys/class/drm/card1-HDMI-A-1")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "modes"), []byte("1920x1080\n1280x720\n"), 0o644)
	s, ok := Screen(root)
	if !ok || s != (config.Screen{Width: 1920, Height: 1080}) {
		t.Fatalf("%v %+v", ok, s)
	}
	if _, ok := Screen(t.TempDir()); ok {
		t.Fatal("expected not ok without drm")
	}
}

func TestResolve(t *testing.T) {
	has := func(set ...string) func(string) bool {
		m := map[string]bool{}
		for _, s := range set {
			m[s] = true
		}
		return func(e string) bool { return m[e] }
	}
	exists := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range paths {
				if x == p {
					return true
				}
			}
			return false
		}
	}
	pi := &config.Config{Decoder: "auto", Sink: "auto", Planes: []int{31}, Screen: config.Screen{Width: 1920, Height: 1080}}
	caps, err := Resolve(pi, has("v4l2h264dec", "compositor"), exists("/dev/video10"))
	if err != nil || caps.Decoder != "v4l2" || caps.Sink != "planes" {
		t.Fatalf("pi: %v %+v", err, caps)
	}
	pi.Planes = nil
	caps, _ = Resolve(pi, has("v4l2h264dec", "compositor"), exists("/dev/video10"))
	if caps.Sink != "compositor" {
		t.Fatalf("pi no planes: %+v", caps)
	}
	x86 := &config.Config{Decoder: "auto", Sink: "auto", Screen: config.Screen{Width: 1920, Height: 1080}}
	caps, _ = Resolve(x86, has("vah264dec", "avdec_h264", "compositor"), exists())
	if caps.Decoder != "va" || caps.Sink != "compositor" {
		t.Fatalf("x86: %+v", caps)
	}
	caps, _ = Resolve(x86, has("avdec_h264", "compositor"), exists())
	if caps.Decoder != "software" {
		t.Fatalf("sw: %+v", caps)
	}
	if _, err := Resolve(x86, has(), exists()); err == nil {
		t.Fatal("expected error with no decoders")
	}
	forced := &config.Config{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 800, Height: 600}}
	caps, err = Resolve(forced, has(), exists())
	if err != nil || caps.Decoder != "software" || caps.Sink != "window" || caps.Screen.Width != 800 {
		t.Fatalf("forced: %v %+v", err, caps)
	}
}

// Two monitors means one instance per connector, so the mode has to come from the connector this
// instance actually drives — not whichever one happens to sort first in sysfs.
func TestScreenForNamedOutput(t *testing.T) {
	root := t.TempDir()
	write := func(conn, modes, id string) {
		dir := filepath.Join(root, "sys/class/drm", conn)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "modes"), []byte(modes), 0o644); err != nil {
			t.Fatal(err)
		}
		if id != "" {
			if err := os.WriteFile(filepath.Join(dir, "connector_id"), []byte(id), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("card0-HDMI-A-1", "1920x1080\n", "32")
	write("card0-HDMI-A-2", "1024x768\n", "41")

	if s, ok := ScreenFor(root, "HDMI-A-2"); !ok || s.Width != 1024 || s.Height != 768 {
		t.Errorf("named output should give its own mode, got %+v ok=%v", s, ok)
	}
	if s, ok := ScreenFor(root, ""); !ok || s.Width != 1920 {
		t.Errorf("unnamed should keep the first-HDMI behaviour, got %+v ok=%v", s, ok)
	}
	if id, ok := ConnectorID(root, "HDMI-A-2"); !ok || id != 41 {
		t.Errorf("connector id = %d ok=%v, want 41", id, ok)
	}
	if _, ok := ConnectorID(root, ""); ok {
		t.Error("no output named: there is no connector to pin")
	}
}
