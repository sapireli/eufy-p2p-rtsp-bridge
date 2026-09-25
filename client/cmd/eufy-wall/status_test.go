package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientStatusReportsActiveHashAndRollback(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest, []byte(validWallYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeClientStatus(dest, clientApplyStatus{SHA256: clientSHA256([]byte(validWallYAML)), AppliedAt: time.Now().UTC(), LastRollback: "decoder stalled"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := showClientStatus(dest, true, &out, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Active    bool              `json:"active"`
		Exists    bool              `json:"exists"`
		SHA256    string            `json:"sha256"`
		LastApply clientApplyStatus `json:"lastApply"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Active || !report.Exists || report.SHA256 != clientSHA256([]byte(validWallYAML)) || report.LastApply.LastRollback != "decoder stalled" {
		t.Fatalf("status=%+v", report)
	}
	out.Reset()
	if err := showClientStatus(dest, false, &out, func() error { return errors.New("inactive") }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "service active: false") || !strings.Contains(out.String(), "decoder stalled") {
		t.Fatalf("text status=%q", out.String())
	}
}

func TestClientStatusRejectsCorruptJournal(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest+".apply-status.json", []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := showClientStatus(dest, true, &bytes.Buffer{}, func() error { return nil }); err == nil {
		t.Fatal("corrupt status was hidden")
	}
}
