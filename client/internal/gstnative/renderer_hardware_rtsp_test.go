package gstnative

import (
	"os"
	"runtime"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/layout"
)

// Run with EUFY_RTSP_HARDWARE_TEST=1 and an H.264 publisher on localhost:8554.
// This checks the real VideoToolbox element and its decoder-input probe, while
// ordinary CI uses synthetic sources that do not require a desktop GPU.
func TestRendererMacHardwareRTSP(t *testing.T) {
	if os.Getenv("EUFY_RTSP_HARDWARE_TEST") != "1" {
		t.Skip("requires local H.264 RTSP feed and macOS VideoToolbox")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("VideoToolbox is macOS-only")
	}
	c := &config.Config{Decoder: "auto", Sink: "window", Screen: config.Screen{Width: 640, Height: 360}, Latency: 200}
	caps, err := detect.Resolve(c, detect.HasElement, detect.FileExists)
	if err != nil {
		t.Fatal(err)
	}
	if got := caps.Element("h264"); got != "vtdec_hw" {
		t.Fatalf("auto selected %q instead of hardware VideoToolbox", got)
	}
	tile := layout.Placed{ID: "hardware", URL: "rtsp://127.0.0.1:8554/h264", Codec: "h264", W: 640, H: 360}
	opts := syntheticOptions(t)
	opts.source = nil
	r, err := New(c, []layout.Placed{tile}, caps, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		status := r.Status()
		if got := status.Tiles[tile.ID]; got.Decoder == "vtdec_hw" && got.DecodedFrames > 10 && status.OutputFrames > 10 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("hardware RTSP tile did not advance: %+v", r.Status())
}
