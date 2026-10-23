package latencydot

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

// WriteUnknownImage makes a16px transparent overlay; composition cannot verify per-camera output age.
func WriteUnknownImage(path string) error {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			d := (x-8)*(x-8) + (y-8)*(y-8)
			if d <= 49 {
				c := color.NRGBA{A: 255}
				if d <= 36 {
					c = color.NRGBA{R: 245, G: 158, B: 11, A: 255}
				}
				img.SetNRGBA(x, y, c)
			}
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err = png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
