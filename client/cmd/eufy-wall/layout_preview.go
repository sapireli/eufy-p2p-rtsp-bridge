package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/layout"
)

// PreviewOptions controls file and HDMI output. Width and Height must be supplied together; omitted
// dimensions use the config's screen mode, or 1920x1080 when no mode is configured.
type PreviewOptions struct {
	PNGPath string
	Display bool
	Width   int
	Height  int
}

func previewLayout(path string, in io.Reader, out io.Writer, opts PreviewOptions) error {
	c, err := parseClientInput(path, in)
	if err != nil {
		return err
	}
	screen := c.Screen
	if opts.Width != 0 || opts.Height != 0 {
		screen = config.Screen{Width: opts.Width, Height: opts.Height}
	}
	if screen.Width == 0 && screen.Height == 0 {
		screen = config.Screen{Width: 1920, Height: 1080}
	}
	if screen.Width < 1 || screen.Height < 1 || screen.Width > 8192 || screen.Height > 8192 || int64(screen.Width)*int64(screen.Height) > 64<<20 {
		return errors.New("preview screen dimensions must be positive, <=8192 per side, and <=64 megapixels")
	}
	placed, err := layout.Place(c, screen)
	if err != nil {
		return err
	}
	if err := textLayout(out, c, placed, screen); err != nil {
		return err
	}
	if opts.PNGPath == "" && !opts.Display {
		return nil
	}
	pathPNG := opts.PNGPath
	if pathPNG == "" {
		f, err := os.CreateTemp("", "eufy-wall-preview-*.png")
		if err != nil {
			return err
		}
		pathPNG = f.Name()
		if err := f.Close(); err != nil {
			os.Remove(pathPNG)
			return err
		}
		defer os.Remove(pathPNG)
	}
	if err := writeLayoutPNGForScreen(pathPNG, screen, placed); err != nil {
		return err
	}
	if opts.PNGPath != "" {
		if _, err := fmt.Fprintf(out, "PNG saved: %s\n", opts.PNGPath); err != nil {
			return err
		}
	}
	if opts.Display {
		return displayLayoutPNG(pathPNG, c.Output, out)
	}
	return nil
}

// textLayout is a monochrome, scaled preview with exact coordinates in a linear legend. One character
// spans more than one source pixel, but each printable cell samples the same pixel rectangles as PNG.
func textLayout(out io.Writer, c *config.Config, placed []layout.Placed, screen config.Screen) error {
	cols, rows := c.GridDims()
	if screen.Width < 1 || screen.Height < 1 {
		return errors.New("preview needs positive screen dimensions")
	}
	const viewW = 64
	viewH := int(math.Round(float64(viewW) * float64(screen.Height) / float64(screen.Width) / 2))
	if viewH < 4 {
		viewH = 4
	}
	if viewH > 24 {
		viewH = 24
	}
	if _, err := fmt.Fprintf(out, "screen %dx%d; canvas %dx%d; %d tile(s). '.' is empty.\n", screen.Width, screen.Height, cols, rows, len(placed)); err != nil {
		return err
	}
	for y := 0; y < viewH; y++ {
		var line strings.Builder
		for x := 0; x < viewW; x++ {
			px := (2*x + 1) * screen.Width / (2 * viewW)
			py := (2*y + 1) * screen.Height / (2 * viewH)
			ch := '.'
			for i, p := range placed {
				if px >= p.X && px < p.X+p.W && py >= p.Y && py < p.Y+p.H {
					ch = tileSymbol(i)
					break
				}
			}
			line.WriteRune(ch)
		}
		if _, err := fmt.Fprintln(out, line.String()); err != nil {
			return err
		}
	}
	for i, p := range placed {
		id := p.ID
		if id == "" {
			id = fmt.Sprintf("tile-%d", p.Index)
		}
		if _, err := fmt.Fprintf(out, "%c %s %s grid=(%d,%d %dx%d) pixels=(%d,%d %dx%d)\n", tileSymbol(i), id, p.Camera, p.Col, p.Row, p.Cols, p.Rows, p.X, p.Y, p.W, p.H); err != nil {
			return err
		}
	}
	return nil
}

func tileSymbol(i int) rune {
	const symbols = "123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if i >= 0 && i < len(symbols) {
		return rune(symbols[i])
	}
	return '?'
}

func writeLayoutPNG(path string, c *config.Config, placed []layout.Placed) error {
	s := c.Screen
	if s.Width == 0 || s.Height == 0 {
		s = config.Screen{Width: 1920, Height: 1080}
	}
	return writeLayoutPNGForScreen(path, s, placed)
}

func writeLayoutPNGForScreen(path string, screen config.Screen, placed []layout.Placed) error {
	if screen.Width < 1 || screen.Height < 1 || screen.Width > 8192 || screen.Height > 8192 || int64(screen.Width)*int64(screen.Height) > 64<<20 {
		return errors.New("preview dimensions are outside 1..8192 and 64 megapixels")
	}
	img := image.NewRGBA(image.Rect(0, 0, screen.Width, screen.Height))
	fillRect(img, img.Bounds(), color.RGBA{12, 16, 22, 255})
	for i, p := range placed {
		r := image.Rect(p.X, p.Y, p.X+p.W, p.Y+p.H).Intersect(img.Bounds())
		if r.Empty() {
			continue
		}
		base := tileColor(p.ID, i)
		fillRect(img, r, base)
		border := color.RGBA{235, 240, 245, 255}
		for x := r.Min.X; x < r.Max.X; x++ {
			img.Set(x, r.Min.Y, border)
			img.Set(x, r.Max.Y-1, border)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			img.Set(r.Min.X, y, border)
			img.Set(r.Max.X-1, y, border)
		}
		if p.Letterbox && r.Dx() > 8 && r.Dy() > 8 {
			inner := r.Inset(5)
			for x := inner.Min.X; x < inner.Max.X; x += 4 {
				img.Set(x, inner.Min.Y, border)
				img.Set(x, inner.Max.Y-1, border)
			}
			for y := inner.Min.Y; y < inner.Max.Y; y += 4 {
				img.Set(inner.Min.X, y, border)
				img.Set(inner.Max.X-1, y, border)
			}
		}
		drawNumber(img, r, i+1)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		return err
	}
	return atomicClientWrite(path, data.Bytes(), 0644, nil)
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func tileColor(id string, index int) color.RGBA {
	h := fnv.New32a()
	if id == "" {
		id = strconv.Itoa(index)
	}
	_, _ = io.WriteString(h, id)
	v := h.Sum32()
	return color.RGBA{R: uint8(38 + v%100), G: uint8(50 + (v>>8)%110), B: uint8(65 + (v>>16)%120), A: 255}
}

var digitGlyphs = [10][5]string{
	{"111", "101", "101", "101", "111"}, {"010", "110", "010", "010", "111"},
	{"111", "001", "111", "100", "111"}, {"111", "001", "111", "001", "111"},
	{"101", "101", "111", "001", "001"}, {"111", "100", "111", "001", "111"},
	{"111", "100", "111", "101", "111"}, {"111", "001", "001", "001", "001"},
	{"111", "101", "111", "101", "111"}, {"111", "101", "111", "001", "111"},
}

func drawNumber(img *image.RGBA, rect image.Rectangle, n int) {
	label := strconv.Itoa(n)
	scale := 3
	width := len(label)*4*scale - scale
	if rect.Dx() < width+12 || rect.Dy() < 5*scale+12 {
		return
	}
	startX, startY := rect.Min.X+(rect.Dx()-width)/2, rect.Min.Y+(rect.Dy()-5*scale)/2
	for i, ch := range label {
		glyph := digitGlyphs[ch-'0']
		for y, row := range glyph {
			for x, bit := range row {
				if bit != '1' {
					continue
				}
				fillRect(img, image.Rect(startX+(i*4+x)*scale, startY+y*scale, startX+(i*4+x+1)*scale, startY+(y+1)*scale), color.RGBA{255, 255, 255, 255})
			}
		}
	}
}

func displayLayoutPNG(path, output string, out io.Writer) error {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return errors.New("gst-launch-1.0 is required for HDMI preview")
	}
	args := []string{"-e", "filesrc", "location=" + path, "!", "pngdec", "!", "imagefreeze", "!", "videoconvert", "!", "kmssink", "sync=false"}
	if output != "" {
		id, ok := detect.ConnectorID("/", output)
		if !ok {
			return fmt.Errorf("cannot identify DRM connector %q for HDMI preview", output)
		}
		args = append(args, fmt.Sprintf("connector-id=%d", id))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, err := io.WriteString(out, "HDMI preview is running; press Ctrl-C to return.\n"); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", args...)
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}
