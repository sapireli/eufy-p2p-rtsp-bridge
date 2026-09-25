package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/wallstate"
)

type nativeRecorder struct {
	mu      sync.Mutex
	updates [][]layout.Placed
	errors  chan error
}

func (n *nativeRecorder) Update(tiles []layout.Placed) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.updates = append(n.updates, append([]layout.Placed(nil), tiles...))
	return nil
}

func (n *nativeRecorder) Errors() <-chan error { return n.errors }

func TestNativeStaticURLTileSurvivesMissingBridge(t *testing.T) {
	tile := layout.Placed{ID: "direct", Index: 0, URL: "rtsp://127.0.0.1:8554/direct", W: 640, H: 360}
	cfg := &config.Config{BridgeURL: "http://127.0.0.1:1", RTSPBase: "rtsp://127.0.0.1:8554",
		Tiles: []config.Tile{{ID: "direct", URL: tile.URL}}}
	native := &nativeRecorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := runDynamicNative(ctx, cfg, pipeline.Caps{Sink: "window"}, []layout.Placed{tile}, []layout.Placed{tile}, native); err != nil {
		t.Fatal(err)
	}
	native.mu.Lock()
	defer native.mu.Unlock()
	if len(native.updates) == 0 || len(native.updates[0]) != 1 || native.updates[0][0].URL != tile.URL {
		t.Fatalf("direct URL tile was dropped without bridge inventory: %+v", native.updates)
	}
}

func TestNativeRendererErrorStopsDynamicLoop(t *testing.T) {
	tile := layout.Placed{ID: "direct", URL: "rtsp://bridge/direct", W: 640, H: 360}
	cfg := &config.Config{BridgeURL: "http://127.0.0.1:1", RTSPBase: "rtsp://bridge",
		Tiles: []config.Tile{{ID: "direct", URL: tile.URL}}}
	native := &nativeRecorder{errors: make(chan error, 1)}
	native.errors <- context.DeadlineExceeded
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runDynamicNative(ctx, cfg, pipeline.Caps{Sink: "window"}, []layout.Placed{tile}, []layout.Placed{tile}, native); err != context.DeadlineExceeded {
		t.Fatalf("native renderer failure was ignored: %v", err)
	}
}

func TestTilesForSnapshotCodecAndPlanNames(t *testing.T) {
	cfg := &config.Config{BridgeURL: "http://bridge.local:3000", RTSPBase: "rtsp://bridge.local:8554",
		Tiles: []config.Tile{{Camera: "serial", Codec: "h264"}}}
	tile := layout.Placed{ID: "front", Index: 0, Camera: "serial", URL: "rtsp://old", W: 640, H: 360}
	showing := map[int]string{0: "serial"}
	snapshot := map[int]string{0: wallstate.ContentSnapshot}
	selected := tilesFor(cfg, []layout.Placed{tile}, showing, snapshot,
		func(string) string { return "Front Door" }, func(string) string { return "h265" })
	if len(selected) != 1 || selected[0].Codec != "h265" ||
		selected[0].URL != "rtsp://bridge.local:8554/Front%20Door" ||
		selected[0].StillURL != "http://bridge.local:3000/snapshot/serial" {
		t.Fatalf("snapshot selection lost stream key/codec/still: %+v", selected)
	}
	selected = tilesFor(cfg, []layout.Placed{tile}, showing, map[int]string{0: wallstate.ContentLive}, nil, nil)
	if len(selected) != 1 || selected[0].Codec != "h264" || selected[0].StillURL != "" {
		t.Fatalf("configured codec fallback failed: %+v", selected)
	}
	if got := tilesFor(cfg, []layout.Placed{tile}, showing, map[int]string{0: wallstate.ContentNone}, nil, nil); len(got) != 0 {
		t.Fatalf("sleeping camera was rendered: %+v", got)
	}
	if got := planNames([]pipeline.Plan{{Name: "front"}, {Name: "yard"}}); got != "front, yard" {
		t.Fatalf("plan names: %s", got)
	}
}
