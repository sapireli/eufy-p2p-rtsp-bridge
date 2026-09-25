package gstnative

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validStatus() Status {
	now := time.Now().UTC()
	return Status{
		SchemaVersion: 1, PID: os.Getpid(), StartedAt: now, UpdatedAt: now,
		Sink: "compositor", ConfigSHA256: HashConfig([]byte("version: 2\n")),
		Tiles: map[string]TileStatus{"front": {ExpectedLive: true, SourceKind: "live", State: "playing"}},
	}
}

func TestStatusAtomicReplaceAndContract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	want := validStatus()
	want.OutputFrames = 42
	if err := writeStatus(path, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Status
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.PID != os.Getpid() || got.ConfigSHA256 != want.ConfigSHA256 ||
		got.OutputFrames != 42 || !got.Tiles["front"].ExpectedLive {
		t.Fatalf("status contract changed: %+v", got)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("status mode: %v, %v", info, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "status.json" {
		t.Fatalf("temp status leaked: %v, %v", files, err)
	}
}

func TestStatusRejectsInvalidAndPreservesPrior(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	base := validStatus()
	cases := map[string]func(*Status){
		"schema":      func(s *Status) { s.SchemaVersion = 2 },
		"pid":         func(s *Status) { s.PID = 0 },
		"sink":        func(s *Status) { s.Sink = "" },
		"hash length": func(s *Status) { s.ConfigSHA256 = "a" },
		"hash hex":    func(s *Status) { s.ConfigSHA256 = strings.Repeat("z", 64) },
		"start":       func(s *Status) { s.StartedAt = time.Time{} },
		"update":      func(s *Status) { s.UpdatedAt = time.Time{} },
		"tiles":       func(s *Status) { s.Tiles = nil },
		"size":        func(s *Status) { s.Tiles["front"] = TileStatus{Error: strings.Repeat("x", 1<<20)} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := base
			s.Tiles = map[string]TileStatus{"front": base.Tiles["front"]}
			change(&s)
			if err := writeStatus(path, s); err == nil {
				t.Fatal("invalid status accepted")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "previous" {
				t.Fatalf("prior status changed: %q, %v", data, err)
			}
		})
	}
	if err := writeStatus("", base); err == nil {
		t.Fatal("empty path accepted")
	}
	if err := writeStatus(filepath.Join(dir, "missing", "status.json"), base); err == nil {
		t.Fatal("missing directory accepted")
	}
}
