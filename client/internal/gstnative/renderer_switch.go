package gstnative

import (
	"errors"
	"time"

	"eufy-wall/internal/layout"
	"github.com/ebitengine/purego"
)

func (r *Renderer) switchSource(s *slot, tile layout.Placed) error {
	key := sourceKey(tile)
	if s.key == key && s.source != 0 && s.state == "playing" {
		return nil
	}
	s.expectedLive = sourceKind(tile) == "live"
	s.tile = tile
	desc, err := r.options.source(tile)
	if err != nil {
		s.state, s.err = "retrying", err.Error()
		s.nextRetry = time.Now().Add(r.options.retryAfter)
		return err
	}
	next, err := r.api.sourceBin(desc)
	if err != nil {
		s.state, s.err = "retrying", err.Error()
		s.nextRetry = time.Now().Add(r.options.retryAfter)
		return err
	}
	if err := r.install(s, next); err != nil {
		if s.source == 0 && s.initialSource == 0 {
			if black, parseErr := r.api.sourceBin(blackSource); parseErr == nil {
				if fallbackErr := r.install(s, black); fallbackErr == nil {
					s.kind, s.state, s.err, s.key = "black", "retrying", err.Error(), ""
					s.tile, s.nextRetry = tile, time.Now().Add(r.options.retryAfter)
					return err
				}
			}
		}
		s.state, s.err = "retrying", err.Error()
		s.nextRetry = time.Now().Add(r.options.retryAfter)
		return err
	}
	s.key, s.kind, s.state, s.err = key, sourceKind(tile), "playing", ""
	s.tile = tile
	s.generation++
	s.installed = time.Now()
	s.nextRetry = time.Time{}
	return nil
}

func (r *Renderer) install(s *slot, next uintptr) error {
	a := r.api
	srcPad := a.staticPad(next, "src")
	if srcPad == 0 {
		a.objectUnref(next)
		return errors.New("native source has no src pad")
	}
	defer a.objectUnref(srcPad)
	// Add and retain a ref before tearing down the old source. Parse failures leave the
	// existing source untouched.
	if a.binAdd(r.pipeline, next) == 0 {
		a.objectUnref(next)
		return errors.New("native renderer could not add source bin")
	}
	a.objectRef(next)
	if s.initialSource != 0 {
		a.setState(s.initialSource, stateNull)
		a.setState(s.initialCaps, stateNull)
		oldPad := a.staticPad(s.initialCaps, "src")
		if oldPad != 0 {
			a.padUnlink(oldPad, s.queueSink)
			a.objectUnref(oldPad)
		}
		a.binRemove(r.pipeline, s.initialSource)
		a.binRemove(r.pipeline, s.initialCaps)
		a.objectUnref(s.initialSource)
		a.objectUnref(s.initialCaps)
		s.initialSource, s.initialCaps = 0, 0
	} else if s.source != 0 {
		r.removeSource(s)
	}
	if a.padLink(srcPad, s.queueSink) != 0 {
		a.binRemove(r.pipeline, next)
		a.objectUnref(next)
		return errors.New("native renderer could not link replacement source")
	}
	counter := new(frameCounter)
	callback := func(_, _, _ uintptr) int32 { counter.tick(); return probeOK }
	probe := a.addProbe(srcPad, probeBuffer, purego.NewCallback(callback), 0, 0)
	if probe == 0 {
		a.padUnlink(srcPad, s.queueSink)
		a.binRemove(r.pipeline, next)
		a.objectUnref(next)
		return errors.New("native renderer could not count source frames")
	}
	s.source, s.sourcePad, s.probe = next, a.objectRef(srcPad), probe
	s.callback, s.counter = callback, counter
	if a.syncState(next) == 0 {
		r.removeSource(s)
		return errors.New("native renderer could not start replacement source")
	}
	return nil
}

func (r *Renderer) removeSource(s *slot) {
	if s.source == 0 {
		return
	}
	a := r.api
	a.setState(s.source, stateNull)
	a.padUnlink(s.sourcePad, s.queueSink)
	a.removeProbe(s.sourcePad, s.probe)
	a.objectUnref(s.sourcePad)
	a.binRemove(r.pipeline, s.source)
	a.objectUnref(s.source)
	s.source, s.sourcePad, s.probe = 0, 0, 0
	s.callback, s.counter = nil, nil
}
