package drmbackground

import (
	"encoding/binary"
	"fmt"
)

// MarkUnavailable paints one static caption into our own black primary-plane
// pixels, behind a tile's video rectangle. A live overlay covers the caption;
// disabling that overlay exposes it without another renderer or per-frame work.
func (b *Background) MarkUnavailable(x, y, w, h int) error {
	if b == nil {
		return fmt.Errorf("DRM caption: invalid tile rectangle")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.mapping == nil || x < 0 || y < 0 || w <= 0 || h <= 0 || x+w > int(b.info.Width) || y+h > int(b.info.Height) {
		return fmt.Errorf("DRM caption: invalid tile rectangle")
	}
	const text = "VIDEO UNAVAILABLE"
	scale := min(4, w/(len(text)*6+4), h/11)
	if scale < 1 {
		return fmt.Errorf("DRM caption: tile too small")
	}
	left := x + (w-(len(text)*6-1)*scale)/2
	top := y + (h-7*scale)/2
	b.restoreMotionLocked()
	for i, ch := range text {
		rows, ok := captionGlyphs[ch]
		if !ok {
			continue
		}
		for row, bits := range rows {
			for col := 0; col < 5; col++ {
				if bits&(1<<uint(4-col)) == 0 {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						offset := (top+row*scale+sy)*int(b.pitch) + (left+(i*6+col)*scale+sx)*int(b.info.BitsPerPixel/8)
						if b.info.PixelFormat == rgb565 {
							binary.LittleEndian.PutUint16(b.mapping[offset:], 0x8410)
						} else {
							binary.LittleEndian.PutUint32(b.mapping[offset:], 0x00909090)
						}
					}
				}
			}
		}
	}
	b.refreshMotionLocked()
	b.paintMotionLocked()
	return nil
}

var captionGlyphs = map[rune][7]byte{
	'V': {17, 17, 17, 17, 17, 10, 4}, 'I': {14, 4, 4, 4, 4, 4, 14}, 'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'O': {14, 17, 17, 17, 17, 17, 14},
	'U': {17, 17, 17, 17, 17, 17, 14}, 'N': {17, 25, 25, 21, 19, 19, 17}, 'A': {14, 17, 17, 31, 17, 17, 17}, 'L': {16, 16, 16, 16, 16, 16, 31}, 'B': {30, 17, 17, 30, 17, 17, 30},
}

// PaintStatus paints only the reserved22px footer, so errors remain visible even
// when a video plane retains its previous image. Empty text clears on recovery.
func (b *Background) PaintStatus(rect MotionRect, text string) error {
	if b == nil {
		return fmt.Errorf("DRM caption unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || rect.X < 0 || rect.Y < 0 || rect.W < 40 || rect.H < 22 || rect.X+rect.W > int(b.info.Width) || rect.Y+rect.H > int(b.info.Height) {
		return fmt.Errorf("DRM caption invalid rectangle")
	}
	b.restoreMotionLocked()
	top := rect.Y + rect.H - 22
	for y := top; y < rect.Y+rect.H; y++ {
		for x := rect.X + 5; x < rect.X+rect.W-30; x++ {
			offset := y*int(b.pitch) + x*int(b.info.BitsPerPixel/8)
			for z := 0; z < int(b.info.BitsPerPixel/8); z++ {
				b.mapping[offset+z] = 0
			}
		}
	}
	if limit := (rect.W - 40) / 6; len(text) > limit {
		text = text[:limit]
	}
	scale := min(2, (rect.W-40)/(max(len(text), 1)*6))
	if scale > 0 {
		for i, ch := range text {
			rows := captionGlyphs[ch]
			for row, bits := range rows {
				for col := 0; col < 5; col++ {
					if bits&(1<<uint(4-col)) == 0 {
						continue
					}
					for sy := 0; sy < scale; sy++ {
						for sx := 0; sx < scale; sx++ {
							offset := (top+3+row*scale+sy)*int(b.pitch) + (rect.X+8+(i*6+col)*scale+sx)*int(b.info.BitsPerPixel/8)
							if b.info.PixelFormat == rgb565 {
								binary.LittleEndian.PutUint16(b.mapping[offset:], 0xffff)
							} else {
								binary.LittleEndian.PutUint32(b.mapping[offset:], 0x00ffffff)
							}
						}
					}
				}
			}
		}
	}
	b.refreshMotionLocked()
	b.paintMotionLocked()
	return nil
}
func init() {
	for ch, rows := range map[rune][7]byte{
		'C': {14, 17, 16, 16, 16, 17, 14}, 'F': {31, 16, 16, 30, 16, 16, 16}, 'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'J': {7, 2, 2, 2, 18, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'M': {17, 27, 21, 21, 17, 17, 17}, 'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17}, 'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'W': {17, 17, 17, 21, 21, 27, 17}, 'X': {17, 17, 10, 4, 10, 17, 17}, 'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31}, '-': {0, 0, 0, 31, 0, 0, 0},
	} {
		captionGlyphs[ch] = rows
	}
}
