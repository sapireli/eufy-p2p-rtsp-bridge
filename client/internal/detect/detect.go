// Package detect resolves "auto" settings against the machine: screen mode from DRM sysfs, decoder and
// sink from the GStreamer elements actually installed.
package detect

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// Screen reads the preferred mode of the first HDMI connector (first line of .../modes).
func Screen(fsRoot string) (config.Screen, bool) {
	matches, _ := filepath.Glob(filepath.Join(fsRoot, "sys/class/drm/card*-HDMI-A-*/modes"))
	for _, m := range matches {
		f, err := os.Open(m)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		if sc.Scan() {
			var w, h int
			if _, err := fmt.Sscanf(strings.TrimSpace(sc.Text()), "%dx%d", &w, &h); err == nil && w > 0 && h > 0 {
				f.Close()
				return config.Screen{Width: w, Height: h}, true
			}
		}
		f.Close()
	}
	return config.Screen{}, false
}

// HasElement asks gst-inspect whether an element exists on this machine.
func HasElement(name string) bool {
	return exec.Command("gst-inspect-1.0", "--exists", name).Run() == nil
}

func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Resolve turns auto decoder/sink into concrete choices. `has`/`fileExists` are injectable for tests.
func Resolve(c *config.Config, has func(string) bool, fileExists func(string) bool) (pipeline.Caps, error) {
	caps := pipeline.Caps{Decoder: c.Decoder, Sink: c.Sink, Screen: c.Screen}
	if caps.Decoder == "auto" {
		switch {
		case fileExists("/dev/video10") && has("v4l2h264dec"):
			caps.Decoder = "v4l2"
		case has("vah264dec"):
			caps.Decoder = "va"
		case has("avdec_h264"):
			caps.Decoder = "software"
		default:
			return caps, fmt.Errorf("detect: no H.264 decoder found (install gstreamer1.0-plugins-good/bad/libav)")
		}
	}
	if caps.Sink == "auto" {
		switch {
		case caps.Decoder == "v4l2" && len(c.Planes) > 0:
			caps.Sink = "planes"
		case has("compositor"):
			caps.Sink = "compositor"
		default:
			return caps, fmt.Errorf("detect: no sink strategy: set `planes:` for kmssink planes or install gstreamer1.0-plugins-base (compositor)")
		}
	}
	return caps, nil
}
