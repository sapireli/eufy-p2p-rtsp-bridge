package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/wallstate"
	"eufy-wall/internal/wsclient"
)

// plansFor builds the pipelines for what each tile is currently showing. A tile showing nothing simply
// has no plan, so a blank tile costs no process at all.
func plansFor(c *config.Config, caps pipeline.Caps, tiles []layout.Placed, showing, content map[int]string, streamKey, codecFor func(string) string) []pipeline.Plan {
	return plansForTiles(c, caps, tilesFor(c, tiles, showing, content, streamKey, codecFor))
}

func tilesFor(c *config.Config, tiles []layout.Placed, showing, content map[int]string, streamKey, codecFor func(string) string) []layout.Placed {
	live := make([]layout.Placed, 0, len(tiles))
	for _, t := range tiles {
		if showing == nil {
			live = append(live, t)
			continue
		}
		cam, ok := showing[t.Index]
		if !ok || cam == "" {
			// A tile with its own URL is independent of the bridge's camera inventory. It must survive
			// the first hello snapshot even though it has no camera serial.
			if t.Camera == "" && t.Index < len(c.Tiles) && c.Tiles[t.Index].URL != "" && c.Tiles[t.Index].Motion == "" {
				live = append(live, t)
			}
			continue
		}
		if content[t.Index] == wallstate.ContentNone {
			// Sleeping camera with no retained still: no RTSP or snapshot process should run.
			continue
		}
		t.Camera = cam
		// go2rtc keys its streams by camera NAME, so the URL is built from the key the bridge reports
		// rather than from the serial. Before the bridge has told us one, the serial is what it falls
		// back to as well, so the two agree either way.
		key := cam
		if streamKey != nil {
			key = streamKey(cam)
		}
		t.URL = c.TileURL(config.Tile{Camera: key})
		if codecFor != nil && codecFor(cam) != "" {
			t.Codec = codecFor(cam)
		} else if tc := c.TileFor(cam); tc != nil {
			t.Codec = tc.Codec
		}
		if content[t.Index] == wallstate.ContentSnapshot {
			// Not streaming yet: put the retained still up rather than pointing a decoder at a camera
			// that is asleep, which shows one frozen frame at best.
			t.StillURL = wsclient.StillURL(controlBase(c), cam)
			if t.StillURL == "" {
				continue
			}
		}
		live = append(live, t)
	}
	return live
}

func plansForTiles(c *config.Config, caps pipeline.Caps, live []layout.Placed) []pipeline.Plan {
	return plansForTilesAt(c, caps, live, time.Now())
}

func plansForTilesAt(c *config.Config, caps pipeline.Caps, live []layout.Placed, now time.Time) []pipeline.Plan {
	if caps.Sink == "planes" {
		// Each plane is an independent process. A late codec change or bad URL on one camera must not
		// tear down the other cameras while that tile waits for a compatible decoder or source.
		plans := make([]pipeline.Plan, 0, len(live))
		for _, tile := range live {
			if tile.StillURL != "" {
				tile.StillURL = planeStillURL(tile.StillURL, now)
			}
			one, err := pipeline.Plans(c, []layout.Placed{tile}, caps)
			if err != nil {
				log.Printf("[wall] tile %s cannot build pipeline: %v", tile.ID, err)
				continue
			}
			plans = append(plans, one...)
		}
		return plans
	}
	plans, err := pipeline.Plans(c, live, caps)
	if err != nil {
		log.Printf("[wall] cannot build pipelines: %v", err)
		return nil
	}
	return plans
}

// imagefreeze repeats its first JPEG forever. A changed source URL makes the planes manager
// restart only this still tile at the next refresh boundary and fetch the current retained JPEG.
func planeStillURL(raw string, now time.Time) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("_wall_refresh", fmt.Sprint(now.Unix()/30))
	u.RawQuery = q.Encode()
	return u.String()
}

// describe renders what the wall is showing as one line, so the log says why a tile changed rather than
// only that some process restarted.
func describe(tiles []layout.Placed, showing, content map[int]string) string {
	parts := make([]string, 0, len(tiles))
	for _, t := range tiles {
		what := "blank"
		if cam := showing[t.Index]; cam != "" {
			what = cam
			if content[t.Index] == wallstate.ContentSnapshot {
				what += "(still)"
			}
		}
		parts = append(parts, fmt.Sprintf("%d=%s", t.Index, what))
	}
	return strings.Join(parts, " ")
}

func controlBase(c *config.Config) string {
	if c.BridgeURL != "" {
		return c.BridgeURL
	}
	return c.RTSPBase
}

// planNames lists what the wall is running, so the log says whether tiles are independent processes or
// one composited pipeline.
func planNames(plans []pipeline.Plan) string {
	names := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}
