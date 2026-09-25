package gstnative

import (
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

func TestStoppedCompositorReportsOutputFreeze(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	for _, tc := range []struct {
		name       string
		priorFrame bool
	}{{"startup", false}, {"after-progress", true}} {
		t.Run(tc.name, func(t *testing.T) {
			opts := syntheticOptions(t)
			opts.monitorEvery = 25 * time.Millisecond
			r, err := New(&config.Config{}, testTiles(),
				pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			// Output progress itself is covered by renderer tests. This isolates
			// the watchdog from host startup timing and covers both failure paths.
			if tc.priorFrame {
				r.output.tick()
			}
			r.mu.Lock()
			r.options.outputStallAfter = 150 * time.Millisecond
			if r.api.setState(r.pipeline, stateNull) == 0 {
				r.mu.Unlock()
				t.Fatal("could not stop compositor pipeline")
			}
			r.mu.Unlock()
			select {
			case err := <-r.Errors():
				if err == nil || !strings.Contains(err.Error(), "output has produced no frames") {
					t.Fatalf("wrong compositor failure: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stopped compositor was not reported")
			}
		})
	}
}

func TestStoppedCompositorRecreatesWithLiveTiles(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	opts := syntheticOptions(t)
	opts.monitorEvery = 25 * time.Millisecond
	opts.outputStallAfter = 250 * time.Millisecond
	start := func() *Renderer {
		r, err := New(&config.Config{}, testTiles(),
			pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close() })
		waitFrames(t, r, "left", 0)
		waitFrames(t, r, "right", 0)
		waitOutput(t, r, 0)
		return r
	}

	first := start()
	before := first.Status()
	faultAt := time.Now()
	first.mu.Lock()
	state := first.api.setState(first.pipeline, stateNull)
	first.mu.Unlock()
	if state == 0 {
		t.Fatal("could not stop compositor pipeline")
	}
	select {
	case err := <-first.Errors():
		if err == nil || !strings.Contains(err.Error(), "output has produced no frames") {
			t.Fatalf("wrong compositor failure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stopped compositor was not reported")
	}
	detectedAfter := time.Since(faultAt)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	restartAt := time.Now()
	second := start()
	restartedAfter := time.Since(restartAt)
	got := second.Status()
	if got.Tiles["left"].Generation != 1 || got.Tiles["right"].Generation != 1 {
		t.Fatalf("replacement did not start fresh live tiles: %+v", got)
	}
	t.Logf("before fault: output=%d left=%d right=%d; detected in %s; recreated in %s; after restart: output=%d left=%d right=%d",
		before.OutputFrames, before.Tiles["left"].DecodedFrames, before.Tiles["right"].DecodedFrames,
		detectedAfter, restartedAfter, got.OutputFrames,
		got.Tiles["left"].DecodedFrames, got.Tiles["right"].DecodedFrames)
}
