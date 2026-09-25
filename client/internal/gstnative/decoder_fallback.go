package gstnative

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"eufy-wall/internal/layout"
)

// A bus decode error can identify a decoder failure after one compressed buffer.
// Silent decoder stalls need sustained, recent compressed input before switching.
const silentFallbackBuffers = 30

type compressedCounter struct {
	frames atomic.Uint64
	last   atomic.Int64
}

func (c *compressedCounter) tick() {
	c.frames.Add(1)
	c.last.Store(time.Now().UnixNano())
}

func (r *Renderer) canFallback(s *slot) bool {
	if !r.nativeSource || r.caps.Decoder != "auto" || s.softwareFallback || sourceKind(s.tile) != "live" {
		return false
	}
	codec := s.tile.Codec
	if codec == "" {
		codec = "h264"
	}
	software := r.caps.AutoSoftwareElements[codec]
	return software != "" && r.caps.Element(codec) != software
}

func (r *Renderer) decoderFor(s *slot, tile layout.Placed) string {
	if sourceKind(tile) != "live" {
		return ""
	}
	codec := tile.Codec
	if codec == "" {
		codec = "h264"
	}
	if s.softwareFallback && r.caps.Decoder == "auto" {
		return r.caps.AutoSoftwareElements[codec]
	}
	return r.caps.Element(codec)
}

func (r *Renderer) sourceFor(s *slot, tile layout.Placed) (string, error) {
	if r.options.sourceWithDecoder != nil {
		return r.options.sourceWithDecoder(tile, r.decoderFor(s, tile))
	}
	if !r.nativeSource || !s.softwareFallback || sourceKind(tile) != "live" {
		return r.options.source(tile)
	}
	codec := tile.Codec
	if codec == "" {
		codec = "h264"
	}
	caps := r.caps
	caps.AutoElements = map[string]string{codec: caps.AutoSoftwareElements[codec]}
	return sourceDescription(tile, caps, r.latency)
}

func (r *Renderer) trackDecoderInput(s *slot, source uintptr) error {
	if !r.canFallback(s) {
		return nil
	}
	decoder := r.api.byName(source, "video_decoder")
	if decoder == 0 {
		return fmt.Errorf("native source has no named hardware decoder")
	}
	pad := r.api.staticPad(decoder, "sink")
	r.api.objectUnref(decoder)
	if pad == 0 {
		return fmt.Errorf("native hardware decoder has no sink pad")
	}
	compressed := new(compressedCounter)
	callback := func(_, _, _ uintptr) int32 { compressed.tick(); return probeOK }
	probe := r.api.addProbe(pad, probeBuffer, purego.NewCallback(callback), 0, 0)
	if probe == 0 {
		r.api.objectUnref(pad)
		return fmt.Errorf("native hardware decoder input probe failed")
	}
	s.decoderPad, s.decoderProbe, s.decoderCallback, s.compressed = pad, probe, callback, compressed
	return nil
}

func (r *Renderer) shouldFallback(s *slot, now time.Time, failure busFailure) bool {
	if !r.canFallback(s) || s.compressed == nil || s.counter == nil {
		return false
	}
	input := s.compressed.frames.Load()
	if input == 0 {
		return false // RTSP connection, publisher, or transport has supplied no video.
	}
	lastFrameInput := s.counter.inputAtFrame.Load()
	lastInput := s.compressed.last.Load()
	recentInput := lastInput != 0 && now.Sub(time.Unix(0, lastInput)) <= 3*time.Second
	if failure.decode {
		return input > lastFrameInput && recentInput
	}
	if input < lastFrameInput+silentFallbackBuffers {
		return false
	}
	return recentInput
}

func (r *Renderer) fallbackDecoder(s *slot, reason error) {
	hardware := s.decoder
	s.softwareFallback = true
	log.Printf("[wall] tile %s: %s failed (%v); switching to %s", tileID(s.tile), hardware, reason, r.decoderFor(s, s.tile))
	s.key = ""
	if err := r.switchSource(s, s.tile); err != nil {
		s.err = fmt.Sprintf("hardware decoder failed (%v); software decoder failed: %v", reason, err)
	}
}
