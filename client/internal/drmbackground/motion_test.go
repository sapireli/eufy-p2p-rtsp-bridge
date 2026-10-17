package drmbackground

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type fakeMotionTimer struct {
	callback func()
	stopped  bool
}

func (t *fakeMotionTimer) Stop() bool { wasActive := !t.stopped; t.stopped = true; return wasActive }

type motionClock struct{ timers []*fakeMotionTimer }

func (c *motionClock) after(delay time.Duration, callback func()) motionTimer {
	if delay != MotionPulse {
		panic("wrong pulse interval")
	}
	t := &fakeMotionTimer{callback: callback}
	c.timers = append(c.timers, t)
	return t
}
func motionBackground(format uint32) (*Background, *motionClock) {
	bits := uint32(32)
	if format == rgb565 {
		bits = 16
	}
	b := &Background{d: fake(), info: Info{Width: 200, Height: 80, PixelFormat: format, BitsPerPixel: bits}, pitch: 200*(bits/8) + 8}
	b.mapping = make([]byte, int(b.pitch)*80)
	c := &motionClock{}
	b.motionAfter = c.after
	return b, c
}
func isMotionBlue(b *Background, x, y int) bool {
	at := y*int(b.pitch) + x*int(b.info.BitsPerPixel/8)
	if b.info.PixelFormat == rgb565 {
		return binary.LittleEndian.Uint16(b.mapping[at:]) == 0x55bf
	}
	return binary.LittleEndian.Uint32(b.mapping[at:]) == 0x0055b6ff
}

func TestMotionFlashesForFiveSecondsAndRestoresImage(t *testing.T) {
	for _, format := range []uint32{rgb565, xrgb8888} {
		b, c := motionBackground(format)
		for i := range b.mapping {
			b.mapping[i] = byte(i * 13)
		}
		original := bytes.Clone(b.mapping)
		if time.Duration(motionPhases)*MotionPulse != 5*time.Second {
			t.Fatal("wrong flash duration")
		}
		if err := b.FlashMotion(MotionRect{10, 10, 100, 50}); err != nil {
			t.Fatal(err)
		}
		for _, point := range [][2]int{{10, 10}, {109, 30}, {50, 59}, {10, 30}} {
			if !isMotionBlue(b, point[0], point[1]) {
				t.Fatal("missing outline edge", point)
			}
		}
		center := 30*int(b.pitch) + 50*int(bitsForFormat(format)/8)
		if !bytes.Equal(b.mapping[center:center+4], original[center:center+4]) {
			t.Fatal("painted tile interior")
		}
		c.timers[0].callback()
		if !bytes.Equal(original, b.mapping) {
			t.Fatal("first off phase did not restore image")
		}
		c.timers[1].callback()
		if !isMotionBlue(b, 10, 10) {
			t.Fatal("second blue pulse missing")
		}
		for phase := 2; phase < motionPhases; phase++ {
			c.timers[phase].callback()
		}
		if !bytes.Equal(original, b.mapping) || len(b.motion) != 0 || len(c.timers) != motionPhases {
			t.Fatal("pulse did not finish without idle timers")
		}
	}
}
func bitsForFormat(format uint32) uint32 {
	if format == rgb565 {
		return 16
	}
	return 32
}

func TestMotionClipsAllEdgesWithoutTouchingPadding(t *testing.T) {
	for _, rect := range []MotionRect{{-2, 10, 30, 30}, {190, 10, 20, 30}, {20, -2, 30, 30}, {20, 70, 30, 20}, {0, 0, 1, 1}} {
		b, _ := motionBackground(rgb565)
		for i := range b.mapping {
			b.mapping[i] = 0x55
		}
		original := bytes.Clone(b.mapping)
		if err := b.FlashMotion(rect); err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(original, b.mapping) {
			t.Fatalf("clipped outline invisible: %+v", rect)
		}
		for y := 0; y < 80; y++ {
			if !bytes.Equal(b.mapping[y*int(b.pitch)+400:(y+1)*int(b.pitch)], original[y*int(b.pitch)+400:(y+1)*int(b.pitch)]) {
				t.Fatal("wrote pitch padding")
			}
		}
		b.ClearMotion()
		if !bytes.Equal(original, b.mapping) {
			t.Fatal("clipped outline was not restored")
		}
	}
	b, c := motionBackground(rgb565)
	if err := b.FlashMotion(MotionRect{400, 400, 20, 20}); err != nil || len(c.timers) != 0 {
		t.Fatal("offscreen outline scheduled work", err)
	}
	if err := b.FlashMotion(MotionRect{0, 0, -1, 20}); err == nil {
		t.Fatal("invalid dimensions accepted")
	}
}

func TestMotionRetriggerAndClearIgnoreOldCallbacks(t *testing.T) {
	b, c := motionBackground(rgb565)
	rect := MotionRect{10, 10, 100, 50}
	_ = b.FlashMotion(rect)
	old := c.timers[0]
	old.callback() // off phase
	_ = b.FlashMotion(rect)
	if !isMotionBlue(b, 10, 10) || !c.timers[1].stopped {
		t.Fatal("retrigger did not restart pulse")
	}
	old.callback() // an already queued callback from the previous generation
	if !isMotionBlue(b, 10, 10) {
		t.Fatal("old callback altered a retriggered pulse")
	}
	b.ClearMotion()
	for _, timer := range c.timers {
		timer.callback()
	}
	if len(b.motion) != 0 || isMotionBlue(b, 10, 10) {
		t.Fatal("cleared pulse was revived")
	}
}

func TestMotionOverlapAndPerTileCancellation(t *testing.T) {
	b, c := motionBackground(xrgb8888)
	first, second := MotionRect{10, 10, 90, 50}, MotionRect{10, 20, 130, 40}
	_ = b.FlashMotion(first)
	_ = b.FlashMotion(second)
	c.timers[0].callback() // first outline off; second must remain on shared edge
	if !isMotionBlue(b, 10, 30) {
		t.Fatal("one pulse erased another outline")
	}
	b.ClearMotionRect(first)
	if !isMotionBlue(b, 10, 30) || len(b.motion) != 1 {
		t.Fatal("per-tile cancellation affected another tile")
	}
	c.timers[0].callback()
	if len(b.motion) != 1 {
		t.Fatal("cancelled tile callback altered another tile")
	}
	b.ClearMotionRect(second)
	if !bytes.Equal(b.mapping, make([]byte, len(b.mapping))) {
		t.Fatal("overlap retained blue pixels")
	}
}

func TestMotionPreservesExistingAndNewCaptions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		b, _ := motionBackground(rgb565)
		reference, _ := motionBackground(rgb565)
		if err := reference.MarkUnavailable(0, 0, 200, 80); err != nil {
			t.Fatal(err)
		}
		if existing {
			if err := b.MarkUnavailable(0, 0, 200, 80); err != nil {
				t.Fatal(err)
			}
		}
		if err := b.FlashMotion(MotionRect{40, 34, 120, 12}); err != nil {
			t.Fatal(err)
		}
		if !existing {
			if err := b.MarkUnavailable(0, 0, 200, 80); err != nil {
				t.Fatal(err)
			}
		}
		b.ClearMotion()
		if !bytes.Equal(b.mapping, reference.mapping) {
			t.Fatal("motion erased underlying caption")
		}
	}
}

func TestMotionCloseCancelsCallbacksBeforeUnmap(t *testing.T) {
	b, c := motionBackground(rgb565)
	_ = b.FlashMotion(MotionRect{10, 10, 100, 50})
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if !c.timers[0].stopped || b.mapping != nil || len(b.motion) != 0 {
		t.Fatal("shutdown retained motion work")
	}
	c.timers[0].callback()
	b.ClearMotion()
	b.ClearMotionRect(MotionRect{10, 10, 100, 50})
	if err := b.FlashMotion(MotionRect{10, 10, 100, 50}); err == nil {
		t.Fatal("paint accepted after shutdown")
	}
}

func TestMotionCallbackCanRaceRetriggerAndShutdown(t *testing.T) {
	b, c := motionBackground(rgb565)
	rect := MotionRect{10, 10, 100, 50}
	_ = b.FlashMotion(rect)
	callback := c.timers[0].callback
	var workers sync.WaitGroup
	workers.Add(3)
	go func() { defer workers.Done(); callback() }()
	go func() { defer workers.Done(); _ = b.FlashMotion(rect); b.ClearMotionRect(rect) }()
	go func() { defer workers.Done(); _ = b.Close() }()
	workers.Wait()
	if b.mapping != nil || len(b.motion) != 0 {
		t.Fatal("concurrent shutdown retained pixels or motion state")
	}
	for _, timer := range c.timers {
		timer.callback()
	}
}
