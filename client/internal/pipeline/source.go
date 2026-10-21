package pipeline

import (
	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"fmt"
)

func sourceArgs(c *config.Config, t layout.Placed, caps Caps, i int) []string {
	family := decoders[caps.Decoder]
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
