package gstnative

import (
	"os"
	"runtime"
	"testing"
)

func TestRunMacOSRejectsNilAndRunsLinuxCallback(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if err := RunMacOS(nil); err == nil {
			t.Fatal("nil macOS main callback accepted")
		}
		if os.Getenv("EUFY_RTSP_FAULT_WINDOW") == "1" || os.Getenv("EUFY_RTSP_WINDOW_TEST") == "1" {
			if err := RunMacOS(func() {}); err == nil {
				t.Fatal("second Cocoa main loop accepted")
			}
		}
		return
	}
	called := false
	if err := RunMacOS(func() { called = true }); err != nil || !called {
		t.Fatalf("non-macOS callback not run: %v", err)
	}
}
