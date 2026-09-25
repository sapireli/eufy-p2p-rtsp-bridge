package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNamedRestartCountIsBoundedAndQueriesItsOwnUnit(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n[ \"$4\" = 'eufy-wall@right' ] || exit 7\necho 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if count, err := wallRestartCountWithTimeout("eufy-wall@right", time.Second); err != nil || count != 3 {
		t.Fatalf("named unit restart count=%d err=%v", count, err)
	}
	if _, err := wallRestartCountWithTimeout("eufy-wall", time.Second); err == nil {
		t.Fatal("queried default service for a named instance")
	}
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := wallRestartCountWithTimeout("eufy-wall@right", 80*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("hung systemctl was not bounded: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("hung systemctl took %s despite 80ms deadline", elapsed)
	}
}
