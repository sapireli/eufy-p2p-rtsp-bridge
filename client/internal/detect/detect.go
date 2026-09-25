// Package detect resolves "auto" settings against the machine: screen mode from DRM sysfs, decoder and
// sink from the GStreamer elements actually installed.
package detect

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// Screen reads the preferred mode of the first connected HDMI output.
func Screen(fsRoot string) (config.Screen, bool) {
	return HostScreen(fsRoot, "")
}

// HostScreen uses the display API appropriate for this host. A macOS window uses logical
// points rather than the Retina framebuffer's physical pixels.
func HostScreen(fsRoot, output string) (config.Screen, bool) {
	if runtime.GOOS == "darwin" {
		return macScreen(output)
	}
	return ScreenFor(fsRoot, output)
}

func macScreen(output string) (config.Screen, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "system_profiler", "SPDisplaysDataType", "-json").Output()
	if err != nil || len(b) > 4<<20 {
		return config.Screen{}, false
	}
	return parseMacDisplays(b, output)
}

func parseMacDisplays(b []byte, output string) (config.Screen, bool) {
	var report struct {
		Displays []struct {
			Drivers []struct {
				Name       string `json:"_name"`
				ID         string `json:"_spdisplays_displayID"`
				Resolution string `json:"_spdisplays_resolution"`
				Pixels     string `json:"_spdisplays_pixels"`
				Main       string `json:"spdisplays_main"`
				Online     string `json:"spdisplays_online"`
			} `json:"spdisplays_ndrvs"`
		} `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(b, &report); err != nil {
		return config.Screen{}, false
	}
	var fallback config.Screen
	for _, gpu := range report.Displays {
		for _, d := range gpu.Drivers {
			if d.Online == "spdisplays_no" || output != "" && output != d.Name && output != d.ID {
				continue
			}
			mode := d.Resolution
			if mode == "" {
				mode = d.Pixels
			}
			var w, h int
			if _, err := fmt.Sscanf(mode, "%d x %d", &w, &h); err != nil || w <= 0 || h <= 0 {
				continue
			}
			screen := config.Screen{Width: w, Height: h}
			if output != "" || d.Main == "spdisplays_yes" {
				return screen, true
			}
			if fallback.Width == 0 {
				fallback = screen
			}
		}
	}
	return fallback, fallback.Width > 0
}

// ScreenFor reads the preferred mode of a named DRM connector ("HDMI-A-2", "DP-1"); an empty name means
// the first HDMI output, as before. Driving two monitors means one instance per connector, so the mode
// has to come from the connector this instance actually drives rather than whichever one sorts first.
func ScreenFor(fsRoot, output string) (config.Screen, bool) {
	pattern := "sys/class/drm/card*-HDMI-A-*/modes"
	if output != "" {
		pattern = "sys/class/drm/card*-" + output + "/modes"
	}
	matches, _ := filepath.Glob(filepath.Join(fsRoot, pattern))
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
	return resolveForOS(c, has, fileExists, runtime.GOOS)
}

func resolveForOS(c *config.Config, has func(string) bool, fileExists func(string) bool, goos string) (pipeline.Caps, error) {
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
		case goos == "darwin" && has("autovideosink"):
			caps.Sink = "window"
		case goos == "darwin":
			return caps, fmt.Errorf("detect: autovideosink is missing (install Homebrew GStreamer plugins-base)")
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

// ConnectorID reads a connector's DRM id, which kmssink needs to target a specific output. The id lives
// in the sysfs directory name on some drivers and in `connector_id` on others; a missing id is not fatal
// (kmssink falls back to the first connected output), so this reports whether one was found.
func ConnectorID(fsRoot, output string) (int, bool) {
	if output == "" {
		return 0, false
	}
	matches, _ := filepath.Glob(filepath.Join(fsRoot, "sys/class/drm/card*-"+output, "connector_id"))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &id); err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}
