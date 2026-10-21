package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

func TestHardwareInitializationErrorRequiresDecoderOwnedFatalMessage(t *testing.T) {
	for _, message := range []string{"Failed to allocate required memory.", "Failed to start decoding thread.", "Could not open device '/dev/video10' for reading and writing.", "Decoder on device /dev/video10 has no supported input format", "Could not initialize supporting library."} {
		if !hardwareInitializationError("ERROR: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: " + message) {
			t.Errorf("missed initialization failure: %s", message)
		}
	}
	if !hardwareInitializationError("ERROR: from element /GstPipeline:p/GstVaH265Dec:vah265dec0: Could not initialize supporting library.") {
		t.Fatal("missed VA initialization failure")
	}
	for _, line := range []string{
		"ERROR: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: Failed to process frame.",
		"ERROR: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: No valid frames decoded before end of stream",
		"ERROR: from element /GstPipeline:p/GstRTSPSrc:src0: Internal data stream error.",
		"ERROR: from element /GstPipeline:p/GstKMSSink:sink0: Failed to allocate required memory.",
		"ERROR: from element /GstPipeline:p/GstWatchdog:w: Watchdog triggered",
		"ERROR: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: streaming stopped, reason not-negotiated (-4)",
		"WARNING: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: Failed to allocate required memory.",
	} {
		if hardwareInitializationError(line) {
			t.Errorf("unsafe fallback classification: %s", line)
		}
	}
}

func TestSoftwareFallbackOnlyForHardwareInitializationFailure(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		software      bool
	}{
		{"hardware", "ERROR: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: Failed to allocate required memory.", true},
		{"network", "ERROR: from element /GstPipeline:p/GstRTSPSrc:src0: Could not read from resource.", false},
		{"corrupt-media", "ERROR: from element /GstPipeline:p/GstV4l2H264Dec:v4l2h264dec0: Failed to process frame.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "player")
			attempts := filepath.Join(dir, "attempts")
			code := "#!/bin/sh\nif [ \"$1\" = healthy ]; then exec sleep 30; fi\nprintf '%s\\n' \"$1\" >> \"$2\"\nprintf '%s\\n' \"$3\" >&2\nexit 1\n"
			if err := os.WriteFile(bin, []byte(code), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			r := &recorder{}
			m := NewManager(bin, config.Restart{}, r.log)
			defer func() { cancel(); m.Stop() }()
			plans := []pipeline.Plan{{Name: "failed", Args: []string{"hardware", attempts, tc.message}, SoftwareArgs: []string{"software", attempts, tc.message}}, plan("healthy", "healthy")}
			m.Update(ctx, plans)
			waitFor(t, func() bool {
				b, _ := os.ReadFile(attempts)
				return len(strings.Fields(string(b))) >= 3 && r.countWith("healthy: started") == 1
			}, "three playback attempts")
			// An identical control refresh must preserve the software fallback latch.
			m.Update(ctx, plans)
			cancel()
			m.Stop()
			b, _ := os.ReadFile(attempts)
			got := strings.Fields(string(b))
			for i, mode := range got {
				want := "hardware"
				if i > 0 && tc.software {
					want = "software"
				}
				if mode != want {
					t.Fatalf("attempts=%v want %s at %d", got, want, i)
				}
			}
			if r.countWith("healthy: started") != 1 || r.countWith("healthy: stopping") != 0 {
				t.Fatal("fallback restarted healthy sibling")
			}
		})
	}
}
