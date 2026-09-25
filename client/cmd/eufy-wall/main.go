// eufy-wall: render N RTSP camera tiles on the HDMI output with one supervised GStreamer pipeline.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/supervisor"
	"eufy-wall/internal/wallstate"
	"eufy-wall/internal/wsclient"
)

func main() {
	if handled, err := runCommand(os.Args[1:], os.Stdin, os.Stdout); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "eufy-wall:", err)
			os.Exit(1)
		}
		return
	}
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
	if *printLayout {
		return
	}
	if *dryRun {
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
	mgr := supervisor.NewManager("gst-launch-1.0", c.Restart, func(name, line string) { log.Printf("[gst %s] %s", name, line) })
	log.Printf("[wall] %d pipeline(s): %s", len(plans), planNames(plans))

	runDynamic(ctx, c, caps, tiles, mgr, plans)
	log.Printf("[wall] stopped")
}

// runDynamic follows /ws and keeps the running pipelines matching what each tile should be showing.
type wallManager interface {
	Run(context.Context, []pipeline.Plan) error
	Update(context.Context, []pipeline.Plan)
}

func runDynamic(ctx context.Context, c *config.Config, caps pipeline.Caps, tiles []layout.Placed, mgr wallManager, static []pipeline.Plan) {
	endpoint := wsclient.EventURL(controlBase(c))
	if endpoint == "" {
		log.Printf("[wall] cannot derive the event channel from rtsp_base %q — running a static wall", c.RTSPBase)
		_ = mgr.Run(ctx, static)
		return
	}
	log.Printf("[wall] following events at %s", endpoint)

	store := wallstate.New()
	var mu sync.Mutex
	var applyMu sync.Mutex
	showing := map[int]string{}
	switchedAt := map[int]time.Time{}
	// What each tile is rendering: live video, or the camera's last still while it wakes.
	content := map[int]string{}
	lastShown := ""
	// Holds are ordered per camera, so an in-flight POST cannot arrive after its DELETE.
	holds := newHoldCoordinator(controlBase(c), requestWallHold)

	apply := func() {
		applyMu.Lock()
		defer applyMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		mu.Lock()
		// Until the server has told us what exists, render the wall exactly as configured. Resolving
		// against an empty store would blank every tile, so a display whose bridge is briefly
		// unreachable would go dark rather than keep showing the always-on cameras it can still pull.
		if len(store.Known()) == 0 {
			mu.Unlock()
			mgr.Update(ctx, static)
			return
		}
		sels := store.Resolve(c.Tiles, showing, switchedAt)
		now := time.Now()
		for _, sel := range sels {
			if showing[sel.TileIndex] != sel.Camera {
				showing[sel.TileIndex] = sel.Camera
				switchedAt[sel.TileIndex] = now
			}
			content[sel.TileIndex] = sel.Content
		}

		// Take a hold for every tile that asked for one. The coordinator refreshes bounded holds
		// and releases cameras whose tiles stopped asking.
		wanted := map[string]bool{}
		for _, sel := range sels {
			if sel.Hold && sel.Camera != "" {
				wanted[sel.Camera] = true
			}
		}
		snapshot := make(map[int]string, len(showing))
		for k, v := range showing {
			snapshot[k] = v
		}
		kinds := make(map[int]string, len(content))
		for k, v := range content {
			kinds[k] = v
		}
		// Snapshot the stream keys too: the plans are built after the lock is dropped, and the store is
		// mutated by the event goroutine.
		keys := make(map[string]string, len(snapshot))
		for _, cam := range snapshot {
			if cam != "" {
				keys[cam] = store.StreamKeyFor(cam)
			}
		}
		line := describe(tiles, snapshot, kinds)
		logLine := ""
		if line != lastShown {
			lastShown = line
			logLine = line
		}
		mu.Unlock()

		holds.Update(wanted)
		if logLine != "" {
			log.Printf("[wall] showing %s", logLine)
		}
		mgr.Update(ctx, plansFor(c, caps, tiles, snapshot, kinds, func(sn string) string {
			if k, ok := keys[sn]; ok && k != "" {
				return k
			}
			return sn
		}, store.CodecFor))
	}

	apply()
	go wsclient.Run(ctx, endpoint, store, apply, func(line string) { log.Printf("[wall] %s", line) })

	// Motion expiry is a deadline, not a bridge event. Reconcile even while /ws is quiet or offline so
	// a motion tile blanks and its hold is released without waiting for another camera event.
	reconcile := time.NewTicker(time.Second)
	defer reconcile.Stop()
	for {
		select {
		case <-reconcile.C:
			apply()
		case <-ctx.Done():
			holds.Close()
			applyMu.Lock()
			mgr.Update(ctx, nil)
			applyMu.Unlock()
			return
		}
	}
}

// plansFor builds the pipelines for what each tile is currently showing. A tile showing nothing simply
// has no plan, so a blank tile costs no process at all.
func plansFor(c *config.Config, caps pipeline.Caps, tiles []layout.Placed, showing, content map[int]string, streamKey, codecFor func(string) string) []pipeline.Plan {
	live := make([]layout.Placed, 0, len(tiles))
	for _, t := range tiles {
		if showing == nil {
			live = append(live, t)
			continue
		}
		cam, ok := showing[t.Index]
		if !ok || cam == "" {
			// A tile with its own URL is independent of the bridge's camera inventory. It must survive
			// the first hello snapshot even though it has no camera serial.
			if t.Camera == "" && t.Index < len(c.Tiles) && c.Tiles[t.Index].URL != "" && c.Tiles[t.Index].Motion == "" {
				live = append(live, t)
			}
			continue
		}
		if content[t.Index] == wallstate.ContentNone {
			// Sleeping camera with no retained still: no RTSP or snapshot process should run.
			continue
		}
		t.Camera = cam
		// go2rtc keys its streams by camera NAME, so the URL is built from the key the bridge reports
		// rather than from the serial. Before the bridge has told us one, the serial is what it falls
		// back to as well, so the two agree either way.
		key := cam
		if streamKey != nil {
			key = streamKey(cam)
		}
		t.URL = c.TileURL(config.Tile{Camera: key})
		if codecFor != nil && codecFor(cam) != "" {
			t.Codec = codecFor(cam)
		} else if tc := c.TileFor(cam); tc != nil {
			t.Codec = tc.Codec
		}
		if content[t.Index] == wallstate.ContentSnapshot {
			// Not streaming yet: put the retained still up rather than pointing a decoder at a camera
			// that is asleep, which shows one frozen frame at best.
			t.StillURL = wsclient.StillURL(controlBase(c), cam)
			if t.StillURL == "" {
				continue
			}
		}
		live = append(live, t)
	}
	if caps.Sink == "planes" {
		// Each plane is an independent process. A late codec change or bad URL on one camera must not
		// tear down the other cameras while that tile waits for a compatible decoder or source.
		plans := make([]pipeline.Plan, 0, len(live))
		for _, tile := range live {
			one, err := pipeline.Plans(c, []layout.Placed{tile}, caps)
			if err != nil {
				log.Printf("[wall] tile %s cannot build pipeline: %v", tile.ID, err)
				continue
			}
			plans = append(plans, one...)
		}
		return plans
	}
	plans, err := pipeline.Plans(c, live, caps)
	if err != nil {
		log.Printf("[wall] cannot build pipelines: %v", err)
		return nil
	}
	return plans
}

// describe renders what the wall is showing as one line, so the log says why a tile changed rather than
// only that some process restarted.
func describe(tiles []layout.Placed, showing, content map[int]string) string {
	parts := make([]string, 0, len(tiles))
	for _, t := range tiles {
		what := "blank"
		if cam := showing[t.Index]; cam != "" {
			what = cam
			if content[t.Index] == wallstate.ContentSnapshot {
				what += "(still)"
			}
		}
		parts = append(parts, fmt.Sprintf("%d=%s", t.Index, what))
	}
	return strings.Join(parts, " ")
}

func controlBase(c *config.Config) string {
	if c.BridgeURL != "" {
		return c.BridgeURL
	}
	return c.RTSPBase
}

// planNames lists what the wall is running, so the log says whether tiles are independent processes or
// one composited pipeline.
func planNames(plans []pipeline.Plan) string {
	names := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}
