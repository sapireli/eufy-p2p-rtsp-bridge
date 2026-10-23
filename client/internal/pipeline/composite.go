package pipeline

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

func cameraPinned(c *config.Config, camera string) bool {
	for _, id := range c.HardwarePinnedCameras {
		if id == camera {
			return true
		}
	}
	return false
}

func hardwareDecoder(c *config.Config, t layout.Placed, caps Caps) string {
	if t.StillURL != "" {
		return ""
	}
	selected, native, _ := selectNative(c, []layout.Placed{t}, caps)
	if decoder := native.decoderOverrides[t.Camera]; decoder != "" {
		return decoder
	}
	if caps.Decoder == "software" {
		return ""
	}
	return decoders[caps.Decoder][codecOf(selected[0])]
}

func compatibleHardwareDecoder(t layout.Placed, caps Caps) string {
	if t.StillURL != "" || caps.Decoder == "software" {
		return ""
	}
	return decoders[caps.Decoder][codecOf(t)]
}

// GStreamer property strings need their own quoting; no shell evaluates this description.
func launchDescription(args []string) string {
	out := make([]string, len(args))
	for i, arg := range args {
		key, value, property := strings.Cut(arg, "=")
		if property && !strings.ContainsAny(key, "/,<> ") {
			out[i] = key + "=" + strconv.Quote(value)
		} else {
			out[i] = arg
		}
	}
	return strings.Join(out, " ")
}

func decoderDescription(c *config.Config, t layout.Placed, caps Caps) string {
	args := sourceArgs(c, t, caps, 0)
	if t.StillURL != "" {
		for i, arg := range args {
			if arg == "imagefreeze" {
				args = append(args[:i+1], append([]string{"is-live=true"}, args[i+1:]...)...)
				break
			}
		}
	}
	args = append(args, "!", "videoconvert", "!", "video/x-raw,format=RGBA", "!", "appsink", "name=tile_output", "sync=false", "max-buffers=1", "drop=true")
	return launchDescription(args)
}

func keyFileValue(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
}

func tileControl(c *config.Config, t layout.Placed, caps Caps) []byte {
	primary, compatible, software, factory := "", "", "", ""
	if t.URL != "" || t.StillURL != "" {
		selected, native, used := selectNative(c, []layout.Placed{t}, caps)
		primary = decoderDescription(c, selected[0], native)
		factory = hardwareDecoder(c, t, caps)
		compatibleCaps := caps
		compatibleCaps.NativeDecoder, compatibleCaps.NativeRenditions, compatibleCaps.decoderOverrides = "", nil, nil
		if used {
			compatible = decoderDescription(c, t, compatibleCaps)
		}
		if c.Decoder == "auto" && caps.Decoder != "software" && t.StillURL == "" && caps.SoftwareDecoders[codecOf(t)] {
			compatibleCaps.Decoder = "software"
			software = decoderDescription(c, t, compatibleCaps)
		}
	}
	body := fmt.Sprintf("pipeline=%s\ncompatible-pipeline=%s\nsoftware-pipeline=%s\nhardware-decoder=%s\nhardware-pinned=%t\nhardware-priority=%d\nrestart-min-ms=%d\nrestart-max-ms=%d\nrestart-stable-ms=%d\n", keyFileValue(primary), keyFileValue(compatible), keyFileValue(software), keyFileValue(factory), cameraPinned(c, t.Camera), c.HardwarePriority[t.Camera], c.Restart.MinSeconds*1000, c.Restart.MaxSeconds*1000, c.Restart.StableSeconds*1000)
	generation := sha256.Sum256([]byte(body))
	return []byte(fmt.Sprintf("[tile]\ngeneration=%x\n%s", generation, body))
}

// The compositor's geometry and process stay stable; camera changes only replace private tile controls.
func compositePlan(c *config.Config, live []layout.Placed, caps Caps) ([]Plan, error) {
	if caps.TileConfigDir == "" {
		return nil, fmt.Errorf("pipeline: isolated compositor needs a private tile-control directory")
	}
	selected, sourceCaps, _ := selectNative(c, live, caps)
	if err := validateSources(c, selected, sourceCaps); err != nil {
		return nil, err
	}
	screen := caps.Screen
	if screen.Width == 0 || screen.Height == 0 {
		screen = c.Screen
	}
	slots, err := layout.Place(c, screen)
	if err != nil {
		return nil, err
	}
	active := make(map[int]layout.Placed, len(live))
	for _, t := range live {
		active[t.Index] = t
	}
	args := []string{"-e"}
	controls := make([]TileConfig, 0, len(slots))
	for _, slot := range slots {
		path := filepath.Join(caps.TileConfigDir, fmt.Sprintf("tile-%d.ini", slot.Index))
		t, ok := active[slot.Index]
		if !ok {
			t = slot
			t.URL = ""
			t.StillURL = ""
			t.Camera = ""
		}
		controls = append(controls, TileConfig{Path: path, Contents: tileControl(c, t, caps)})
		args = append(args, "eufytile", fmt.Sprintf("name=tile%d", slot.Index), "config-file="+path, "!", "videoconvert", "!", fmt.Sprintf("mix.sink_%d", slot.Index))
	}
	args = append(args, dotSources(slots, caps.DotImage)...)
	args = append(args, "compositor", "name=mix", "background=black", "force-live=true", "ignore-inactive-pads=true")
	for _, t := range slots {
		args = append(args, fmt.Sprintf("sink_%d::xpos=%d", t.Index, t.X), fmt.Sprintf("sink_%d::ypos=%d", t.Index, t.Y), fmt.Sprintf("sink_%d::width=%d", t.Index, t.W), fmt.Sprintf("sink_%d::height=%d", t.Index, t.H), fmt.Sprintf("sink_%d::sizing-policy=keep-aspect-ratio", t.Index))
	}
	args = append(args, dotPads(slots, caps.DotImage)...)
	args = append(args, "!", fmt.Sprintf("video/x-raw,width=%d,height=%d", screen.Width, screen.Height))
	clockArgs := []string{"sync=false"}
	if caps.DecodedPacing {
		args = append(args, "!", "queue", "max-size-buffers=1", "max-size-bytes=0", "max-size-time=0", "leaky=downstream", "!", "eufypacing", "!", "watchdog", "timeout=15000")
		clockArgs = []string{"sync=true", "max-lateness=67000000", "processing-deadline=0"}
	}
	args = append(args, "!")
	if caps.Sink == "window" {
		args = append(args, "autovideosink", clockArgs[0])
	} else {
		args = append(args, caps.kmssinkArgs(clockArgs...)...)
	}
	return []Plan{{Name: "wall", Args: args, TileConfigs: controls}}, nil
}

// Composition currently has no per-camera final-output identity. Its stationary
// orange indicators explicitly mean unknown, regardless of incoming frame rate.
func dotSources(tiles []layout.Placed, image string) []string {
	if image == "" {
		return nil
	}
	var args []string
	for _, t := range tiles {
		args = append(args, "filesrc", "location="+image, "!", "pngdec", "!", "imagefreeze", "is-live=true", "!", "video/x-raw,format=RGBA,framerate=1/1", "!", fmt.Sprintf("mix.sink_%d", 10000+t.Index))
	}
	return args
}
func dotPads(tiles []layout.Placed, image string) []string {
	if image == "" {
		return nil
	}
	var args []string
	for _, t := range tiles {
		id := 10000 + t.Index
		args = append(args, fmt.Sprintf("sink_%d::xpos=%d", id, t.X+t.W-22), fmt.Sprintf("sink_%d::ypos=%d", id, t.Y+t.H-20), fmt.Sprintf("sink_%d::width=16", id), fmt.Sprintf("sink_%d::height=16", id), fmt.Sprintf("sink_%d::zorder=1000", id))
	}
	return args
}
