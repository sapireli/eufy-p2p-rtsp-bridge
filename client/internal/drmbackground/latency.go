package drmbackground

import (
	"encoding/binary"
	"fmt"
)

// LatencyGutter keeps a12px dot and black ring visible below opaque video planes.
const LatencyGutter = 22

// SetLatencyDot changes only its tiny primary-plane patch. It neither maps nor copies video.
func (b *Background) SetLatencyDot(rect MotionRect, rgb uint32) error {
	if b == nil {
		return fmt.Errorf("DRM latency dot: background unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.mapping == nil || rect.X < 0 || rect.Y < 0 || rect.W < 28 || rect.H < 28 || rect.X+rect.W > int(b.info.Width) || rect.Y+rect.H > int(b.info.Height) {
		return fmt.Errorf("DRM latency dot: invalid tile rectangle")
	}
	cx, cy := rect.X+rect.W-14, rect.Y+rect.H-12
	b.restoreMotionLocked()
	for y := -7; y <= 7; y++ {
		for x := -7; x <= 7; x++ {
			color := uint32(0)
			if x*x+y*y <= 36 {
				color = rgb
			}
			offset := (cy+y)*int(b.pitch) + (cx+x)*int(b.info.BitsPerPixel/8)
			if b.info.PixelFormat == rgb565 {
				r, g, bl := (color>>16)&255, (color>>8)&255, color&255
				binary.LittleEndian.PutUint16(b.mapping[offset:], uint16((r>>3)<<11|(g>>2)<<5|bl>>3))
			} else {
				binary.LittleEndian.PutUint32(b.mapping[offset:], color)
			}
		}
	}
	b.refreshMotionLocked()
	b.paintMotionLocked()
	return nil
}
