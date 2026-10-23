package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/displaystatus"
	"eufy-wall/internal/drmbackground"
)

type startupNotice struct {
	show  func(string)
	close func()
}

func (n *startupNotice) Show(text string) { n.show(text) }
func (n *startupNotice) Close()           { n.close() }
func newStartupNotice(c *config.Config) (*startupNotice, error) {
	screen := c.Screen
	if screen.Width == 0 || screen.Height == 0 {
		if s, ok := detect.ScreenFor("/", c.Output); ok {
			screen = s
		} else {
			screen = config.Screen{Width: 1280, Height: 720}
		}
	}
	if c.Sink != "window" && c.Sink != "compositor" {
		device, err := detect.DRMDevice("/", c.Output)
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(device, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		id, _ := detect.ConnectorID("/", c.Output)
		bg, err := drmbackground.Open(f, id)
		if err != nil {
			f.Close()
			return nil, err
		}
		rect := drmbackground.MotionRect{W: int(bg.Info().Width), H: int(bg.Info().Height)}
		return &startupNotice{show: func(text string) { _ = bg.PaintStatus(rect, text) }, close: func() { _ = bg.Close(); _ = f.Close() }}, nil
	}
	dir, err := os.MkdirTemp("", "eufy-discovery-caption-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "message.txt")
	os.WriteFile(path, []byte(displaystatus.Discovery), 0600)
	sink := "autovideosink"
	if c.Sink == "compositor" {
		sink = "kmssink"
	}
	cmd := exec.Command("gst-launch-1.0", "-e", "eufytile", "caption-file="+path, "caption-width="+strconv.Itoa(screen.Width), "caption-height="+strconv.Itoa(screen.Height), "!", "videoconvert", "!", sink, "sync=false")
	cmd.Env = append(os.Environ(), "GST_PLUGIN_PATH=/usr/local/lib/eufy-wall/gstreamer-1.0"+string(os.PathListSeparator)+os.Getenv("GST_PLUGIN_PATH"))
	if err = cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	return &startupNotice{show: func(text string) { _ = os.WriteFile(path, []byte(text), 0600) }, close: func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = os.RemoveAll(dir)
	}}, nil
}
func runCaptionFiles(ctx context.Context, store *displaystatus.Store, dir string) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	previous := map[int]string{}
	paint := func() {
		for index, text := range store.Captions() {
			if last, ok := previous[index]; ok && last == text {
				continue
			}
			path := filepath.Join(dir, "caption-"+strconv.Itoa(index)+".txt")
			tmp := path + ".new"
			if os.WriteFile(tmp, []byte(text), 0600) == nil && os.Rename(tmp, path) == nil {
				previous[index] = text
			}
		}
	}
	paint()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			paint()
		}
	}
}
