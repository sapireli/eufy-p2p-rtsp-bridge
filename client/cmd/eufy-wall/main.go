// eufy-wall: render N RTSP camera tiles on the HDMI output with one supervised GStreamer pipeline.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/supervisor"
)

func main() {
	cfgPath := flag.String("config", "/etc/eufy-wall.yaml", "config file")
	dryRun := flag.Bool("dry-run", false, "print the resolved layout and pipeline, then exit")
	printLayout := flag.Bool("print-layout", false, "print the resolved layout table, then exit")
	flag.Parse()
	log.SetFlags(log.Ltime)

	c, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	if c.Screen.Width == 0 || c.Screen.Height == 0 {
		if s, ok := detect.ScreenFor("/", c.Output); ok {
			c.Screen = s
		} else {
			c.Screen = config.Screen{Width: 1920, Height: 1080}
			where := "no HDMI mode found in sysfs"
			if c.Output != "" {
				where = "no mode found for output " + c.Output
			}
			log.Printf("[wall] %s — assuming 1920x1080 (set screen: in config)", where)
		}
	}
	caps, err := detect.Resolve(c, detect.HasElement, detect.FileExists)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	if id, ok := detect.ConnectorID("/", c.Output); ok {
		caps.ConnectorID = id
		log.Printf("[wall] rendering on output %s (connector %d)", c.Output, id)
	} else if c.Output != "" {
		log.Printf("[wall] output %s: no connector id in sysfs — kmssink will pick the first connected output", c.Output)
	}
	tiles, err := layout.Place(c, c.Screen)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	args, err := pipeline.Build(c, tiles, caps)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}

	log.Printf("[wall] screen %dx%d layout %s decoder %s sink %s", c.Screen.Width, c.Screen.Height, c.Layout, caps.Decoder, caps.Sink)
	for _, t := range tiles {
		lb := ""
		if t.Letterbox {
			lb = " (letterbox)"
		}
		log.Printf("[wall] tile %d %-18s cell %d,%d span %dx%d px %d,%d %dx%d%s", t.Index, t.Camera, t.Col, t.Row, t.Cols, t.Rows, t.X, t.Y, t.W, t.H, lb)
	}
	if *printLayout {
		return
	}
	if *dryRun {
		fmt.Println("gst-launch-1.0 " + pipeline.String(args))
		return
	}
	if os.Getenv("GST_DEBUG") == "" {
		os.Setenv("GST_DEBUG", "2")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	_ = supervisor.Run(ctx, "gst-launch-1.0", args, c.Restart, func(s string) { log.Printf("[gst] %s", s) })
	log.Printf("[wall] stopped")
}
