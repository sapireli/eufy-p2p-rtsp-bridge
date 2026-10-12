package drmbackground

import (
	"encoding/binary"
	"fmt"
)

// MarkUnavailable paints one static caption into our own black primary-plane
// pixels, behind a tile's video rectangle. A live overlay covers the caption;
// disabling that overlay exposes it without another renderer or per-frame work.
func (b *Background) MarkUnavailable(x, y, w, h int) error {
	if b == nil || b.closed || b.mapping == nil || x < 0 || y < 0 || w <= 0 || h <= 0 || x+w > int(b.info.Width) || y+h > int(b.info.Height) {
		return fmt.Errorf("DRM caption: invalid tile rectangle")
	}
	const text = "VIDEO UNAVAILABLE"
	scale := min(4, w/(len(text)*6+4), h/11)
	if scale < 1 {
		return fmt.Errorf("DRM caption: tile too small")
	}
	left := x + (w-(len(text)*6-1)*scale)/2
	top := y + (h-7*scale)/2
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
	return nil
}

var captionGlyphs = map[rune][7]byte{
	'V': {17, 17, 17, 17, 17, 10, 4}, 'I': {14, 4, 4, 4, 4, 4, 14}, 'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'O': {14, 17, 17, 17, 17, 17, 14},
	'U': {17, 17, 17, 17, 17, 17, 14}, 'N': {17, 25, 25, 21, 19, 19, 17}, 'A': {14, 17, 17, 31, 17, 17, 17}, 'L': {16, 16, 16, 16, 16, 16, 31}, 'B': {30, 17, 17, 30, 17, 17, 30},
}
