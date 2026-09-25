package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
)

func TestClientMigrationPreviewsThenWritesOnlyNewCandidate(t *testing.T) {
	legacy := "rtsp_base: rtsp://bridge:8554\nlayout: 1\ntiles:\n  - url: rtsp://user:secret@camera/live\n"
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.yaml")
	output := filepath.Join(dir, "candidate.yaml")
	if err := os.WriteFile(source, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	target, _ := targetForPlatform("linux", "", "")
	var preview bytes.Buffer
	if err := migrateClientCommand([]string{source}, strings.NewReader(""), &preview, target); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.String(), "layout: 1 -> custom") || strings.Contains(preview.String(), "secret") {
		t.Fatalf("preview omitted diff or printed sensitive candidate: %s", preview.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("preview wrote a candidate: %v", err)
	}
	var result bytes.Buffer
	if err := migrateClientCommand([]string{"-", "--output", output, "--json"}, strings.NewReader(legacy), &result, target); err != nil {
		t.Fatal(err)
	}
	var report clientMigrationReport
	if err := json.Unmarshal(result.Bytes(), &report); err != nil || !report.OK || report.CandidateFile != output || report.CandidateSchema != 2 {
		t.Fatalf("migration report %q, %v", result.String(), err)
	}
	if strings.Contains(result.String(), "secret") {
		t.Fatal("JSON report exposed an inline RTSP password")
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(b)
	if err != nil || c.SchemaVersion != 2 || c.Tiles[0].URL != "rtsp://user:secret@camera/live" {
		t.Fatalf("candidate lost source behavior: %+v, %v", c, err)
	}
	info, _ := os.Stat(output)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("candidate mode %o", info.Mode().Perm())
	}
	if err := migrateClientCommand([]string{source, "--output", output}, strings.NewReader(""), &bytes.Buffer{}, target); err == nil {
		t.Fatal("migration overwrote an existing candidate")
	}
	unchanged, _ := os.ReadFile(source)
	if string(unchanged) != legacy {
		t.Fatal("migration changed the legacy source")
	}
}

func TestMigrationRejectsCredentialBearingDerivedBridgeWithoutPrintingSecret(t *testing.T) {
	legacy := "rtsp_base: rtsp://user:secret@bridge:8554\nlayout: 1\ntiles: [{camera: A}]\n"
	target, _ := targetForPlatform("linux", "", "")
	for _, args := range [][]string{{"-"}, {"-", "--json"}} {
		var out bytes.Buffer
		err := migrateClientCommand(args, strings.NewReader(legacy), &out, target)
		if err == nil || strings.Contains(out.String(), "secret") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("credential-bearing derived bridge was accepted or leaked: args=%v err=%v output=%q", args, err, out.String())
		}
		if len(args) == 2 && !errors.Is(err, errClientJSONReported) {
			t.Fatalf("JSON diagnostic will be followed by a plain stderr error: %v", err)
		}
	}
	// An explicit credential-free control origin permits migration while preserving the RTSP source
	// credentials privately in the candidate file.
	legacy = "bridge_url: http://bridge:3000\n" + legacy
	output := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := migrateClientCommand([]string{"-", "--output", output}, strings.NewReader(legacy), &bytes.Buffer{}, target); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(b)
	if err != nil || c.BridgeURL != "http://bridge:3000" || c.RTSPBase != "rtsp://user:secret@bridge:8554" {
		t.Fatalf("private candidate changed source URLs: %+v, %v", c, err)
	}
}

func TestClientMigrationRejectsLossAndActivePaths(t *testing.T) {
	target, _ := targetForPlatform("linux", "", "east")
	legacy := "rtsp_base: rtsp://bridge:8554\nlayout: 1\noutput: HDMI-A-2\ntiles: [{camera: A}]\n"
	for _, active := range []string{target.ConfigPath, "/etc/eufy-wall.yaml", "/etc/eufy-wall-west.yaml"} {
		if err := migrateClientCommand([]string{"-", "--output", active}, strings.NewReader(legacy), &bytes.Buffer{}, target); err == nil {
			t.Errorf("migration accepted active config output %q", active)
		}
	}
	if err := migrateClientCommand([]string{"-"}, strings.NewReader(strings.Replace(legacy, "output: HDMI-A-2\n", "", 1)), &bytes.Buffer{}, target); err == nil || !strings.Contains(err.Error(), "explicit output") {
		t.Fatalf("named migration gave unusable candidate: %v", err)
	}
	var report bytes.Buffer
	bad := legacy + "unrecognized: lost\n"
	if err := migrateClientCommand([]string{"-", "--json"}, strings.NewReader(bad), &report, target); err == nil {
		t.Fatal("unknown legacy setting was discarded")
	}
	if !strings.Contains(report.String(), "CONFIG_UNSUPPORTED_KEY") || strings.Contains(report.String(), "candidateYaml") {
		t.Fatalf("lossy migration report %q", report.String())
	}
}
