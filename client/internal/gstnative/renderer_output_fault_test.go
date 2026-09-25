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
