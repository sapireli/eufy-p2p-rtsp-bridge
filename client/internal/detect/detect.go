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
		if status, err := os.ReadFile(filepath.Join(filepath.Dir(m), "status")); err == nil && strings.TrimSpace(string(status)) != "connected" {
			continue
		}
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

// CheckNativeElements verifies plugins needed by the selected live renderer.
// Offline layout commands do not need these elements.
func CheckNativeElements(sink string, has func(string) bool) error {
	if (sink == "planes" || sink == "compositor" || sink == "window") && !has("watchdog") {
		return fmt.Errorf("GStreamer watchdog is missing (install gstreamer1.0-plugins-bad or Homebrew GStreamer)")
	}
	if sink != "compositor" && sink != "window" {
		return nil
	}
	for _, element := range []string{"compositor", "appsink", "appsrc", "videoconvert", "videoscale", "videorate"} {
		if !has(element) {
			return fmt.Errorf("GStreamer %s is missing (install gstreamer1.0-plugins-base or Homebrew GStreamer)", element)
		}
	}
	if sink == "window" && !has("autovideosink") {
		return fmt.Errorf("GStreamer autovideosink is missing (install gstreamer1.0-plugins-base or Homebrew GStreamer)")
	}
	return nil
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
		caps.AutoElements = make(map[string]string, 2)
		if goos == "darwin" {
			if has("vtdec_hw") {
				caps.AutoElements["h264"] = "vtdec_hw"
				caps.AutoElements["h265"] = "vtdec_hw"
			}
		} else {
			if fileExists("/dev/video10") && has("v4l2h264dec") {
				caps.AutoElements["h264"] = "v4l2h264dec"
			}
			if fileExists("/dev/video19") && has("v4l2slh265dec") {
				caps.AutoElements["h265"] = "v4l2slh265dec"
			}
			vaDevice := false
			for id := 128; id < 136; id++ {
				if fileExists(fmt.Sprintf("/dev/dri/renderD%d", id)) {
					vaDevice = true
					break
				}
			}
			if vaDevice {
				for codec, element := range map[string]string{"h264": "vah264dec", "h265": "vah265dec"} {
					if caps.AutoElements[codec] == "" && has(element) {
						caps.AutoElements[codec] = element
					}
				}
			}
		}
		for codec, element := range map[string]string{"h264": "avdec_h264", "h265": "avdec_h265"} {
			if caps.AutoElements[codec] == "" && has(element) {
				caps.AutoElements[codec] = element
			}
		}
		if caps.Element("h264") == "" && caps.Element("h265") == "" {
			return caps, fmt.Errorf("detect: no H.264 or H.265 decoder found (install GStreamer hardware plugins or libav)")
		}
	}
	if caps.Sink == "auto" {
		switch {
		case goos == "darwin" && has("autovideosink"):
			caps.Sink = "window"
		case goos == "darwin":
			return caps, fmt.Errorf("detect: autovideosink is missing (install Homebrew GStreamer plugins-base)")
		case len(c.Planes) > 0 && (caps.Element("h264") == "v4l2h264dec" || caps.Element("h265") == "v4l2slh265dec"):
			caps.Sink = "planes"
		case has("compositor"):
			caps.Sink = "compositor"
		default:
			return caps, fmt.Errorf("detect: no sink strategy: set `planes:` for kmssink planes or install gstreamer1.0-plugins-base (compositor)")
		}
	}
	return caps, nil
}

// ConnectorID reads the selected connected output's numeric DRM ID from sysfs. Callers that require
// exact routing must fail if it is absent instead of allowing kmssink to pick another output.
func ConnectorID(fsRoot, output string) (int, bool) {
	if output == "" {
		return 0, false
	}
	card, connector, err := selectedDRMCard(fsRoot, output)
	if err != nil {
		return 0, false
	}
	b, err := os.ReadFile(filepath.Join(fsRoot, "sys/class/drm", card+"-"+connector, "connector_id"))
	if err == nil {
		var id int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &id); err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}

// SelectedConnector refuses to silently route a named DRM output to the first connected screen.
// An explicit output must have both a mode and an ID kmssink can select.
func SelectedConnector(fsRoot, output string) (int, error) {
	if output == "" {
		return 0, nil
	}
	if _, ok := ScreenFor(fsRoot, output); !ok {
		return 0, fmt.Errorf("output %q has no connected DRM mode; check the cable and output name", output)
	}
	id, ok := ConnectorID(fsRoot, output)
	if !ok {
		return 0, fmt.Errorf("output %q has no DRM connector ID; kmssink cannot select it safely", output)
	}
	return id, nil
}
