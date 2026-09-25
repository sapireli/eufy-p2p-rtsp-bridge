package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"eufy-wall/internal/config"
)

func TestPlaneHealthProbesLiveTilesAndReleasesDemandHold(t *testing.T) {
	var mu sync.Mutex
	var holds, releases int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/hold/DEMAND" || r.URL.Query().Get("seconds") != "20" {
			t.Errorf("unexpected hold request %s", r.URL)
		}
		switch r.Method {
		case http.MethodPost:
			holds++
		case http.MethodDelete:
			releases++
		default:
			t.Errorf("unexpected hold method %s", r.Method)
		}
	}))
	defer server.Close()
	c := planeProbeConfig(server.URL)
	var locations []string
	var codecs []string
	run := func(_ context.Context, args []string) error {
		mu.Lock()
		defer mu.Unlock()
		for _, arg := range args {
			if strings.HasPrefix(arg, "location=") {
				locations = append(locations, arg)
			}
			if strings.HasPrefix(arg, "rtph26") {
				codecs = append(codecs, arg)
			}
		}
		return nil
	}
	cameras := []setupCamera{
		{SN: "DEMAND", Mode: "on_demand", StreamKey: "demand-key", Codec: "h264"},
		{SN: "MOTION", Mode: "on_motion", StreamKey: "motion-key", Codec: "h264"},
	}
	if err := probePlaneSources(context.Background(), c, "software", cameras, run); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if holds != 1 || releases != 1 {
		t.Fatalf("on-demand hold lifecycle: post=%d delete=%d", holds, releases)
	}
	if len(locations) != 2 || !hasArg(locations, "location=rtsp://bridge/demand-key") || !hasArg(locations, "location=rtsp://direct/video") {
		t.Fatalf("probed unexpected sources, including sleeping motion: %v", locations)
	}
	if len(codecs) != 2 || !hasArg(codecs, "rtph265depay") || !hasArg(codecs, "rtph264depay") {
		t.Fatalf("did not use configured codecs: %v", codecs)
	}
}

func TestPlaneHealthRejectsStaleConfiguredCodec(t *testing.T) {
	c := planeProbeConfig("http://bridge")
	c.Tiles = c.Tiles[:1]
	probeCalls := 0
	err := probePlaneSources(context.Background(), c, "software", []setupCamera{{SN: "DEMAND", Mode: "always", StreamKey: "key", Codec: "h264"}}, func(_ context.Context, args []string) error {
		probeCalls++
		if hasArg(args, "rtph265depay") {
			return errors.New("H.264 camera cannot feed H.265 depayloader")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "configured h265 codec") || probeCalls != 1 {
		t.Fatalf("stale codec was accepted or silently retried: err=%v calls=%d", err, probeCalls)
	}
}

func TestPlaneHealthReportsHoldReleaseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	c := planeProbeConfig(server.URL)
	c.Tiles = c.Tiles[:1]
	err := probePlaneSources(context.Background(), c, "software", []setupCamera{{SN: "DEMAND", Mode: "on_demand", StreamKey: "key"}}, func(context.Context, []string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "hold release failed") {
		t.Fatalf("release failure was silently accepted: %v", err)
	}
}

func planeProbeConfig(bridgeURL string) *config.Config {
	return &config.Config{BridgeURL: bridgeURL, RTSPBase: "rtsp://bridge", Latency: 200, Tiles: []config.Tile{
		{ID: "demand", Camera: "DEMAND", Codec: "h265"},
		{ID: "motion", Camera: "MOTION"},
		{ID: "direct", URL: "rtsp://direct/video"},
	}}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
