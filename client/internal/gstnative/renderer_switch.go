package gstnative

import (
	"errors"
	"fmt"
	"time"

	"eufy-wall/internal/layout"
)

func (r *Renderer) switchSource(s *slot, tile layout.Placed) error {
	key := sourceKey(tile)
	if s.key == key && (s.state == "playing" || s.state == "starting") {
		return nil
	}
	if s.state == "retrying" && sourceKey(s.tile) == key && time.Now().Before(s.nextRetry) {
		return nil
	}
	s.expectedLive = sourceKind(tile) == "live"
	s.tile = tile
	if sourceKind(tile) == "black" {
		r.blackout(s)
		s.key, s.kind, s.state, s.err = key, "black", "playing", ""
		s.generation++
		return nil
	}
	desc, err := r.options.source(tile)
	if err != nil {
		return r.retrySource(s, err)
	}
	if err := r.installSource(s, desc); err != nil {
		return r.retrySource(s, err)
	}
	s.key, s.kind, s.state, s.err = key, sourceKind(tile), "starting", ""
	s.generation++
	s.installed = time.Now()
	s.nextRetry = time.Time{}
	return nil
}

// A failed source pipeline cannot affect the permanent appsrc. The black pump
// keeps that same tile feed alive until a replacement decodes a frame.
func (r *Renderer) retrySource(s *slot, cause error) error {
	r.blackout(s)
	s.key, s.kind = "", "black"
	s.state, s.err = "retrying", cause.Error()
	s.nextRetry = time.Now().Add(r.options.retryAfter)
	return cause
}

func (r *Renderer) blackout(s *slot) {
	s.showLive.Store(false)
	s.lastLivePush.Store(0)
	r.removeSource(s)
	s.clearLastFrame(r.api)
}

func (r *Renderer) installSource(s *slot, description string) error {
	a := r.api
	r.blackout(s)
	chain := fmt.Sprintf("%s ! videoconvert ! videoscale ! videorate ! video/x-raw,format=I420,width=%d,height=%d,framerate=15/1 ! appsink name=source_output max-buffers=2 drop=true sync=false wait-on-eos=false", description, s.tile.W, s.tile.H)
	next, err := a.parsed(chain)
	if err != nil {
		return err
	}
	output := a.byName(next, "source_output")
	if output == 0 {
		a.objectUnref(next)
		return errors.New("native source has no output element")
	}
	bus := a.getBus(next)
	if bus == 0 {
		a.objectUnref(output)
		a.objectUnref(next)
		return errors.New("native source has no bus")
	}
	s.source, s.sourceBus, s.sourceSink = next, bus, output
	s.counter = new(frameCounter)
	if a.setState(next, statePlaying) == 0 {
		r.removeSource(s)
		return errors.New("native renderer could not start replacement source")
	}
	s.pumpStop, s.pumpDone = make(chan struct{}), make(chan struct{})
	go pumpSource(a, s.sourceSink, s, s.counter, s.pumpStop, s.pumpDone)
	return nil
}

func (r *Renderer) removeSource(s *slot) {
	if s.source == 0 {
		return
	}
	a := r.api
	if s.pumpStop != nil {
		close(s.pumpStop)
	}
	a.setState(s.source, stateNull)
	if s.pumpDone != nil {
		<-s.pumpDone
	}
	a.objectUnref(s.sourceSink)
	a.objectUnref(s.sourceBus)
	a.objectUnref(s.source)
	s.source, s.sourceBus, s.sourceSink = 0, 0, 0
	s.pumpStop, s.pumpDone, s.counter = nil, nil, nil
}
