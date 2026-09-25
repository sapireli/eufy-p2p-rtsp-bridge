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
