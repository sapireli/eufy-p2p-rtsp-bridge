package gstnative

import (
	"errors"
	"fmt"
	"time"
)

func (r *Renderer) startBlackPumps() {
	for _, id := range r.order {
		s := r.slots[id]
		s.blackStop, s.blackDone = make(chan struct{}), make(chan struct{})
		go pumpBlack(r.api, s)
	}
}

func (r *Renderer) stopBlackPumps() {
	for _, id := range r.order {
		s := r.slots[id]
		if s == nil || s.blackStop == nil {
			continue
		}
		close(s.blackStop)
		<-s.blackDone
		s.blackStop, s.blackDone = nil, nil
	}
}

// Check the stable compositor before a camera can enter it. GStreamer can
// report PLAYING while its aggregator has made no output progress; a state
// restart flushes that startup condition without disturbing any source.
func (r *Renderer) startOutput() error {
	for attempt := 0; attempt < 2; attempt++ {
		if err := r.waitForOutput(); err == nil {
			return nil
		} else if attempt == 1 {
			return fmt.Errorf("native compositor produced no startup output after restart: %w", err)
		}
		r.stopBlackPumps()
		if r.api.setState(r.pipeline, stateNull) == 0 || r.api.setState(r.pipeline, statePlaying) == 0 {
			return errors.New("native compositor failed to restart during startup")
		}
		r.startBlackPumps()
	}
	return errors.New("native compositor startup exhausted")
}

func (r *Renderer) waitForOutput() error {
	deadline := time.NewTimer(r.options.startupOutputAfter)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if r.output.frames.Load() > 0 {
			return nil
		}
		if err := r.api.busError(r.bus); err != nil {
			return err
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return fmt.Errorf("no frames within %s", r.options.startupOutputAfter)
		}
	}
}
