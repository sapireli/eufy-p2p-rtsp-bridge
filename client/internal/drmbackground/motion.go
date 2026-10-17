package drmbackground

import (
	"encoding/binary"
	"fmt"
	"time"
)

// MotionBorderPixels is the margin video overlays must leave inside each tile.
const MotionBorderPixels = 4

// MotionPulse is the duration of each step: alternating blue and black for five seconds.
const MotionPulse = 250 * time.Millisecond
const motionPhases = 20

// MotionRect uses the full tile rectangle, before the video inset.
type MotionRect struct{ X, Y, W, H int }

type motionTimer interface{ Stop() bool }
type motionPatch struct{ offset, start, length int }
type motionOverlay struct {
	patches    []motionPatch
	base       []byte
	phase      uint8
	generation uint64
	timer      motionTimer
}

// FlashMotion flashes a blue outline for five seconds into the existing primary image.
// Only outline pixels change; there is no renderer or per-video-frame work.
// Retriggering the same tile restarts its pulse sequence.
func (b *Background) FlashMotion(rect MotionRect) error {
	if b == nil {
		return fmt.Errorf("DRM motion: background unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.mapping == nil {
		return fmt.Errorf("DRM motion: background unavailable")
	}
	// Bound coordinate arithmetic on both 32-bit ARM and 64-bit hosts.
	const bound = 1 << 20
	if rect.W <= 0 || rect.H <= 0 || rect.W > bound || rect.H > bound || rect.X < -bound || rect.X > bound || rect.Y < -bound || rect.Y > bound {
		return fmt.Errorf("DRM motion: invalid tile rectangle")
	}
	b.restoreMotionLocked()
	entry := b.motion[rect]
	if entry == nil {
		entry = b.newMotionLocked(rect)
		if len(entry.patches) == 0 {
			b.paintMotionLocked()
			return nil
		}
		if b.motion == nil {
			b.motion = make(map[MotionRect]*motionOverlay)
		}
		b.motion[rect] = entry
	} else if entry.timer != nil {
		entry.timer.Stop()
	}
	b.motionGeneration++
	entry.generation = b.motionGeneration
	entry.phase = 0
	b.paintMotionLocked()
	b.scheduleMotionLocked(rect, entry)
	return nil
}

// ClearMotion cancels pulses and restores the underlying black image/captions.
// Call it when the layout or the cameras assigned to its rectangles change.
func (b *Background) ClearMotion() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelMotionLocked(!b.closed)
}

// ClearMotionRect cancels one tile without interrupting other camera pulses.
func (b *Background) ClearMotionRect(rect MotionRect) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := b.motion[rect]
	if entry == nil {
		return
	}
	if !b.closed {
		b.restoreMotionLocked()
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	delete(b.motion, rect)
	if !b.closed {
		b.paintMotionLocked()
	}
}

func (b *Background) cancelMotionLocked(restore bool) {
	if restore {
		b.restoreMotionLocked()
	}
	for _, entry := range b.motion {
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}
	b.motion = nil
	b.motionGeneration++
}

func (b *Background) scheduleMotionLocked(rect MotionRect, entry *motionOverlay) {
	generation := entry.generation
	callback := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		current := b.motion[rect]
		if b.closed || current == nil || current.generation != generation {
			return
		}
		b.restoreMotionLocked()
		current.phase++
		if current.phase == motionPhases {
			delete(b.motion, rect)
		} else {
			b.scheduleMotionLocked(rect, current)
		}
		b.paintMotionLocked()
	}
	if b.motionAfter != nil {
		entry.timer = b.motionAfter(MotionPulse, callback)
	} else {
		entry.timer = time.AfterFunc(MotionPulse, callback)
	}
}

func (b *Background) newMotionLocked(rect MotionRect) *motionOverlay {
	entry := &motionOverlay{}
	top, bottom := min(MotionBorderPixels, rect.H), min(MotionBorderPixels, max(0, rect.H-MotionBorderPixels))
	left, right := min(MotionBorderPixels, rect.W), min(MotionBorderPixels, max(0, rect.W-MotionBorderPixels))
	strips := []MotionRect{
		{rect.X, rect.Y, rect.W, top},
		{rect.X, rect.Y + rect.H - bottom, rect.W, bottom},
		{rect.X, rect.Y + top, left, rect.H - top - bottom},
		{rect.X + rect.W - right, rect.Y + top, right, rect.H - top - bottom},
	}
	bytesPerPixel := int(b.info.BitsPerPixel / 8)
	for _, strip := range strips {
		x0, y0 := max(0, strip.X), max(0, strip.Y)
		x1, y1 := min(int(b.info.Width), strip.X+strip.W), min(int(b.info.Height), strip.Y+strip.H)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		for y := y0; y < y1; y++ {
			offset, length := y*int(b.pitch)+x0*bytesPerPixel, (x1-x0)*bytesPerPixel
			entry.patches = append(entry.patches, motionPatch{offset, len(entry.base), length})
			entry.base = append(entry.base, b.mapping[offset:offset+length]...)
		}
	}
	return entry
}

// Every saved patch contains the unadorned image. Restore all patches before
// updating one pulse, then redraw the remaining blue outlines. This preserves
// captions and overlapping outlines without allocating another full framebuffer.
func (b *Background) restoreMotionLocked() {
	for _, entry := range b.motion {
		for _, patch := range entry.patches {
			copy(b.mapping[patch.offset:patch.offset+patch.length], entry.base[patch.start:patch.start+patch.length])
		}
	}
}

func (b *Background) refreshMotionLocked() {
	for _, entry := range b.motion {
		for _, patch := range entry.patches {
			copy(entry.base[patch.start:patch.start+patch.length], b.mapping[patch.offset:patch.offset+patch.length])
		}
	}
}

func (b *Background) paintMotionLocked() {
	for _, entry := range b.motion {
		if entry.phase%2 == 1 {
			continue
		}
		for _, patch := range entry.patches {
			pixels := b.mapping[patch.offset : patch.offset+patch.length]
			if b.info.PixelFormat == rgb565 {
				for at := 0; at < len(pixels); at += 2 {
					binary.LittleEndian.PutUint16(pixels[at:], 0x55bf)
				}
			} else {
				for at := 0; at < len(pixels); at += 4 {
					binary.LittleEndian.PutUint32(pixels[at:], 0x0055b6ff)
				}
			}
		}
	}
}
