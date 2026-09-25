package gstnative

import (
	"encoding/hex"
	"errors"
	"fmt"
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
	StatusPath   string
	ConfigSHA256 string
	sink         string
	source       func(layout.Placed) (string, error)
}

type frameCounter struct {
	frames atomic.Uint64
	last   atomic.Int64
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
	tile         layout.Placed
	queueSink    uintptr
	source       uintptr
	sourcePad    uintptr
	probe        uint64
	callback     func(uintptr, uintptr, uintptr) int32
	counter      *frameCounter
	kind         string
	expectedLive bool
	key          string
	generation   uint64
	state        string
	err          string
	installed    time.Time
	nextRetry    time.Time
	// The initial black source is parsed as two top-level elements.
	initialSource uintptr
	initialCaps   uintptr
}

// Renderer owns one GStreamer pipeline with stable queue/compositor pads. Update replaces only
// changed source bins. GStreamer callbacks only touch atomics; all native mutations hold mu.
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
	if caps.Screen.Width <= 0 || caps.Screen.Height <= 0 || len(tiles) > 1024 {
		return nil, errors.New("native renderer has invalid screen or tile count")
	}
	if opts.StatusPath == "" {
		opts.StatusPath = DefaultStatusPath
	}
	if _, err := hex.DecodeString(opts.ConfigSHA256); len(opts.ConfigSHA256) != 64 || err != nil {
		return nil, errors.New("native renderer requires active config SHA256")
	}
	if opts.source == nil {
		opts.source = func(tile layout.Placed) (string, error) { return sourceDescription(tile, caps, c.Latency) }
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
	desc, order, err := pipelineDescription(tiles, caps, opts.sink)
	if err != nil {
		return nil, err
	}
	p, err := a.parsed(desc, false)
	if err != nil {
		return nil, err
	}
	r := &Renderer{
		api: a, pipeline: p, slots: make(map[string]*slot, len(tiles)), order: order,
		caps: caps, latency: c.Latency, options: opts, started: time.Now().UTC(),
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
	// Initial blanks render immediately. Sources are installed after the compositor is playing
	// so a slow RTSP handshake never delays other tiles or the black output frame.
	if err := r.Update(tiles); err != nil {
		r.closeNative()
		return nil, err
	}
	go r.monitor()
	return r, nil
}

func pipelineDescription(tiles []layout.Placed, caps pipeline.Caps, sink string) (string, []string, error) {
	parts := []string{
		"videotestsrc is-live=true pattern=black ! video/x-raw,format=I420,width=1,height=1,framerate=1/1 ! mix.sink_0",
	}
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
			"videotestsrc name=blank_%d is-live=true pattern=black ! capsfilter name=blankcaps_%d caps=\"video/x-raw,format=I420,width=1,height=1,framerate=1/1\" ! queue name=entry_%d max-size-buffers=2 leaky=downstream ! mix.sink_%d",
			i, i, i, i+1))
	}
	mix := fmt.Sprintf("compositor name=mix background=black ignore-inactive-pads=true sink_0::xpos=0 sink_0::ypos=0 sink_0::width=%d sink_0::height=%d", caps.Screen.Width, caps.Screen.Height)
	for i, tile := range tiles {
		mix += fmt.Sprintf(" sink_%d::xpos=%d sink_%d::ypos=%d sink_%d::width=%d sink_%d::height=%d sink_%d::sizing-policy=keep-aspect-ratio",
			i+1, tile.X, i+1, tile.Y, i+1, tile.W, i+1, tile.H, i+1)
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
		entry := a.byName(r.pipeline, fmt.Sprintf("entry_%d", i))
		if entry == 0 {
			return fmt.Errorf("native renderer missing entry queue %d", i)
		}
		sink := a.staticPad(entry, "sink")
		a.objectUnref(entry)
		initial := a.byName(r.pipeline, fmt.Sprintf("blank_%d", i))
		initialCaps := a.byName(r.pipeline, fmt.Sprintf("blankcaps_%d", i))
		if sink == 0 || initial == 0 || initialCaps == 0 {
			if sink != 0 {
				a.objectUnref(sink)
			}
			if initial != 0 {
				a.objectUnref(initial)
			}
			if initialCaps != 0 {
				a.objectUnref(initialCaps)
			}
			return fmt.Errorf("native renderer missing initial source %d", i)
		}
		r.slots[tileID(tile)] = &slot{
			tile: tile, queueSink: sink, initialSource: initial, initialCaps: initialCaps,
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
