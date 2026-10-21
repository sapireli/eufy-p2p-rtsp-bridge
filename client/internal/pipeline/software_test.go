package pipeline

import (
	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"strings"
	"testing"
)

func TestAutomaticHardwarePlanHasMatchingSoftwareFallback(t *testing.T) {
	c := &config.Config{Decoder: "auto", Planes: []int{31}}
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://bridge/A", Codec: "h264", W: 1920, H: 1080}}
	caps := Caps{Decoder: "v4l2", Sink: "planes", SoftwareDecoders: map[string]bool{"h264": true}}
	plans, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(String(plans[0].Args), "v4l2h264dec") || !strings.Contains(String(plans[0].SoftwareArgs), "avdec_h264") {
		t.Fatalf("not hardware-first with software fallback: %+v", plans)
	}
	if got := strings.ReplaceAll(String(plans[0].Args), "v4l2h264dec", "avdec_h264"); got != String(plans[0].SoftwareArgs) {
		t.Fatal("fallback changed resource, timing or quality")
	}
	for _, which := range []string{"explicit", "missing-factory", "software", "still", "compositor"} {
		t.Run(which, func(t *testing.T) {
			cc := *c
			cap := caps
			ts := append([]layout.Placed(nil), tiles...)
			switch which {
			case "explicit":
				cc.Decoder = "v4l2"
			case "missing-factory":
				cap.SoftwareDecoders = nil
			case "software":
				cap.Decoder = "software"
			case "still":
				ts[0].StillURL = "http://bridge/still"
			case "compositor":
				cap.Sink = "compositor"
			}
			p, e := Plans(&cc, ts, cap)
			if e != nil {
				t.Fatal(e)
			}
			if len(p[0].SoftwareArgs) > 0 {
				t.Fatalf("unexpected fallback for %s", which)
			}
		})
	}
}
