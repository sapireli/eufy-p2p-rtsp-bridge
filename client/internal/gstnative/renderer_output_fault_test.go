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
	opts := syntheticOptions(t)
	opts.monitorEvery = 25 * time.Millisecond
	opts.outputStallAfter = 150 * time.Millisecond
	r, err := New(&config.Config{}, testTiles(),
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	before := r.Status().OutputFrames
	waitOutput(t, r, before)

	r.mu.Lock()
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
}
