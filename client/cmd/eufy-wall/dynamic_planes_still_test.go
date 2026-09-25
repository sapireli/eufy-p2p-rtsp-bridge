package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func TestPlaneStillRefreshChangesOnlyStillTilePlan(t *testing.T) {
	c := &config.Config{Latency: 200, Planes: []int{31, 32}}
	caps := pipeline.Caps{Decoder: "software", Sink: "planes"}
	tiles := []layout.Placed{
		{ID: "sleeping", Index: 0, StillURL: "http://bridge/snapshot/camera%20one?token=a%2Bb", W: 64, H: 64},
		{ID: "live", Index: 1, URL: "rtsp://bridge/live", Codec: "h264", X: 64, W: 64, H: 64},
	}
	before := plansForTilesAt(c, caps, tiles, time.Unix(29, 0))
	after := plansForTilesAt(c, caps, tiles, time.Unix(30, 0))
	if len(before) != 2 || len(after) != 2 || before[0].Name != "sleeping" || after[1].Name != "live" {
		t.Fatalf("missing independently managed plane: before=%+v after=%+v", before, after)
	}
	if reflect.DeepEqual(before[0].Args, after[0].Args) || !reflect.DeepEqual(before[1].Args, after[1].Args) {
		t.Fatalf("refresh did not isolate still plane: before=%+v after=%+v", before, after)
	}
	for i, plans := range [][]pipeline.Plan{before, after} {
		var location string
		for _, arg := range plans[0].Args {
			if strings.HasPrefix(arg, "location=") {
				location = strings.TrimPrefix(arg, "location=")
			}
		}
		u, err := url.Parse(location)
		if err != nil || u.Path != "/snapshot/camera one" || u.Query().Get("token") != "a+b" || u.Query().Get("_wall_refresh") != string(rune('0'+i)) {
			t.Fatalf("bad retained-still request %d: %q (%v)", i, location, err)
		}
	}
	if tiles[0].StillURL != "http://bridge/snapshot/camera%20one?token=a%2Bb" {
		t.Fatal("source layout was mutated")
	}
}

func TestPlaneStillRefreshKeepsMalformedURLForValidation(t *testing.T) {
	const raw = "http://bridge/snapshot/%zz"
	if got := planeStillURL(raw, time.Unix(30, 0)); got != raw {
		t.Fatalf("malformed URL changed: %q", got)
	}
}
