// eufy-wall: render RTSP camera tiles with supervised GStreamer pipelines on HDMI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/displaystatus"
	"eufy-wall/internal/drmbackground"
	"eufy-wall/internal/freshness"
	"eufy-wall/internal/latencydot"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/rendition"
	"eufy-wall/internal/supervisor"
	"eufy-wall/internal/wallstate"
	"eufy-wall/internal/wsclient"
)

func main() {
	switchEndpoint := false
	defer func() {
		if switchEndpoint {
			exe, err := os.Executable()
			if err != nil {
				log.Fatal(err)
			}
			if err = syscall.Exec(exe, os.Args, os.Environ()); err != nil {
				log.Fatal(err)
			}
		}
	}()
	if len(os.Args) > 1 && os.Args[1] == "settings" {
		if err := runSettings(os.Args[2:], os.Stdout, os.Stderr); err != nil {
			log.Fatal(err)
		}
		return
	}
	resetIdentity := flag.Bool("reset-bridge-identity", false, "forget saved public bridge identity pins, then exit; restart the client to reselect")
	cfgPath := flag.String("config", "/etc/eufy-wall.yaml", "config file")
	dryRun := flag.Bool("dry-run", false, "print the resolved layout and pipeline, then exit")
	timingDiagnostics := flag.String("timing-diagnostics", "", "optional bounded private frame-phase metadata file (updated native plugin required)")
	printLayout := flag.Bool("print-layout", false, "print the resolved layout table, then exit")
	settingsTokenFile := flag.String("settings-token-file", "", "bridge admin token file for -view-camera/-view-mode (or EUFY_WALL_ADMIN_TOKEN)")
	viewCamera := flag.String("view-camera", "", "camera name or serial to change (then exit)")
	viewMode := flag.String("view-mode", "", "dual-lens mode: split, pip-tl, pip-tr, pip-bl, pip-br, or single")
	flag.Parse()
	log.SetFlags(log.Ltime)
	if *resetIdentity {
		path := identityCachePath()
		if err := clearBridgeIdentities(path); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Cleared saved bridge identities in %s; restart this display client to reselect.\n", path)
		return
	}

	c, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("[wall] %v", err)
	}
	autoBridge := c.RTSPBase == "auto"
	if autoBridge {
		var known []string
		for _, tile := range c.Tiles {
			if tile.Camera != "" {
				known = append(known, tile.Camera)
			}
			known = append(known, tile.Watch...)
		}
		_ = os.Setenv("EUFY_WALL_EXPECTED_CAMERAS", strings.Join(known, ","))
	}
	bridgeIdentity := os.Getenv("EUFY_WALL_DISCOVERY_ID")
	var startup *startupNotice
	defer func() {
		if startup != nil {
			startup.Close()
		}
	}()
	if c.RTSPBase == "auto" {
		for {
			base, err := initialDiscoveredBridge(bridgeIdentity, os.Getenv("EUFY_WALL_RESOLVED_BRIDGE"), os.Getenv("EUFY_WALL_RESOLVED_API"), probeBridge, discoverBridgePinned)
			_ = os.Unsetenv("EUFY_WALL_RESOLVED_BRIDGE")
			_ = os.Unsetenv("EUFY_WALL_RESOLVED_API")
			if err == nil {
				if startup != nil {
					startup.Close()
					startup = nil
				}
				c.RTSPBase = base
				if id, ok := resolvedPublicIDs.Load(wsclient.APIBase(base)); ok {
					_ = os.Setenv("EUFY_WALL_BRIDGE_ID", id.(string))
				}
				if id, ok := resolvedIdentities.Load(base); ok {
					bridgeIdentity = id.(string)
				}
				log.Printf("[wall] discovered bridge at %s", base)
				break
			}
			if !*dryRun && !*printLayout && *viewCamera == "" {
				if startup == nil {
					startup, _ = newStartupNotice(c)
				}
				if startup != nil {
					startup.Show(displaystatus.Classify("discovery: " + err.Error()))
				}
			}
			log.Printf("[wall] _eufy-wall._tcp discovery failed: %v; retrying in 5s", err)
			time.Sleep(5 * time.Second)
		}
	}
	if *viewCamera != "" || *viewMode != "" {
		if *viewCamera == "" || *viewMode == "" {
			log.Fatal("[wall] both -view-camera and -view-mode are required")
		}
		token, err := settingsAdminToken(*settingsTokenFile)
		if err != nil {
			log.Fatalf("[wall] view switch: %v", err)
		}
		if err := changeViewWithToken(c.RTSPBase, *viewCamera, *viewMode, token); err != nil {
			log.Fatalf("[wall] view switch: %v", err)
		}
		log.Printf("[wall] %s view changed to %s", *viewCamera, *viewMode)
		return
	}
	notices := displaystatus.New()
	if err := detectAspects(c); err != nil {
		notices.Bridge(err.Error())
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
	// Installer builds this native plugin for the target architecture.
	pluginPath := "/usr/local/lib/eufy-wall/gstreamer-1.0"
	if existing := os.Getenv("GST_PLUGIN_PATH"); existing != "" {
		pluginPath += string(os.PathListSeparator) + existing
	}
	os.Setenv("GST_PLUGIN_PATH", pluginPath)
	if caps.Sink != "planes" {
		if !detect.HasElement("eufytile") {
			log.Fatal("[wall] compositor/window requires the updated eufytile plugin; rerun install-client.sh")
		}
		dir, e := os.MkdirTemp("", "eufy-wall-tiles-")
		if e != nil {
			log.Fatal(e)
		}
		if *dryRun {
			log.Printf("[wall] preview tile controls retained at %s; remove this directory after running the printed command", dir)
		} else {
			defer os.RemoveAll(dir)
		}
		caps.IsolatedTiles, caps.TileConfigDir = true, dir
	}
	caps.DecodedPacing = detect.HasElement("eufypacing")
	caps.StampReader = detect.HasElement("eufystamp")
	caps.ObservePacing = detect.HasElementProperty("eufypacing", "observe-only")
	caps.OutputStatus = detect.HasElementProperty("eufypacing", "output-status")
	if caps.Sink != "planes" {
		caps.CaptionDir = caps.TileConfigDir
		caps.DotImage = filepath.Join(caps.TileConfigDir, "latency-unknown.png")
		if err := latencydot.WriteUnknownImage(caps.DotImage); err != nil {
			log.Fatal(err)
		}
	} else {
		caps.LatencyGutter = drmbackground.LatencyGutter
	}
	if *timingDiagnostics != "" {
		if !filepath.IsAbs(*timingDiagnostics) || strings.ContainsAny(*timingDiagnostics, " \t\r\n\"") {
			log.Fatal("[wall] timing diagnostics needs an absolute path without whitespace or quotes")
		}
		if !detect.HasElement("eufytrace") || !detect.HasElementProperty("eufypacing", "diagnostic-file") {
			log.Fatal("[wall] timing diagnostics requires an updated native plugin")
		}
		caps.TimingDiagnostics = *timingDiagnostics
	}
	caps.NativeDecoder = detect.HardwareHEVCFactory(c.Decoder, caps.Decoder, detect.HasElement)
	caps.NativeHEVCSinkCaps = detect.HardwareHEVCSinkCaps(caps.NativeDecoder)
	caps.Freshness, caps.NativeRenditions = discoverRenditions(c.RTSPBase)
	if caps.NativeDecoder != "" {
		log.Printf("[wall] native HEVC factory %s found; automatic selection also requires explicit sink profile/level/tier support", caps.NativeDecoder)
	}
	{
		f, e := os.CreateTemp("", "eufy-wall-clock-")
		if e != nil {
			log.Fatal(e)
		}
		caps.ClockFile = f.Name()
		f.Close()
		defer os.Remove(caps.ClockFile)
	}
	if !caps.DecodedPacing {
		log.Printf("[wall] eufypacing plugin unavailable; using immediate display without stabilization")
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
	tiles = initialStreamResources(c, tiles, caps.Freshness)
	var sharedFiles []*os.File
	if caps.Sink == "planes" && (*dryRun || *printLayout) {
		caps.DRMFD = 3
	} // Describe the inherited descriptor without opening DRM in preview mode.
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
		if err := writeTileConfigs(plans); err != nil {
			log.Fatal(err)
		}
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
	for _, cap := range allFreshness(caps) {
		if !cap.Supported() {
			continue
		}
		go freshness.Poll(ctx, cap.ClockURL, caps.ClockFile, func(line string) { log.Printf("[freshness] %s", line) })
		if cap.Enforced() {
			log.Printf("[freshness] matching RTSP resource advertises an enforced %dms bridge-ingress deadline; camera capture age unverified", cap.MaxAgeMs)
		} else if caps.ObservePacing && caps.StampReader && caps.DecodedPacing && caps.Sink == "planes" {
			log.Printf("[freshness] observe-only bridge-ingress ages at decoded output and KMS sink return; no deadline enforcement or verified scanout; camera capture age unverified")
		} else {
			log.Printf("[freshness] observe metadata available but local presentation ages are unmeasured (observation plugin or planes sink unavailable)")
		}
		break
	}
	dots := latencydot.New(freshness.MonotonicNow)
	caps.DotSelect = func(index int, camera string, live, measured bool) {
		dots.Select(index, camera, live, measured)
		notices.Select(index, camera)
	}
	for _, tile := range tiles {
		caps.DotSelect(tile.Index, tile.Camera, tile.StillURL == "", pipeline.CanMeasureOutput(c, tile, caps))
	}
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
		marked := false
		caps.UnavailableMarker = func() {
			if marked {
				return
			}
			for _, t := range tiles {
				if err := background.MarkUnavailable(t.X, t.Y, t.W, t.H); err != nil {
					log.Printf("[freshness] unavailable caption: %v", err)
				}
			}
			marked = true
		}
		if hasEnforcedFreshness(caps.Freshness) {
			caps.UnavailableMarker()
		}
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			previous := map[int]latencydot.Color{}
			lastCaptions := map[int]string{}
			paint := func() {
				for index, text := range notices.Captions() {
					if old, seen := lastCaptions[index]; seen && old == text {
						continue
					}
					for _, tile := range tiles {
						if tile.Index == index {
							_ = background.PaintStatus(drmbackground.MotionRect{X: tile.X, Y: tile.Y, W: tile.W, H: tile.H}, text)
							lastCaptions[index] = text
							break
						}
					}
				}
				clock, _ := os.ReadFile(caps.ClockFile)
				for index, color := range dots.Colors(freshness.MonotonicNow(), clock) {
					if previous[index] == color {
						continue
					}
					for _, tile := range tiles {
						if tile.Index == index {
							_ = background.SetLatencyDot(drmbackground.MotionRect{X: tile.X, Y: tile.Y, W: tile.W, H: tile.H}, uint32(color))
							previous[index] = color
							break
						}
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
		}()
		info := background.Info()
		caps.ConnectorID = int(info.ConnectorID)
		if c.MotionFlash {
			caps.MotionBorderPixels = drmbackground.MotionBorderPixels
			rects := make(map[int]drmbackground.MotionRect, len(tiles))
			for _, t := range tiles {
				rects[t.Index] = drmbackground.MotionRect{X: t.X, Y: t.Y, W: t.W, H: t.H}
			}
			caps.MotionFlash = func(index int) {
				if rect, ok := rects[index]; ok {
					if err := background.FlashMotion(rect); err != nil {
						log.Printf("[motion] tile %d outline: %v", index, err)
					}
				}
			}
			caps.MotionClear = func(index int) {
				if rect, ok := rects[index]; ok {
					background.ClearMotionRect(rect)
				}
			}
			log.Printf("[motion] camera outlines enabled: five seconds of 250ms flashes; motion_flash: false disables them")
		}
		// Pin the video sinks to the same connector chosen for the background, even for output=auto.
		plans, err = pipeline.Plans(c, tiles, caps)
		if err != nil {
			log.Printf("[wall] %v", err)
			return
		}
		log.Printf("[wall] black image %dx%d %dbpp on primary plane %d framebuffer %d connector %d CRTC %d", info.Width, info.Height, info.BitsPerPixel, info.PlaneID, info.FramebufferID, info.ConnectorID, info.CRTCID)
	}
	mgr := supervisor.NewManager("gst-launch-1.0", c.Restart, func(name, line string) {
		dots.Observe(name, line)
		notices.Observe(name, line)
		if !strings.HasPrefix(line, "eufy-output ") {
			log.Printf("[gst %s] %s", name, line)
		}
	}, sharedFiles...)
	defer mgr.Stop() // Registered last: children stop before background.Close and shared file.Close.
	log.Printf("[wall] %d pipeline(s): %s", len(plans), planNames(plans))

	caps.ConnectionStatus = func(line string) {
		if line == "events: connected" {
			notices.Bridge("")
		} else {
			notices.Bridge(line)
		}
	}
	if caps.CaptionDir != "" {
		go runCaptionFiles(ctx, notices, caps.CaptionDir)
	}
	if autoBridge {
		caps.ResolveBridge = func() (string, error) {
			if !migrationAllowed(os.Getenv("EUFY_WALL_LAST_MIGRATION"), time.Now()) {
				return "", nil
			}
			return replacementBridge(c.RTSPBase, bridgeIdentity, probeBridge, discoverBridgePinned)
		}
	}
	if next := runDynamic(ctx, c, caps, tiles, mgr, plans); next != "" {
		log.Printf("[wall] verified same-service bridge address changed; rebinding playback and clock")
		_ = os.Setenv("EUFY_WALL_DISCOVERY_ID", bridgeIdentity)
		_ = os.Setenv("EUFY_WALL_RESOLVED_BRIDGE", next)
		_ = os.Setenv("EUFY_WALL_RESOLVED_API", wsclient.APIBase(next))
		_ = os.Setenv("EUFY_WALL_LAST_MIGRATION", fmt.Sprint(time.Now().Unix()))
		switchEndpoint = true
	}
	log.Printf("[wall] stopped")
}

// Camera identities and advertised stream aliases differ. Resolve automatic
// initial URLs before plans are built, just as the subsequent WS update does.
// Keep the configured authority, explicit manual URLs and packet-size policy.
func initialStreamResources(c *config.Config, tiles []layout.Placed, advertised map[string]freshness.Capability) []layout.Placed {
	out := append([]layout.Placed(nil), tiles...)
	base, err := url.Parse(c.RTSPBase)
	if err != nil {
		return out
	}
	prefix := strings.TrimRight(base.EscapedPath(), "/") + "/"
	for i, t := range out {
		if t.Index < 0 || t.Index >= len(c.Tiles) || c.Tiles[t.Index].URL != "" {
			continue
		}
		cap, ok := advertised[t.Camera]
		if !ok || !cap.Supported() {
			continue
		}
		target, err := url.Parse(cap.StreamURL)
		if err != nil || target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) || target.Fragment != "" || !strings.HasPrefix(target.EscapedPath(), prefix) {
			continue
		}
		key := strings.TrimPrefix(target.EscapedPath(), prefix)
		if key == "" {
			continue
		}
		if target.RawQuery != "" {
			key += "?" + target.RawQuery
		}
		candidate := c.TileURLForStream(c.Tiles[t.Index], key)
		if cap.BoundTo(candidate) {
			out[i].URL = candidate
		}
	}
	return out
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
	token, err := settingsAdminToken("")
	if err != nil {
		return err
	}
	return changeViewWithToken(rtspBase, camera, mode, token)
}

func changeViewWithToken(rtspBase, camera, mode, token string) error {
	base := wsclient.APIBase(rtspBase)
	if base == "" {
		return fmt.Errorf("cannot derive bridge API from rtsp_base %q", rtspBase)
	}
	return changeViewAt(base, camera, mode, token)
}

func changeViewAt(base, camera, mode string, tokens ...string) error {
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
	if len(tokens) > 0 && tokens[0] != "" {
		request.Header.Set("Authorization", "Bearer "+tokens[0])
	}
	result, err := client.Do(request)
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("shared view changes require bridge administrator access; use -settings-token-file FILE or EUFY_WALL_ADMIN_TOKEN, or settings --token-file FILE camera NAME dual_view JSON_VALUE")
	}
	if result.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(result.Body, 512))
		return fmt.Errorf("bridge HTTP %d: %s", result.StatusCode, strings.TrimSpace(string(body)))
	}
	var reply struct {
		Apply struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"apply"`
	}
	// Older servers may return an empty body and have no durable apply state.
	body, readErr := io.ReadAll(io.LimitReader(result.Body, 4096))
	if readErr != nil {
		return readErr
	}
	if len(body) > 0 && json.Unmarshal(body, &reply) == nil && reply.Apply.State == "failed" {
		return fmt.Errorf("view setting was saved but could not be applied: %s; inspect shared settings before retrying", reply.Apply.Error)
	}
	return nil
}

// discoverBridge accepts only a resolved _eufy-wall._tcp Avahi record. Its HTTP
// port is advertised as the service port and its RTSP port is in TXT.
func discoverBridge() (string, error) { return discoverBridgePinned("") }

// runDynamic follows /ws and keeps the running pipelines matching what each tile should be showing.
func runDynamic(ctx context.Context, c *config.Config, caps pipeline.Caps, tiles []layout.Placed, mgr *supervisor.Manager, static []pipeline.Plan) string {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	bridgeFailures := make(chan struct{}, 1)
	endpoint := wsclient.EventURL(c.RTSPBase)
	if endpoint == "" {
		log.Printf("[wall] cannot derive the event channel from rtsp_base %q — running a static wall", c.RTSPBase)
		if err := writeTileConfigs(static); err != nil {
			log.Printf("[wall] tile controls: %v", err)
			return ""
		}
		_ = mgr.Run(ctx, static)
		return ""
	}
	log.Printf("[wall] following events at %s", endpoint)

	store := wallstate.New()
	var mu sync.Mutex
	showing := map[int]string{}
	switchedAt := map[int]time.Time{}
	// What each tile is rendering: live video, or the camera's last still while it wakes.
	content := map[int]string{}
	lastShown := ""
	clockPolling := hasSupportedFreshness(allFreshness(caps))
	// Cameras this wall is keeping awake. A hold is bounded on the server, so showing one means
	// refreshing it; no longer showing one means releasing it, or a battery camera would be held awake
	// by a tile that stopped looking at it.
	holding := map[string]bool{}
	releaseHeld := func() {
		mu.Lock()
		held := make([]string, 0, len(holding))
		for camera := range holding {
			held = append(held, camera)
		}
		mu.Unlock()
		release, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		var wg sync.WaitGroup
		for _, camera := range held {
			wg.Add(1)
			go func(sn string) { defer wg.Done(); holdRequest(release, c.RTSPBase, http.MethodDelete, sn) }(camera)
		}
		wg.Wait()
	}
	defer func() { cancel(); releaseHeld() }()

	apply := func() {
		mu.Lock()
		// Until the server has told us what exists, render the wall exactly as configured. Resolving
		// against an empty store would blank every tile, so a display whose bridge is briefly
		// unreachable would go dark rather than keep showing the always-on cameras it can still pull.
		known := store.Known()
		// A reconnect can advertise capabilities even when startup API discovery failed.
		playbackCaps := caps
		playbackCaps.Freshness = make(map[string]freshness.Capability, len(caps.Freshness)+len(known))
		playbackCaps.NativeRenditions = make(map[string]rendition.Native, len(caps.NativeRenditions)+len(known))
		for sn, native := range caps.NativeRenditions {
			playbackCaps.NativeRenditions[sn] = native
		}
		for sn, cap := range caps.Freshness {
			playbackCaps.Freshness[sn] = cap
		}
		for _, cam := range known {
			if cam.NativeRendition != nil {
				playbackCaps.NativeRenditions[cam.SN] = *cam.NativeRendition
				if cam.NativeRendition.Freshness.Supported() && caps.NativeDecoder != "" && !clockPolling {
					clockPolling = true
					go freshness.Poll(ctx, cam.NativeRendition.Freshness.ClockURL, caps.ClockFile, func(line string) { log.Printf("[freshness] %s", line) })
				}
			} else {
				delete(playbackCaps.NativeRenditions, cam.SN)
			}
			if cam.Freshness.Supported() {
				playbackCaps.Freshness[cam.SN] = cam.Freshness
				if cam.Freshness.Supported() && !clockPolling {
					clockPolling = true
					go freshness.Poll(ctx, cam.Freshness.ClockURL, caps.ClockFile, func(line string) { log.Printf("[freshness] %s", line) })
				}
			}
		}
		caps = playbackCaps
		if hasEnforcedFreshness(playbackCaps.Freshness) && playbackCaps.UnavailableMarker != nil {
			playbackCaps.UnavailableMarker()
		}

		if len(known) == 0 {
			mu.Unlock()
			if len(playbackCaps.Freshness) > 0 {
				updatePlans(ctx, mgr, plansFor(c, playbackCaps, tiles, nil, nil, nil))
			} else {
				updatePlans(ctx, mgr, static)
			}
			return
		}
		sels := store.Resolve(c.Tiles, showing, switchedAt)
		now := time.Now()
		for _, sel := range sels {

			if showing[sel.TileIndex] != sel.Camera {
				if caps.MotionClear != nil {
					caps.MotionClear(sel.TileIndex)
				}
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
		updatePlans(ctx, mgr, plansFor(playbackConfig, playbackCaps, tiles, snapshot, kinds, func(sn string) string {
			if k, ok := keys[sn]; ok && k != "" {
				return k
			}
			return sn
		}))
	}

	apply()
	go wsclient.RunWithMotion(ctx, endpoint, store, apply, func(camera string) {
		if caps.MotionFlash == nil || ctx.Err() != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, index := range motionTileIndices(tiles, showing, camera) {
			caps.MotionFlash(index)
			log.Printf("[motion] flash tile=%d camera=%s", index, camera)
		}
	}, func(line string) {
		if caps.ConnectionStatus != nil {
			caps.ConnectionStatus(line)
		}
		if caps.ResolveBridge != nil && strings.HasPrefix(line, "events:") && line != "events: connected" {
			select {
			case bridgeFailures <- struct{}{}:
			default:
			}
		}
		log.Printf("[wall] %s", line)
	})

	// A hold is deliberately short-lived on the server, so a wall that is still showing a camera has to
	// say so. Refreshing well inside that window keeps the picture up without ever pinning a battery
	// camera awake: stop refreshing and it sleeps on its own.
	refresh := time.NewTicker(holdRefreshInterval)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			updatePlans(ctx, mgr, nil)
			return ""
		case <-bridgeFailures:
			if caps.ResolveBridge != nil {
				if next, err := caps.ResolveBridge(); err == nil && next != "" {
					return next
				}
			}
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
			if caps.DotSelect != nil {
				caps.DotSelect(t.Index, t.Camera, content[t.Index] != wallstate.ContentSnapshot, pipeline.CanMeasureOutput(c, t, caps))
			}
			live = append(live, t)
			continue
		}
		cam, ok := showing[t.Index]
		if !ok || cam == "" {
			if caps.DotSelect != nil {
				caps.DotSelect(t.Index, "", false, false)
			}
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
		if caps.DotSelect != nil {
			caps.DotSelect(t.Index, t.Camera, content[t.Index] != wallstate.ContentSnapshot, pipeline.CanMeasureOutput(c, t, caps))
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
	base := wsclient.APIBase(rtspBase)
	if base == "" {
		return
	}
	endpoint := base + "/hold/" + url.PathEscape(sn)
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

// Discovery is opt-in: explicit legacy RTSP feeds without advertised stamps remain usable.
func discoverRenditions(rtspBase string) (map[string]freshness.Capability, map[string]rendition.Native) {
	out := map[string]freshness.Capability{}
	native := map[string]rendition.Native{}
	base := wsclient.APIBase(rtspBase)
	if base == "" {
		return out, native
	}
	return discoverRenditionsAt(base)
}

func discoverRenditionsAt(base string) (map[string]freshness.Capability, map[string]rendition.Native) {
	out := map[string]freshness.Capability{}
	native := map[string]rendition.Native{}
	response, err := (&http.Client{Timeout: 4 * time.Second}).Get(base + "/api/cameras")
	if err != nil {
		return out, native
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return out, native
	}
	var cams []struct {
		SN              string               `json:"sn"`
		RTSP            string               `json:"rtsp"`
		Freshness       freshness.Capability `json:"freshness"`
		NativeRendition *rendition.Native    `json:"nativeRendition"`
	}
	if json.NewDecoder(response.Body).Decode(&cams) != nil {
		return out, native
	}
	for _, cam := range cams {
		if cam.NativeRendition != nil {
			cam.NativeRendition.Freshness.StreamURL = cam.NativeRendition.RTSP
			native[cam.SN] = *cam.NativeRendition
		}
		if cam.Freshness.Supported() {
			cam.Freshness.StreamURL = cam.RTSP
			out[cam.SN] = cam.Freshness
		}
	}
	return out, native
}

func hasEnforcedFreshness(caps map[string]freshness.Capability) bool {
	for _, cap := range caps {
		if cap.Enforced() {
			return true
		}
	}
	return false
}

// Candidates retain independent URL bindings. Polling the bridge clock once is
// sufficient for compatible and native resources from this same bridge epoch.
func allFreshness(caps pipeline.Caps) map[string]freshness.Capability {
	out := make(map[string]freshness.Capability)
	for sn, cap := range caps.Freshness {
		out[sn] = cap
	}
	if caps.NativeDecoder != "" {
		for sn, n := range caps.NativeRenditions {
			if n.Freshness.BoundTo(n.RTSP) {
				out[sn+"#native"] = n.Freshness
			}
		}
	}
	return out
}

func hasSupportedFreshness(caps map[string]freshness.Capability) bool {
	for _, c := range caps {
		if c.Supported() {
			return true
		}
	}
	return false
}
