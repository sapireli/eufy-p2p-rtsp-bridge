package gstnative

import (
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func TestRendererRestartsSilentCompositorBeforeOpeningSources(t *testing.T) {
	a, err := load()
	if err != nil {
		t.Skip(err)
	}
	for _, tc := range []struct {
		name       string
		silentRuns int
		wantError  bool
	}{{"recovers", 1, false}, {"fails closed", 2, true}} {
		t.Run(tc.name, func(t *testing.T) {
			mock := *a
			var compositor uintptr
			var starts, sources int
			originalParse, originalState := mock.parse, mock.setState
			mock.parse = func(desc string, errorPointer uintptr) uintptr {
				p := originalParse(desc, errorPointer)
				if strings.Contains(desc, "compositor name=mix") {
					compositor = p
				}
				return p
			}
			mock.setState = func(element uintptr, state int32) int32 {
				if element == compositor && state == statePlaying {
					starts++
					if starts <= tc.silentRuns {
						return 1 // Native reports success but makes no output progress.
					}
				}
				return originalState(element, state)
			}
			opts := syntheticOptions(t)
			opts.api = &mock
			opts.startupOutputAfter = 500 * time.Millisecond
			opts.source = func(_ layout.Placed) (string, error) {
				sources++
				return "videotestsrc is-live=true pattern=ball ! videoconvert", nil
			}
			r, err := New(&config.Config{}, testTiles(),
				pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
			if tc.wantError {
				if err == nil {
					r.Close()
					t.Fatal("silent compositor was accepted")
				}
				if !strings.Contains(err.Error(), "no startup output") || starts != 2 || sources != 0 {
					t.Fatalf("wrong startup failure: starts=%d sources=%d err=%v", starts, sources, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if starts != 2 || sources != 2 || r.Status().OutputFrames == 0 {
				t.Fatalf("compositor did not recover before sources: starts=%d sources=%d status=%+v", starts, sources, r.Status())
			}
		})
	}
}
