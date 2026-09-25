package detect

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var gstVersionLine = regexp.MustCompile(`\bversion[ \t]+([0-9]+)\.([0-9]+)(?:\.[0-9]+)?\b`)

// CheckNativeVersion rejects GStreamer versions that lack the compositor and
// appsrc properties used by the native wall. A planes wall does not use them.
func CheckNativeVersion(sink string) error {
	if sink != "compositor" && sink != "window" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gst-inspect-1.0", "--version").Output()
	if err != nil {
		return fmt.Errorf("GStreamer version check failed: %w", err)
	}
	return checkNativeVersionOutput(string(out))
}

func checkNativeVersionOutput(out string) error {
	match := gstVersionLine.FindStringSubmatch(out)
	if len(match) != 3 {
		return fmt.Errorf("GStreamer version could not be read; run gst-inspect-1.0 --version")
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major < 1 || major == 1 && minor < 20 {
		return fmt.Errorf("GStreamer version %d.%d is unsupported for the native compositor; install 1.20 or newer", major, minor)
	}
	return nil
}
