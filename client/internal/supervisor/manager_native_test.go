package supervisor

import (
	"context"
	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdenticalUpdateKeepsCompatibleFallbackLatch(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "player")
	attempts := filepath.Join(dir, "attempts")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$2\"\nsleep 0.03\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewManager(bin, config.Restart{}, func(string, string) {})
	defer m.Stop()
	plans := []pipeline.Plan{{Name: "A", Args: []string{"native", attempts}, FallbackArgs: []string{"compatible", attempts}}}
	m.Update(ctx, plans)
	waitFor(t, func() bool { b, _ := os.ReadFile(attempts); return len(strings.Fields(string(b))) >= 3 }, "fallback retries")
	m.Update(ctx, plans)
	time.Sleep(100 * time.Millisecond)
	cancel()
	m.Stop()
	data, _ := os.ReadFile(attempts)
	lines := strings.Fields(string(data))
	count := 0
	for _, line := range lines {
		if line == "native" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unchanged update retried native: %v", lines)
	}
}
