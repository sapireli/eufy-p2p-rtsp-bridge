package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsCameraUsesRevisionAndPreservesDisplayConfig(t *testing.T) {
	var patch map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/cameras":
			w.Write([]byte(`[{"sn":"S1","name":"Front Door"}]`))
		case "/api/settings":
			if r.Method == http.MethodGet {
				w.Write([]byte(`{"revision":7,"settings":{"cameras":{}}}`))
				return
			}
			if r.Method != http.MethodPatch {
				t.Errorf("method %s", r.Method)
			}
			json.NewDecoder(r.Body).Decode(&patch)
			w.Write([]byte(`{"revision":8,"apply":{"state":"applied"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	cfg := filepath.Join(t.TempDir(), "display.yaml")
	os.WriteFile(cfg, []byte("unchanged"), 0600)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("test-token\n"), 0600)
	var out bytes.Buffer
	if err := runSettings([]string{"--bridge", s.URL, "--config", cfg, "--token-file", token, "camera", "front door", "power_override", `"battery"`}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if patch["revision"] != float64(7) {
		t.Fatalf("revision: %#v", patch)
	}
	cam := patch["settings"].(map[string]any)["cameras"].(map[string]any)["S1"].(map[string]any)
	if len(cam) != 1 || cam["power_override"] != "battery" {
		t.Fatalf("patch: %#v", patch)
	}
	data, _ := os.ReadFile(cfg)
	if string(data) != "unchanged" {
		t.Fatal("display config changed")
	}
}

func TestSettingsConflictNeverRetriesAndNullPreserved(t *testing.T) {
	gets, patches := 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			w.Write([]byte(`{"revision":"abc"}`))
			return
		}
		patches++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["revision"] != "abc" {
			t.Error("lost revision")
		}
		defaults := body["settings"].(map[string]any)["defaults"].(map[string]any)
		if value, ok := defaults["dual_view"]; !ok || value != nil {
			t.Error("lost null")
		}
		w.WriteHeader(409)
		w.Write([]byte(`{"error":"revision conflict"}`))
	}))
	defer s.Close()
	var out bytes.Buffer
	err := runSettings([]string{"--bridge", s.URL, "patch", `{"defaults":{"dual_view":null}}`}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "another client") || gets != 1 || patches != 1 {
		t.Fatalf("err=%v gets=%d patches=%d", err, gets, patches)
	}
}

func TestSettingsBackupRoundTripPermissionsAndNoClobber(t *testing.T) {
	backup := `{"format":"eufy-wall-settings","version":1,"settings":{"go2rtc":{"streams":{"external":"rtsp://user:private@cam/live"}}}}`
	var imported map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/settings/export":
			w.Write([]byte(backup))
		case "/api/settings":
			w.Write([]byte(`{"revision":2}`))
		case "/api/settings/import":
			json.NewDecoder(r.Body).Decode(&imported)
			w.Write([]byte(`{"revision":3,"apply":{"state":"restart-required"}}`))
		}
	}))
	defer s.Close()
	file := filepath.Join(t.TempDir(), "backup.json")
	var out bytes.Buffer
	run := func(args ...string) error {
		return runSettings(append([]string{"--bridge", s.URL}, args...), &out, &out)
	}
	if err := run("export", file); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := run("export", file); err == nil {
		t.Fatal("overwrote backup")
	}
	if err := run("import", file); err != nil {
		t.Fatal(err)
	}
	if imported["revision"] != float64(2) || imported["backup"].(map[string]any)["format"] != "eufy-wall-settings" {
		t.Fatalf("import %#v", imported)
	}
	if !strings.Contains(out.String(), "restart-required") {
		t.Fatal("apply status hidden")
	}
}

func TestSettingsCameraAmbiguousNameRejected(t *testing.T) {
	writes := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Write([]byte(`[{"sn":"A","name":"Door"},{"sn":"B","name":"Door"}]`))
	}))
	defer s.Close()
	var out bytes.Buffer
	err := runSettings([]string{"--bridge", s.URL, "camera", "door", "mode", `"always"`}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || writes != 0 {
		t.Fatalf("err %v writes %d", err, writes)
	}
}

func TestSettingsRestartIsExplicitAndUsesRevision(t *testing.T) {
	called := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/settings" {
			w.Write([]byte(`{"revision":4}`))
			return
		}
		if r.URL.Path != "/api/settings/restart" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["revision"] != float64(4) {
			t.Error(body)
		}
		called = true
		w.Write([]byte(`{"restarting":true}`))
	}))
	defer s.Close()
	var out bytes.Buffer
	if err := runSettings([]string{"--bridge", s.URL, "restart"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("restart missing")
	}
}

func TestLegacyViewSwitchAuthenticatesAndExplains401(t *testing.T) {
	token := ""
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cameras" {
			w.Write([]byte(`[{"sn":"S1","name":"Front Door","dual":true}]`))
			return
		}
		token = r.Header.Get("Authorization")
		if token != "Bearer private-token" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Write([]byte(`{"dualView":"split"}`))
	}))
	defer s.Close()
	if err := changeViewAt(s.URL, "Front Door", "split", "private-token"); err != nil {
		t.Fatal(err)
	}
	if token != "Bearer private-token" {
		t.Fatal("missing view auth")
	}
	err := changeViewAt(s.URL, "Front Door", "split")
	if err == nil || !strings.Contains(err.Error(), "-settings-token-file") || !strings.Contains(err.Error(), "EUFY_WALL_ADMIN_TOKEN") {
		t.Fatalf("unhelpful auth failure: %v", err)
	}
}

func TestLegacyViewReportsSavedButFailedApply(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cameras" {
			w.Write([]byte(`[{"sn":"S1","name":"Door","dual":true}]`))
			return
		}
		w.Write([]byte(`{"dualView":"split","apply":{"state":"failed","error":"Camera could not apply view"}}`))
	}))
	defer s.Close()
	err := changeViewAt(s.URL, "S1", "split")
	if err == nil || !strings.Contains(err.Error(), "saved but could not be applied") {
		t.Fatalf("masked failed apply: %v", err)
	}
}
