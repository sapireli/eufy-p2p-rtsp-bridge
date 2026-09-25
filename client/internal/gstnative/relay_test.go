package gstnative

import (
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func TestShortSourceGapRetainsPictureThenBlack(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := []layout.Placed{{ID: "camera", URL: "test://white", W: 33, H: 31}}
	opts := syntheticOptions(t)
	opts.source = func(layout.Placed) (string, error) {
		return "videotestsrc is-live=true num-buffers=8 pattern=white ! video/x-raw,framerate=15/1", nil
	}
	opts.monitorEvery = 100 * time.Millisecond
	opts.stallAfter = 3 * time.Second
	opts.retryAfter = 10 * time.Second
	r, err := New(&config.Config{}, tiles,
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 33, Height: 31}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitTileGeneration(t, r, "camera", 1)
	time.Sleep(time.Second)
	r.mu.Lock()
	s := r.slots["camera"]
	s.feedMu.Lock()
	retained := s.lastFrame != 0 && s.needsBlack()
	s.feedMu.Unlock()
	r.mu.Unlock()
	if !retained {
		t.Fatal("short gap did not retain the last decoded picture")
	}
	before := r.Status().OutputFrames
	waitOutput(t, r, before)
	deadline := time.Now().Add(5 * time.Second)
	for r.Status().Tiles["camera"].State != "retrying" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := r.Status().Tiles["camera"]; got.State != "retrying" || got.DecodedFrames != 0 {
		t.Fatalf("stale picture claimed live progress: %+v", got)
	}
	r.mu.Lock()
	s.feedMu.Lock()
	cleared := s.lastFrame == 0 && !s.showLive.Load()
	s.feedMu.Unlock()
	r.mu.Unlock()
	if !cleared {
		t.Fatal("failed source kept last picture instead of black")
	}
	waitOutput(t, r, before)
}

func TestOneFramePerSecondSourceKeepsTileAndOutputMoving(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := []layout.Placed{{ID: "slow", URL: "test://slow", W: 32, H: 32}}
	opts := syntheticOptions(t)
	opts.monitorEvery = 100 * time.Millisecond
	opts.source = func(layout.Placed) (string, error) {
		return "videotestsrc is-live=true pattern=white ! video/x-raw,framerate=1/1", nil
	}
	r, err := New(&config.Config{}, tiles,
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 32, Height: 32}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	waitTileGeneration(t, r, "slow", 1)
	before := r.Status().OutputFrames
	time.Sleep(2 * time.Second)
	status := r.Status()
	if status.OutputFrames < before+20 || status.Tiles["slow"].State != "playing" || status.Tiles["slow"].Generation != 1 {
		t.Fatalf("slow camera did not sustain output: before=%d after=%+v", before, status)
	}
}
