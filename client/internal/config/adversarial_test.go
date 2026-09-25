package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestV2MalformedInputs(t *testing.T) {
	valid := "schema_version: 2\nbridge_url: http://bridge:3000\nrtsp_base: rtsp://bridge:8554\nlayout: custom\ncanvas: {cols: 32, rows: 32}\ntiles: [{id: a, camera: A, rect: {x: 0, y: 0, w: 1, h: 1}}]\n"
	for name, data := range map[string]string{
		"future version":     strings.Replace(valid, "schema_version: 2", "schema_version: 99", 1),
		"missing bridge":     strings.Replace(valid, "bridge_url: http://bridge:3000\n", "", 1),
		"missing rtsp":       strings.Replace(valid, "rtsp_base: rtsp://bridge:8554\n", "", 1),
		"invalid origin":     strings.Replace(valid, "http://bridge:3000", "https://user:secret@bridge:3000/config", 1),
		"oversize canvas":    strings.Replace(valid, "cols: 32", "cols: 33", 1),
		"unknown nested key": strings.Replace(valid, "w: 1, h: 1", "w: 1, h: 1, z: 4", 1),
		"multiple documents": valid + "---\nlayout: 1\n",
		"duplicate YAML key": valid + "layout: 1\n",
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: malformed config accepted", name)
		}
	}
}

func TestV2HalfOpenTouchingEdgesAndGaps(t *testing.T) {
	y := `schema_version: 2
bridge_url: http://bridge:3000
rtsp_base: rtsp://bridge:8554
layout: custom
canvas: {cols: 32, rows: 32}
tiles:
  - {id: a, camera: A, rect: {x: 0, y: 0, w: 10, h: 10}}
  - {id: b, camera: B, rect: {x: 10, y: 0, w: 10, h: 10}}
  - {id: c, camera: C, rect: {x: 30, y: 30, w: 2, h: 2}}
`
	if _, err := Parse([]byte(y)); err != nil {
		t.Fatalf("touching edges and gaps are valid: %v", err)
	}
}

func TestV2TileDefinitionLimitAndLegacyCapacity(t *testing.T) {
	header := "schema_version: 2\nbridge_url: http://bridge:3000\nrtsp_base: rtsp://bridge:8554\nlayout: custom\ncanvas: {cols: 32, rows: 32}\ntiles:\n"
	for i := 0; i < 33; i++ {
		header += fmt.Sprintf("  - {id: tile_%d, camera: A, rect: {x: %d, y: 0, w: 1, h: 1}}\n", i, i%32)
	}
	if _, err := Parse([]byte(header)); err == nil || !strings.Contains(err.Error(), "32 tile") {
		t.Fatalf("want explicit tile limit, got %v", err)
	}
}

func TestLegacyDoesNotSilentlyIgnoreSecondYAMLDocument(t *testing.T) {
	y := "rtsp_base: rtsp://x\nlayout: 1\ntiles: [{camera: A}]\n---\nlayout: 2x2\n"
	if _, err := Parse([]byte(y)); err == nil {
		t.Fatal("second YAML document was ignored")
	}
}
