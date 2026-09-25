package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

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

func TestDynamicFixedOnDemandHoldAndOnMotionSleep(t *testing.T) {
	stopServer := make(chan struct{})
	holds := make(chan string, 8)
	var motionHolds atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			data, _ := json.Marshal(wallstate.Message{Type: "hello", At: time.Now().UnixMilli(),
				Cameras: []wallstate.HelloCamera{
					{SN: "DEMAND", Mode: "on_demand", State: "idle", HoldSeconds: 5},
					{SN: "MOTION", Mode: "on_motion", State: "idle", HoldSeconds: 5},
				}})
			if conn.Write(context.Background(), websocket.MessageText, data) != nil {
				return
			}
			<-stopServer
		case "/hold/DEMAND":
			select {
			case holds <- r.Method:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
		case "/hold/MOTION":
			motionHolds.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() { close(stopServer); server.Close() }()
	cfg := &config.Config{BridgeURL: server.URL, RTSPBase: "rtsp://bridge:8554",
		Tiles: []config.Tile{{ID: "demand", Camera: "DEMAND"}, {ID: "motion", Camera: "MOTION"}}}
	tiles := []layout.Placed{
		{ID: "demand", Index: 0, Camera: "DEMAND", URL: "rtsp://bridge:8554/DEMAND", W: 64, H: 64},
		{ID: "motion", Index: 1, Camera: "MOTION", URL: "rtsp://bridge:8554/MOTION", X: 64, W: 64, H: 64},
	}
	native := &nativeRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDynamicNative(ctx, cfg, pipeline.Caps{Sink: "window"}, tiles, tiles, native) }()
	select {
	case method := <-holds:
		if method != http.MethodPost {
			t.Fatalf("first on-demand hold method %s", method)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("fixed on-demand camera never received hold")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dynamic wall did not shut down")
	}
	if motionHolds.Load() != 0 {
		t.Fatal("sleeping fixed on_motion camera was held")
	}
}
