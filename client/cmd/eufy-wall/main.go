// eufy-wall: render N RTSP camera tiles on the HDMI output with one supervised GStreamer pipeline.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/gstnative"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/supervisor"
)

func main() {
	if handled, err := runCommand(os.Args[1:], os.Stdin, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "eufy-wall:", err)
			os.Exit(1)
		}
		return
	}
	cfgPath := flag.String("config", clientConfigPath, "config file")
	dryRun := flag.Bool("dry-run", false, "print the resolved layout and pipeline, then exit")
	printLayout := flag.Bool("print-layout", false, "print the resolved layout table, then exit")
	flag.Parse()
	render := func() { runWall(*cfgPath, *dryRun, *printLayout) }
	if runtime.GOOS == "darwin" && !*dryRun && !*printLayout {
		if err := gstnative.RunMacOS(render); err != nil {
			log.Fatalf("[wall] macOS display loop: %v", err)
		}
		return
	}
	render()
}

func runWall(cfgPath string, dryRun, printLayout bool) {
	log.SetFlags(log.Ltime)

	if runtime.GOOS == "darwin" && cfgPath == clientConfigPath {
		if err := recoverClientConfig(cfgPath); err != nil {
			log.Fatalf("[wall] recover config: %v", err)
		}
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("[wall] read config: %v", err)
	}
	c, err := config.Parse(raw)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	if c.Screen.Width == 0 || c.Screen.Height == 0 {
		if s, ok := detect.HostScreen("/", c.Output); ok {
			c.Screen = s
		} else {
			c.Screen = config.Screen{Width: 1920, Height: 1080}
			where := "no connected display mode found"
			if runtime.GOOS == "linux" {
				where = "no HDMI mode found in sysfs"
			}
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
	if runtime.GOOS == "linux" && caps.Sink != "window" {
		if id, err := detect.SelectedConnector("/", c.Output); err != nil {
			log.Fatalf("[wall] %v", err)
		} else if id > 0 {
			caps.ConnectorID = id
			log.Printf("[wall] rendering on output %s (connector %d)", c.Output, id)
		}
	}
	tiles, err := layout.Place(c, c.Screen)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	staticTiles := make([]layout.Placed, 0, len(tiles))
	for _, tile := range tiles {
		if c.Tiles[tile.Index].Motion == "" {
			staticTiles = append(staticTiles, tile)
		}
	}
	plans, err := pipeline.Plans(c, staticTiles, caps)
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
	if printLayout {
		return
	}
	if dryRun {
		for _, p := range plans {
			fmt.Printf("# %s\ngst-launch-1.0 %s\n", p.Name, pipeline.String(p.Args))
		}
		return
	}
	if os.Getenv("GST_DEBUG") == "" {
		os.Setenv("GST_DEBUG", "2")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if caps.Sink != "planes" {
		renderer, err := gstnative.New(c, tiles, caps, gstnative.Options{
			StatusPath: clientRuntimeStatusPath(), ConfigSHA256: gstnative.HashConfig(raw), Initial: staticTiles,
		})
		if err != nil {
			log.Fatalf("[wall] native compositor: %v", err)
		}
		defer renderer.Close()
		log.Printf("[wall] native %s compositor with %d independently switched tiles", caps.Sink, len(tiles))
		if err := runDynamicNative(ctx, c, caps, tiles, staticTiles, renderer); err != nil {
			log.Printf("[wall] native compositor: %v", err)
			_ = renderer.Close()
			os.Exit(1)
		}
	} else {
		mgr := supervisor.NewManager("gst-launch-1.0", c.Restart, func(name, line string) { log.Printf("[gst %s] %s", name, line) })
		log.Printf("[wall] %d pipeline(s): %s", len(plans), planNames(plans))
		runDynamic(ctx, c, caps, tiles, mgr, plans)
	}
	log.Printf("[wall] stopped")
}
