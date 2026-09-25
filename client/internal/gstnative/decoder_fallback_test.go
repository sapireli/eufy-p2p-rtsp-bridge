package gstnative

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func fallbackCaps() pipeline.Caps {
	return pipeline.Caps{
		Decoder: "auto", Sink: "window", Screen: config.Screen{Width: 128, Height: 64},
		AutoElements:         map[string]string{"h264": "vtdec_hw"},
		AutoSoftwareElements: map[string]string{"h264": "avdec_h264"},
	}
}

func TestDecoderFallbackRequiresDecoderInput(t *testing.T) {
	r := &Renderer{caps: fallbackCaps(), nativeSource: true}
	s := &slot{tile: layout.Placed{URL: "rtsp://bridge/camera", Codec: "h264"},
		counter: new(frameCounter), compressed: new(compressedCounter)}
	now := time.Now()
	for name, prepare := range map[string]func(){
		"no publisher":                     func() {},
		"one packet without decoder error": func() { s.compressed.frames.Store(1); s.compressed.last.Store(now.UnixNano()) },
		"stale input after network outage": func() { s.compressed.frames.Store(50); s.compressed.last.Store(now.Add(-5 * time.Second).UnixNano()) },
	} {
		s.compressed.frames.Store(0)
		s.compressed.last.Store(0)
		prepare()
		if r.shouldFallback(s, now, busFailure{}) {
			t.Fatalf("%s selected software without evidence of an active decoder stall", name)
		}
	}
	s.compressed.frames.Store(30)
	s.compressed.last.Store(now.UnixNano())
	if !r.shouldFallback(s, now, busFailure{}) {
		t.Fatal("sustained compressed input with no decoded frames did not trigger fallback")
	}
	s.counter.inputAtFrame.Store(29)
	if r.shouldFallback(s, now, busFailure{}) {
		t.Fatal("input already associated with a decoded frame caused fallback")
	}
	s.compressed.frames.Store(30)
	if !r.shouldFallback(s, now, busFailure{decode: true}) {
		t.Fatal("decoder error after compressed input did not trigger fallback")
	}
	s.compressed.last.Store(now.Add(-5 * time.Second).UnixNano())
	if r.shouldFallback(s, now, busFailure{decode: true}) {
		t.Fatal("stale decoder error after publisher outage triggered fallback")
	}
	s.compressed.last.Store(now.UnixNano())
	s.counter.inputAtFrame.Store(30)
	if r.shouldFallback(s, now, busFailure{decode: true}) {
		t.Fatal("decoder error without new input since the last frame triggered fallback")
	}
	s.counter.inputAtFrame.Store(29)
	s.compressed.frames.Store(0)
	if r.shouldFallback(s, now, busFailure{decode: true}) {
		t.Fatal("decoder error without stream input caused fallback")
	}
	s.compressed.frames.Store(30)
	r.caps.Decoder = "videotoolbox"
	if r.shouldFallback(s, now, busFailure{decode: true}) {
		t.Fatal("explicit hardware decoder switched to software")
	}
	r.caps.Decoder = "auto"
	s.softwareFallback = true
	if r.shouldFallback(s, now, busFailure{decode: true}) {
		t.Fatal("software decoder retried hardware fallback")
	}
}

func TestNativeHardwareDecoderFallbackIsPerTileAndResetsOnSourceChange(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	opts := syntheticOptions(t)
	opts.Initial = []layout.Placed{}
	opts.monitorEvery = 100 * time.Millisecond
	opts.startupAfter = 2500 * time.Millisecond
	opts.retryAfter = 100 * time.Millisecond
	var hardware, software atomic.Int32
	opts.sourceWithDecoder = func(tile layout.Placed, decoder string) (string, error) {
		if tile.ID == "left" {
			switch decoder {
			case "vtdec_hw":
				hardware.Add(1)
				if tile.URL == "test://recovered" {
					return "videotestsrc is-live=true pattern=ball ! identity name=video_decoder", nil
				}
				return "videotestsrc is-live=true pattern=ball ! valve name=video_decoder drop=true", nil
			case "avdec_h264":
				software.Add(1)
				return "videotestsrc is-live=true pattern=snow", nil
			}
		}
		return "videotestsrc is-live=true pattern=ball ! identity name=video_decoder", nil
	}
	r, err := New(&config.Config{}, tiles, fallbackCaps(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Update(tiles); err != nil {
		t.Fatal(err)
	}
	peerBefore := waitFrames(t, r, "right", 0)
	deadline := time.Now().Add(9 * time.Second)
	for time.Now().Before(deadline) {
		status := r.Status()
		left, right := status.Tiles["left"], status.Tiles["right"]
		if left.Decoder == "avdec_h264" && left.State == "playing" && left.DecodedFrames > 0 {
			if right.Generation != 2 || right.DecodedFrames <= peerBefore || hardware.Load() != 1 || software.Load() != 1 {
				t.Fatalf("fallback disturbed peer or repeated decoder switch: %+v hardware=%d software=%d", status, hardware.Load(), software.Load())
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if r.Status().Tiles["left"].Decoder != "avdec_h264" {
		t.Fatalf("hardware decoder did not fall back: %+v", r.Status())
	}
	tiles[0].URL = "test://recovered"
	if err := r.Update(tiles); err != nil {
		t.Fatal(err)
	}
	want := waitFrames(t, r, "left", 0)
	if want == 0 || hardware.Load() != 2 || r.Status().Tiles["left"].Decoder != "vtdec_hw" {
		t.Fatalf("changed source did not retry hardware: %+v hardware=%d", r.Status(), hardware.Load())
	}
}

func TestNativeDecoderDoesNotFallbackWhenPublisherHasNoPackets(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	opts := syntheticOptions(t)
	opts.Initial = []layout.Placed{}
	opts.monitorEvery = 100 * time.Millisecond
	opts.startupAfter = 300 * time.Millisecond
	opts.retryAfter = 100 * time.Millisecond
	var software atomic.Int32
	opts.sourceWithDecoder = func(tile layout.Placed, decoder string) (string, error) {
		if tile.ID == "left" {
			if decoder == "avdec_h264" {
				software.Add(1)
			}
			return "videotestsrc is-live=true num-buffers=0 ! identity name=video_decoder", nil
		}
		return "videotestsrc is-live=true pattern=ball ! identity name=video_decoder", nil
	}
	r, err := New(&config.Config{}, tiles, fallbackCaps(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Update(tiles); err != nil {
		t.Fatal(err)
	}
	peer := waitFrames(t, r, "right", 0)
	time.Sleep(1200 * time.Millisecond)
	status := r.Status()
	if software.Load() != 0 || status.Tiles["right"].DecodedFrames <= peer ||
		status.Tiles["left"].Decoder == "avdec_h264" {
		t.Fatalf("publisher outage changed decoder or stopped peer: %+v software=%d", status, software.Load())
	}
}

func TestAutoHardwareSourceNamesProbeOnlyWhenSoftwareIsAvailable(t *testing.T) {
	tile := layout.Placed{URL: "rtsp://bridge/front", Codec: "h264"}
	caps := fallbackCaps()
	desc, err := sourceDescription(tile, caps, 200)
	if err != nil || !strings.Contains(desc, "vtdec_hw name=video_decoder") {
		t.Fatalf("auto hardware source has no observable decoder input: %q %v", desc, err)
	}
	delete(caps.AutoSoftwareElements, "h264")
	desc, err = sourceDescription(tile, caps, 200)
	if err != nil || strings.Contains(desc, "name=video_decoder") {
		t.Fatalf("source without software alternative added fallback probe: %q %v", desc, err)
	}
}
