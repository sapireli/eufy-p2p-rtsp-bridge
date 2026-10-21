package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"eufy-wall/internal/pipeline"
)

func TestPrivateTileControlsAreAtomicAndUnchangedInputsStayUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tile.ini")
	plans := []pipeline.Plan{{TileConfigs: []pipeline.TileConfig{{Path: path, Contents: []byte("[tile]\npipeline=one\n")}}}}
	if err := writeTileConfigs(plans); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode().Perm() != 0600 {
		t.Fatal("camera URLs are not private")
	}
	stamp := time.Unix(100, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := writeTileConfigs(plans); err != nil {
		t.Fatal(err)
	}
	same, _ := os.Stat(path)
	if !same.ModTime().Equal(stamp) {
		t.Fatal("unchanged control would reset decoder fallback")
	}
	plans[0].TileConfigs[0].Contents = []byte("[tile]\npipeline=two\n")
	if err := writeTileConfigs(plans); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if os.SameFile(first, after) {
		t.Fatal("control was modified in place")
	}
	bytes, _ := os.ReadFile(path)
	if string(bytes) != "[tile]\npipeline=two\n" {
		t.Fatal(string(bytes))
	}
}
