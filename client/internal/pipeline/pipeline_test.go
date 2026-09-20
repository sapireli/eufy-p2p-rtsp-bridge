package pipeline

import (
	"strings"
	"testing"

	"eufy-wall/internal/config"
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

func TestPlanesPipeline(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "v4l2", Sink: "planes", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	want := "-e " +
		"rtspsrc location=rtsp://s/A latency=200 protocols=tcp name=src0 ! rtph264depay ! h264parse ! v4l2h264dec ! kmssink name=sink0 plane-id=31 render-rectangle=<0,0,960,1080> force-aspect-ratio=true sync=false " +
		"rtspsrc location=rtsp://s/B latency=200 protocols=tcp name=src1 ! rtph264depay ! h264parse ! v4l2h264dec ! kmssink name=sink1 plane-id=32 render-rectangle=<960,0,960,1080> force-aspect-ratio=true sync=false"
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

func TestCompositorPipeline(t *testing.T) {
	c, tiles := two()
	args, err := Build(c, tiles, Caps{Decoder: "va", Sink: "compositor", Screen: config.Screen{Width: 1920, Height: 1080}})
	if err != nil {
		t.Fatal(err)
	}
	got := String(args)
	want := "-e " +
		"rtspsrc location=rtsp://s/A latency=200 protocols=tcp name=src0 ! rtph264depay ! h264parse ! vah264dec ! videoconvert ! mix.sink_0 " +
		"rtspsrc location=rtsp://s/B latency=200 protocols=tcp name=src1 ! rtph264depay ! h264parse ! vah264dec ! videoconvert ! mix.sink_1 " +
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
