package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"eufy-wall/internal/config"
)

func (e *layoutEditor) ensureInventory(file string) error {
	if file == "" && e.inventory != nil {
		return nil
	}
	c, err := e.config()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cameras, err := fetchSetupCameras(ctx, c.BridgeURL, file)
	if err != nil {
		return err
	}
	inventory := make(map[string]setupCamera, len(cameras))
	for _, camera := range cameras {
		inventory[camera.SN] = camera
	}
	e.inventory = inventory
	return nil
}

func (e *layoutEditor) inventoryIssues(c *config.Config) []string {
	if e.inventory == nil {
		return nil
	}
	issues := []string{}
	for _, t := range c.Tiles {
		if t.Camera != "" {
			if _, ok := e.inventory[t.Camera]; !ok {
				issues = append(issues, fmt.Sprintf("tile %s uses unknown camera %s", t.ID, t.Camera))
			}
		}
		for _, sn := range t.Watch {
			if _, ok := e.inventory[sn]; !ok {
				issues = append(issues, fmt.Sprintf("tile %s watches unknown camera %s", t.ID, sn))
			}
		}
	}
	return issues
}

func (e *layoutEditor) checkInventory() error {
	c, err := e.config()
	if err != nil {
		return err
	}
	if e.inventory == nil {
		return errors.New("camera inventory is unavailable")
	}
	issues := e.inventoryIssues(c)
	if len(issues) > 0 {
		return fmt.Errorf("cannot apply: %s", issues[0])
	}
	return nil
}

func (e *layoutEditor) showInventory(out io.Writer) error {
	serials := make([]string, 0, len(e.inventory))
	for sn := range e.inventory {
		serials = append(serials, sn)
	}
	sort.Strings(serials)
	if _, err := fmt.Fprintf(out, "inventory: %d camera(s)\n", len(serials)); err != nil {
		return err
	}
	for _, sn := range serials {
		cam := e.inventory[sn]
		if _, err := fmt.Fprintf(out, "  %s %s codec=%s mode=%s\n", cam.SN, cam.Name, cam.Codec, cam.Mode); err != nil {
			return err
		}
	}
	return nil
}
