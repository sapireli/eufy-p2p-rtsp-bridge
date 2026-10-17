package pipeline

import (
	"strings"
	"testing"

	"eufy-wall/internal/config"
	"eufy-wall/internal/freshness"
	"eufy-wall/internal/layout"
)

func two() (*config.Config, []layout.Placed) {
	c := &config.Config{Latency: 200, Planes: []int{31, 32}}
	tiles := []layout.Placed{
		{Index: 0, Camera: "A", URL: "rtsp://s/A", X: 0, Y: 0, W: 960, H: 1080},
		{Index: 1, Camera: "B", URL: "rtsp://s/B", X: 960, Y: 0, W: 960, H: 1080, Letterbox: true},
	}
	return c, tiles
}

func TestMotionBorderReservesOnlyVideoGutter(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", MotionBorderPixels: 4})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	for _, rect := range []string{"render-rectangle=<4,4,952,1072>", "render-rectangle=<964,4,952,1072>"} {
		if !strings.Contains(got, rect) {
			t.Fatalf("missing %s in %s", rect, got)
		}
	}
	if tiles[0].X != 0 || tiles[0].W != 960 {
		t.Fatal("layout identity changed")
	}
	if _, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", MotionBorderPixels: 600}); err == nil {
		t.Fatal("oversized inset accepted")
	}
}

func TestPlanesPipeline(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	want := "-e " +
		"rtspsrc location=rtsp://s/A latency=200 protocols=tcp name=src0 ! rtph264depay ! h264parse ! v4l2h264dec ! watchdog timeout=15000 ! kmssink name=sink0 plane-id=31 render-rectangle=<0,0,960,1080> sync=false " +
		"rtspsrc location=rtsp://s/B latency=200 protocols=tcp name=src1 ! rtph264depay ! h264parse ! v4l2h264dec ! watchdog timeout=15000 ! kmssink name=sink1 plane-id=32 render-rectangle=<960,0,960,1080> sync=false"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestPlanesNeedsPlaneIDs(t *testing.T) {
	c, tiles := two()
	c.Planes = []int{31}
	if _, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes"}); err == nil || !strings.Contains(err.Error(), "planes") {
		t.Fatalf("expected plane error, got %v", err)
	}
}

func TestPlaneProcessesShareDRMWithoutCompetingPageFlips(t *testing.T) {
	c, tiles := two()
	plans, err := Plans(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", DRMFD: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range plans {
		got := String(plan.Args)
		if !strings.Contains(got, "fd=3 skip-vsync=true") || strings.Contains(got, "force-aspect-ratio") {
			t.Fatalf("invalid shared plane pipeline: %s", got)
		}
	}
}

func TestCompositorPipeline(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "va", Sink: "compositor", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	want := "-e " +
		"rtspsrc location=rtsp://s/A latency=200 protocols=tcp name=src0 ! rtph264depay ! h264parse ! vah264dec ! watchdog timeout=15000 ! videoconvert ! mix.sink_0 " +
		"rtspsrc location=rtsp://s/B latency=200 protocols=tcp name=src1 ! rtph264depay ! h264parse ! vah264dec ! watchdog timeout=15000 ! videoconvert ! mix.sink_1 " +
		"compositor name=mix background=black sink_0::xpos=0 sink_0::ypos=0 sink_0::width=960 sink_0::height=1080 sink_0::sizing-policy=keep-aspect-ratio " +
		"sink_1::xpos=960 sink_1::ypos=0 sink_1::width=960 sink_1::height=1080 sink_1::sizing-policy=keep-aspect-ratio " +
		"! video/x-raw,width=1920,height=1080 ! kmssink sync=false"
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestWindowSoftware(t *testing.T) {
	c, tiles := two()
	args, _ := Build(c, tiles, Caps{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 1920, Height: 1080}})
	s := String(args)
	if !strings.Contains(s, "avdec_h264") || !strings.HasSuffix(s, "! autovideosink sync=false") {
		t.Fatalf("%s", s)
	}
}

func TestArgsAreNotShellJoined(t *testing.T) {
	c, tiles := two()
	args, _ := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes"})
	for _, a := range args {
		if strings.ContainsAny(a, " \t\n") {
			t.Fatalf("arg with whitespace: %q", a)
		}
	}
}

// A wall can mix codecs. An eufy HomeBase composes some cameras as H.264 and others as HEVC, and where
// the bridge passes a camera through untranscoded the tile has to decode what the camera actually sends —
// depayloader, parser and decoder all differ, so picking one per tile is the whole point.
func TestMixedCodecTilesGetTheirOwnDecoder(t *testing.T) {
	tiles := []layout.Placed{
		{Index: 0, Camera: "FRONTDOOR", Codec: "h264", URL: "rtsp://s/FD", W: 960, H: 1080},
		{Index: 1, Camera: "GARAGE", Codec: "h265", URL: "rtsp://s/GA", X: 960, W: 960, H: 1080},
	}
	args, err := Build(&config.Config{Latency: 200}, tiles, Caps{Decoder: "va", Sink: "compositor", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := String(args)
	for _, want := range []string{"rtph264depay", "h264parse", "vah264dec", "rtph265depay", "h265parse", "vah265dec"} {
		if !strings.Contains(got, want) {
			t.Errorf("pipeline missing %q\n%s", want, got)
		}
	}
}

// An unset codec means H.264: that is what a transcoding bridge serves, and it keeps existing configs working.
func TestTileCodecDefaultsToH264(t *testing.T) {
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://s/A", W: 1920, H: 1080}}
	args, err := Build(&config.Config{Latency: 200}, tiles, Caps{Decoder: "software", Sink: "window", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := String(args); !strings.Contains(got, "avdec_h264") || strings.Contains(got, "h265") {
		t.Errorf("want h264 decode, got: %s", got)
	}
}

// A wall spread over two monitors runs one instance per monitor, each driving its own output. Without a
// connector id every instance renders on whichever output kmssink picks first, so both land on the same
// screen; with it, each instance keeps its own CRTC and its own hardware planes — which is what avoids
// compositing a single framebuffer spanned across both displays.
func TestConnectorIDTargetsOneOutput(t *testing.T) {
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://s/A", W: 1024, H: 768}}
	for _, sink := range []string{"compositor", "planes"} {
		c := &config.Config{Latency: 200, Planes: []int{31}}
		args, err := Build(c, tiles, Caps{Decoder: "software", Sink: sink, Screen: config.Screen{Width: 1024, Height: 768}, ConnectorID: 42})
		if err != nil {
			t.Fatalf("%s: %v", sink, err)
		}
		if got := String(args); !strings.Contains(got, "connector-id=42") {
			t.Errorf("%s: pipeline should target connector 42: %s", sink, got)
		}
	}
}

// Unset means "first connected output" — the single-screen case, and what every existing config does.
func TestNoConnectorIDLeavesOutputToKmssink(t *testing.T) {
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://s/A", W: 1920, H: 1080}}
	args, err := Build(&config.Config{Latency: 200}, tiles, Caps{Decoder: "software", Sink: "compositor", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := String(args); strings.Contains(got, "connector-id") {
		t.Errorf("no output configured, so none should be pinned: %s", got)
	}
}

// With DRM planes each tile owns its own plane and is genuinely independent, so it becomes its own
// process: one camera dropping out restarts one tile rather than the whole wall. A compositor mixes
// every tile into one frame, so those tiles cannot be split.
func TestPlansSplitPerTileOnlyForPlanes(t *testing.T) {
	tiles := []layout.Placed{
		{Index: 0, Camera: "GARAGE", URL: "rtsp://s/GA", W: 960, H: 1080},
		{Index: 1, Camera: "FRONTDOOR", URL: "rtsp://s/FD", X: 960, W: 960, H: 1080},
	}
	c := &config.Config{Latency: 200, Planes: []int{31, 32}}
	screen := config.Screen{Width: 1920, Height: 1080}

	planes, err := Plans(c, tiles, Caps{Decoder: "software", Sink: "planes", Screen: screen})
	if err != nil {
		t.Fatalf("planes: %v", err)
	}
	if len(planes) != 2 {
		t.Fatalf("planes should give one process per tile, got %d", len(planes))
	}
	if planes[0].Name != "GARAGE" || planes[1].Name != "FRONTDOOR" {
		t.Errorf("plans should be named for their camera: %v, %v", planes[0].Name, planes[1].Name)
	}
	// Each tile must drive its OWN plane, not the first one twice.
	if !strings.Contains(String(planes[0].Args), "plane-id=31") || !strings.Contains(String(planes[1].Args), "plane-id=32") {
		t.Errorf("each tile should take the plane at its own index:\n%s\n%s", String(planes[0].Args), String(planes[1].Args))
	}
	if !strings.Contains(String(planes[1].Args), "rtsp://s/FD") || strings.Contains(String(planes[1].Args), "rtsp://s/GA") {
		t.Errorf("a per-tile plan should carry only its own source: %s", String(planes[1].Args))
	}

	comp, err := Plans(c, tiles, Caps{Decoder: "software", Sink: "compositor", Screen: screen})
	if err != nil {
		t.Fatalf("compositor: %v", err)
	}
	if len(comp) != 1 {
		t.Fatalf("a composited wall is one process, got %d", len(comp))
	}
	for _, url := range []string{"rtsp://s/GA", "rtsp://s/FD"} {
		if !strings.Contains(String(comp[0].Args), url) {
			t.Errorf("the single plan should carry every source, missing %s", url)
		}
	}
}

// A wall with fewer plane ids than tiles cannot be rendered, and saying so beats a tile silently
// landing on the wrong plane.
func TestPlanesNeedAPlanePerTile(t *testing.T) {
	tiles := []layout.Placed{
		{Index: 0, Camera: "A", URL: "rtsp://s/A", W: 960, H: 1080},
		{Index: 1, Camera: "B", URL: "rtsp://s/B", W: 960, H: 1080},
	}
	_, err := Plans(&config.Config{Latency: 200, Planes: []int{31}}, tiles, Caps{Decoder: "software", Sink: "planes", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err == nil {
		t.Fatal("expected an error when a tile has no plane")
	}
	if !strings.Contains(err.Error(), "plane") {
		t.Errorf("the error should point at planes: %v", err)
	}
}

// A tile showing a still must not touch RTSP at all: pointing a decoder at a sleeping camera is what
// leaves a tile frozen on one frame.
func TestStillTileRendersTheJPEGAndNeverOpensTheStream(t *testing.T) {
	c := &config.Config{Latency: 100, Planes: []int{34, 41}}
	tiles := []layout.Placed{{Index: 0, Camera: "YARD", StillURL: "http://b:3000/snapshot/YARD", W: 960, H: 540}}
	args, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	for _, want := range []string{"souphttpsrc", "location=http://b:3000/snapshot/YARD", "jpegdec", "imagefreeze", "kmssink"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in: %s", want, got)
		}
	}
	for _, unwanted := range []string{"rtspsrc", "v4l2h264dec", "watchdog"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a still must not use %q: %s", unwanted, got)
		}
	}
}

// H.265 on a decoder family that cannot handle it is an error for a live tile — but a still is a JPEG,
// so the same tile showing its thumbnail must still build.
func TestStillTileNeedsNoDecoderSupport(t *testing.T) {
	c := &config.Config{Latency: 100}
	tiles := []layout.Placed{{Index: 0, Camera: "A", Codec: "h265", StillURL: "http://b:3000/snapshot/A", W: 640, H: 480}}
	if _, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "window", Screen: config.Screen{Width: 1920, Height: 1080}}); err != nil {
		t.Fatalf("a JPEG needs no video decoder: %v", err)
	}
}

// The drop point must remain after decoding: RTP reference loss corrupts later frames.
func TestPacingDropsOnlyDecodedFramesAndPreservesRTPClock(t *testing.T) {
	c, tiles := two()
	for _, codec := range []string{"h264", "h265"} {
		tiles[0].Codec = codec
		args, err := Build(c, tiles[:1], Caps{Decoder: "v4l2", Sink: "planes", DecodedPacing: true})
		if err != nil {
			t.Fatal(err)
		}
		got := String(args)
		if strings.Contains(got, "tcp-timestamp=true") || !strings.Contains(got, "buffer-mode=none") {
			t.Fatalf("RTP media clock must survive receive jitter: %s", got)
		}
		if strings.Contains(got, "drop-on-latency=true") || !strings.Contains(got, "v4l2"+codec+"dec ! watchdog timeout=15000 ! queue max-size-buffers=1") {
			t.Fatalf("compressed frames must not be dropped: %s", got)
		}
		if !strings.Contains(got, "eufypacing ! watchdog timeout=15000 ! kmssink") {
			t.Fatalf("output stalls must remain watched after the leaky queue: %s", got)
		}
		if !strings.Contains(got, "sync=true max-lateness=67000000 processing-deadline=0") {
			t.Fatalf("clocked bounded lateness missing: %s", got)
		}
	}
}

func TestCompositorPacesOutputExactlyOnce(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "va", Sink: "compositor", DecodedPacing: true, Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	if strings.Count(got, "eufypacing") != 1 || strings.Index(got, "eufypacing") < strings.Index(got, "compositor name=mix") {
		t.Fatalf("output clock must be rebased once: %s", got)
	}
}

func TestStrictFreshnessPairsEncodedStampWithRawPacer(t *testing.T) {
	c, tiles := two()
	caps := Caps{Decoder: "v4l2", Sink: "planes", DRMFD: 3, DecodedPacing: true, StampReader: true, ClockFile: "/tmp/clock", Freshness: map[string]freshness.Capability{"A": {Version: 1, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://host/api/clock", StreamURL: "rtsp://s/A"}}}
	args, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	s := String(args)
	for _, want := range []string{"h264parse ! video/x-h264,stream-format=byte-stream,alignment=au ! eufystamp channel=1 ! v4l2h264dec", "eufypacing channel=1 clock-file=/tmp/clock max-age-ms=500 drm-fd=3 plane-id=31", "kmssink name=sink0 plane-id=31 render-rectangle=<0,0,960,1080> sync=false"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	if strings.Count(s, "eufystamp") != 1 {
		t.Fatal("legacy unadvertised feed opted into strict mode")
	}
	caps.StampReader = false
	if _, err = Build(c, tiles, caps); err == nil {
		t.Fatal("advertised strict feed silently downgraded")
	}
	caps.StampReader = true
	caps.Sink = "compositor"
	if _, err = Build(c, tiles, caps); err == nil {
		t.Fatal("compositor incorrectly claimed stale-picture expiration support")
	}
}

func TestSameCameraSerialCannotOptLegacyURLIntoFreshness(t *testing.T) {
	c, tiles := two()
	tiles[0].URL = "rtsp://legacy/A"
	caps := Caps{Decoder: "v4l2", Sink: "planes", Freshness: map[string]freshness.Capability{"A": {Version: 1, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://s/A"}}}
	args, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(String(args), "eufystamp") {
		t.Fatal("same serial incorrectly bound a legacy media resource")
	}
}

func TestObserveRetainsLegacyPlaybackWithoutStrictPlugin(t *testing.T) {
	c, tiles := two()
	caps := Caps{Decoder: "v4l2", Sink: "planes", DecodedPacing: true, Freshness: map[string]freshness.Capability{"A": {Version: 2, Mode: "observe", Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://s/A"}}}
	observe, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	caps.Freshness = nil
	legacy, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if String(observe) != String(legacy) {
		t.Fatal("observe changed legacy pipeline")
	}
}

func TestObserveModeMeasuresMatchedResourceWithoutEnforcement(t *testing.T) {
	c := &config.Config{Planes: []int{98}, Latency: 0}
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://bridge/A", W: 960, H: 1080}}
	caps := Caps{Decoder: "v4l2", Sink: "planes", DRMFD: 3, DecodedPacing: true, StampReader: true, ObservePacing: true, ClockFile: "/tmp/clock", Freshness: map[string]freshness.Capability{"A": {Version: 2, Mode: "observe", Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://bridge/A"}}}
	args, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	for _, want := range []string{"eufystamp channel=1", "eufypacing channel=1 clock-file=/tmp/clock max-age-ms=500 observe-only=true", "sync=true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("observe telemetry missing %s: %s", want, got)
		}
	}
	if strings.Contains(got, "eufypacing channel=1 clock-file=/tmp/clock max-age-ms=500 drm-fd=") {
		t.Fatal("observe enabled deadline plane control")
	}
	caps.ObservePacing = false
	args, err = Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(String(args), "eufystamp") || strings.Contains(String(args), "observe-only") {
		t.Fatal("old plugin no longer usable")
	}
	caps.ObservePacing = true
	tiles[0].URL = "rtsp://manual/A"
	args, err = Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(String(args), "clock-file") {
		t.Fatal("observation bound to unrelated manual URI")
	}
}

func TestTimingDiagnosticsObserveEachPhaseWithoutAddingQueues(t *testing.T) {
	c := &config.Config{Planes: []int{98}, Latency: 0}
	tiles := []layout.Placed{{Index: 0, Camera: "A", URL: "rtsp://bridge/A", W: 960, H: 1080}}
	caps := Caps{Decoder: "v4l2", Sink: "planes", DRMFD: 3, DecodedPacing: true, StampReader: true, ObservePacing: true, ClockFile: "/tmp/clock", Freshness: map[string]freshness.Capability{"A": {Version: 2, Mode: "observe", Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://bridge/A"}}}
	baseline, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	caps.TimingDiagnostics = "/tmp/private-frames.jsonl"
	args, err := Build(c, tiles, caps)
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	phases := []string{"phase=depay-au", "phase=parsed-au", "phase=decoder-out", "phase=post-queue", "eufypacing diagnostic-file="}
	previous := -1
	for _, phase := range phases {
		at := strings.Index(got, phase)
		if at <= previous {
			t.Fatalf("phase absent or ordered incorrectly: %s in %s", phase, got)
		}
		previous = at
	}
	if strings.Count(got, "! queue ") != strings.Count(String(baseline), "! queue ") || !strings.Contains(got, "max-size-buffers=1") || !strings.Contains(got, "sync=true") {
		t.Fatal("diagnostics changed queue/presentation policy")
	}
	tiles[0].URL = "rtsp://manual/A"
	if _, err = Build(c, tiles, caps); err == nil {
		t.Fatal("diagnostics borrowed unrelated resource identity")
	}
}
