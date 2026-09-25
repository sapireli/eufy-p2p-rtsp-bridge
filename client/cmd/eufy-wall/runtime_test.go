package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"github.com/coder/websocket"
)

type recordingWall struct {
	mu     sync.Mutex
	plans  [][]pipeline.Plan
	notify chan struct{}
}

func (m *recordingWall) Run(ctx context.Context, plans []pipeline.Plan) error {
	m.Update(ctx, plans)
	<-ctx.Done()
	return nil
}
func (m *recordingWall) Update(_ context.Context, plans []pipeline.Plan) {
	m.mu.Lock()
	m.plans = append(m.plans, append([]pipeline.Plan(nil), plans...))
	m.mu.Unlock()
	select {
	case m.notify <- struct{}{}:
	default:
	}
}
func (m *recordingWall) latest() []pipeline.Plan {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plans[len(m.plans)-1]
}

func TestDynamicWallRecoversLiveCameraAndReleasesHold(t *testing.T) {
	holds := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/hold/") {
			holds <- r.Method + " " + r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"hello","cameras":[{"sn":"CAM1","mode":"on_demand","state":"idle","streamKey":"front","codec":"h264"}]}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"streamState","sn":"CAM1","state":"live","codec":"h265"}`))
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, err := config.Parse([]byte("schema_version: 2\nbridge_url: " + srv.URL + "\nrtsp_base: rtsp://bridge:8554\nlayout: 1\ntiles:\n  - id: front\n    camera: CAM1\n"))
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := layout.Place(c, config.Screen{Width: 640, Height: 480})
	if err != nil {
		t.Fatal(err)
	}
	mgr := &recordingWall{notify: make(chan struct{}, 16)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runDynamic(ctx, c, pipeline.Caps{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 640, Height: 480}}, tiles, mgr, nil)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	foundLive := false
	for !foundLive {
		select {
		case <-mgr.notify:
			for _, p := range mgr.latest() {
				args := pipeline.String(p.Args)
				if strings.Contains(args, "rtsp://bridge:8554/front") && strings.Contains(args, "avdec_h265") {
					foundLive = true
				}
			}
		case <-deadline:
			cancel()
			t.Fatal("dynamic wall never selected the live camera and current codec")
		}
	}
	select {
	case got := <-holds:
		if got != "POST /hold/CAM1" {
			t.Fatalf("first hold = %q", got)
		}
	case <-deadline:
		t.Fatal("on-demand tile did not take a hold")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("wall did not stop")
	}
	for {
		select {
		case got := <-holds:
			if got == "DELETE /hold/CAM1" {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("wall did not release its hold")
		}
	}
}

func TestDynamicWallWithoutControlEndpointRunsStaticUntilStopped(t *testing.T) {
	c := &config.Config{RTSPBase: "bad-url"}
	mgr := &recordingWall{notify: make(chan struct{}, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runDynamic(ctx, c, pipeline.Caps{}, nil, mgr, []pipeline.Plan{{Name: "static"}}); close(done) }()
	select {
	case <-mgr.notify:
	case <-time.After(time.Second):
		t.Fatal("static plan was not started")
	}
	if got := mgr.latest(); len(got) != 1 || got[0].Name != "static" {
		t.Fatalf("plans=%v", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("static wall did not stop")
	}
}

func TestMotionTileExpiresAndReleasesHoldWithoutAnotherBridgeEvent(t *testing.T) {
	holds := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/hold/") {
			holds <- r.Method
			w.WriteHeader(http.StatusNoContent)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"hello","cameras":[{"sn":"CAM1","mode":"on_motion","state":"live","streamKey":"front","codec":"h264"}]}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"motion","sn":"CAM1","event":"motion"}`))
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, err := config.Parse([]byte("schema_version: 2\nbridge_url: " + srv.URL + "\nrtsp_base: rtsp://bridge:8554\nlayout: 1\ntiles:\n  - id: recent\n    motion: latest\n    watch: [CAM1]\n    blank_after_seconds: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := layout.Place(c, config.Screen{Width: 640, Height: 480})
	if err != nil {
		t.Fatal(err)
	}
	mgr := &recordingWall{notify: make(chan struct{}, 16)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runDynamic(ctx, c, pipeline.Caps{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 640, Height: 480}}, tiles, mgr, nil)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("wall did not stop")
		}
	}()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-mgr.notify:
			plans := mgr.latest()
			if len(plans) > 0 && strings.Contains(pipeline.String(plans[0].Args), "rtsp://bridge:8554/front") {
				goto live
			}
		case <-deadline:
			t.Fatal("motion tile never showed the live camera")
		}
	}
live:
	select {
	case method := <-holds:
		if method != http.MethodPost {
			t.Fatalf("first hold request = %s", method)
		}
	case <-deadline:
		t.Fatal("motion tile did not take a hold")
	}
	for {
		select {
		case <-mgr.notify:
			plans := mgr.latest()
			if len(plans) > 0 && !strings.Contains(pipeline.String(plans[0].Args), "rtspsrc") {
				goto blank
			}
		case <-deadline:
			t.Fatal("motion tile stayed live after its blank deadline without another event")
		}
	}
blank:
	select {
	case method := <-holds:
		if method != http.MethodDelete {
			t.Fatalf("expired motion hold request = %s", method)
		}
	case <-deadline:
		t.Fatal("expired motion tile did not release its hold")
	}
}
