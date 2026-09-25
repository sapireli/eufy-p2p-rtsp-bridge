package gstnative

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"eufy-wall/internal/pipeline"
)

func TestRendererRejectsBadConstructionAndClosedUpdates(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	validCaps := pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}
	for name, mutate := range map[string]func(*config.Config, *[]layout.Placed, *pipeline.Caps, *Options){
		"nil config":  func(c *config.Config, _ *[]layout.Placed, _ *pipeline.Caps, _ *Options) { *c = config.Config{} },
		"wrong sink":  func(_ *config.Config, _ *[]layout.Placed, caps *pipeline.Caps, _ *Options) { caps.Sink = "planes" },
		"zero screen": func(_ *config.Config, _ *[]layout.Placed, caps *pipeline.Caps, _ *Options) { caps.Screen.Width = 0 },
		"bad hash": func(_ *config.Config, _ *[]layout.Placed, _ *pipeline.Caps, opts *Options) {
			opts.ConfigSHA256 = strings.Repeat("z", 64)
		},
		"bad directory": func(_ *config.Config, _ *[]layout.Placed, _ *pipeline.Caps, opts *Options) {
			file := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			opts.StatusPath = filepath.Join(file, "status.json")
		},
		"duplicate tile": func(_ *config.Config, tiles *[]layout.Placed, _ *pipeline.Caps, _ *Options) {
			*tiles = append(*tiles, (*tiles)[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := &config.Config{}
			selected := append([]layout.Placed(nil), tiles...)
			caps := validCaps
			opts := syntheticOptions(t)
			mutate(c, &selected, &caps, &opts)
			if name == "nil config" {
				c = nil
			}
			if r, err := New(c, selected, caps, opts); err == nil {
				r.Close()
				t.Fatal("invalid renderer accepted")
			}
		})
	}
	opts := syntheticOptions(t)
	opts.Initial = make([]layout.Placed, 0)
	r, err := New(&config.Config{}, tiles, validCaps, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Status().Tiles["left"]; got.ExpectedLive || got.SourceKind != "black" {
		t.Fatalf("empty initial selection woke camera: %+v", got)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal("second close failed:", err)
	}
	if err := r.Update(tiles); err == nil {
		t.Fatal("closed renderer accepted update")
	}
	if _, err := os.Stat(opts.StatusPath); !os.IsNotExist(err) {
		t.Fatalf("status survived close: %v", err)
	}
}

func TestRendererReportsStatusWriteFailure(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	opts := syntheticOptions(t)
	r, err := New(&config.Config{}, testTiles(),
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := os.RemoveAll(filepath.Dir(opts.StatusPath)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-r.Errors():
		if err == nil || !strings.Contains(err.Error(), "no such file") {
			t.Fatalf("wrong status error: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("status writer failed silently")
	}
}

func TestRendererSnapshotRetryDoesNotRestartBlackPeer(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	tiles := testTiles()
	tiles[0].URL, tiles[0].StillURL = "", "http://bridge/still"
	tiles[1].URL = ""
	opts := syntheticOptions(t)
	var reject atomic.Bool
	opts.source = func(tile layout.Placed) (string, error) {
		if tile.StillURL != "" && reject.Load() {
			return "", errors.New("snapshot offline")
		}
		return blackSource, nil
	}
	r, err := New(&config.Config{}, tiles,
		pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	reject.Store(true)
	r.mu.Lock()
	still := r.slots["left"]
	still.installed = time.Now().Add(-21 * time.Second)
	still.counter.last.Store(time.Now().Add(-21 * time.Second).UnixNano())
	r.slots["right"].installed = time.Now().Add(-21 * time.Second)
	r.recoverStalledLocked(time.Now(), nil)
	r.mu.Unlock()
	status := r.Status()
	if status.Tiles["left"].State != "retrying" || status.Tiles["left"].Generation != 1 ||
		status.Tiles["right"].Generation != 1 {
		t.Fatalf("snapshot retry or black peer wrong: %+v", status.Tiles)
	}
	reject.Store(false)
	r.mu.Lock()
	still.nextRetry = time.Now().Add(-time.Second)
	r.recoverStalledLocked(time.Now(), nil)
	r.mu.Unlock()
	if got := r.Status().Tiles["left"]; got.Generation != 2 || got.State != "playing" {
		t.Fatalf("snapshot did not recover: %+v", got)
	}
}

func TestRendererSourceInstallFailureKeepsPeerAndFallsBack(t *testing.T) {
	if _, err := load(); err != nil {
		t.Skip(err)
	}
	for _, failure := range []string{"link", "probe", "start", "add"} {
		t.Run(failure, func(t *testing.T) {
			tiles := testTiles()
			r, err := New(&config.Config{}, tiles,
				pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, syntheticOptions(t))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			peerBefore := waitFrames(t, r, "right", 0)
			r.mu.Lock()
			mock := *r.api
			r.mu.Unlock()
			var calls atomic.Int32
			switch failure {
			case "link":
				original := mock.padLink
				mock.padLink = func(a, b uintptr) int32 {
					if calls.Add(1) == 1 {
						return -1
					}
					return original(a, b)
				}
			case "probe":
				original := mock.addProbe
				mock.addProbe = func(a uintptr, b uint64, c, d, e uintptr) uint64 {
					if calls.Add(1) == 1 {
						return 0
					}
					return original(a, b, c, d, e)
				}
			case "start":
				original := mock.syncState
				mock.syncState = func(a uintptr) int32 {
					if calls.Add(1) == 1 {
						return 0
					}
					return original(a)
				}
			case "add":
				original := mock.binAdd
				mock.binAdd = func(a, b uintptr) int32 {
					if calls.Add(1) == 1 {
						return 0
					}
					return original(a, b)
				}
			}
			r.mu.Lock()
			r.api = &mock
			r.mu.Unlock()
			tiles[0].URL = "test://snow"
			if err := r.Update(tiles); err == nil {
				t.Fatal("injected source install failure ignored")
			}
			peer := waitFrames(t, r, "right", peerBefore)
			status := r.Status()
			if peer <= peerBefore || status.Tiles["right"].Generation != 1 || !status.Tiles["left"].ExpectedLive {
				t.Fatalf("failure disturbed peer or desired-live state: %+v", status)
			}
			if failure != "add" && (status.Tiles["left"].SourceKind != "black" || status.Tiles["left"].State != "retrying") {
				t.Fatalf("failed source did not fall back to black: %+v", status.Tiles["left"])
			}
		})
	}
}

func TestRendererNativeSetupFailuresReleasePipeline(t *testing.T) {
	a, err := load()
	if err != nil {
		t.Skip(err)
	}
	for name, breakAPI := range map[string]func(*gstAPI){
		"bus": func(mock *gstAPI) { mock.getBus = func(uintptr) uintptr { return 0 } },
		"output": func(mock *gstAPI) {
			original := mock.byName
			mock.byName = func(bin uintptr, name string) uintptr {
				if name == "output_probe" {
					return 0
				}
				return original(bin, name)
			}
		},
		"output pad": func(mock *gstAPI) { mock.staticPad = func(uintptr, string) uintptr { return 0 } },
		"output probe": func(mock *gstAPI) {
			mock.addProbe = func(uintptr, uint64, uintptr, uintptr, uintptr) uint64 { return 0 }
		},
		"entry queue": func(mock *gstAPI) {
			original := mock.byName
			mock.byName = func(bin uintptr, name string) uintptr {
				if name == "entry_0" {
					return 0
				}
				return original(bin, name)
			}
		},
		"initial blank": func(mock *gstAPI) {
			original := mock.byName
			mock.byName = func(bin uintptr, name string) uintptr {
				if name == "blank_0" {
					return 0
				}
				return original(bin, name)
			}
		},
		"start": func(mock *gstAPI) {
			original := mock.setState
			mock.setState = func(element uintptr, state int32) int32 {
				if state == statePlaying {
					return 0
				}
				return original(element, state)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			mock := *a
			breakAPI(&mock)
			opts := syntheticOptions(t)
			opts.api = &mock
			if r, err := New(&config.Config{}, testTiles(),
				pipeline.Caps{Sink: "window", Screen: config.Screen{Width: 128, Height: 64}}, opts); err == nil {
				r.Close()
				t.Fatal("broken native pipeline accepted")
			}
			if _, err := os.Stat(opts.StatusPath); !os.IsNotExist(err) {
				t.Fatalf("failed startup left status file: %v", err)
			}
		})
	}
}
