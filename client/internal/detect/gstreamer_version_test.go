package detect

import "testing"

func TestNativeGStreamerMinimumVersion(t *testing.T) {
	for _, tc := range []struct {
		output string
		valid  bool
	}{
		{"gst-inspect-1.0 version 1.18.5\nGStreamer 1.18.5\n", false},
		{"gst-inspect-1.0 version 1.20.0\nGStreamer 1.20.0\n", true},
		{"gst-inspect-1.0 version 1.28.7\nGStreamer 1.28.7\n", true},
		{"gst-inspect-1.0 version 2.0.0\nGStreamer 2.0.0\n", true},
		{"unrecognized version output", false},
	} {
		if err := checkNativeVersionOutput(tc.output); (err == nil) != tc.valid {
			t.Errorf("version output %q: valid=%t, err=%v", tc.output, tc.valid, err)
		}
	}
	if err := CheckNativeVersion("planes"); err != nil {
		t.Fatalf("planes do not require native compositor properties: %v", err)
	}
}
