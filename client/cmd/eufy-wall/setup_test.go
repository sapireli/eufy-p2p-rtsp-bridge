package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"eufy-wall/internal/config"
)

func TestSetupFromOfflineInventoryCreatesValidatedDraft(t *testing.T) {
	dir := t.TempDir()
	inventory := filepath.Join(dir, "inventory.json")
	answers := filepath.Join(dir, "answers.yaml")
	draft := filepath.Join(dir, "draft.yaml")
	if err := os.WriteFile(inventory, []byte(`{"schema_version":1,"cameras":[{"sn":"FRONT","codec":"h264","mode":"always","powered":true},{"sn":"GARAGE","codec":"h265","mode":"on_demand","powered":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	data := "bridge_url: http://bridge.local:3000\nrtsp_base: rtsp://bridge.local:8554\ninventory_file: " + inventory + "\ncameras: [FRONT, GARAGE]\ntemplate: split\n"
	if err := os.WriteFile(answers, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := setupWall(context.Background(), []string{"--answers", answers, "--output", draft}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "draft saved") {
		t.Fatalf("output=%q", out.String())
	}
	b, err := os.ReadFile(draft)
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if c.SchemaVersion != 2 || c.Layout != "custom" || len(c.Tiles) != 2 || c.Tiles[1].Codec != "h265" || c.Tiles[1].Rect.X != 16 {
		t.Fatalf("generated config=%+v", c)
	}
	if err := setupWall(context.Background(), []string{"--answers", answers, "--output", draft}, strings.NewReader(""), &out); err == nil {
		t.Fatal("setup overwrote existing draft")
	}
}

func TestSetupProbeFailureLeavesNoDraftOrActiveConfig(t *testing.T) {
	draft := filepath.Join(t.TempDir(), "draft.yaml")
	a := setupAnswers{BridgeURL: "http://bridge.local:3000", RTSPBase: "rtsp://bridge.local:8554", Cameras: []string{"A"}, Template: "one", ProbeStreams: true}
	available := []setupCamera{{SN: "A", Mode: "on_demand", Codec: "h264", StreamKey: "door"}}
	var out bytes.Buffer
	called := 0
	err := finishSetup(context.Background(), a, available, draft, &out, func(_ context.Context, _ *config.Config, sn string, _ io.Writer) error {
		called++
		if sn != "A" {
			t.Errorf("probed unexpected camera %q", sn)
		}
		return errors.New("no frames")
	})
	if err == nil || !strings.Contains(err.Error(), "no config applied") || called != 1 {
		t.Fatalf("failed probe was not enforced: called=%d err=%v", called, err)
	}
	if _, err := os.Stat(draft); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed probe created a draft: %v", err)
	}
	err = finishSetup(context.Background(), a, available, draft, &out, func(context.Context, *config.Config, string, io.Writer) error { return nil })
	if err != nil {
		t.Fatalf("healthy probe did not permit draft: %v", err)
	}
	if _, err := os.Stat(draft); err != nil {
		t.Fatalf("draft missing after healthy probe: %v", err)
	}
}

func TestSetupAdversarialInventoryAndSelections(t *testing.T) {
	available := []setupCamera{{SN: "A"}, {SN: "B"}}
	base := setupAnswers{BridgeURL: "http://bridge:3000", RTSPBase: "rtsp://bridge:8554", Cameras: []string{"A", "B"}, Template: "split"}
	for _, tc := range []struct {
		name string
		edit func(*setupAnswers)
	}{
		{"unknown camera", func(a *setupAnswers) { a.Cameras[1] = "X" }},
		{"duplicate camera", func(a *setupAnswers) { a.Cameras[1] = "A" }},
		{"wrong template count", func(a *setupAnswers) { a.Template = "one" }},
		{"unknown template", func(a *setupAnswers) { a.Template = "grid-of-99" }},
		{"bad bridge origin", func(a *setupAnswers) { a.BridgeURL = "file:///tmp/config" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			a.Cameras = append([]string(nil), base.Cameras...)
			tc.edit(&a)
			if _, err := setupYAML(a, available); err == nil {
				t.Fatal("invalid setup was accepted")
			}
		})
	}
	if _, err := setupYAML(setupAnswers{BridgeURL: base.BridgeURL, RTSPBase: base.RTSPBase}, available); err == nil {
		t.Fatal("empty selection accepted")
	}
}

func TestSetupFetchIsReadOnlyBoundedAndRejectsBadInventory(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/cameras" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]setupCamera{{SN: "A", Name: "Door", Codec: "h264"}})
	}))
	defer srv.Close()
	cameras, err := fetchSetupCameras(context.Background(), srv.URL, "")
	if err != nil || calls != 1 || len(cameras) != 1 {
		t.Fatalf("cameras=%v calls=%d err=%v", cameras, calls, err)
	}
	for _, body := range []string{`{"schema_version":99,"cameras":[{"sn":"A"}]}`, `{"schema_version":1,"cameras":[{"sn":"A"},{"sn":"A"}]}`, `{"schema_version":1,"cameras":[]}`} {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := fetchSetupCameras(context.Background(), srv.URL, path); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestSetupRejectsUnknownAnswersAndTruncatedPrompts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.yaml")
	if err := os.WriteFile(path, []byte("bridge_url: http://bridge\nsecret_token: must-reject\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := setupWall(context.Background(), []string{"--answers", path}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("unknown answer field accepted")
	}
	if err := setupWall(context.Background(), nil, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("truncated interactive setup accepted")
	}
	if err := setupWall(context.Background(), []string{"--answers"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("missing answer file accepted")
	}
}

func TestInteractiveSetupCanSaveDraftAndRejectApply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]setupCamera{{SN: "A", Name: "Door", Codec: "h264", Mode: "always", Powered: true}})
	}))
	defer srv.Close()
	draft := filepath.Join(t.TempDir(), "wall.draft.yaml")
	answers := srv.URL + "\n\n\nA\n\n\n"
	if runtime.GOOS == "darwin" {
		answers = srv.URL + "\n\nA\n\n\n"
	}
	var out bytes.Buffer
	if err := setupWall(context.Background(), []string{"--output", draft}, strings.NewReader(answers), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Door") || !strings.Contains(out.String(), "Template: one") {
		t.Fatalf("interactive summary=%q", out.String())
	}
	if _, err := config.Load(draft); err != nil {
		t.Fatalf("saved draft invalid: %v", err)
	}
	out.Reset()
	if err := setupWall(context.Background(), nil, strings.NewReader(answers+"no\n"), &out); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected safe cancellation, got %v", err)
	}
}
