package supervisor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// The manager runs a SET of pipelines. What the rest of the wall depends on is the reconcile: repointing
// one tile must touch one process, and a tile already showing the right thing must not be restarted —
// a needless restart is a visible glitch on a wall.
func fastRestart() config.Restart {
	return config.Restart{MinSeconds: 1, MaxSeconds: 1, StableSeconds: 1}
}

type recorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *recorder) log(name, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, name+": "+line)
}

func (r *recorder) countWith(sub string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, l := range r.lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// `true` exits 0 immediately; the manager then restarts it on its backoff. That is enough to observe
// which plans are running without needing gst-launch.
func plan(name string, args ...string) pipeline.Plan {
	return pipeline.Plan{Name: name, Args: args}
}

func waitFor(t *testing.T, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}

func TestManagerRunsEveryPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &recorder{}
	m := NewManager("true", fastRestart(), r.log)
	m.Update(ctx, []pipeline.Plan{plan("garage"), plan("frontdoor")})
	waitFor(t, func() bool { return len(m.Names()) == 2 }, "both plans running")
	waitFor(t, func() bool { return r.countWith("garage: started") > 0 && r.countWith("frontdoor: started") > 0 }, "both started")
}

func TestUpdateLeavesUnchangedPlansAlone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &recorder{}
	m := NewManager("true", fastRestart(), r.log)
	m.Update(ctx, []pipeline.Plan{plan("garage", "--a"), plan("frontdoor", "--a")})
	waitFor(t, func() bool { return len(m.Names()) == 2 }, "both running")

	// Repoint ONE tile. The other must not be stopped.
	m.Update(ctx, []pipeline.Plan{plan("garage", "--a"), plan("frontdoor", "--b")})
	waitFor(t, func() bool { return r.countWith("frontdoor: stopping") == 1 }, "the changed tile restarts")
	if n := r.countWith("garage: stopping"); n != 0 {
		t.Errorf("garage was unchanged and should not have been stopped (got %d stops)", n)
	}
}

func TestUpdateStopsPlansThatAreGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &recorder{}
	m := NewManager("true", fastRestart(), r.log)
	m.Update(ctx, []pipeline.Plan{plan("garage"), plan("yard")})
	waitFor(t, func() bool { return len(m.Names()) == 2 }, "both running")

	m.Update(ctx, []pipeline.Plan{plan("garage")})
	waitFor(t, func() bool { return len(m.Names()) == 1 }, "the dropped tile is stopped")
	if names := m.Names(); names[0] != "garage" {
		t.Errorf("wrong plan survived: %v", names)
	}
}

func TestCancellingStopsEverything(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &recorder{}
	m := NewManager("true", fastRestart(), r.log)
	done := make(chan struct{})
	go func() {
		_ = m.Run(ctx, []pipeline.Plan{plan("garage"), plan("yard")})
		close(done)
	}()
	waitFor(t, func() bool { return len(m.Names()) == 2 }, "both running")
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if n := len(m.Names()); n != 0 {
		t.Errorf("%d plans still running after cancel", n)
	}
}

func TestUpdateAfterCancelIsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewManager("true", fastRestart(), func(string, string) {})
	m.Update(ctx, []pipeline.Plan{plan("garage")})
	if n := len(m.Names()); n != 0 {
		t.Errorf("a cancelled wall should start nothing, got %d", n)
	}
}
