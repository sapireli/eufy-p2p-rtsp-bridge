package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/pipeline"
)

// Plane pipelines have no frame counter yet. Probe each fixed live source with the exact decoder
// and codec the running plane pipeline uses. This establishes decode progress, not HDMI visibility.
func probePlaneFrames(c *config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cameras, err := preflightClientRemote(ctx, c)
	if err != nil {
		return err
	}
	copy := *c
	copy.Sink = "window"
	caps, err := detect.Resolve(&copy, detect.HasElement, detect.FileExists)
	if err != nil {
		return err
	}
	return probePlaneSources(ctx, c, caps.Decoder, cameras, runDecodedProbe)
}

func probePlaneSources(ctx context.Context, c *config.Config, decoder string, cameras []setupCamera, run func(context.Context, []string) error) error {
	bySerial := make(map[string]setupCamera, len(cameras))
	for _, camera := range cameras {
		bySerial[camera.SN] = camera
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	limit := make(chan struct{}, 4)
	var workers sync.WaitGroup
	var firstErr error
	var once sync.Once
	for _, tile := range c.Tiles {
		if tile.Motion != "" {
			continue // A motion tile can be asleep without being broken.
		}
		camera, hasCamera := bySerial[tile.Camera]
		if tile.Camera != "" && !hasCamera {
			return fmt.Errorf("tile %s: camera %q is absent from bridge inventory", tile.ID, tile.Camera)
		}
		if tile.URL == "" && hasCamera && camera.Mode == "on_motion" {
			continue // A fixed on_motion tile has no required live stream while idle.
		}
		workers.Add(1)
		go func(tile config.Tile, camera setupCamera, hasCamera bool) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				return
			}
			probeCtx, stop := context.WithTimeout(ctx, frameProbeTimeout)
			defer stop()
			if err := probePlaneSource(probeCtx, c, tile, camera, hasCamera, decoder, run); err != nil {
				once.Do(func() { firstErr = fmt.Errorf("tile %s: %w", tile.ID, err); cancel() })
			}
		}(tile, camera, hasCamera)
	}
	workers.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func probePlaneSource(ctx context.Context, c *config.Config, tile config.Tile, camera setupCamera, hasCamera bool, decoder string, run func(context.Context, []string) error) (err error) {
	codec := tile.Codec
	if codec == "" {
		codec = "h264" // The plane pipeline uses the same default.
	}
	rtspURL := c.TileURL(tile)
	if hasCamera && tile.URL == "" {
		if camera.StreamKey == "" {
			return fmt.Errorf("camera %q has no RTSP stream key", tile.Camera)
		}
		rtspURL = c.TileURL(config.Tile{Camera: camera.StreamKey})
		if camera.Mode != "always" {
			owner := fmt.Sprintf("wall-health-%d", os.Getpid())
			endpoint := strings.TrimRight(c.BridgeURL, "/") + "/hold/" + url.PathEscape(tile.Camera) + "?" + url.Values{"owner": {owner}, "seconds": {"20"}}.Encode()
			if err := probeHold(ctx, "POST", endpoint); err != nil {
				return fmt.Errorf("cannot wake camera %q: %w", tile.Camera, err)
			}
			defer func() {
				release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if releaseErr := probeHold(release, "DELETE", endpoint); releaseErr != nil && err == nil {
					err = fmt.Errorf("decoded frames, but hold release failed: %w (server hold expires after 20 seconds)", releaseErr)
				}
			}()
		}
	}
	args, err := pipeline.ProbeArgs(c, decoder, codec, rtspURL)
	if err != nil {
		return err
	}
	if err := run(ctx, args); err != nil {
		return fmt.Errorf("no decoded frame progress with configured %s codec: %w", codec, err)
	}
	return nil
}
