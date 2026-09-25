package gstnative

import (
	"errors"
	"fmt"
	"time"
)

const (
	copyVideoMemory = 1<<2 | 1<<3 // GstMeta and GstMemory, without source timestamps.
	pullIntervalNS  = 100_000_000
)

// makeBlackBuffer asks GStreamer for a correctly aligned I420 black frame.
// Reusing its memory avoids hand-encoding GstVideoMeta and odd-width strides.
func makeBlackBuffer(a *gstAPI, width, height int) (uintptr, error) {
	desc := fmt.Sprintf("videotestsrc num-buffers=1 pattern=black ! video/x-raw,format=I420,width=%d,height=%d,framerate=15/1 ! appsink name=black_output max-buffers=1 drop=true sync=false wait-on-eos=false", width, height)
	p, err := a.parsed(desc)
	if err != nil {
		return 0, err
	}
	defer a.objectUnref(p)
	sink := a.byName(p, "black_output")
	if sink == 0 {
		return 0, errors.New("black frame pipeline has no appsink")
	}
	defer a.objectUnref(sink)
	if a.setState(p, statePlaying) == 0 {
		return 0, errors.New("black frame pipeline failed to start")
	}
	sample := a.appSinkPull(sink, uint64(3*time.Second))
	a.setState(p, stateNull)
	if sample == 0 {
		return 0, errors.New("black frame pipeline produced no sample")
	}
	defer a.miniUnref(sample)
	buffer := a.sampleBuffer(sample)
	if buffer == 0 {
		return 0, errors.New("black sample has no buffer")
	}
	copy := a.bufferNew()
	if copy == 0 {
		return 0, errors.New("black buffer allocation failed")
	}
	if a.bufferCopyInto(copy, buffer, copyVideoMemory, 0, ^uintptr(0)) == 0 {
		a.miniUnref(copy)
		return 0, errors.New("black buffer copy failed")
	}
	return copy, nil
}

// Each compositor tile has exactly one permanent appsrc. Camera pipeline events
// cannot reach it. A short live gap repeats the last picture; an established
// source failure clears that picture and the same feed supplies timed black.
func pumpBlack(a *gstAPI, s *slot) {
	defer close(s.blackDone)
	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()
	for {
		if s.needsBlack() {
			s.pushFallback(a)
		}
		select {
		case <-s.blackStop:
			return
		case <-ticker.C:
		}
	}
}

func (s *slot) pushFallback(a *gstAPI) bool {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if !s.needsBlack() {
		return false
	}
	buffer := s.blackBuffer
	if s.showLive.Load() && s.lastFrame != 0 {
		buffer = s.lastFrame
	}
	return s.pushCopyLocked(a, buffer, false)
}

func (s *slot) needsBlack() bool {
	if !s.showLive.Load() {
		return true
	}
	last := s.lastLivePush.Load()
	return last == 0 || time.Since(time.Unix(0, last)) > 500*time.Millisecond
}

// pushCopy serializes all pushes to a tile. appsrc max-bytes only emits a
// signal with block=false, so the current level is checked before each push.
func (s *slot) pushCopy(a *gstAPI, source uintptr, live bool) bool {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if live && !s.showLive.Load() {
		return false
	}
	return s.pushCopyLocked(a, source, live)
}

func (s *slot) pushCopyLocked(a *gstAPI, source uintptr, live bool) bool {
	if source == 0 {
		return false
	}
	size := a.bufferSize(source)
	if size == 0 || a.appSrcLevel(s.feed) >= uint64(size)*2 {
		return false
	}
	copy := a.bufferNew()
	if copy == 0 {
		return false
	}
	if a.bufferCopyInto(copy, source, copyVideoMemory, 0, ^uintptr(0)) == 0 {
		a.miniUnref(copy)
		return false
	}
	var retained uintptr
	if live {
		retained = a.miniRef(source)
	}
	// gst_app_src_push_buffer takes ownership even on a flow error.
	ok := a.appSrcPush(s.feed, copy) == 0
	if live {
		if ok {
			previous := s.lastFrame
			s.lastFrame = retained
			if previous != 0 {
				a.miniUnref(previous)
			}
			s.lastLivePush.Store(time.Now().UnixNano())
		} else if retained != 0 {
			a.miniUnref(retained)
		}
	}
	return ok
}

func (s *slot) clearLastFrame(a *gstAPI) {
	s.feedMu.Lock()
	if s.lastFrame != 0 {
		a.miniUnref(s.lastFrame)
		s.lastFrame = 0
	}
	s.feedMu.Unlock()
}

// Source pipelines end at appsink. Pulling samples never mutates the stable
// compositor graph; a stale source sample is discarded once black is selected.
func pumpSource(a *gstAPI, sink uintptr, s *slot, counter *frameCounter, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-stop:
			return
		default:
		}
		sample := a.appSinkPull(sink, pullIntervalNS)
		if sample == 0 {
			continue
		}
		select {
		case <-stop:
			a.miniUnref(sample)
			return
		default:
		}
		if buffer := a.sampleBuffer(sample); buffer != 0 {
			counter.tick()
			if s.showLive.Load() {
				s.pushCopy(a, buffer, true)
			}
		}
		a.miniUnref(sample)
	}
}
