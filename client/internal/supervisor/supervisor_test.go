package supervisor

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func TestRestartsInheritTheSameOpenDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("ab"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seen := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "sh", []string{"-c", "dd bs=1 count=1 <&3 2>/dev/null; echo; exit 1"},
			config.Restart{StableSeconds: 60}, func(line string) {
				if line == "a" || line == "b" {
					seen <- line
				}
			}, file)
	}()
	for _, want := range []string{"a", "b"} {
		select {
		case got := <-seen:
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal("child did not inherit descriptor on restart")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

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

func TestStartFailureKeepsLoopingUntilCancel(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	log := func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "eufy-wall-does-not-exist-xyz", nil, config.Restart{MinSeconds: 0, MaxSeconds: 0, StableSeconds: 60}, log)
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
	failures, restarts := 0, 0
	for _, l := range lines {
		if strings.Contains(l, "start failed") {
			failures++
		}
		if strings.Contains(l, "restarting in") {
			restarts++
		}
	}
	if failures < 2 || restarts < 2 {
		t.Fatalf("expected repeated start failures and restarts, got failures=%d restarts=%d lines=%v", failures, restarts, lines)
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
