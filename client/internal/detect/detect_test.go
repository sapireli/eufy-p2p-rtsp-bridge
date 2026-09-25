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
	s, ok := ScreenFor(root, "")
	if !ok || s != (config.Screen{Width: 1920, Height: 1080}) {
		t.Fatalf("%v %+v", ok, s)
	}
	if _, ok := ScreenFor(t.TempDir(), ""); ok {
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
	caps, err := resolveForOS(pi, has("v4l2h264dec", "compositor"), exists("/dev/video10"), "linux")
	if err != nil || caps.Decoder != "v4l2" || caps.Sink != "planes" {
		t.Fatalf("pi: %v %+v", err, caps)
	}
	pi.Planes = nil
	caps, _ = resolveForOS(pi, has("v4l2h264dec", "compositor"), exists("/dev/video10"), "linux")
	if caps.Sink != "compositor" {
		t.Fatalf("pi no planes: %+v", caps)
	}
	x86 := &config.Config{Decoder: "auto", Sink: "auto", Screen: config.Screen{Width: 1920, Height: 1080}}
	caps, _ = resolveForOS(x86, has("vah264dec", "avdec_h264", "compositor"), exists(), "linux")
	if caps.Decoder != "va" || caps.Sink != "compositor" {
		t.Fatalf("x86: %+v", caps)
	}
	caps, _ = resolveForOS(x86, has("avdec_h264", "compositor"), exists(), "linux")
	if caps.Decoder != "software" {
		t.Fatalf("sw: %+v", caps)
	}
	if _, err := resolveForOS(x86, has(), exists(), "linux"); err == nil {
		t.Fatal("expected error with no decoders")
	}
	forced := &config.Config{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 800, Height: 600}}
	caps, err = Resolve(forced, has(), exists())
	if err != nil || caps.Decoder != "software" || caps.Sink != "window" || caps.Screen.Width != 800 {
		t.Fatalf("forced: %v %+v", err, caps)
	}
}

func TestDarwinAutoSink(t *testing.T) {
	c := &config.Config{Decoder: "auto", Sink: "auto", Screen: config.Screen{Width: 1920, Height: 1080}}
	has := func(name string) bool { return name == "avdec_h264" || name == "autovideosink" }
	caps, err := resolveForOS(c, has, func(string) bool { return false }, "darwin")
	if err != nil || caps.Decoder != "software" || caps.Sink != "window" {
		t.Fatalf("mac auto: %+v, %v", caps, err)
	}
	if _, err := resolveForOS(c, func(name string) bool { return name == "avdec_h264" }, func(string) bool { return false }, "darwin"); err == nil {
		t.Fatal("missing window sink should fail")
	}
}

func TestParseMacDisplays(t *testing.T) {
	b := []byte(`{"SPDisplaysDataType":[{"spdisplays_ndrvs":[{"_name":"secondary","_spdisplays_displayID":"ab2","_spdisplays_resolution":"1280 x 720","spdisplays_online":"spdisplays_yes"},{"_name":"main","_spdisplays_displayID":"cd1","_spdisplays_resolution":"2048 x 1152","_spdisplays_pixels":"4096 x 2304","spdisplays_main":"spdisplays_yes"}]}]}`)
	if s, ok := parseMacDisplays(b, ""); !ok || s != (config.Screen{Width: 2048, Height: 1152}) {
		t.Fatalf("main logical mode: %+v, %v", s, ok)
	}
	if s, ok := parseMacDisplays(b, "ab2"); !ok || s != (config.Screen{Width: 1280, Height: 720}) {
		t.Fatalf("explicit display: %+v, %v", s, ok)
	}
	if _, ok := parseMacDisplays(b, "unknown"); ok {
		t.Fatal("unknown display should fail")
	}
	if _, ok := parseMacDisplays([]byte(`{`), ""); ok {
		t.Fatal("malformed profiler data should fail")
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
