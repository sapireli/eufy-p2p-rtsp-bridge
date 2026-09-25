package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfigExplainResolvesNestedAndIndexedPaths(t *testing.T) {
	for path, want := range map[string]string{
		"canvas.cols":                   "1..32",
		"restart.max_seconds":           "min_seconds",
		"tiles[0].rect.w":               "x+w",
		"tiles[31].blank_after_seconds": "blanks",
		"tiles[].codec":                 "offline fallback",
	} {
		var out bytes.Buffer
		handled, err := runCommand([]string{"config", "explain", path}, strings.NewReader(""), &out)
		if !handled || err != nil || !strings.Contains(out.String(), want) {
			t.Errorf("explain %q: handled=%v err=%v output=%q", path, handled, err, out.String())
		}
	}
	for _, path := range []string{"tiles[].missing", "tiles[-1].rect", "tiles[0]rect", "canvas.color"} {
		if _, err := explainClientField(path); err {
			t.Errorf("unknown path %q was explained", path)
		}
	}
}

func TestConfigExplainListsCompleteFieldReference(t *testing.T) {
	var out bytes.Buffer
	if handled, err := runCommand([]string{"config", "explain"}, strings.NewReader(""), &out); !handled || err != nil {
		t.Fatalf("explain handled=%v err=%v", handled, err)
	}
	for _, path := range []string{"bridge_url:", "canvas.rows:", "tiles[].rect.h:", "restart.stable_seconds:"} {
		if !strings.Contains(out.String(), path) {
			t.Errorf("field %q missing from reference", path)
		}
	}
}
