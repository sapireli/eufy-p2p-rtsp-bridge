package gstnative

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// Headless native tests do not need Cocoa. The optional window trial opts in to
// the same Cocoa main-loop entry as the shipped binary.
func TestMain(m *testing.M) {
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
