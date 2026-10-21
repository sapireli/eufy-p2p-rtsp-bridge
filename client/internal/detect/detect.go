// Package detect resolves "auto" settings against the machine: screen mode from DRM sysfs, decoder and
// sink from the GStreamer elements actually installed.
package detect

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// Screen reads the preferred mode of the first connected HDMI output.
func Screen(fsRoot string) (config.Screen, bool) {
	return ScreenFor(fsRoot, "")
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

// DRMDevice selects the card containing the configured connector, rather than assuming card0.
func DRMDevice(fsRoot, output string) (string, error) {
	if output == "" {
		output = "HDMI-A-*"
	}
	matches, _ := filepath.Glob(filepath.Join(fsRoot, "sys/class/drm/card*-"+output))
	for _, path := range matches {
		status, err := os.ReadFile(filepath.Join(path, "status"))
		if err != nil || strings.TrimSpace(string(status)) != "connected" {
			continue
		}
		card, _, _ := strings.Cut(filepath.Base(path), "-")
		return filepath.Join(fsRoot, "dev/dri", card), nil
	}
	return "", fmt.Errorf("detect: no connected DRM connector for %s", output)
}

// Resolve turns auto decoder/sink into concrete choices. `has`/`fileExists` are injectable for tests.
func Resolve(c *config.Config, has func(string) bool, fileExists func(string) bool) (pipeline.Caps, error) {
	caps := pipeline.Caps{Decoder: c.Decoder, Sink: c.Sink, Screen: c.Screen,
		SoftwareDecoders: map[string]bool{"h264": has("avdec_h264"), "h265": has("avdec_h265")}}
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

// HardwareHEVCFactory checks the installed hardware decoder factories, rather
// than treating a software HEVC decoder or H.264-only GPU as HEVC support.
func HardwareHEVCFactory(configured, resolved string, has func(string) bool) string {
	if configured == "software" {
		return ""
	}
	candidates := map[string][]string{"v4l2": {"v4l2h265dec", "v4l2slh265dec"}, "va": {"vah265dec"}}
	families := []string{resolved}
	if configured == "auto" {
		families = append(families, "v4l2", "va")
	}
	for _, family := range families {
		for _, factory := range candidates[family] {
			if has(factory) {
				return factory
			}
		}
	}
	return ""
}

// HardwareHEVCSinkCaps obtains only the selected factory's real sink template.
// Most drivers omit HEVC level/tier information; callers must conservatively
// decline automatic native selection rather than infer support from geometry.
func HardwareHEVCSinkCaps(factory string) string {
	switch factory {
	case "v4l2h265dec", "v4l2slh265dec", "vah265dec":
	default:
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	bytes, err := exec.CommandContext(ctx, "gst-inspect-1.0", factory).Output()
	if err != nil {
		return ""
	}
	return SinkTemplate(string(bytes))
}

func SinkTemplate(inspect string) string {
	lines := strings.Split(inspect, "\n")
	var out []string
	inSink := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.Contains(trim, "template:") {
			if inSink {
				break
			}
			inSink = strings.HasPrefix(trim, "SINK template:")
			continue
		}
		if inSink {
			if strings.HasPrefix(trim, "Element ") || strings.HasPrefix(trim, "Pad Properties:") {
				break
			}
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// HasElementProperty guards optional plugin upgrades: an older pacing plugin
// stays usable, but cannot claim observe-mode ingress age telemetry.
func HasElementProperty(element, property string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "gst-inspect-1.0", element).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(key) == property {
			return true
		}
	}
	return false
}
