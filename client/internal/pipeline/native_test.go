package pipeline

import (
	"eufy-wall/internal/config"
	"eufy-wall/internal/freshness"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/rendition"
	"strings"
	"testing"
)

func intRef(n int) *int { return &n }

func nativeFixture() (*config.Config, []layout.Placed, Caps) {
	c := &config.Config{Tiles: []config.Tile{{Camera: "A"}}, Latency: 0, Planes: []int{31}}
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://bridge/A", W: 1920, H: 1080}}
	caps := Caps{Decoder: "v4l2", Sink: "planes", NativeDecoder: "v4l2slh265dec", NativeHEVCSinkCaps: "video/x-h265\n profile: main\n level: 5\n tier: high", NativeRenditions: map[string]rendition.Native{"A": {RTSP: "rtsp://bridge/A_native", Codec: "h265", Profile: intRef(1), Level: intRef(150), Tier: intRef(1), Width: 2560, Height: 1440, Freshness: freshness.Capability{Version: 2, Mode: "observe", Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://bridge/A_native"}}}}
	return c, tiles, caps
}
func TestNativeHardwarePlanAndCompatibleFallback(t *testing.T) {
	c, tiles, caps := nativeFixture()
	plans, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	got := String(plans[0].Args)
	back := String(plans[0].FallbackArgs)
	if !strings.Contains(got, "location=rtsp://bridge/A_native") || !strings.Contains(got, "rtph265depay ! h265parse ! v4l2slh265dec") {
		t.Fatalf("native hardware missing: %s", got)
	}
	if !strings.Contains(back, "location=rtsp://bridge/A") || !strings.Contains(back, "rtph264depay ! h264parse ! v4l2h264dec") || strings.Contains(back, "A_native") {
		t.Fatalf("wrong fallback: %s", back)
	}
	if tiles[0].URL != "rtsp://bridge/A" || caps.Freshness != nil {
		t.Fatal("selection mutated base plan")
	}
}
func TestNativePreferencePreservesExplicitChoicesAndMissingHardware(t *testing.T) {
	for _, which := range []string{"codec", "manual", "missing-hardware", "software-factory", "still"} {
		t.Run(which, func(t *testing.T) {
			c, tiles, caps := nativeFixture()
			switch which {
			case "codec":
				c.Tiles[0].Codec = "h264"
			case "manual":
				c.Tiles[0].URL = "rtsp://manual/A"
				tiles[0].URL = c.Tiles[0].URL
			case "missing-hardware":
				caps.NativeDecoder = ""
			case "software-factory":
				caps.NativeDecoder = "avdec_h265"
			case "still":
				tiles[0].StillURL = "http://bridge/snapshot/A"
			}
			plans, err := Plans(c, tiles, caps)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(String(plans[0].Args), "A_native") || len(plans[0].FallbackArgs) != 0 {
				t.Fatalf("overrode explicit/ineligible choice: %+v", plans)
			}
		})
	}
}
func TestNativeFreshnessBindsOnlySelectedResource(t *testing.T) {
	c, tiles, caps := nativeFixture()
	caps.Freshness = map[string]freshness.Capability{"A": {Version: 2, Mode: "observe", Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://bridge/A"}}
	selected, chosen, ok := selectNative(c, tiles, caps)
	if !ok || selected[0].URL != "rtsp://bridge/A_native" || chosen.Freshness["A"].StreamURL != "rtsp://bridge/A_native" || chosen.Freshness["A"].Enforced() {
		t.Fatal("native inherited compatible strict capability")
	}
	n := caps.NativeRenditions["A"]
	n.Freshness.StreamURL = "rtsp://wrong/A"
	caps.NativeRenditions["A"] = n
	selected, chosen, ok = selectNative(c, tiles, caps)
	if ok || selected[0].URL != "rtsp://bridge/A" || chosen.Freshness["A"].StreamURL != "rtsp://bridge/A" {
		t.Fatal("wrong URL contract changed compatible plan")
	}

}

func TestNativeStrictCannotDowngradeOrBypassUnavailableGuards(t *testing.T) {
	c, tiles, caps := nativeFixture()
	caps.Freshness = map[string]freshness.Capability{"A": {Version: 1, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://bridge/A"}}
	if _, _, selected := selectNative(c, tiles, caps); selected {
		t.Fatal("strict compatible resource downgraded to observe native")
	}
	caps.Freshness = nil
	n := caps.NativeRenditions["A"]
	n.Freshness.Version = 1
	n.Freshness.Mode = "enforce"
	caps.NativeRenditions["A"] = n
	if _, _, selected := selectNative(c, tiles, caps); selected {
		t.Fatal("strict native selected without stamp/DRM guard support")
	}
	caps.DecodedPacing = true
	caps.StampReader = true
	caps.ClockFile = "/tmp/clock"
	caps.DRMFD = 3
	plans, err := Plans(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	got := String(plans[0].Args)
	if !strings.Contains(got, "video/x-h265,stream-format=byte-stream,alignment=au ! eufystamp") || !strings.Contains(got, "max-age-ms=500") {
		t.Fatalf("native strict contract lost: %s", got)
	}
	if strings.Contains(String(plans[0].FallbackArgs), "max-age-ms=500") {
		t.Fatal("native contract leaked onto legacy compatible fallback")
	}
}

func TestNativeRetainsPacketSizeWithoutCopyingResourceSelector(t *testing.T) {
	c, tiles, caps := nativeFixture()
	tiles[0].URL = "rtsp://bridge/A?pkt_size=8192&video=compatible"
	n := caps.NativeRenditions["A"]
	n.RTSP = "rtsp://bridge/A_native?video=native"
	n.Freshness.StreamURL = n.RTSP
	caps.NativeRenditions["A"] = n
	selected, chosen, ok := selectNative(c, tiles, caps)
	if !ok || selected[0].URL != "rtsp://bridge/A_native?pkt_size=8192&video=native" || !chosen.Freshness["A"].BoundTo(selected[0].URL) {
		t.Fatalf("hint/binding lost: %+v", selected)
	}
	size := 4096
	c.RTSPPacketSize = &size
	selected, _, _ = selectNative(c, tiles, caps)
	if selected[0].URL != "rtsp://bridge/A_native?pkt_size=4096&video=native" {
		t.Fatalf("explicit packet override lost: %s", selected[0].URL)
	}
}

func TestNativeFactoryWithoutExplicitTupleStaysCompatible(t *testing.T) {
	for _, which := range []string{"missing-metadata", "missing-caps", "main-tier-only"} {
		t.Run(which, func(t *testing.T) {
			c, tiles, caps := nativeFixture()
			switch which {
			case "missing-metadata":
				n := caps.NativeRenditions["A"]
				n.Tier = nil
				caps.NativeRenditions["A"] = n
			case "missing-caps":
				caps.NativeHEVCSinkCaps = "video/x-h265\n profile: main"
			case "main-tier-only":
				caps.NativeHEVCSinkCaps = "video/x-h265\n profile: main\n level: 5\n tier: main"
			}
			plans, err := Plans(c, tiles, caps)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(String(plans[0].Args), "A_native") || len(plans[0].FallbackArgs) > 0 {
				t.Fatal("unknown/unsupported tuple selected")
			}
		})
	}
}
