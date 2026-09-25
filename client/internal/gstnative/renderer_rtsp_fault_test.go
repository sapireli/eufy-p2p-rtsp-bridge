package gstnative

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

// Run with EUFY_RTSP_FAULT_TEST=1 while MediaMTX accepts publishers on 127.0.0.1:8554.
// The test owns a unique publisher path and stops only its own FFmpeg process.
func TestRendererRTSPPublisherLossKeepsOutputAlive(t *testing.T) {
	if os.Getenv("EUFY_RTSP_FAULT_TEST") != "1" {
		t.Skip("requires local MediaMTX and FFmpeg")
	}
	ffmpeg := os.Getenv("EUFY_FFMPEG_BIN")
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	url := fmt.Sprintf("rtsp://127.0.0.1:8554/eufy-core-fault-%d", os.Getpid())
	cmd := exec.Command(ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "warning", "-re", "-f", "lavfi", "-i",
		"testsrc2=size=640x360:rate=15", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", "30", "-f", "rtsp", "-rtsp_transport", "tcp", url)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	time.Sleep(time.Second)
	tiles := []layout.Placed{{ID: "sole", URL: url, Codec: "h264", W: 320, H: 180}}
	opts := syntheticOptions(t)
	opts.source = nil
	if os.Getenv("EUFY_RTSP_FAULT_WINDOW") == "1" {
		opts.sink = ""
	}
	if selected := os.Getenv("EUFY_RTSP_FAULT_SINK"); selected != "" {
		opts.sink = selected
	}
	r, err := New(&config.Config{Latency: 200}, tiles,
		pipeline.Caps{Sink: "window", Decoder: "software", Screen: config.Screen{Width: 320, Height: 180}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitFrames(t, r, "sole", 0)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Logf("publisher exited: %v", err)
	}
	stopped = true
	time.Sleep(3 * time.Second)
	first := r.Status()
	time.Sleep(4 * time.Second)
	second := r.Status()
	t.Logf("publisher stopped: after 3s output=%d tile=%+v; after 7s output=%d tile=%+v",
		first.OutputFrames, first.Tiles["sole"], second.OutputFrames, second.Tiles["sole"])
	if second.OutputFrames <= first.OutputFrames+2 {
		t.Fatalf("output stopped after RTSP publisher loss: 3s=%+v 7s=%+v", first, second)
	}
	time.Sleep(18 * time.Second)
	third := r.Status()
	t.Logf("after 25s output=%d tile=%+v", third.OutputFrames, third.Tiles["sole"])
	select {
	case err := <-r.Errors():
		t.Fatalf("publisher loss killed compositor: %v", err)
	default:
	}
	if third.OutputFrames <= second.OutputFrames+2 {
		t.Fatalf("compositor stopped after publisher loss: 7s=%d 25s=%d", second.OutputFrames, third.OutputFrames)
	}
	time.Sleep(20 * time.Second)
	fourth := r.Status()
	t.Logf("after 45s output=%d tile=%+v", fourth.OutputFrames, fourth.Tiles["sole"])
	select {
	case err := <-r.Errors():
		t.Fatalf("repeated publisher retries killed compositor: %v", err)
	default:
	}
	if fourth.OutputFrames <= third.OutputFrames+2 || fourth.Tiles["sole"].Generation < 3 {
		t.Fatalf("second retry stopped black output: 25s=%+v 45s=%+v", third, fourth)
	}
}
