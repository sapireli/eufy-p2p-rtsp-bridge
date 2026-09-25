package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/wallstate"
)

func TestConfigValidateAndPreviewFromStdin(t *testing.T) {
	var out bytes.Buffer
	handled, err := runCommand([]string{"config", "validate", "-"}, strings.NewReader(validWallYAML), &out)
	if !handled || err != nil || !strings.Contains(out.String(), "config valid") {
		t.Fatalf("validate: handled=%v err=%v out=%q", handled, err, out.String())
	}
	out.Reset()
	handled, err = runCommand([]string{"layout", "preview", "-"}, strings.NewReader(validWallYAML), &out)
	if !handled || err != nil || !strings.Contains(out.String(), "CAM1") || !strings.Contains(out.String(), "pixels=(0,0 1920x1080)") {
		t.Fatalf("preview: handled=%v err=%v out=%q", handled, err, out.String())
	}
}

func TestConfigValidateRejectsMalformedAndOversizedInput(t *testing.T) {
	for _, input := range []string{"tiles: []\n", "schema_version: 2\nporrt: 3000\n", strings.Repeat("x", (4<<20)+1)} {
		handled, err := runCommand([]string{"config", "validate", "-"}, strings.NewReader(input), &bytes.Buffer{})
		if !handled || err == nil {
			t.Fatalf("accepted malformed config prefix %q", input[:min(len(input), 24)])
		}
	}
}

func TestEmbeddedExampleAndCommandErrors(t *testing.T) {
	var out bytes.Buffer
	if handled, err := runCommand([]string{"config", "example"}, strings.NewReader(""), &out); !handled || err != nil {
		t.Fatalf("example: handled=%v err=%v", handled, err)
	}
	if _, err := config.Parse(out.Bytes()); err != nil {
		t.Fatalf("embedded example is invalid: %v", err)
	}
	if strings.Contains(strings.ToLower(out.String()), "password:") {
		t.Fatal("example embeds password")
	}
	for _, args := range [][]string{{"config"}, {"config", "unknown"}, {"layout"}, {"doctor", "--bad"}, {"unknown"}} {
		if handled, err := runCommand(args, strings.NewReader(""), &out); !handled || err == nil {
			t.Fatalf("command %v: handled=%v err=%v", args, handled, err)
		}
	}
	if handled, err := runCommand([]string{"-dry-run"}, strings.NewReader(""), &out); handled || err != nil {
		t.Fatalf("legacy flag not preserved: handled=%v err=%v", handled, err)
	}
}

func TestPlansForRetainsURLOnlyTileAndSkipsSleepingCamera(t *testing.T) {
	c, err := config.Parse([]byte("rtsp_base: rtsp://bridge:8554\nlayout: 2x1\ntiles:\n  - url: rtsp://other/live\n  - camera: CAM2\n"))
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := layout.Place(c, config.Screen{Width: 1920, Height: 1080})
	if err != nil {
		t.Fatal(err)
	}
	plans := plansFor(c, pipeline.Caps{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 1920, Height: 1080}}, tiles,
		map[int]string{0: "", 1: "CAM2"}, map[int]string{0: wallstate.ContentLive, 1: wallstate.ContentNone}, nil, nil)
	if len(plans) != 1 {
		t.Fatalf("plans=%v", plans)
	}
	args := strings.Join(plans[0].Args, " ")
	if !strings.Contains(args, "rtsp://other/live") || strings.Contains(args, "CAM2") {
		t.Fatalf("wrong sources in %s", args)
	}
}

func TestPlansForUsesBridgeCodecForMotionCamera(t *testing.T) {
	c, err := config.Parse([]byte("rtsp_base: rtsp://bridge:8554\nbridge_url: https://bridge:7443\nschema_version: 2\nlayout: 1\ntiles:\n  - id: motion\n    motion: latest\n"))
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := layout.Place(c, config.Screen{Width: 1920, Height: 1080})
	if err != nil {
		t.Fatal(err)
	}
	plans := plansFor(c, pipeline.Caps{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 1920, Height: 1080}}, tiles,
		map[int]string{0: "CAM2"}, map[int]string{0: wallstate.ContentLive}, func(string) string { return "garage" }, func(string) string { return "h265" })
	if len(plans) != 1 {
		t.Fatalf("plans=%v", plans)
	}
	args := strings.Join(plans[0].Args, " ")
	for _, want := range []string{"rtsp://bridge:8554/garage", "rtph265depay", "avdec_h265", "watchdog"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %s", want, args)
		}
	}
	if got := controlBase(c); got != "https://bridge:7443" {
		t.Fatalf("control base = %q", got)
	}
}

func TestHoldRequestUsesInstanceOwnerAndExplicitBridgePort(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, fmt.Sprintf("%s %s %s", r.Method, r.URL.Path, r.URL.Query().Get("owner")))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	holdRequest(context.Background(), ts.URL, http.MethodPost, "CAM1")
	holdRequest(context.Background(), ts.URL, http.MethodDelete, "CAM1")
	if len(paths) != 2 || paths[0] != "POST /hold/CAM1 "+wallHoldOwner || paths[1] != "DELETE /hold/CAM1 "+wallHoldOwner {
		t.Fatalf("requests=%v", paths)
	}
	if wallHoldOwner == "wall" {
		t.Fatal("hold owner would collide across wall instances")
	}
}

func TestDoctorReportsStructuredProblemsAndCommandHelp(t *testing.T) {
	var out bytes.Buffer
	handled, _ := runCommand([]string{"doctor", "--json"}, strings.NewReader(""), &out)
	if !handled {
		t.Fatal("doctor command was not handled")
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid doctor JSON: %v", err)
	}
	if report.Problems == nil {
		t.Fatal("doctor must include a problems array")
	}
	out.Reset()
	if handled, err := runCommand([]string{"help"}, strings.NewReader(""), &out); !handled || err != nil || !strings.Contains(out.String(), "layout edit") {
		t.Fatalf("help: %v %v %q", handled, err, out.String())
	}
}

func TestLayoutPreviewCommandFlagsAndEditDispatch(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{
		{"layout", "preview", "-", "--width", "400"},
		{"layout", "preview", "-", "--png"},
		{"layout", "preview", "-", "--height", "abc"},
		{"layout", "preview", "-", "--bogus"},
		{"layout", "edit", "one", "two"},
	} {
		if handled, err := runCommand(args, strings.NewReader(validWallYAML), &out); !handled || err == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
	out.Reset()
	pngPath := t.TempDir() + "/wall.png"
	if handled, err := runCommand([]string{"layout", "preview", "-", "--png", pngPath, "--width", "37", "--height", "29"}, strings.NewReader(validWallYAML), &out); !handled || err != nil {
		t.Fatalf("PNG command handled=%v err=%v", handled, err)
	}
	if !strings.Contains(out.String(), "screen 37x29") {
		t.Fatalf("preview dimensions ignored: %q", out.String())
	}
	out.Reset()
	if handled, err := runCommand([]string{"layout", "edit"}, strings.NewReader("quit\n"), &out); !handled || err != nil {
		t.Fatalf("editor command handled=%v err=%v", handled, err)
	}
}
