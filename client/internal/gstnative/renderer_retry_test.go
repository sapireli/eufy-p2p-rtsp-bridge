package gstnative

import (
	"sync/atomic"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func TestRendererRepeatedSourceFailureKeepsBlackAndPeerThenRecovers(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	tiles[0].URL = "test://finite"
	opts := syntheticOptions(t)
	opts.monitorEvery = 100 * time.Millisecond
	opts.stallAfter = 1500 * time.Millisecond
	opts.startupAfter = 3 * time.Second
	opts.retryAfter = 150 * time.Millisecond
	var attempts atomic.Int32
	var restored atomic.Bool
	opts.source = func(tile layout.Placed) (string, error) {
		if tile.ID == "left" {
			attempts.Add(1)
			if !restored.Load() {
				return "videotestsrc is-live=true num-buffers=1 pattern=ball ! videoconvert", nil
			}
			return "videotestsrc is-live=true pattern=snow ! videoconvert", nil
		}
		return "videotestsrc is-live=true pattern=ball ! videoconvert", nil
	}
	r, err := New(&config.Config{}, tiles,
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	peerBefore := waitFrames(t, r, "right", 0)
	outputBefore := r.Status().OutputFrames
	deadline := time.Now().Add(10 * time.Second)
	for attempts.Load() < 3 && time.Now().Before(deadline) {
		wall := r.Status()
		status := wall.Tiles["left"]
		if wall.OutputLastFrameAt == nil || time.Since(*wall.OutputLastFrameAt) > 3*time.Second {
			t.Fatalf("black fallback stopped compositor output: %+v", wall)
		}
		if status.State == "retrying" && (status.SourceKind != "black" || status.DecodedFrames != 0) {
			t.Fatalf("retrying tile showed stale source progress: %+v", status)
		}
		if status.State == "retrying" || status.State == "starting" {
			r.mu.Lock()
			black := !r.slots["left"].showLive.Load()
			r.mu.Unlock()
			if !black {
				t.Fatalf("failed source was not isolated behind black tile: %+v", status)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if attempts.Load() < 3 {
		t.Fatalf("source did not retry: attempts=%d", attempts.Load())
	}
	waitOutput(t, r, outputBefore)
	if status := r.Status(); status.Tiles["right"].DecodedFrames <= peerBefore ||
		status.Tiles["right"].Generation != 1 {
		t.Fatalf("failure stopped compositor or peer: %+v", status)
	}
	restored.Store(true)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status := r.Status()
		if status.Tiles["left"].Generation >= 4 && status.Tiles["left"].State == "playing" &&
			status.Tiles["left"].DecodedFrames > 5 {
			r.mu.Lock()
			live := r.slots["left"].showLive.Load()
			r.mu.Unlock()
			if !live {
				t.Fatal("recovered source was not selected")
			}
			if status.Tiles["right"].Generation != 1 || status.Tiles["right"].DecodedFrames <= peerBefore {
				t.Fatalf("recovery disturbed peer: %+v", status)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("restored source did not recover: attempts=%d status=%+v", attempts.Load(), r.Status())
}
