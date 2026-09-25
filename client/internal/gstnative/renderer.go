package gstnative

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

// Options identify the active config and the local status file. The private fields let package
// tests exercise live switching with synthetic sources and a headless sink.
type Options struct {
	StatusPath         string
	ConfigSHA256       string
	Initial            []layout.Placed
	sink               string
	source             func(layout.Placed) (string, error)
	sourceWithDecoder  func(layout.Placed, string) (string, error)
	stallAfter         time.Duration
	retryAfter         time.Duration
	startupAfter       time.Duration
	startupOutputAfter time.Duration
	monitorEvery       time.Duration
	outputStallAfter   time.Duration
	stillRefresh       time.Duration
	api                *gstAPI
}

type frameCounter struct {
	frames atomic.Uint64
	last   atomic.Int64
	// Compressed buffers seen at the decoder when the last decoded frame arrived.
	inputAtFrame atomic.Uint64
}

func (c *frameCounter) tick() {
	c.frames.Add(1)
	c.last.Store(time.Now().UnixNano())
}

func (c *frameCounter) lastTime() *time.Time {
	n := c.last.Load()
	if n == 0 {
		return nil
	}
	t := time.Unix(0, n).UTC()
	return &t
}

type slot struct {
	tile             layout.Placed
	feed             uintptr
	blackBuffer      uintptr
	lastFrame        uintptr
	blackStop        chan struct{}
	blackDone        chan struct{}
	showLive         atomic.Bool
	lastLivePush     atomic.Int64
	feedMu           sync.Mutex
	source           uintptr
	sourceBus        uintptr
	sourceSink       uintptr
	decoderPad       uintptr
	decoderProbe     uint64
	decoderCallback  func(uintptr, uintptr, uintptr) int32
	compressed       *compressedCounter
	pumpStop         chan struct{}
	pumpDone         chan struct{}
	counter          *frameCounter
	kind             string
	expectedLive     bool
	key              string
	generation       uint64
	state            string
	decoder          string
	softwareFallback bool
	err              string
	installed        time.Time
	nextRetry        time.Time
}

// Renderer owns one GStreamer pipeline with stable appsrc/queue/compositor pads.
// Update replaces only changed source pipelines. Native graph mutations hold mu.
type Renderer struct {
	mu             sync.Mutex
	api            *gstAPI
	pipeline       uintptr
	bus            uintptr
	outputPad      uintptr
	outputProbe    uint64
	outputCallback func(uintptr, uintptr, uintptr) int32
	output         frameCounter
	slots          map[string]*slot
	order          []string
	caps           pipeline.Caps
	latency        int
	options        Options
	nativeSource   bool
	started        time.Time
	closed         bool
	stop           chan struct{}
	done           chan struct{}
	errors         chan error
}

func New(c *config.Config, tiles []layout.Placed, caps pipeline.Caps, opts Options) (*Renderer, error) {
	if c == nil || (caps.Sink != "compositor" && caps.Sink != "window") {
		return nil, errors.New("native renderer requires config and compositor or window sink")
	}
	if caps.Screen.Width <= 0 || caps.Screen.Height <= 0 || len(tiles) == 0 || len(tiles) > 1024 {
		return nil, errors.New("native renderer has invalid screen or tile count")
	}
	if opts.StatusPath == "" {
		opts.StatusPath = DefaultStatusPath
	}
	if err := os.MkdirAll(filepath.Dir(opts.StatusPath), 0700); err != nil {
		return nil, fmt.Errorf("native renderer status directory: %w", err)
	}
	if _, err := hex.DecodeString(opts.ConfigSHA256); len(opts.ConfigSHA256) != 64 || err != nil {
		return nil, errors.New("native renderer requires active config SHA256")
	}
	nativeSource := opts.source == nil || opts.sourceWithDecoder != nil
	if opts.source == nil {
		opts.source = func(tile layout.Placed) (string, error) { return sourceDescription(tile, caps, c.Latency) }
	}
	if opts.stallAfter <= 0 {
		opts.stallAfter = 5 * time.Second
	}
	if opts.retryAfter <= 0 {
		opts.retryAfter = 10 * time.Second
	}
	if opts.startupAfter <= 0 {
		opts.startupAfter = 10 * time.Second
	}
	if opts.startupOutputAfter <= 0 {
		opts.startupOutputAfter = 5 * time.Second
	}
	if opts.monitorEvery <= 0 {
		opts.monitorEvery = 2 * time.Second
	}
	if opts.outputStallAfter <= 0 {
		opts.outputStallAfter = 20 * time.Second
	}
	if opts.stillRefresh <= 0 {
		opts.stillRefresh = 30 * time.Second
	}
	if opts.sink == "" {
		if caps.Sink == "window" {
			opts.sink = "autovideosink sync=false"
		} else {
			opts.sink = "kmssink sync=false"
			if caps.ConnectorID > 0 {
				opts.sink += fmt.Sprintf(" connector-id=%d", caps.ConnectorID)
			}
		}
	}
	a, err := load()
	if err != nil {
		return nil, err
	}
	if opts.api != nil {
		a = opts.api
	}
	desc, order, err := pipelineDescription(tiles, caps, opts.sink)
	if err != nil {
		return nil, err
	}
	p, err := a.parsed(desc)
	if err != nil {
		return nil, err
	}
	r := &Renderer{
		api: a, pipeline: p, slots: make(map[string]*slot, len(tiles)), order: order,
		caps: caps, latency: c.Latency, options: opts, nativeSource: nativeSource, started: time.Now().UTC(),
		stop: make(chan struct{}), done: make(chan struct{}), errors: make(chan error, 1),
	}
	if err := r.prepare(tiles); err != nil {
		r.closeNative()
		return nil, err
	}
	if a.setState(p, statePlaying) == 0 {
		r.closeNative()
		return nil, errors.New("native renderer failed to start GStreamer pipeline")
	}
	r.startBlackPumps()
	// Permanent feeds render black immediately. Camera pipelines start after the
	// compositor so a slow RTSP handshake cannot delay other tiles.
	if err := r.startOutput(); err != nil {
		r.closeNative()
		return nil, err
	}
	initial := opts.Initial
	if initial == nil {
		initial = tiles
	}
	if err := r.Update(initial); err != nil {
		r.closeNative()
		return nil, err
	}
	go r.monitor()
	return r, nil
}

func pipelineDescription(tiles []layout.Placed, caps pipeline.Caps, sink string) (string, []string, error) {
	parts := []string{}
	order := make([]string, 0, len(tiles))
	seen := map[string]bool{}
	for i, tile := range tiles {
		id := tileID(tile)
		if id == "" || seen[id] || tile.W <= 0 || tile.H <= 0 ||
			tile.X < 0 || tile.Y < 0 || tile.X+tile.W > caps.Screen.Width || tile.Y+tile.H > caps.Screen.Height {
			return "", nil, fmt.Errorf("native renderer: invalid or duplicate tile %q", id)
		}
		seen[id] = true
		order = append(order, id)
		parts = append(parts, fmt.Sprintf(
			"appsrc name=feed_%d is-live=true format=time do-timestamp=true block=false max-buffers=2 leaky-type=downstream max-bytes=%d caps=\"video/x-raw,format=I420,width=%d,height=%d,framerate=15/1\" ! queue name=entry_%d max-size-buffers=2 leaky=downstream ! mix.sink_%d",
			i, tile.W*tile.H*4, tile.W, tile.H, i, i))
	}
	mix := "compositor name=mix background=black ignore-inactive-pads=true"
	for i, tile := range tiles {
		mix += fmt.Sprintf(" sink_%d::xpos=%d sink_%d::ypos=%d sink_%d::width=%d sink_%d::height=%d sink_%d::sizing-policy=keep-aspect-ratio",
			i, tile.X, i, tile.Y, i, tile.W, i, tile.H, i)
	}
	parts = append(parts, mix+fmt.Sprintf(" ! video/x-raw,width=%d,height=%d ! identity name=output_probe ! %s", caps.Screen.Width, caps.Screen.Height, sink))
	return strings.Join(parts, " "), order, nil
}

func (r *Renderer) prepare(tiles []layout.Placed) error {
	a := r.api
	r.bus = a.getBus(r.pipeline)
	if r.bus == 0 {
		return errors.New("native renderer pipeline has no bus")
	}
	output := a.byName(r.pipeline, "output_probe")
	if output == 0 {
		return errors.New("native renderer has no output probe")
	}
	r.outputPad = a.staticPad(output, "src")
	a.objectUnref(output)
	if r.outputPad == 0 {
		return errors.New("native renderer output has no source pad")
	}
	r.outputCallback = func(_, _, _ uintptr) int32 { r.output.tick(); return probeOK }
	r.outputProbe = a.addProbe(r.outputPad, probeBuffer, purego.NewCallback(r.outputCallback), 0, 0)
	if r.outputProbe == 0 {
		return errors.New("native renderer could not count output frames")
	}
	for i, tile := range tiles {
		feed := a.byName(r.pipeline, fmt.Sprintf("feed_%d", i))
		if feed == 0 {
			return fmt.Errorf("native renderer missing feed %d", i)
		}
		black, err := makeBlackBuffer(a, tile.W, tile.H)
		if err != nil {
			a.objectUnref(feed)
			return fmt.Errorf("native renderer black tile %d: %w", i, err)
		}
		r.slots[tileID(tile)] = &slot{
			tile: tile, feed: feed, blackBuffer: black,
			kind: "black", key: "black", state: "playing",
		}
	}
	return nil
}

func sourceKey(tile layout.Placed) string {
	return sourceKind(tile) + "\x00" + tile.URL + "\x00" + tile.StillURL + "\x00" + tile.Codec
}

// Update applies a complete active-tile selection. Missing slots become black. Geometry and
// slot identity are fixed until the config changes, but a source can change without restarting
// the pipeline or any other slot.
func (r *Renderer) Update(active []layout.Placed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("native renderer is closed")
	}
	selected := make(map[string]layout.Placed, len(active))
	for _, tile := range active {
		id := tileID(tile)
		base, ok := r.slots[id]
		_, duplicate := selected[id]
		if !ok || duplicate || tile.X != base.tile.X || tile.Y != base.tile.Y ||
			tile.W != base.tile.W || tile.H != base.tile.H {
			return fmt.Errorf("native renderer: unknown, duplicate or moved tile %q", id)
		}
		selected[id] = tile
	}
	var first error
	for _, id := range r.order {
		tile, ok := selected[id]
		if !ok {
			tile = r.slots[id].tile
			tile.URL, tile.StillURL = "", ""
		}
		if err := r.switchSource(r.slots[id], tile); err != nil && first == nil {
			first = err
		}
	}
	if err := r.writeStatusLocked(); err != nil && first == nil {
		first = err
	}
	return first
}
