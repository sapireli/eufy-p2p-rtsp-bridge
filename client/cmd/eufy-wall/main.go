// eufy-wall: render RTSP camera tiles with supervised GStreamer pipelines on HDMI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/drmbackground"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/supervisor"
	"eufy-wall/internal/wallstate"
	"eufy-wall/internal/wsclient"
)

func main() {
	cfgPath := flag.String("config", "/etc/eufy-wall.yaml", "config file")
	dryRun := flag.Bool("dry-run", false, "print the resolved layout and pipeline, then exit")
	printLayout := flag.Bool("print-layout", false, "print the resolved layout table, then exit")
	viewCamera := flag.String("view-camera", "", "camera name or serial to change (then exit)")
	viewMode := flag.String("view-mode", "", "dual-lens mode: split, pip-tl, pip-tr, pip-bl, pip-br, or single")
	flag.Parse()
	log.SetFlags(log.Ltime)

	c, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	if c.RTSPBase == "auto" {
		for {
			base, err := discoverBridge()
			if err == nil {
				c.RTSPBase = base
				log.Printf("[wall] discovered bridge at %s", base)
				break
			}
			log.Printf("[wall] _eufy-wall._tcp discovery failed: %v; retrying in 5s", err)
			time.Sleep(5 * time.Second)
		}
	}
	if *viewCamera != "" || *viewMode != "" {
		if *viewCamera == "" || *viewMode == "" {
			log.Fatal("[wall] both -view-camera and -view-mode are required")
		}
		if err := changeView(c.RTSPBase, *viewCamera, *viewMode); err != nil {
			log.Fatalf("[wall] view switch: %v", err)
		}
		log.Printf("[wall] %s view changed to %s", *viewCamera, *viewMode)
		return
	}
	if err := detectAspects(c); err != nil {
		log.Printf("[wall] camera geometry unavailable: %v; using configured tile aspects", err)
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
	var sharedFiles []*os.File
	if caps.Sink == "planes" && !*dryRun && !*printLayout {
		device, err := detect.DRMDevice("/", c.Output)
		if err != nil {
			log.Fatalf("[wall] %v", err)
		}
		file, err := os.OpenFile(device, os.O_RDWR, 0)
		if err != nil {
			log.Fatalf("[wall] open shared DRM device: %v", err)
		}
		defer file.Close()
		sharedFiles = []*os.File{file}
		caps.DRMFD = 3 // exec.ExtraFiles starts after stdin/stdout/stderr.
		log.Printf("[wall] sharing DRM device %s across tile processes", device)
	}
	plans, err := pipeline.Plans(c, tiles, caps)
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
	if !detect.HasElement("watchdog") {
		log.Fatal("[wall] GStreamer watchdog element is missing (install gstreamer1.0-plugins-bad)")
	}
	if os.Getenv("GST_DEBUG") == "" {
		os.Setenv("GST_DEBUG", "2")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if caps.Sink == "planes" {
		background, err := drmbackground.Open(sharedFiles[0], caps.ConnectorID)
		if err != nil {
			log.Fatalf("[wall] black background: %v", err)
		}
		defer func() {
			if err := background.Close(); err != nil {
				log.Printf("[wall] release black background: %v", err)
			}
		}()
		info := background.Info()
		caps.ConnectorID = int(info.ConnectorID)
		// Pin the video sinks to the same connector chosen for the background, even for output=auto.
		plans, err = pipeline.Plans(c, tiles, caps)
		if err != nil {
			log.Printf("[wall] %v", err)
			return
		}
		log.Printf("[wall] black image %dx%d %dbpp on primary plane %d framebuffer %d connector %d CRTC %d", info.Width, info.Height, info.BitsPerPixel, info.PlaneID, info.FramebufferID, info.ConnectorID, info.CRTCID)
	}
	mgr := supervisor.NewManager("gst-launch-1.0", c.Restart, func(name, line string) { log.Printf("[gst %s] %s", name, line) }, sharedFiles...)
	defer mgr.Stop() // Registered last: children stop before background.Close and shared file.Close.
	log.Printf("[wall] %d pipeline(s): %s", len(plans), planNames(plans))

	runDynamic(ctx, c, caps, tiles, mgr, plans)
	log.Printf("[wall] stopped")
}

// detectAspects sizes fixed tiles from the bridge's observed stream geometry. An explicit aspect in
// the client config still wins; an unavailable bridge leaves the original layout intact.
func detectAspects(c *config.Config) error {
	base := wsclient.APIBase(c.RTSPBase)
	if base == "" {
		return fmt.Errorf("no bridge API for %q", c.RTSPBase)
	}
	return detectAspectsAt(c, base)
}

func detectAspectsAt(c *config.Config, base string) error {
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get(base + "/api/cameras")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("camera list: HTTP %d", response.StatusCode)
	}
	var cameras []struct {
		SN                string `json:"sn"`
		Width             int    `json:"width"`
		Height            int    `json:"height"`
		RTSPTCPPacketSize int    `json:"rtspTcpPacketSize"`
	}
	if err := json.NewDecoder(response.Body).Decode(&cameras); err != nil {
		return err
	}
	bySN := make(map[string]struct{ Width, Height int }, len(cameras))
	packetHints := make(map[string]int, len(cameras))
	for _, cam := range cameras {
		bySN[cam.SN] = struct{ Width, Height int }{cam.Width, cam.Height}
		packetHints[cam.SN] = cam.RTSPTCPPacketSize
	}
	*c = *c.WithRTSPPacketSizeHints(packetHints)
	for i := range c.Tiles {
		tile := &c.Tiles[i]
		if tile.Aspect != "" || tile.Camera == "" || tile.URL != "" {
			continue
		}
		if dimensions, ok := bySN[tile.Camera]; ok && dimensions.Width > 0 && dimensions.Height > 0 {
			if dimensions.Height > dimensions.Width {
				tile.Aspect = "tall"
			} else {
				tile.Aspect = "wide"
			}
		}
	}
	return nil
}

// changeView uses the bridge's camera list so an operator can name a camera instead of looking up its serial.
func changeView(rtspBase, camera, mode string) error {
	base := wsclient.APIBase(rtspBase)
	if base == "" {
		return fmt.Errorf("cannot derive bridge API from rtsp_base %q", rtspBase)
	}
	return changeViewAt(base, camera, mode)
}

func changeViewAt(base, camera, mode string) error {
	valid := map[string]bool{"split": true, "pip-tl": true, "pip-tr": true, "pip-bl": true, "pip-br": true, "single": true}
	if !valid[mode] {
		return fmt.Errorf("invalid mode %q", mode)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Get(base + "/api/cameras")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("camera list: HTTP %d", response.StatusCode)
	}
	var cameras []struct {
		SN   string `json:"sn"`
		Name string `json:"name"`
		Dual bool   `json:"dual"`
	}
	if err := json.NewDecoder(response.Body).Decode(&cameras); err != nil {
		return err
	}
	var sn string
	for _, cam := range cameras {
		if strings.EqualFold(cam.SN, camera) || strings.EqualFold(cam.Name, camera) {
			if !cam.Dual {
				return fmt.Errorf("%s is not a dual-lens camera", cam.Name)
			}
			sn = cam.SN
			break
		}
	}
	if sn == "" {
		return fmt.Errorf("camera %q was not found", camera)
	}
	endpoint := base + "/api/cameras/" + url.PathEscape(sn) + "/view?mode=" + url.QueryEscape(mode)
	request, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	result, err := client.Do(request)
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(result.Body, 512))
		return fmt.Errorf("bridge HTTP %d: %s", result.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// discoverBridge accepts only a resolved _eufy-wall._tcp Avahi record. Its HTTP
// port is advertised as the service port and its RTSP port is in TXT.
func discoverBridge() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "avahi-browse", "--resolve", "--terminate", "--parsable", "_eufy-wall._tcp").Output()
	if err != nil {
		return "", fmt.Errorf("avahi-browse: %w", err)
	}
	return parseAvahi(string(out))
}

func parseAvahi(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ";")
		if len(fields) < 10 || fields[0] != "=" || fields[4] != "_eufy-wall._tcp" || fields[8] != "3000" {
			continue
		}
		port := ""
		for _, field := range fields[9:] {
			if value, ok := strings.CutPrefix(strings.Trim(field, "\""), "rtsp="); ok {
				if n, err := strconv.Atoi(value); err == nil && n > 0 && n <= 65535 {
					port = value
				}
			}
		}
		if port == "" {
			continue
		}
		if ip := net.ParseIP(fields[7]); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			return "rtsp://" + net.JoinHostPort(ip.String(), port), nil
		}
	}
	return "", fmt.Errorf("no resolved bridge with HTTP port 3000 and a valid rtsp TXT port")
}

// runDynamic follows /ws and keeps the running pipelines matching what each tile should be showing.
func runDynamic(ctx context.Context, c *config.Config, caps pipeline.Caps, tiles []layout.Placed, mgr *supervisor.Manager, static []pipeline.Plan) {
	endpoint := wsclient.EventURL(c.RTSPBase)
	if endpoint == "" {
		log.Printf("[wall] cannot derive the event channel from rtsp_base %q — running a static wall", c.RTSPBase)
		_ = mgr.Run(ctx, static)
		return
	}
	log.Printf("[wall] following events at %s", endpoint)

	store := wallstate.New()
	var mu sync.Mutex
	showing := map[int]string{}
	switchedAt := map[int]time.Time{}
	// What each tile is rendering: live video, or the camera's last still while it wakes.
	content := map[int]string{}
	lastShown := ""
	// Cameras this wall is keeping awake. A hold is bounded on the server, so showing one means
	// refreshing it; no longer showing one means releasing it, or a battery camera would be held awake
	// by a tile that stopped looking at it.
	holding := map[string]bool{}

	apply := func() {
		mu.Lock()
		// Until the server has told us what exists, render the wall exactly as configured. Resolving
		// against an empty store would blank every tile, so a display whose bridge is briefly
		// unreachable would go dark rather than keep showing the always-on cameras it can still pull.
		known := store.Known()
		if len(known) == 0 {
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

		// Take a hold for every tile that asked for one, and release the ones that stopped asking. A
		// tile keeps asking for as long as it is watching, because the server's hold is bounded.
		wanted := map[string]bool{}
		for _, sel := range sels {
			if sel.Hold && sel.Camera != "" {
				wanted[sel.Camera] = true
			}
		}
		var take, drop []string
		for cam := range wanted {
			take = append(take, cam)
			holding[cam] = true
		}
		for cam := range holding {
			if !wanted[cam] {
				drop = append(drop, cam)
				delete(holding, cam)
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
		packetHints := make(map[string]int, len(known))
		for _, cam := range known {
			packetHints[cam.SN] = cam.RTSPTCPPacketSize
		}
		playbackConfig := c.WithRTSPPacketSizeHints(packetHints)
		for _, cam := range snapshot {
			if cam != "" {
				keys[cam] = store.StreamKeyFor(cam)
			}
		}
		mu.Unlock()

		for _, cam := range take {
			go holdRequest(ctx, c.RTSPBase, http.MethodPost, cam)
		}
		for _, cam := range drop {
			go holdRequest(ctx, c.RTSPBase, http.MethodDelete, cam)
		}
		if line := describe(tiles, snapshot, kinds); line != lastShown {
			lastShown = line
			log.Printf("[wall] showing %s", line)
		}
		mgr.Update(ctx, plansFor(playbackConfig, caps, tiles, snapshot, kinds, func(sn string) string {
			if k, ok := keys[sn]; ok && k != "" {
				return k
			}
			return sn
		}))
	}

	apply()
	go wsclient.Run(ctx, endpoint, store, apply, func(line string) { log.Printf("[wall] %s", line) })

	// A hold is deliberately short-lived on the server, so a wall that is still showing a camera has to
	// say so. Refreshing well inside that window keeps the picture up without ever pinning a battery
	// camera awake: stop refreshing and it sleeps on its own.
	refresh := time.NewTicker(holdRefreshInterval)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			held := make([]string, 0, len(holding))
			for cam := range holding {
				held = append(held, cam)
			}
			mu.Unlock()
			// Let the cameras sleep rather than waiting out the hold we took.
			release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			var wg sync.WaitGroup
			for _, cam := range held {
				wg.Add(1)
				go func(c2 string) { defer wg.Done(); holdRequest(release, c.RTSPBase, http.MethodDelete, c2) }(cam)
			}
			wg.Wait()
			cancel()
			mgr.Update(ctx, nil)
			return
		case <-refresh.C:
			mu.Lock()
			held := make([]string, 0, len(holding))
			for cam := range holding {
				held = append(held, cam)
			}
			mu.Unlock()
			for _, cam := range held {
				go holdRequest(ctx, c.RTSPBase, http.MethodPost, cam)
			}
		}
	}
}

// holdRefreshInterval is well inside the server's default hold so a refresh cannot arrive late, and a
// wall that dies simply stops refreshing and the camera sleeps.
const holdRefreshInterval = 20 * time.Second

// plansFor builds the pipelines for what each tile is currently showing. A tile showing nothing simply
// has no plan, so a blank tile costs no process at all.
func plansFor(c *config.Config, caps pipeline.Caps, tiles []layout.Placed, showing, content map[int]string, streamKey func(string) string) []pipeline.Plan {
	live := make([]layout.Placed, 0, len(tiles))
	for _, t := range tiles {
		if showing == nil {
			live = append(live, t)
			continue
		}
		cam, ok := showing[t.Index]
		if !ok || cam == "" {
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
		t.URL = c.TileURLForStream(config.Tile{Camera: cam}, key)
		if tc := c.TileFor(cam); tc != nil {
			// Keep an explicit camera URL through event-driven reconnects, including a hello
			// received before the bridge has populated its camera registry and stream keys.
			t.URL = c.TileURLForStream(config.Tile{Camera: cam, URL: tc.URL}, key)
			t.Codec = tc.Codec
		}
		if content[t.Index] == wallstate.ContentSnapshot {
			// Not streaming yet: put the retained still up rather than pointing a decoder at a camera
			// that is asleep, which shows one frozen frame at best.
			t.StillURL = wsclient.StillURL(c.RTSPBase, cam)
			if t.StillURL == "" {
				continue
			}
		}
		live = append(live, t)
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

// holdRequest takes (POST) or releases (DELETE) this wall's hold on a camera.
func holdRequest(ctx context.Context, rtspBase, method, sn string) {
	u := wsclient.EventURL(rtspBase)
	if u == "" {
		return
	}
	endpoint := strings.Replace(strings.Replace(u, "ws://", "http://", 1), "/ws", "/hold/"+sn, 1)
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"?owner=wall", nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("[wall] %s hold %s failed: %v", strings.ToLower(method), sn, err)
		return
	}
	resp.Body.Close()
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
