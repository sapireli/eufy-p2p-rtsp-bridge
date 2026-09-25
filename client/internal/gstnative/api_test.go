package gstnative

import (
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestNativeGStreamerBindingsAndParseErrors(t *testing.T) {
	a, err := load()
	if err != nil {
		t.Skipf("GStreamer runtime unavailable: %v", err)
	}
	pipeline, err := a.parsed("videotestsrc num-buffers=1 ! fakesink", false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.objectUnref(pipeline)
	if a.setState(pipeline, statePlaying) == 0 {
		t.Fatal("pipeline would not play")
	}
	defer a.setState(pipeline, stateNull)
	if _, err := a.parsed("this-element-does-not-exist ! fakesink", false); err == nil || !strings.Contains(err.Error(), "GStreamer pipeline") {
		t.Fatalf("missing plugin error=%v", err)
	}
	if _, err := a.parsed("this-element-does-not-exist", true); err == nil {
		t.Fatal("invalid source bin accepted")
	}
}

func TestCStringBoundaries(t *testing.T) {
	if got := cString(0, 100); got != "unknown error" {
		t.Fatal(got)
	}
	text := []byte{'a', 'b', 'c', 0, 'd'}
	if got := cString(uintptr(unsafe.Pointer(&text[0])), 2); got != "ab" {
		t.Fatal(got)
	}
	if got := cString(uintptr(unsafe.Pointer(&text[0])), 5); got != "abc" {
		t.Fatal(got)
	}
}

func TestNativeSourceBinHasLinkableGhostPad(t *testing.T) {
	a, err := load()
	if err != nil {
		t.Skip(err)
	}
	bin, err := a.parsed(blackSource, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.objectUnref(bin)
	pad := a.staticPad(bin, "src")
	if pad == 0 {
		t.Fatal("source bin has no src ghost pad")
	}
	a.objectUnref(pad)
}

func TestNativeBusErrorIsReadable(t *testing.T) {
	a, err := load()
	if err != nil {
		t.Skipf("GStreamer runtime unavailable: %v", err)
	}
	pipeline, err := a.parsed("filesrc location=/definitely/missing/eufy-wall-frame ! fakesink", false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.objectUnref(pipeline)
	bus := a.getBus(pipeline)
	if bus == 0 {
		t.Fatal("pipeline has no bus")
	}
	defer a.objectUnref(bus)
	defer a.setState(pipeline, stateNull)
	_ = a.setState(pipeline, statePlaying)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := a.busError(bus); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "resource") && !strings.Contains(strings.ToLower(err.Error()), "file") && !strings.Contains(strings.ToLower(err.Error()), "open") {
				t.Fatalf("unexpected bus error: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("missing file did not raise a GStreamer bus error")
}
