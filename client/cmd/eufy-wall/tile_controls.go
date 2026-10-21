package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/supervisor"
)

var planUpdateMu sync.Mutex

// Keep unchanged files untouched so retries and software selections remain latched.
func writeTileConfigs(plans []pipeline.Plan) error {
	for _, plan := range plans {
		for _, config := range plan.TileConfigs {
			old, err := os.ReadFile(config.Path)
			if err == nil && bytes.Equal(old, config.Contents) {
				continue
			}
			file, err := os.CreateTemp(filepath.Dir(config.Path), ".tile-*")
			if err != nil {
				return err
			}
			name := file.Name()
			_, err = file.Write(config.Contents)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(name, config.Path)
			}
			if err != nil {
				os.Remove(name)
				return fmt.Errorf("tile control: %w", err)
			}
		}
	}
	return nil
}

func updatePlans(ctx context.Context, manager *supervisor.Manager, plans []pipeline.Plan) {
	planUpdateMu.Lock()
	defer planUpdateMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if err := writeTileConfigs(plans); err != nil {
		log.Printf("[wall] keeping current rendering: %v", err)
		return
	}
	manager.Update(ctx, plans)
}
