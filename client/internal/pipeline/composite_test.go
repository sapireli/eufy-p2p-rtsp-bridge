package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

func isolatedFixture(t *testing.T) (*config.Config, Caps, []layout.Placed) {
	t.Helper()
	c := &config.Config{RTSPBase: "rtsp://bridge.test:8554", Layout: "2x1", Decoder: "auto", Tiles: []config.Tile{{Camera: "A"}, {Camera: "B"}}, HardwarePinnedCameras: []string{"A"}, HardwarePriority: map[string]int{"A": 100}}
	caps := Caps{Decoder: "v4l2", Sink: "window", Screen: config.Screen{Width: 640, Height: 360}, IsolatedTiles: true, TileConfigDir: t.TempDir(), SoftwareDecoders: map[string]bool{"h264": true}}
	tiles, err := layout.Place(c, caps.Screen)
	if err != nil {
		t.Fatal(err)
	}
	return c, caps, tiles
}

func TestCompositeCameraChangesKeepTheRendererPlan(t *testing.T) {
	c, caps, tiles := isolatedFixture(t)
	before, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]layout.Placed(nil), tiles...)
	changed[1].Camera = "C"
	changed[1].URL = "rtsp://bridge.test:8554/C?pkt_size=8192"
	after, err := Plans(c, changed, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before[0].Args, after[0].Args) {
		t.Fatal("camera switch replaced shared renderer")
	}
	if string(before[0].TileConfigs[0].Contents) != string(after[0].TileConfigs[0].Contents) {
		t.Fatal("healthy tile control changed")
	}
	if string(before[0].TileConfigs[1].Contents) == string(after[0].TileConfigs[1].Contents) {
		t.Fatal("changed camera has no control update")
	}
	if strings.Contains(String(after[0].Args), "rtsp://") {
		t.Fatal("decoder resources leaked into renderer identity")
	}
	idle, err := Plans(c, nil, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before[0].Args, idle[0].Args) || len(idle[0].TileConfigs) != 2 {
		t.Fatal("idle sources stopped renderer")
	}
	for _, f := range idle[0].TileConfigs {
		if !strings.Contains(string(f.Contents), "\npipeline=\n") {
			t.Fatal("idle tile still opens media")
		}
	}
}

func TestCompositeControlsPreserveDecoderPreferenceAndEscaping(t *testing.T) {
	c, caps, tiles := isolatedFixture(t)
	tiles[0].URL = `rtsp://bridge.test:8554/a?note=a\b%22c`
	plans, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	body := string(plans[0].TileConfigs[0].Contents)
	if !strings.HasPrefix(body, "[tile]\ngeneration=") {
		t.Fatalf("not a keyfile: %q", body)
	}
	if !strings.Contains(body, "hardware-pinned=true\nhardware-priority=100\n") {
		t.Fatal(body)
	}
	primary := strings.Split(strings.Split(body, "\npipeline=")[1], "\n")[0]
	software := strings.Split(strings.Split(body, "\nsoftware-pipeline=")[1], "\n")[0]
	if !strings.Contains(primary, "v4l2h264dec") || strings.Contains(primary, "avdec_h264") {
		t.Fatal("hardware not first")
	}
	if !strings.Contains(software, "avdec_h264") || strings.Contains(software, "v4l2h264dec") {
		t.Fatal("software fallback missing")
	}
	if strings.Contains(primary, "\\b") && !strings.Contains(primary, "\\\\b") {
		t.Fatal("INI lost backslash escape")
	}
	c.Decoder = "v4l2"
	explicit, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(explicit[0].TileConfigs[0].Contents), "\nsoftware-pipeline=\n") {
		t.Fatal("explicit hardware choice got software override")
	}
}

func TestPlanePlansCarryCameraHardwarePriority(t *testing.T) {
	c, caps, tiles := isolatedFixture(t)
	caps.Sink = "planes"
	c.Planes = []int{98, 120}
	plans, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !plans[0].HardwarePinned || plans[0].HardwarePriority != 100 || plans[0].HardwareDecoder != "v4l2h264dec" {
		t.Fatalf("lost priority: %+v", plans[0])
	}
	if plans[1].HardwarePinned || plans[1].HardwarePriority != 0 {
		t.Fatal("unconfigured camera pinned")
	}
	if len(plans[0].SoftwareArgs) == 0 {
		t.Fatal("hardware owner cannot yield to software")
	}
}

func TestCompositeKeepsSourceValidation(t *testing.T) {
	c, caps, tiles := isolatedFixture(t)
	caps.TimingDiagnostics = "/tmp/private-diagnostic"
	if _, err := Plans(c, tiles, caps); err == nil || !strings.Contains(err.Error(), "timing diagnostics unavailable") {
		t.Fatalf("diagnostic guard lost: %v", err)
	}
	caps.TimingDiagnostics = ""
	tiles[0].Codec = "unsupported"
	if _, err := Plans(c, tiles, caps); err == nil || !strings.Contains(err.Error(), "cannot decode") {
		t.Fatalf("codec guard lost: %v", err)
	}
}

func TestUnknownDotOverlayDoesNotChangeVideoDecoderControls(t *testing.T) {
	c, caps, tiles := isolatedFixture(t)
	before, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	caps.DotImage = "/private/dot.png"
	after, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before[0].TileConfigs, after[0].TileConfigs) {
		t.Fatal("dot changed video decoder controls")
	}
	args := String(after[0].Args)
	if !strings.Contains(args, "imagefreeze is-live=true ! video/x-raw,format=RGBA,framerate=1/1") || !strings.Contains(args, "sink_10000::zorder=1000") || strings.Contains(args, "framerate=30/1") {
		t.Fatal(args)
	}
}
