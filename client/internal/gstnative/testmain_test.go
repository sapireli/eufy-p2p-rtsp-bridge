package gstnative

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
)

// Headless native tests do not need Cocoa. The optional window trial opts in to
// the same Cocoa main-loop entry as the shipped binary.
func TestMain(m *testing.M) {
	if runtime.GOOS == "darwin" {
		// A later LockOSThread can pin main to the wrong thread. Exercise a
		// reschedule and check the OS contract before starting Cocoa.
		runtime.Gosched()
		main, err := macMainThread()
		if err != nil || !main {
			fmt.Fprintf(os.Stderr, "macOS main goroutine left startup OS thread: %v\n", err)
			os.Exit(1)
		}
	}
	if runtime.GOOS != "darwin" || (os.Getenv("EUFY_RTSP_FAULT_WINDOW") != "1" && os.Getenv("EUFY_RTSP_WINDOW_TEST") != "1") {
		os.Exit(m.Run())
	}
	code := 1
	if err := RunMacOS(func() { code = m.Run() }); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func macMainThread() (bool, error) {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW)
	if err != nil {
		return false, err
	}
	symbol, err := purego.Dlsym(lib, "pthread_main_np")
	if err != nil {
		return false, err
	}
	var onMain func() int32
	purego.RegisterFunc(&onMain, symbol)
	return onMain() == 1, nil
}
