package gstnative

import (
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
	ticker := time.NewTicker(r.options.monitorEvery)
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
				if time.Since(r.started) > r.options.outputStallAfter &&
					(last == 0 || time.Since(time.Unix(0, last)) > r.options.outputStallAfter) {
					r.reportError(fmt.Errorf("native compositor output has produced no frames for %s", r.options.outputStallAfter))
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

// recoverStalledLocked restarts only pipelines whose decoded frames stopped. A source bus
// error gives useful detail; the frame clock also catches silent freezes.
func (r *Renderer) recoverStalledLocked(now time.Time, busErr error) {
	for _, s := range r.slots {
		if (!s.expectedLive && s.tile.StillURL == "") || now.Before(s.nextRetry) {
			continue
		}
		if s.state == "starting" {
			if s.counter != nil && s.counter.frames.Load() > 0 {
				s.showLive.Store(true)
				s.state = "playing"
			} else if now.Sub(s.installed) >= r.options.startupAfter {
				_ = r.retrySource(s, fmt.Errorf("source produced no initial decoded frame for %s", r.options.startupAfter))
			}
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
		var reason error
		if s.sourceBus != 0 {
			reason = r.api.busError(s.sourceBus)
		}
		if reason == nil && busErr != nil {
			reason = busErr
		} else {
			reason = fmt.Errorf("source produced no decoded frames for %s", r.options.stallAfter)
		}
		_ = r.retrySource(s, reason)
	}
}

// imagefreeze keeps yielding the first JPEG forever, so a stable snapshot URL must be fetched
// again periodically. Replacing only its source bin leaves other tiles and output running.
func (r *Renderer) refreshStillsLocked(now time.Time) {
	for _, s := range r.slots {
		if s.kind != "still" || s.state != "playing" || now.Sub(s.installed) < r.options.stillRefresh {
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
	r.stopBlackPumps()
	for _, s := range r.slots {
		r.removeSource(s)
		s.clearLastFrame(a)
		if s.blackBuffer != 0 {
			a.miniUnref(s.blackBuffer)
		}
		if s.feed != 0 {
			a.objectUnref(s.feed)
		}
	}
	a.setState(r.pipeline, stateNull)
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
