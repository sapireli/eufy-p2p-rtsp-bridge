package main

import (
	"context"
	"log"
	"sync"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/wallstate"
	"eufy-wall/internal/wsclient"
)

// runDynamic follows /ws and keeps the running pipelines matching what each tile should be showing.
type wallManager interface {
	Run(context.Context, []pipeline.Plan) error
	Update(context.Context, []pipeline.Plan)
}

type nativeWall interface {
	Update([]layout.Placed) error
	Errors() <-chan error
}

func runDynamic(ctx context.Context, c *config.Config, caps pipeline.Caps, tiles []layout.Placed, mgr wallManager, static []pipeline.Plan) {
	_ = runDynamicWithNative(ctx, c, caps, tiles, mgr, static, nil, nil)
}

func runDynamicNative(ctx context.Context, c *config.Config, caps pipeline.Caps, tiles, initial []layout.Placed, native nativeWall) error {
	return runDynamicWithNative(ctx, c, caps, tiles, nil, nil, initial, native)
}

func runDynamicWithNative(ctx context.Context, c *config.Config, caps pipeline.Caps, tiles []layout.Placed, mgr wallManager, static []pipeline.Plan, initial []layout.Placed, native nativeWall) error {
	endpoint := wsclient.EventURL(controlBase(c))
	if endpoint == "" {
		log.Printf("[wall] cannot derive the event channel from rtsp_base %q — running a static wall", c.RTSPBase)
		if native != nil {
			select {
			case err := <-native.Errors():
				return err
			case <-ctx.Done():
				return nil
			}
		}
		return mgr.Run(ctx, static)
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
			if native != nil {
				if err := native.Update(initial); err != nil {
					log.Printf("[wall] native update: %v", err)
				}
			} else {
				mgr.Update(ctx, static)
			}
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
		wanted := map[string]time.Duration{}
		for _, sel := range sels {
			if sel.Hold && sel.Camera != "" {
				seconds := min(store.HoldSecondsFor(sel.Camera), 3600)
				wanted[sel.Camera] = time.Duration(seconds * float64(time.Second))
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
		selected := tilesFor(c, tiles, snapshot, kinds, func(sn string) string {
			if k, ok := keys[sn]; ok && k != "" {
				return k
			}
			return sn
		}, store.CodecFor)
		if native != nil {
			if err := native.Update(selected); err != nil {
				log.Printf("[wall] native update: %v", err)
			}
		} else {
			mgr.Update(ctx, plansForTiles(c, caps, selected))
		}
	}

	apply()
	go wsclient.Run(ctx, endpoint, store, apply, func(line string) { log.Printf("[wall] %s", line) })

	// Motion expiry is a deadline, not a bridge event. Reconcile even while /ws is quiet or offline so
	// a motion tile blanks and its hold is released without waiting for another camera event.
	reconcile := time.NewTicker(time.Second)
	defer reconcile.Stop()
	var nativeErrors <-chan error
	if native != nil {
		nativeErrors = native.Errors()
	}
	for {
		select {
		case err := <-nativeErrors:
			holds.Close()
			return err
		case <-reconcile.C:
			apply()
		case <-ctx.Done():
			holds.Close()
			applyMu.Lock()
			if mgr != nil {
				mgr.Update(ctx, nil)
			}
			applyMu.Unlock()
			return nil
		}
	}
}
