package gstnative

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// On macOS the test process uses the same Cocoa main-loop entry as the shipped binary.
// This also keeps optional window tests on the supported application thread.
func TestMain(m *testing.M) {
	if runtime.GOOS != "darwin" {
		os.Exit(m.Run())
	}
	code := 1
	if err := RunMacOS(func() { code = m.Run() }); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}
