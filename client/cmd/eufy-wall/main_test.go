package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/wallstate"
)

func TestAPIPacketHintWorksWithConfiguredAspectAndExplicitURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"sn":"FRONT","width":1600,"height":2200,"rtspTcpPacketSize":8192}]`))
	}))
	defer server.Close()
	c := &config.Config{RTSPBase: "rtsp://bridge:8554", Tiles: []config.Tile{{Camera: "FRONT", Aspect: "tall"}, {Camera: "FRONT", URL: "rtsp://manual/custom"}}}
	if err := detectAspectsAt(c, server.URL); err != nil {
		t.Fatal(err)
	}
	if got := c.TileURL(c.Tiles[0]); got != "rtsp://bridge:8554/FRONT?pkt_size=8192" {
		t.Fatalf("configured aspect prevented packet hint: %q", got)
	}
	if got := c.TileURL(c.Tiles[1]); got != "rtsp://manual/custom" {
		t.Fatalf("API hint changed explicit URL: %q", got)
	}
}

func TestHelloPacketHintTunesMotionAndReconnectStreamAlias(t *testing.T) {
	store := wallstate.New()
	store.Apply(wallstate.Message{Type: "hello", Cameras: []wallstate.HelloCamera{{SN: "FRONT", StreamKey: "front_door", RTSPTCPPacketSize: 8192}}})
	c := &config.Config{RTSPBase: "rtsp://bridge:8554", Latency: 200, Planes: []int{98}, Tiles: []config.Tile{{Motion: "latest"}}}
	tiles := []layout.Placed{{Index: 0, W: 960, H: 1080}}
	check := func(want string) {
		t.Helper()
		cam, _ := store.Camera("FRONT")
		snapshot := c.WithRTSPPacketSizeHints(map[string]int{cam.SN: cam.RTSPTCPPacketSize})
		plans := plansFor(snapshot, pipeline.Caps{Decoder: "v4l2", Sink: "planes"}, tiles,
			map[int]string{0: "FRONT"}, map[int]string{0: "live"}, func(string) string { return cam.StreamKey })
		if len(plans) != 1 || !strings.Contains(pipeline.String(plans[0].Args), "location="+want+" latency=") {
			t.Fatalf("hello hint/stream alias: %+v", plans)
		}
	}
	check("rtsp://bridge:8554/front_door?pkt_size=8192")
	// Reconnecting to a bridge without this capability removes the automatic hint.
	store.Apply(wallstate.Message{Type: "hello", Cameras: []wallstate.HelloCamera{{SN: "FRONT", StreamKey: "front_door"}}})
	check("rtsp://bridge:8554/front_door")
}

func TestEventReconnectPreservesExplicitCameraURL(t *testing.T) {
	c := &config.Config{RTSPBase: "rtsp://bridge:8554", Latency: 200, Planes: []int{98},
		Tiles: []config.Tile{{Camera: "FRONT", URL: "rtsp://manual:8565/custom", Codec: "h264"}}}
	tiles := []layout.Placed{{Index: 0, Camera: "FRONT", W: 960, H: 1080}}
	for _, key := range []string{"FRONT", "front_door"} {
		plans := plansFor(c, pipeline.Caps{Decoder: "v4l2", Sink: "planes"}, tiles,
			map[int]string{0: "FRONT"}, map[int]string{0: "live"}, func(string) string { return key })
		if len(plans) != 1 || !strings.Contains(pipeline.String(plans[0].Args), "location=rtsp://manual:8565/custom") {
			t.Fatalf("explicit URL lost during reconnect with key %q: %+v", key, plans)
		}
	}
}

func TestParseAvahi(t *testing.T) {
	wrong := "=;eth0;IPv4;Other;_http._tcp;local;other.local;192.168.1.3;3000;\"rtsp=8554\"\n"
	good := "=;eth0;IPv4;Eufy Wall;_eufy-wall._tcp;local;bridge.local;192.168.1.8;3000;\"rtsp=8554\"\n"
	loopback := "=;lo;IPv4;Eufy Wall;_eufy-wall._tcp;local;bridge.local;127.0.0.1;3000;\"rtsp=8554\"\n"
	base, err := parseAvahi(wrong + loopback + good)
	if err != nil || base != "rtsp://192.168.1.8:8554" {
		t.Fatalf("got %q, %v", base, err)
	}
	if _, err := parseAvahi(wrong); err == nil {
		t.Fatal("accepted unrelated service")
	}
	if _, err := parseAvahi(loopback); err == nil {
		t.Fatal("accepted loopback address")
	}
	base, err = parseAvahi("=;eth0;IPv4;Eufy Wall;_eufy-wall._tcp;local;bridge.local;192.168.1.8;3000;\"rtsp=8565\"\n")
	if err != nil || base != "rtsp://192.168.1.8:8565" {
		t.Fatalf("custom RTSP port: got %q, %v", base, err)
	}
}

func TestDetectAspectsFromObservedStreamGeometry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"sn":"FRONT","width":1600,"height":2200},{"sn":"GARAGE","width":1280,"height":720}]`))
	}))
	defer server.Close()
	c := &config.Config{Tiles: []config.Tile{{Camera: "FRONT"}, {Camera: "GARAGE"}, {Camera: "FRONT", Aspect: "wide"}}}
	if err := detectAspectsAt(c, server.URL); err != nil {
		t.Fatal(err)
	}
	if c.Tiles[0].Aspect != "tall" || c.Tiles[1].Aspect != "wide" || c.Tiles[2].Aspect != "wide" {
		t.Fatalf("aspects: %+v", c.Tiles)
	}
}

func TestChangeViewByCameraName(t *testing.T) {
	var posted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cameras" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"sn":"T8214","name":"Front Door CLE","dual":true},{"sn":"OTHER","name":"Balcony CLE","dual":false}]`))
			return
		}
		posted = r.Method + " " + r.URL.String()
		_, _ = w.Write([]byte(`{"sn":"T8214","dualView":"pip-br"}`))
	}))
	defer server.Close()
	if err := changeViewAt(server.URL, "front door cle", "pip-br"); err != nil {
		t.Fatal(err)
	}
	if posted != "POST /api/cameras/T8214/view?mode=pip-br" {
		t.Fatalf("posted %q", posted)
	}
	if err := changeViewAt(server.URL, "Balcony CLE", "pip-br"); err == nil || !strings.Contains(err.Error(), "not a dual-lens") {
		t.Fatalf("got %v", err)
	}
	if err := changeViewAt(server.URL, "Front Door CLE", "bad"); err == nil {
		t.Fatal("accepted bad mode")
	}
}
