package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func healthyRuntimePair(now time.Time) (runtimeWallStatus, runtimeWallStatus) {
	before := runtimeWallStatus{
		SchemaVersion: 1, PID: 1234, StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-2 * time.Second),
		Sink: "compositor", ConfigSHA256: "candidate", OutputFrames: 100, OutputLastFrameAt: now.Add(-2 * time.Second),
		Tiles: map[string]runtimeTileStatus{"live": {ExpectedLive: true, DecodedFrames: 10, LastDecodedFrameAt: now.Add(-2 * time.Second), Generation: 3, State: "playing"}},
	}
	after := before
	after.Tiles = map[string]runtimeTileStatus{"live": {ExpectedLive: true, DecodedFrames: 12, LastDecodedFrameAt: now.Add(-time.Second), Generation: 3, State: "playing"}}
	after.UpdatedAt = now.Add(-time.Second)
	after.OutputFrames = 120
	after.OutputLastFrameAt = now.Add(-time.Second)
	return before, after
}

func TestRuntimeProgressRejectsStaleAndFrozenStatus(t *testing.T) {
	now := time.Now()
	before, after := healthyRuntimePair(now)
	if err := runtimeProgress(before, after, 1234, "candidate", "compositor", []string{"live"}, now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*runtimeWallStatus){
		"wrong process": func(s *runtimeWallStatus) { s.PID = 9999 },
		"old config":    func(s *runtimeWallStatus) { s.ConfigSHA256 = "old" },
		"stale writer":  func(s *runtimeWallStatus) { s.UpdatedAt = now.Add(-time.Minute) },
		"frozen output": func(s *runtimeWallStatus) { s.OutputFrames = before.OutputFrames },
		"frozen tile": func(s *runtimeWallStatus) {
			tile := s.Tiles["live"]
			tile.DecodedFrames = before.Tiles["live"].DecodedFrames
			s.Tiles["live"] = tile
		},
		"missing tile": func(s *runtimeWallStatus) { delete(s.Tiles, "live") },
		"tile error": func(s *runtimeWallStatus) {
			tile := s.Tiles["live"]
			tile.State = "error"
			tile.Error = "decoder died"
			s.Tiles["live"] = tile
		},
		"retrying old source": func(s *runtimeWallStatus) {
			tile := s.Tiles["live"]
			tile.State = "retrying"
			s.Tiles["live"] = tile
		},
		"unexpected live state": func(s *runtimeWallStatus) {
			tile := s.Tiles["live"]
			tile.State = "idle"
			s.Tiles["live"] = tile
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := after
			bad.Tiles = map[string]runtimeTileStatus{"live": after.Tiles["live"]}
			change(&bad)
			if err := runtimeProgress(before, bad, 1234, "candidate", "compositor", []string{"live"}, now); err == nil {
				t.Fatal("bad runtime status passed apply health")
			}
		})
	}
}

func TestRuntimeProgressAcceptsIdleTilesOnlyWithLiveOutput(t *testing.T) {
	now := time.Now()
	before, after := healthyRuntimePair(now)
	before.Tiles = map[string]runtimeTileStatus{"idle": {State: "idle"}}
	after.Tiles = before.Tiles
	if err := runtimeProgress(before, after, 1234, "candidate", "compositor", []string{"idle"}, now); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeProgressRejectsFailedSnapshotTile(t *testing.T) {
	now := time.Now()
	before, after := healthyRuntimePair(now)
	before.Tiles = map[string]runtimeTileStatus{"still": {State: "retrying"}}
	after.Tiles = before.Tiles
	if err := runtimeProgress(before, after, 1234, "candidate", "compositor", []string{"still"}, now); err == nil {
		t.Fatal("failed still source passed apply health")
	}
}

func TestAwaitRuntimeProgressRequiresTwoMatchingFreshSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	now := time.Now()
	before, after := healthyRuntimePair(now)
	write := func(s runtimeWallStatus) {
		t.Helper()
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(before)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(25 * time.Millisecond)
		b, _ := json.Marshal(after)
		_ = os.WriteFile(path+".new", b, 0600)
		_ = os.Rename(path+".new", path)
	}()
	if err := awaitRuntimeProgress(ctx, path, 1234, "candidate", "compositor", []string{"live"}, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"pid":1234}`), 0600); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer stop()
	if err := awaitRuntimeProgress(short, path, 1234, "candidate", "compositor", []string{"live"}, 5*time.Millisecond); err == nil || !strings.Contains(err.Error(), "no verified") {
		t.Fatalf("stale or incomplete status accepted: %v", err)
	}
}
