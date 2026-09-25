package gstnative

import (
	"errors"
	"fmt"
	"os"
	"time"
)

func (r *Renderer) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusLocked()
}

// Errors reports an output pipeline freeze or status I/O failure. The caller should exit or
// recreate the renderer; source freezes are recovered in place by the monitor.
func (r *Renderer) Errors() <-chan error { return r.errors }

func (r *Renderer) statusLocked() Status {
	now := time.Now().UTC()
	s := Status{
		SchemaVersion: 1, PID: os.Getpid(), StartedAt: r.started, UpdatedAt: now,
		Sink: r.caps.Sink, ConfigSHA256: r.options.ConfigSHA256,
		OutputFrames: r.output.frames.Load(), OutputLastFrameAt: r.output.lastTime(),
		Tiles: make(map[string]TileStatus, len(r.slots)),
	}
	for _, id := range r.order {
		tile := r.slots[id]
		t := TileStatus{ExpectedLive: tile.expectedLive, SourceKind: tile.kind,
			Generation: tile.generation, State: tile.state, Error: tile.err}
		if tile.state == "playing" && tile.counter != nil && (tile.kind == "live" || tile.kind == "still") {
			t.DecodedFrames = tile.counter.frames.Load()
			t.LastDecodedFrameAt = tile.counter.lastTime()
		}
		s.Tiles[id] = t
	}
	return s
}

func (r *Renderer) writeStatusLocked() error {
	return writeStatus(r.options.StatusPath, r.statusLocked())
}

func (r *Renderer) monitor() {
	defer close(r.done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			if !r.closed {
				now := time.Now()
				r.recoverStalledLocked(now, r.api.busError(r.bus))
				r.refreshStillsLocked(now)
				if err := r.writeStatusLocked(); err != nil {
					r.reportError(err)
				}
				last := r.output.last.Load()
				if time.Since(r.started) > 20*time.Second &&
					(last == 0 || time.Since(time.Unix(0, last)) > 20*time.Second) {
					r.reportError(errors.New("native compositor output has produced no frames for 20 seconds"))
				}
			}
			r.mu.Unlock()
		}
	}
}

func (r *Renderer) reportError(err error) {
	select {
	case r.errors <- err:
	default:
	}
}

// recoverStalledLocked restarts only sources whose decoded frames stopped. A watchdog bus error
// gives useful detail, while the frame clock catches silent freezes without relying on bus order.
func (r *Renderer) recoverStalledLocked(now time.Time, busErr error) {
	for _, s := range r.slots {
		if (!s.expectedLive && s.tile.StillURL == "") || now.Before(s.nextRetry) {
			continue
		}
		if s.state == "retrying" {
			s.key = ""
			if err := r.switchSource(s, s.tile); err != nil {
				s.nextRetry = now.Add(r.options.retryAfter)
			}
			continue
		}
		last := s.installed
		if s.counter != nil && s.counter.last.Load() > 0 {
			last = time.Unix(0, s.counter.last.Load())
		}
		if now.Sub(last) < r.options.stallAfter {
			continue
		}
		s.state = "stalled"
		if busErr != nil {
			s.err = busErr.Error()
		} else {
			s.err = fmt.Sprintf("source produced no decoded frames for %s", r.options.stallAfter)
		}
		s.key = ""
		if err := r.switchSource(s, s.tile); err != nil {
			s.state, s.err = "retrying", err.Error()
			s.nextRetry = now.Add(r.options.retryAfter)
		}
	}
}

// imagefreeze keeps yielding the first JPEG forever, so a stable snapshot URL must be fetched
// again periodically. Replacing only its source bin leaves other tiles and output running.
func (r *Renderer) refreshStillsLocked(now time.Time) {
	for _, s := range r.slots {
		if s.kind != "still" || now.Before(s.nextRetry) || now.Sub(s.installed) < r.options.stillRefresh {
			continue
		}
		s.key = ""
		if err := r.switchSource(s, s.tile); err != nil {
			s.nextRetry = now.Add(r.options.retryAfter)
		}
	}
}

func (r *Renderer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.stop)
	r.closeNative()
	r.mu.Unlock()
	<-r.done
	_ = os.Remove(r.options.StatusPath)
	return nil
}

func (r *Renderer) closeNative() {
	a := r.api
	if r.pipeline == 0 {
		return
	}
	a.setState(r.pipeline, stateNull)
	for _, s := range r.slots {
		if s.sourcePad != 0 {
			a.removeProbe(s.sourcePad, s.probe)
			a.objectUnref(s.sourcePad)
		}
		if s.source != 0 {
			a.objectUnref(s.source)
		}
		if s.queueSink != 0 {
			a.objectUnref(s.queueSink)
		}
		if s.initialSource != 0 {
			a.objectUnref(s.initialSource)
		}
		if s.initialCaps != 0 {
			a.objectUnref(s.initialCaps)
		}
	}
	if r.outputPad != 0 {
		if r.outputProbe != 0 {
			a.removeProbe(r.outputPad, r.outputProbe)
		}
		a.objectUnref(r.outputPad)
	}
	if r.bus != 0 {
		a.objectUnref(r.bus)
	}
	a.objectUnref(r.pipeline)
	r.pipeline = 0
}
