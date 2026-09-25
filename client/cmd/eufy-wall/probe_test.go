package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

func TestAutoFrameProbeUsesHardwareForAvailableCodec(t *testing.T) {
	c := &config.Config{RTSPBase: "rtsp://bridge:8554", Latency: 200}
	caps := pipeline.Caps{Decoder: "auto", AutoElements: map[string]string{"h264": "avdec_h264", "h265": "v4l2slh265dec"}}
	codec, err := probeClientCameraWithCaps(context.Background(), c, "YARD", []setupCamera{{SN: "YARD", Mode: "always", Codec: "h265", StreamKey: "yard"}}, caps, func(_ context.Context, args []string) error {
		if got := pipeline.String(args); !strings.Contains(got, "h265parse ! v4l2slh265dec !") || strings.Contains(got, "avdec_h265") {
			t.Errorf("probe used the wrong decoder: %s", got)
		}
		return nil
	})
	if err != nil || codec != "h265" {
		t.Fatalf("codec=%q err=%v", codec, err)
	}
}

func TestBatteryFrameProbeUsesCurrentCodecAndReleasesBoundedHold(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		if r.URL.Query().Get("seconds") != "20" || r.URL.Query().Get("owner") == "" {
			t.Errorf("hold lacked bounded owner and lifetime: %s", r.URL.String())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &config.Config{BridgeURL: srv.URL, RTSPBase: "rtsp://bridge:8554", Latency: 200}
	cam := setupCamera{SN: "BAT", Mode: "on_demand", Codec: "h265", StreamKey: "Front Door/1"}
	codec, err := probeClientCamera(context.Background(), c, "BAT", []setupCamera{cam}, "software", func(_ context.Context, args []string) error {
		command := pipeline.String(args)
		for _, want := range []string{"Front%20Door%2F1", "rtph265depay", "avdec_h265", "identity eos-after=2"} {
			if !strings.Contains(command, want) {
				t.Errorf("frame probe lacks %q: %s", want, command)
			}
		}
		return nil
	})
	if err != nil || codec != "h265" {
		t.Fatalf("probe codec=%q err=%v", codec, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodDelete {
		t.Fatalf("battery hold calls = %v", methods)
	}
}

func TestFrameProbeReleasesHoldAfterDecodeFailureAndRejectsBadInventory(t *testing.T) {
	var mu sync.Mutex
	methods := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &config.Config{BridgeURL: srv.URL, RTSPBase: "rtsp://bridge:8554", Latency: 200}
	cam := setupCamera{SN: "BAT", Mode: "on_motion", StreamKey: "door"}
	_, err := probeClientCamera(context.Background(), c, "BAT", []setupCamera{cam}, "software", func(context.Context, []string) error { return errors.New("no frames") })
	if err == nil || !strings.Contains(err.Error(), "no decoded frame progress") {
		t.Fatalf("decode failure was hidden: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 2 || methods[1] != http.MethodDelete {
		t.Fatalf("failed probe retained a hold: %v", methods)
	}
	if _, err := probeClientCamera(context.Background(), c, "MISSING", []setupCamera{cam}, "software", nil); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("missing camera accepted: %v", err)
	}
	cam.StreamKey = ""
	if _, err := probeClientCamera(context.Background(), c, "BAT", []setupCamera{cam}, "software", nil); err == nil || !strings.Contains(err.Error(), "stream key") {
		t.Fatalf("camera without RTSP path accepted: %v", err)
	}
}

func TestFrameProbeRejectsFailedHoldBeforeOpeningRTSP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer srv.Close()
	c := &config.Config{BridgeURL: srv.URL, RTSPBase: "rtsp://bridge:8554", Latency: 200}
	called := false
	_, err := probeClientCamera(context.Background(), c, "BAT", []setupCamera{{SN: "BAT", Mode: "on_demand", StreamKey: "door"}}, "software", func(context.Context, []string) error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || called {
		t.Fatalf("failed hold launched a stream: called=%v err=%v", called, err)
	}
}

func TestFrameProbeRecoversFromUnknownOrStaleInventoryCodec(t *testing.T) {
	for _, reported := range []string{"", "h264"} {
		t.Run("reported="+reported, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			defer srv.Close()
			c := &config.Config{BridgeURL: srv.URL, RTSPBase: "rtsp://bridge:8554", Latency: 200}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			var tried []string
			codec, err := probeClientCamera(ctx, c, "BAT", []setupCamera{{SN: "BAT", Mode: "on_demand", Codec: reported, StreamKey: "door"}}, "software", func(attempt context.Context, args []string) error {
				command := pipeline.String(args)
				if strings.Contains(command, "rtph264depay") {
					tried = append(tried, "h264")
					<-attempt.Done() // a frozen first attempt must leave time for the other codec
					return attempt.Err()
				}
				tried = append(tried, "h265")
				if err := attempt.Err(); err != nil {
					return err
				}
				return nil
			})
			if err != nil || codec != "h265" || strings.Join(tried, ",") != "h264,h265" {
				t.Fatalf("codec=%q tried=%v err=%v", codec, tried, err)
			}
		})
	}
}

func TestProbeOutputIsBounded(t *testing.T) {
	output := &boundedProbeOutput{}
	if n, err := output.Write([]byte(strings.Repeat("x", 1<<20))); err != nil || n != 1<<20 {
		t.Fatalf("log writer n=%d err=%v", n, err)
	}
	if output.buf.Len() != 64*1024 {
		t.Fatalf("probe log grew without bound: %d", output.buf.Len())
	}
}
