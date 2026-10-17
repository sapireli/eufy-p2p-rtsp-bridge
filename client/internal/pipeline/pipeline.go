// Package pipeline renders the wall as gst-launch-1.0 arguments. Two sink strategies:
//
//	planes:     one kmssink per tile on its own DRM overlay plane (the Pi's HVS composites for free)
//	compositor: N decoders → compositor → one kmssink (CPU/GPU composite; works everywhere)
package pipeline

import (
	"fmt"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/freshness"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/rendition"
)

type Caps struct {
	NativeHEVCSinkCaps string
	NativeDecoder      string // actual probed hardware HEVC factory, empty disables selection
	NativeRenditions   map[string]rendition.Native
	decoderOverrides   map[string]string
	Decoder            string // v4l2 | va | software
	Sink               string // planes | compositor | window
	Screen             config.Screen
	// ConnectorID is the DRM connector this instance renders on (0 = let kmssink pick the first connected
	// output). Naming it is what keeps a two-monitor wall on the cheap path: each instance drives its own
	// CRTC with its own planes, instead of one pipeline compositing a framebuffer spanned across both.
	ConnectorID        int
	DRMFD              int // inherited shared DRM descriptor (0 = let kmssink open the device)
	Freshness          map[string]freshness.Capability
	ClockFile          string
	UnavailableMarker  func()
	MotionBorderPixels int // reserved primary-plane gutter; zero when motion feedback is disabled
	MotionFlash        func(tileIndex int)
	MotionClear        func(tileIndex int)
	StampReader        bool
	ObservePacing      bool   // installed pacing plugin accepts observe-only telemetry
	TimingDiagnostics  string // optional bounded private per-frame phase metadata
	DecodedPacing      bool   // installed eufypacing raw-video clock mapper
}

// kmssinkArgs is the sink plus the output it renders on, shared by every sink strategy.
func (c Caps) kmssinkArgs(extra ...string) []string {
	args := append([]string{"kmssink"}, extra...)
	if c.DRMFD > 0 {
		args = append(args, fmt.Sprintf("fd=%d", c.DRMFD))
		// Tile processes share one DRM event queue. Each updates only its overlay;
		// independent page flips would compete for the same CRTC and consume sibling events.
		args = append(args, "skip-vsync=true")
	}
	if c.ConnectorID > 0 {
		args = append(args, fmt.Sprintf("connector-id=%d", c.ConnectorID))
	}
	return args
}

// Decoder element per hardware family and codec. A wall can mix the two: an eufy HomeBase composes some
// cameras as H.264 and others as HEVC, and where the bridge passes a camera through untranscoded the tile
// has to decode what the camera actually sends.
var decoders = map[string]map[string]string{
	"v4l2":     {"h264": "v4l2h264dec", "h265": "v4l2h265dec"},
	"va":       {"h264": "vah264dec", "h265": "vah265dec"},
	"software": {"h264": "avdec_h264", "h265": "avdec_h265"},
}

// RTP depayloader + parser per codec; they are codec-specific in the same way the decoder is.
var depayParse = map[string][2]string{
	"h264": {"rtph264depay", "h264parse"},
	"h265": {"rtph265depay", "h265parse"},
}

// codecOf defaults a tile with no codec set to H.264, which is what a transcoding bridge serves.
func codecOf(t layout.Placed) string {
	if t.Codec == "" || t.Codec == "auto" {
		return "h264"
	}
	return t.Codec
}

// Plan is one process that renders part (or all) of the wall.
type Plan struct {
	// Name identifies it in logs and, more importantly, identifies it across an Update: a plan whose
	// args are unchanged keeps running rather than being restarted.
	Name         string
	Args         []string
	FallbackArgs []string
}

// Plans returns the processes this wall needs.
//
// With sink=planes each tile owns a DRM overlay plane and is genuinely independent, so it gets its own
// process: one camera dropping out then restarts one tile instead of every tile, and a tile can be
// started, stopped or repointed on its own — which is what a wall with motion tiles needs.
//
// A compositor mixes every tile into one frame, so its tiles cannot be split; that wall is one process
// and changing any tile restarts all of them. This is the honest trade of the two sinks, not a gap.
func Plans(c *config.Config, tiles []layout.Placed, caps Caps) ([]Plan, error) {
	if caps.Sink != "planes" {
		args, err := Build(c, tiles, caps)
		if err != nil {
			return nil, err
		}
		fallback, err := fallbackArgs(c, tiles, caps)
		if err != nil {
			return nil, err
		}
		return []Plan{{Name: "wall", Args: args, FallbackArgs: fallback}}, nil
	}
	out := make([]Plan, 0, len(tiles))
	for i, t := range tiles {
		args, err := Build(c, []layout.Placed{t}, caps)
		if err != nil {
			return nil, err
		}
		name := t.Camera
		if name == "" {
			name = fmt.Sprintf("tile%d", i)
		}
		fallback, err := fallbackArgs(c, []layout.Placed{t}, caps)
		if err != nil {
			return nil, err
		}
		out = append(out, Plan{Name: name, Args: args, FallbackArgs: fallback})
	}
	return out, nil
}

func Build(c *config.Config, tiles []layout.Placed, caps Caps) ([]string, error) {
	tiles, caps, _ = selectNative(c, tiles, caps)
	family, ok := decoders[caps.Decoder]
	if !ok {
		return nil, fmt.Errorf("pipeline: unknown decoder %q", caps.Decoder)
	}
	for _, t := range tiles {
		if caps.TimingDiagnostics != "" {
			cap, ok := caps.Freshness[t.Camera]
			if !ok || !cap.Supported() || !cap.BoundTo(t.URL) || caps.ClockFile == "" || !caps.ObservePacing || !caps.StampReader || !caps.DecodedPacing || caps.Sink != "planes" || t.StillURL != "" {
				return nil, fmt.Errorf("pipeline: timing diagnostics unavailable for tile %d: advertised=%t supported=%t matching=%t observer=%t stamp=%t pacing=%t clock=%t planes=%t still=%t", t.Index, ok, cap.Supported(), cap.BoundTo(t.URL), caps.ObservePacing, caps.StampReader, caps.DecodedPacing, caps.ClockFile != "", caps.Sink == "planes", t.StillURL != "")
			}
		}
		if cap, strict := caps.Freshness[t.Camera]; strict && cap.Enforced() && cap.BoundTo(t.URL) && t.StillURL == "" {
			if !caps.DecodedPacing || !caps.StampReader || caps.ClockFile == "" || caps.Sink != "planes" || caps.DRMFD <= 0 {
				return nil, fmt.Errorf("pipeline: advertised strict freshness requires stamp reader, clock mapping and independent shared DRM planes")
			}
		}
		if t.StillURL != "" {
			continue // a JPEG needs no video decoder
		}
		if _, ok := family[codecOf(t)]; !ok && caps.decoderOverrides[t.Camera] == "" {
			return nil, fmt.Errorf("pipeline: decoder %q cannot decode %s (tile %s)", caps.Decoder, codecOf(t), t.Camera)
		}
	}
	if caps.Sink == "planes" {
		// Each tile takes the plane at its OWN index, so this holds whether the wall is built as one
		// process or split into one per tile.
		need := 0
		for _, t := range tiles {
			if t.Index+1 > need {
				need = t.Index + 1
			}
		}
		if len(c.Planes) < need {
			return nil, fmt.Errorf("pipeline: sink=planes needs %d plane ids in `planes:` (have %d) — list overlay planes with `modetest -M vc4 -p`", need, len(c.Planes))
		}
	}
	args := []string{"-e"}
	src := func(i int, t layout.Placed) []string {
		if t.StillURL != "" {
			// One JPEG held on screen as a video stream. `imagefreeze` repeats the single frame forever, so
			// this costs nothing once it has decoded. If the bridge has no thumbnail yet the request 404s
			// and the process exits; the supervisor's backoff retries it, which is also how the tile picks
			// up a newer still after the next event.
			return []string{
				"souphttpsrc", "location=" + t.StillURL, "is-live=false", fmt.Sprintf("name=src%d", i),
				"!", "jpegdec", "!", "imagefreeze", "!", "videoconvert",
			}
		}
		codec := codecOf(t)
		dp := depayParse[codec]
		args := []string{"rtspsrc", "location=" + t.URL, fmt.Sprintf("latency=%d", c.Latency), "protocols=tcp", fmt.Sprintf("name=src%d", i)}
		if caps.DecodedPacing {
			// Preserve media RTP cadence. The raw-video mapper owns drift compensation;
			// receive-time timestamping would turn delivery bursts into presentation jitter.
			args = append(args, "buffer-mode=none")
		}
		trace := func(phase string) []string {
			return []string{"!", "eufytrace", fmt.Sprintf("channel=%d", t.Index+1), "phase=" + phase, "clock-file=" + caps.ClockFile, "diagnostic-file=" + caps.TimingDiagnostics}
		}
		args = append(args, "!", dp[0])
		if caps.TimingDiagnostics != "" {
			args = append(args, trace("depay-au")...)
		}
		args = append(args, "!", dp[1])
		if caps.TimingDiagnostics != "" {
			args = append(args, trace("parsed-au")...)
		}
		cap, advertised := caps.Freshness[t.Camera]
		strict := advertised && cap.Enforced() && cap.BoundTo(t.URL)
		observe := advertised && cap.Supported() && !cap.Enforced() && cap.BoundTo(t.URL) && caps.ObservePacing && caps.DecodedPacing && caps.StampReader && caps.ClockFile != "" && caps.Sink == "planes"
		if strict || observe {
			args = append(args, "!", "video/x-"+codec+",stream-format=byte-stream,alignment=au", "!", "eufystamp", fmt.Sprintf("channel=%d", t.Index+1))
		}
		decoder := family[codec]
		if override := caps.decoderOverrides[t.Camera]; override != "" {
			decoder = override
		}
		args = append(args, "!", decoder)
		if caps.TimingDiagnostics != "" {
			args = append(args, trace("decoder-out")...)
		}
		args = append(args, "!", "watchdog", "timeout=15000")
		if caps.DecodedPacing {
			// Drop only decoded frames: every compressed reference still reaches the decoder.
			args = append(args, "!", "queue", "max-size-buffers=1", "max-size-bytes=0", "max-size-time=0", "leaky=downstream")
			if caps.TimingDiagnostics != "" {
				args = append(args, trace("post-queue")...)
			}
			if caps.Sink == "planes" {
				args = append(args, "!", "eufypacing")
				if caps.TimingDiagnostics != "" {
					args = append(args, "diagnostic-file="+caps.TimingDiagnostics)
				}
				if strict || observe {
					args = append(args, fmt.Sprintf("channel=%d", t.Index+1), "clock-file="+caps.ClockFile, fmt.Sprintf("max-age-ms=%d", cap.MaxAgeMs))
					if strict {
						args = append(args, fmt.Sprintf("drm-fd=%d", caps.DRMFD), fmt.Sprintf("plane-id=%d", c.Planes[t.Index]))
					} else {
						args = append(args, "observe-only=true")
					}
				}
				args = append(args, "!", "watchdog", "timeout=15000")
			}
		}
		return args
	}
	clockArgs := []string{"sync=false"}
	if caps.DecodedPacing {
		clockArgs = []string{"sync=true", "max-lateness=67000000", "processing-deadline=0"}
	}
	switch caps.Sink {
	case "planes":
		for i, t := range tiles {
			border := caps.MotionBorderPixels
			if border < 0 || 2*border >= t.W || 2*border >= t.H {
				return nil, fmt.Errorf("motion border does not fit tile %d", t.Index)
			}
			args = append(args, src(i, t)...)
			args = append(args, "!")
			tileClockArgs := clockArgs
			if cap, strict := caps.Freshness[t.Camera]; strict && cap.Enforced() && cap.BoundTo(t.URL) && t.StillURL == "" {
				tileClockArgs = []string{"sync=false", "processing-deadline=0"}
			} // Strict pacer owns wait and synchronized plane updates.
			args = append(args, caps.kmssinkArgs(append([]string{fmt.Sprintf("name=sink%d", t.Index), fmt.Sprintf("plane-id=%d", c.Planes[t.Index]), fmt.Sprintf("render-rectangle=<%d,%d,%d,%d>", t.X+border, t.Y+border, t.W-2*border, t.H-2*border)}, tileClockArgs...)...)...)
		}
	case "compositor", "window":
		for i, t := range tiles {
			args = append(args, src(i, t)...)
			args = append(args, "!", "videoconvert", "!", fmt.Sprintf("mix.sink_%d", i))
		}
		args = append(args, "compositor", "name=mix", "background=black")
		for i, t := range tiles {
			args = append(args,
				fmt.Sprintf("sink_%d::xpos=%d", i, t.X), fmt.Sprintf("sink_%d::ypos=%d", i, t.Y),
				fmt.Sprintf("sink_%d::width=%d", i, t.W), fmt.Sprintf("sink_%d::height=%d", i, t.H),
				fmt.Sprintf("sink_%d::sizing-policy=keep-aspect-ratio", i))
		}
		args = append(args, "!", fmt.Sprintf("video/x-raw,width=%d,height=%d", caps.Screen.Width, caps.Screen.Height))
		if caps.DecodedPacing {
			// Rebase the final composite clock, not each input plus the final sink.
			args = append(args, "!", "queue", "max-size-buffers=1", "max-size-bytes=0", "max-size-time=0", "leaky=downstream", "!", "eufypacing", "!", "watchdog", "timeout=15000")
		}
		if caps.Sink == "window" {
			args = append(args, "!", "autovideosink", clockArgs[0])
		} else {
			args = append(args, "!")
			args = append(args, caps.kmssinkArgs(clockArgs...)...)
		}
	default:
		return nil, fmt.Errorf("pipeline: unknown sink %q", caps.Sink)
	}
	return args, nil
}

// String joins args for logging; the supervisor execs the slice directly (no shell).
func String(args []string) string { return strings.Join(args, " ") }
