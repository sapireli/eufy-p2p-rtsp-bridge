package gstnative

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func syntheticOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		StatusPath:   filepath.Join(t.TempDir(), "status.json"),
		ConfigSHA256: HashConfig([]byte("synthetic config")),
		sink:         "fakesink sync=false",
		source: func(tile layout.Placed) (string, error) {
			switch tile.URL {
			case "test://ball":
				return "videotestsrc is-live=true pattern=ball ! videoconvert", nil
			case "test://snow":
				return "videotestsrc is-live=true pattern=snow ! videoconvert", nil
			case "test://error":
				return "filesrc location=/definitely/missing/eufy-wall-test ! decodebin ! videoconvert", nil
			case "test://single":
				return "videotestsrc is-live=true num-buffers=1 pattern=ball ! videoconvert", nil
			case "test://invalid":
				return "element-that-does-not-exist", nil
			default:
				return blackSource, nil
			}
		},
	}
}

func testTiles() []layout.Placed {
	return []layout.Placed{
		{ID: "left", Index: 0, URL: "test://ball", X: 0, Y: 0, W: 64, H: 64},
		{ID: "right", Index: 1, URL: "test://snow", X: 64, Y: 0, W: 64, H: 64},
	}
}

func waitFrames(t *testing.T, r *Renderer, id string, before uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		n := r.Status().Tiles[id].DecodedFrames
		if n > before+5 {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s produced no frames after %d: %+v", id, before, r.Status())
	return 0
}

func waitOutput(t *testing.T, r *Renderer, before uint64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r.Status().OutputFrames > before {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("compositor output stopped after frame %d: %+v", before, r.Status())
}

func TestRendererSwitchKeepsUnaffectedTileProducing(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	opts := syntheticOptions(t)
	r, err := New(&config.Config{Latency: 200}, tiles,
		pipeline.Caps{Sink: "compositor", Decoder: "software", Screen: config.Screen{Width: 128, Height: 64}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitFrames(t, r, "left", 0)
	rightBefore := waitFrames(t, r, "right", 0)
	outBefore := r.Status().OutputFrames

	tiles[0].URL = "test://snow"
	if err := r.Update(tiles); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, r, "right", rightBefore)
	waitFrames(t, r, "left", 0)
	status := r.Status()
	waitOutput(t, r, outBefore)
	if status.Tiles["right"].Generation != 1 || status.Tiles["left"].Generation != 2 {
		t.Fatalf("whole output or unaffected tile restarted: %+v", status)
	}
	if status.Tiles["right"].DecodedFrames <= rightBefore {
		t.Fatal("unaffected tile froze during switch")
	}
	data, err := os.ReadFile(opts.StatusPath)
	if err != nil || !strings.Contains(string(data), "\"config_sha256\"") {
		t.Fatalf("status not written: %v %q", err, data)
	}

	// A sleeping tile goes black while the other stream and output continue.
	tiles[0].URL = ""
	rightBefore = r.Status().Tiles["right"].DecodedFrames
	if err := r.Update(tiles); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, r, "right", rightBefore)
	if got := r.Status().Tiles["left"]; got.ExpectedLive || got.SourceKind != "black" {
		t.Fatalf("sleeping tile status: %+v", got)
	}
}

func TestRendererRejectsInvalidUpdateWithoutChangingTiles(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	opts := syntheticOptions(t)
	r, err := New(&config.Config{}, tiles,
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	before := r.Status().Tiles["left"].Generation
	for _, invalid := range [][]layout.Placed{
		{{ID: "unknown", X: 0, Y: 0, W: 64, H: 64}},
		{tiles[0], tiles[0]},
		{{ID: "left", X: 1, Y: 0, W: 64, H: 64}},
	} {
		if err := r.Update(invalid); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
		if got := r.Status().Tiles["left"].Generation; got != before {
			t.Fatal("invalid update changed source")
		}
	}
	badSource := append([]layout.Placed(nil), tiles...)
	badSource[0].URL = "test://invalid"
	if err := r.Update(badSource); err == nil {
		t.Fatal("invalid source accepted")
	}
	if got := r.Status().Tiles["left"].Generation; got != before {
		t.Fatal("source parse failure interrupted existing tile")
	}
}

func TestRendererRecoversFailedTileWithoutRestartingPeer(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	tiles[0].URL = "test://single"
	r, err := New(&config.Config{}, tiles,
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, syntheticOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitFrames(t, r, "right", 0)
	peerBefore := r.Status().Tiles["right"].DecodedFrames
	r.mu.Lock()
	left := r.slots["left"]
	left.installed = time.Now().Add(-21 * time.Second)
	left.counter.last.Store(time.Now().Add(-21 * time.Second).UnixNano())
	r.recoverStalledLocked(time.Now(), nil)
	r.mu.Unlock()
	if got := r.Status().Tiles["left"]; got.Generation != 2 || got.State != "playing" {
		t.Fatalf("failed tile was not restarted: %+v", got)
	}
	waitFrames(t, r, "right", peerBefore)
}

func TestPipelineDescriptionRejectsBadGeometry(t *testing.T) {
	caps := pipeline.Caps{Sink: "compositor", Screen: config.Screen{Width: 128, Height: 64}}
	for _, tiles := range [][]layout.Placed{
		{{ID: "a", X: 0, Y: 0, W: 0, H: 64}},
		{{ID: "a", X: 100, Y: 0, W: 64, H: 64}},
		{{ID: "a", X: 0, Y: 0, W: 64, H: 64}, {ID: "a", X: 64, Y: 0, W: 64, H: 64}},
	} {
		if _, _, err := pipelineDescription(tiles, caps, "fakesink"); err == nil {
			t.Fatalf("accepted %+v", tiles)
		}
	}
}

// Run explicitly with EUFY_RTSP_TEST=1 against local MediaMTX feeds. This exercises the
// rtspsrc dynamic pads, depayloaders, both decoders, and switching in one live compositor.
func TestRendererLocalRTSPMixedCodecSwitch(t *testing.T) {
	if os.Getenv("EUFY_RTSP_TEST") != "1" {
		t.Skip("requires local h264 and h265 RTSP feeds")
	}
	if _, err := load(); err != nil {
		t.Fatal(err)
	}
	tiles := []layout.Placed{
		{ID: "h264", URL: "rtsp://127.0.0.1:8554/h264", Codec: "h264", W: 640, H: 360},
		{ID: "h265", URL: "rtsp://127.0.0.1:8554/h265", Codec: "h265", X: 640, W: 640, H: 360},
	}
	opts := syntheticOptions(t)
	opts.source = nil
	r, err := New(&config.Config{Latency: 200}, tiles,
		pipeline.Caps{Sink: "window", Decoder: "software", Screen: config.Screen{Width: 1280, Height: 360}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitFrames(t, r, "h264", 0)
	peerBefore := waitFrames(t, r, "h265", 0)
	tiles[0].URL, tiles[0].Codec = "rtsp://127.0.0.1:8554/h265", "h265"
	if err := r.Update(tiles); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, r, "h264", 0)
	waitFrames(t, r, "h265", peerBefore)
	if r.Status().Tiles["h265"].Generation != 1 {
		t.Fatal("unaffected H.265 tile restarted")
	}
}
