package gstnative

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

var macOSMainStarted atomic.Bool

// RunMacOS starts Cocoa's NSApplication on the process main thread before rendering. GStreamer
// runs fn on a secondary thread and keeps the Cocoa loop alive until fn returns. This is needed
// by autovideosink's GL window on macOS; calling it twice is prohibited by GStreamer.
func RunMacOS(fn func()) error {
	if runtime.GOOS != "darwin" {
		fn()
		return nil
	}
	if fn == nil {
		return errors.New("macOS renderer main function is nil")
	}
	if !macOSMainStarted.CompareAndSwap(false, true) {
		return errors.New("macOS GStreamer main loop already started")
	}
	a, err := load()
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	callback := func(_ uintptr) int32 { fn(); return 0 }
	code := a.macosMain(purego.NewCallback(callback), 0)
	runtime.KeepAlive(callback)
	if code != 0 {
		return fmt.Errorf("macOS GStreamer main loop exited with status %d", code)
	}
	return nil
}
