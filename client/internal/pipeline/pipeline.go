// Package pipeline renders the wall as gst-launch-1.0 arguments. Two sink strategies:
//
//	planes:     one kmssink per tile on its own DRM overlay plane (the Pi's HVS composites for free)
//	compositor: N decoders → compositor → one kmssink (CPU/GPU composite; works everywhere)
package pipeline

import (
	"fmt"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
)

type Caps struct {
	Decoder string // v4l2 | va | software
	Sink    string // planes | compositor | window
	Screen  config.Screen
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
	if t.Codec == "" {
		return "h264"
	}
	return t.Codec
}

func Build(c *config.Config, tiles []layout.Placed, caps Caps) ([]string, error) {
	family, ok := decoders[caps.Decoder]
	if !ok {
		return nil, fmt.Errorf("pipeline: unknown decoder %q", caps.Decoder)
	}
	for _, t := range tiles {
		if _, ok := family[codecOf(t)]; !ok {
			return nil, fmt.Errorf("pipeline: decoder %q cannot decode %s (tile %s)", caps.Decoder, codecOf(t), t.Camera)
		}
	}
	if caps.Sink == "planes" && len(c.Planes) < len(tiles) {
		return nil, fmt.Errorf("pipeline: sink=planes needs %d plane ids in `planes:` (have %d) — list overlay planes with `modetest -M vc4 -p`", len(tiles), len(c.Planes))
	}
	args := []string{"-e"}
	src := func(i int, t layout.Placed) []string {
		codec := codecOf(t)
		dp := depayParse[codec]
		return []string{
			"rtspsrc", "location=" + t.URL, fmt.Sprintf("latency=%d", c.Latency), "protocols=tcp", fmt.Sprintf("name=src%d", i),
			"!", dp[0], "!", dp[1], "!", family[codec],
		}
	}
	switch caps.Sink {
	case "planes":
		for i, t := range tiles {
			args = append(args, src(i, t)...)
			args = append(args, "!", "kmssink", fmt.Sprintf("name=sink%d", i), fmt.Sprintf("plane-id=%d", c.Planes[i]),
				fmt.Sprintf("render-rectangle=<%d,%d,%d,%d>", t.X, t.Y, t.W, t.H), "force-aspect-ratio=true", "sync=false")
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
		if caps.Sink == "window" {
			args = append(args, "!", "autovideosink", "sync=false")
		} else {
			args = append(args, "!", "kmssink", "sync=false")
		}
	default:
		return nil, fmt.Errorf("pipeline: unknown sink %q", caps.Sink)
	}
	return args, nil
}

// String joins args for logging; the supervisor execs the slice directly (no shell).
func String(args []string) string { return strings.Join(args, " ") }
