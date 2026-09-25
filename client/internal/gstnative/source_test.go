package gstnative

import (
	"strings"
	"testing"

	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func TestNativeSourceDescriptionsAreCodecSpecificAndQuoted(t *testing.T) {
	caps := pipeline.Caps{Decoder: "software"}
	for _, tc := range []struct {
		tile layout.Placed
		want []string
	}{
		{layout.Placed{ID: "front", URL: "rtsp://bridge/front", Codec: "h264"}, []string{"rtph264depay", "h264parse", "avdec_h264", "watchdog timeout=15000"}},
		{layout.Placed{ID: "yard", URL: "rtsp://bridge/yard", Codec: "h265"}, []string{"rtph265depay", "h265parse", "avdec_h265", "watchdog timeout=15000"}},
		{layout.Placed{ID: "still", StillURL: "http://bridge/snapshot", Codec: "h265"}, []string{"souphttpsrc", "jpegdec", "imagefreeze"}},
		{layout.Placed{ID: "blank"}, []string{"videotestsrc", "framerate=1/1"}},
	} {
		desc, err := sourceDescription(tc.tile, caps, 200)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range tc.want {
			if !strings.Contains(desc, fragment) {
				t.Fatalf("%s missing %q", desc, fragment)
			}
		}
		if tc.tile.StillURL != "" && strings.Contains(desc, "watchdog") {
			t.Fatal("still has live watchdog")
		}
	}
	quoted, err := quoteProperty(`rtsp://bridge/entry/side door #1?x="quoted"&y=\slash`)
	if err != nil || !strings.Contains(quoted, `\"quoted\"`) || !strings.Contains(quoted, `\\slash`) {
		t.Fatalf("quoted=%q err=%v", quoted, err)
	}
}

func TestNativeSourceRejectsUnknownCodecAndUnsafeURL(t *testing.T) {
	for _, tc := range []struct {
		tile    layout.Placed
		caps    pipeline.Caps
		latency int
	}{
		{layout.Placed{URL: "rtsp://bridge/x", Codec: "av1"}, pipeline.Caps{Decoder: "software"}, 200},
		{layout.Placed{URL: "rtsp://bridge/x"}, pipeline.Caps{Decoder: "none"}, 200},
		{layout.Placed{URL: "rtsp://bridge/x"}, pipeline.Caps{Decoder: "software"}, -1},
		{layout.Placed{URL: "rtsp://bridge/x\n! filesink location=/tmp/evil"}, pipeline.Caps{Decoder: "software"}, 200},
		{layout.Placed{StillURL: "http://bridge/\x00evil"}, pipeline.Caps{Decoder: "software"}, 200},
	} {
		if _, err := sourceDescription(tc.tile, tc.caps, tc.latency); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}
