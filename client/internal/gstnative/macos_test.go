package gstnative

import (
	"runtime"
	"testing"
)

func TestRunMacOSRejectsNilAndRunsLinuxCallback(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if err := RunMacOS(nil); err == nil {
			t.Fatal("nil macOS main callback accepted")
		}
		if err := RunMacOS(func() {}); err == nil {
			t.Fatal("second Cocoa main loop accepted")
		}
		return
	}
	called := false
	if err := RunMacOS(func() { called = true }); err != nil || !called {
		t.Fatalf("non-macOS callback not run: %v", err)
	}
}
