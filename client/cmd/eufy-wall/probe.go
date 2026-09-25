package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/pipeline"
)

const frameProbeTimeout = 15 * time.Second

// probeFrames checks actual decode progress on this host without taking over its display sink.
func probeFrames(ctx context.Context, c *config.Config, serial string, out io.Writer) error {
	if c.BridgeURL == "" {
		return fmt.Errorf("frame probe needs bridge_url; add the bridge HTTP origin to this config")
	}
	cameras, err := preflightClientRemote(ctx, c)
	if err != nil {
		return err
	}
	copy := *c
	copy.Sink = "window" // Only decoder detection matters; the probe uses fakesink.
	caps, err := detect.Resolve(&copy, detect.HasElement, detect.FileExists)
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, frameProbeTimeout)
	defer cancel()
	codec, err := probeClientCameraWithCaps(probeCtx, c, serial, cameras, caps, runDecodedProbe)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "decoded 2 frames from %s (%s) within %s\n", serial, codec, frameProbeTimeout)
	return err
}

func probeClientCamera(ctx context.Context, c *config.Config, serial string, cameras []setupCamera, decoder string, run func(context.Context, []string) error) (codec string, err error) {
	return probeClientCameraWithCaps(ctx, c, serial, cameras, pipeline.Caps{Decoder: decoder}, run)
}

func probeClientCameraWithCaps(ctx context.Context, c *config.Config, serial string, cameras []setupCamera, caps pipeline.Caps, run func(context.Context, []string) error) (codec string, err error) {
	var camera *setupCamera
	for i := range cameras {
		if cameras[i].SN == serial {
			camera = &cameras[i]
			break
		}
	}
	if camera == nil {
		return "", fmt.Errorf("camera %q is absent from bridge inventory", serial)
	}
	if camera.StreamKey == "" {
		return "", fmt.Errorf("camera %q has no RTSP stream key", serial)
	}
	codec = camera.Codec
	if codec == "" {
		if tile := c.TileFor(serial); tile != nil {
			codec = tile.Codec
		}
	}
	if codec == "" {
		codec = "h264"
	}
	if camera.Mode != "always" {
		owner := fmt.Sprintf("wall-probe-%d", os.Getpid())
		endpoint := strings.TrimRight(c.BridgeURL, "/") + "/hold/" + url.PathEscape(serial) + "?" + url.Values{"owner": {owner}, "seconds": {"20"}}.Encode()
		if err := probeHold(ctx, http.MethodPost, endpoint); err != nil {
			return "", fmt.Errorf("cannot wake %s for a bounded probe: %w", serial, err)
		}
		defer func() {
			release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if releaseErr := probeHold(release, http.MethodDelete, endpoint); releaseErr != nil && err == nil {
				err = fmt.Errorf("decoded frames, but hold release failed: %w (server hold expires after 20 seconds)", releaseErr)
			}
		}()
	}
	// Inventory may be cold or stale when a camera changed codec. Give each codec a share of the
	// caller's deadline; otherwise the first stalled attempt could consume the entire probe window.
	candidates := []string{codec}
	if codec == "h264" {
		candidates = append(candidates, "h265")
	} else if codec == "h265" {
		candidates = append(candidates, "h264")
	}
	var failures []string
	for i, candidate := range candidates {
		args, buildErr := pipeline.ProbeArgsForCaps(c, caps, candidate, c.TileURL(config.Tile{Camera: camera.StreamKey}))
		if buildErr != nil {
			failures = append(failures, candidate+": "+buildErr.Error())
			continue
		}
		attemptCtx := ctx
		cancel := func() {}
		if deadline, ok := ctx.Deadline(); ok && i+1 < len(candidates) {
			remaining := time.Until(deadline)
			attemptCtx, cancel = context.WithTimeout(ctx, remaining/time.Duration(len(candidates)-i))
		}
		runErr := run(attemptCtx, args)
		cancel()
		if runErr == nil {
			return candidate, nil
		}
		failures = append(failures, candidate+": "+runErr.Error())
	}
	return "", fmt.Errorf("%s: no decoded frame progress (%s)", serial, strings.Join(failures, "; "))
}

func probeHold(ctx context.Context, method, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

type boundedProbeOutput struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *boundedProbeOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := 64*1024 - b.buf.Len(); room > 0 {
		_, _ = b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func runDecodedProbe(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", args...)
	output := &boundedProbeOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("probe deadline expired; check camera wake, RTSP, codec, and decoder: %w", ctx.Err())
		}
		return fmt.Errorf("gst-launch-1.0: %w: %s", err, strings.TrimSpace(output.buf.String()))
	}
	return nil
}
