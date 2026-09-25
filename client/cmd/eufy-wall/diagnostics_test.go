package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestClientJSONValidationReportsPathAndKeepsYAMLSecretsOut(t *testing.T) {
	const unknown = "schema_version: 2\nbridge_url: http://bridge:3000\nrtsp_base: rtsp://bridge:8554\nlayout: 1\ntiles: [{id: front, camera: CAM}]\ninvented: true\n"
	var out bytes.Buffer
	handled, err := runCommand([]string{"config", "validate", "-", "--json"}, strings.NewReader(unknown), &out)
	if !handled || err == nil {
		t.Fatalf("unknown key passed: handled=%v err=%v", handled, err)
	}
	var result struct {
		OK          bool               `json:"ok"`
		Diagnostics []clientDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, out.String())
	}
	if result.OK || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "CONFIG_UNSUPPORTED_KEY" || result.Diagnostics[0].Path != "invented" || result.Diagnostics[0].Remedy == "" {
		t.Fatalf("diagnostics: %+v", result)
	}
	out.Reset()
	const secret = "schema_version: 2\nbridge_url: http://bridge:3000\npassword: VERY_SECRET: broken\n"
	_, err = runCommand([]string{"config", "validate", "-", "--json"}, strings.NewReader(secret), &out)
	if err == nil || strings.Contains(out.String(), "VERY_SECRET") || !strings.Contains(out.String(), "CONFIG_YAML_INVALID") {
		t.Fatalf("YAML secret leaked or error missed: %v %q", err, out.String())
	}
	out.Reset()
	if handled, err := runCommand([]string{"config", "validate", "-", "--json"}, strings.NewReader(validWallYAML), &out); !handled || err != nil {
		t.Fatalf("valid config failed: %v", err)
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || !result.OK || len(result.Diagnostics) != 0 {
		t.Fatalf("valid JSON result: %v %+v", err, result)
	}
}

func TestClientApplyJSONFailureIsStructuredAndSafe(t *testing.T) {
	var out bytes.Buffer
	_, err := runCommand([]string{"config", "apply", "-", "--json"}, strings.NewReader("password: VERY_SECRET: bad\n"), &out)
	if err == nil || strings.Contains(out.String(), "VERY_SECRET") || !strings.Contains(out.String(), "CONFIG_YAML_INVALID") {
		t.Fatalf("apply error was not structured safely: %v %q", err, out.String())
	}
}

func TestNestedUnknownFieldDiagnosticNamesFullYAMLPath(t *testing.T) {
	const invalid = "schema_version: 2\nbridge_url: http://bridge:3000\nrtsp_base: rtsp://bridge:8554\nlayout: custom\ncanvas: {cols: 32, rows: 32}\ntiles:\n  - id: front\n    camera: CAM\n    rect: {x: 0, y: 0, w: 32, h: 32, width_typo: 1}\n"
	for _, operation := range []string{"validate", "apply"} {
		var out bytes.Buffer
		_, err := runCommand([]string{"config", operation, "-", "--json"}, strings.NewReader(invalid), &out)
		if err == nil {
			t.Fatalf("%s accepted unknown rectangle key", operation)
		}
		var report struct {
			Diagnostics []clientDiagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Diagnostics) != 1 {
			t.Fatalf("%s returned bad JSON: %v %s", operation, err, out.String())
		}
		d := report.Diagnostics[0]
		if d.Code != "CONFIG_UNSUPPORTED_KEY" || d.Path != "tiles[0].rect.width_typo" || d.Line != 9 {
			t.Fatalf("%s gave incomplete location: %+v", operation, d)
		}
	}
}

func TestMigrationPrerequisiteDiagnosticsGiveConfigAdvice(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, code, path, remedy string
	}{
		{"offline URL", "layout: 1\ntiles: [{url: rtsp://127.0.0.1:8554/live}]\n", "CONFIG_MIGRATION_NEEDS_RTSP_BASE", "rtsp_base", "legacy format"},
		{"credential-bearing bridge", "rtsp_base: rtsp://user:SECRET@127.0.0.1:8554\nlayout: 1\ntiles: [{camera: CAM}]\n", "BRIDGE_URL_INVALID", "bridge_url", "credential-free"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			_, err := runCommand([]string{"config", "migrate", "-", "--json"}, strings.NewReader(tc.yaml), &out)
			if err == nil {
				t.Fatal("invalid migration accepted")
			}
			var report struct {
				Diagnostics []clientDiagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Diagnostics) != 1 {
				t.Fatalf("bad JSON: %v %s", err, out.String())
			}
			d := report.Diagnostics[0]
			if d.Code != tc.code || d.Path != tc.path || !strings.Contains(d.Remedy, tc.remedy) || strings.Contains(out.String(), "SECRET") {
				t.Fatalf("misleading or leaking migration diagnostic: %+v", d)
			}
		})
	}
}

func TestOldGStreamerVersionHasActionableDiagnostic(t *testing.T) {
	d := diagnosticForClient("GStreamer version 1.18 is unsupported for the native compositor", "doctor")
	if d.Code != "GSTREAMER_VERSION_UNSUPPORTED" || !strings.Contains(d.Remedy, "1.20") {
		t.Fatalf("old GStreamer version diagnostic: %+v", d)
	}
}
