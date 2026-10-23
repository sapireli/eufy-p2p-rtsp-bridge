package drmbackground

import (
	"bytes"
	"testing"
)

func TestLatencyDotDrawsInsideReservedGutterAndCoexistsWithMotion(t *testing.T) {
	for _, format := range []uint32{rgb565, xrgb8888} {
		b, clock := motionBackground(format)
		rect := MotionRect{X: 4, Y: 4, W: 40, H: 32}
		before := append([]byte(nil), b.mapping...)
		if err := b.FlashMotion(rect); err != nil {
			t.Fatal(err)
		}
		if err := b.SetLatencyDot(rect, 0x22c55e); err != nil {
			t.Fatal(err)
		}
		b.ClearMotion()
		after := append([]byte(nil), b.mapping...)
		if bytes.Equal(before, after) {
			t.Fatal("dot invisible")
		}
		stride := int(b.info.BitsPerPixel / 8)
		for i := range before {
			if before[i] == after[i] {
				continue
			}
			y, x := i/int(b.pitch), (i%int(b.pitch))/stride
			if y < rect.Y+rect.H-LatencyGutter || y >= rect.Y+rect.H || x < rect.X || x >= rect.X+rect.W {
				t.Fatalf("dot escaped footer: %d,%d", x, y)
			}
		}
		for _, timer := range clock.timers {
			timer.callback()
		}
		if !bytes.Equal(after, b.mapping) {
			t.Fatal("stale motion callback erased dot")
		}
	}
}
