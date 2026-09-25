package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
)

func TestPreflightChecksBridgeInventoryAndRTSPBeforeApply(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	state, go2rtc, inventory := "ok", "running", `[{"sn":"A","codec":"h265"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			fmt.Fprintf(w, `{"ok":true,"auth":{"state":%q},"go2rtc":%q}`, state, go2rtc)
		case "/api/cameras":
			fmt.Fprint(w, inventory)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &config.Config{BridgeURL: srv.URL, RTSPBase: "rtsp://" + ln.Addr().String(), Tiles: []config.Tile{{Camera: "A"}}}
	cameras, err := preflightClientRemote(context.Background(), c)
	if err != nil || len(cameras) != 1 {
		t.Fatalf("healthy preflight cameras=%v err=%v", cameras, err)
	}
	if err := codecPreflight(c, cameras, "software", func(name string) bool { return name == "avdec_h265" }); err != nil {
		t.Fatal(err)
	}
	if err := codecPreflight(c, cameras, "software", func(string) bool { return false }); err == nil {
		t.Fatal("missing H.265 decoder accepted")
	}
	state = "pending"
	if _, err := preflightClientRemote(context.Background(), c); err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("pending auth accepted: %v", err)
	}
	state, go2rtc = "ok", "stopped"
	if _, err := preflightClientRemote(context.Background(), c); err == nil || !strings.Contains(err.Error(), "go2rtc") {
		t.Fatalf("stopped RTSP backend accepted: %v", err)
	}
	go2rtc, inventory = "running", `[{"sn":"OTHER"}]`
	if _, err := preflightClientRemote(context.Background(), c); err == nil || !strings.Contains(err.Error(), "A") {
		t.Fatalf("unknown camera accepted: %v", err)
	}
	inventory = `[{"sn":"A"}]`
	ln.Close()
	if _, err := preflightClientRemote(context.Background(), c); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("closed RTSP port accepted: %v", err)
	}
}

func TestPreflightRejectsUnknownMotionWatchCodec(t *testing.T) {
	c := &config.Config{Tiles: []config.Tile{{Motion: "latest", Watch: []string{"A", "B"}}}}
	cameras := []setupCamera{{SN: "A", Codec: "h264"}, {SN: "B", Codec: "h265"}}
	if err := codecPreflight(c, cameras, "software", func(name string) bool { return name == "avdec_h264" }); err == nil || !strings.Contains(err.Error(), "h265") {
		t.Fatalf("mixed motion codecs accepted: %v", err)
	}
	if err := codecPreflight(c, cameras, "software", func(name string) bool { return name == "avdec_h264" || name == "avdec_h265" }); err != nil {
		t.Fatal(err)
	}
}

func TestHostValidationUsesActualBridgeAndDecoderBeforeMutation(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gst-inspect-1.0"), []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'gst-inspect-1.0 version 1.22.0'; fi\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			fmt.Fprint(w, `{"ok":true,"auth":{"state":"ok"},"go2rtc":"running"}`)
		case "/api/cameras":
			fmt.Fprint(w, `[{"sn":"A","codec":"h264"}]`)
		}
	}))
	defer srv.Close()
	base := fmt.Sprintf("schema_version: 2\nbridge_url: %s\nrtsp_base: rtsp://%s\nscreen: {width: 640, height: 480}\nlayout: 1\ndecoder: software\nsink: window\ntiles:\n  - {id: front, camera: A}\n", srv.URL, ln.Addr())
	if err := validateClientConfig([]byte(base), true); err != nil {
		t.Fatalf("working host config rejected: %v", err)
	}
	if err := validateClientConfig([]byte(strings.Replace(base, "camera: A", "camera: MISSING", 1)), true); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("unknown host camera accepted: %v", err)
	}
	if err := validateClientConfig([]byte(strings.Replace(base, "decoder: software", "decoder: impossible", 1)), true); err == nil {
		t.Fatal("impossible decoder accepted")
	}
}
