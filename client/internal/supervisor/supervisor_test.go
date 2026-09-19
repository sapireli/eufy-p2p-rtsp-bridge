package supervisor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func TestRestartsWithBackoffAndStopsOnCancel(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	log := func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Child prints and exits 1 immediately; backoff min=max=0s via 0 → we use a tiny custom unit.
	go func() {
		done <- Run(ctx, "sh", []string{"-c", "echo hello; exit 1"}, config.Restart{MinSeconds: 0, MaxSeconds: 0, StableSeconds: 60}, log)
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	hello, restarts := 0, 0
	for _, l := range lines {
		if strings.Contains(l, "hello") {
			hello++
		}
		if strings.Contains(l, "restarting in") {
			restarts++
		}
	}
	if hello < 2 || restarts < 1 {
		t.Fatalf("expected repeated runs, got hello=%d restarts=%d lines=%v", hello, restarts, lines)
	}
}

func TestBackoffSchedule(t *testing.T) {
	r := config.Restart{MinSeconds: 1, MaxSeconds: 8, StableSeconds: 60}
	b := newBackoff(r)
	got := []time.Duration{b.next(0), b.next(0), b.next(0), b.next(0), b.next(0)}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: %v want %v", i, got[i], want[i])
		}
	}
	if d := b.next(61 * time.Second); d != 1*time.Second {
		t.Fatalf("stable run should reset: %v", d)
	}
}
