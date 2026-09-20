package wallstate

import (
	"testing"
	"time"

	"eufy-wall/internal/config"
)

// The wall is driven by pushed events. These cover what a viewer actually experiences: a screen that
// lights up for motion and goes dark again, a tile that does not strobe between two cameras firing
// together, and a battery camera's tile showing nothing rather than a dead stream while it sleeps.

var base = time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC)

func storeAt(t *testing.T, offset *time.Duration) *Store {
	t.Helper()
	return NewAt(func() time.Time { return base.Add(*offset) })
}

// Cameras are declared as sn/name/mode/state; every camera in these tests has a retained thumbnail
// unless a test says otherwise, since that is the normal case once a camera has ever reported motion.
func hello(cams ...[4]string) Message {
	m := Message{Type: "hello"}
	for _, c := range cams {
		m.Cameras = append(m.Cameras, HelloCamera{SN: c[0], Name: c[1], Mode: c[2], State: c[3], Still: true})
	}
	return m
}

func motionTile(watch []string, blankAfter, dwell int) config.Tile {
	return config.Tile{Motion: "latest", Watch: watch, BlankAfterSeconds: blankAfter, DwellSeconds: dwell}
}

func TestHelloSeedsTheWall(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "Yard", "on_motion", "idle"}, [4]string{"GAR", "Garage", "always", "live"}))

	if !s.IsLive("GAR") || s.IsLive("YARD") {
		t.Fatalf("hello should seed live state: gar=%v yard=%v", s.IsLive("GAR"), s.IsLive("YARD"))
	}
	if got := s.Known(); len(got) != 2 || got[0].SN != "GAR" {
		t.Errorf("Known should be stable and complete: %+v", got)
	}
}

func TestMotionTileFollowsWhateverMovedLast(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "", "on_motion", "idle"}, [4]string{"SOLAR", "", "on_motion", "idle"}))

	s.Apply(Message{Type: "motion", SN: "YARD"})
	sel := s.Resolve([]config.Tile{motionTile(nil, 0, 0)}, nil, nil)
	if sel[0].Camera != "YARD" {
		t.Fatalf("tile should follow the camera that moved, got %q", sel[0].Camera)
	}
	if !sel[0].Hold {
		t.Error("YARD is a battery camera, so the tile must ask for a hold or it would show nothing")
	}

	off = 5 * time.Second
	s.Apply(Message{Type: "motion", SN: "SOLAR"})
	sel = s.Resolve([]config.Tile{motionTile(nil, 0, 0)}, map[int]string{0: "YARD"}, map[int]time.Time{0: base})
	if sel[0].Camera != "SOLAR" {
		t.Errorf("a newer event should win, got %q", sel[0].Camera)
	}
}

// The magic screen: dark until something moves, lit while it is recent, dark again afterwards.
func TestBlankAfterTurnsTheScreenOnAndOffAgain(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "", "on_motion", "idle"}))
	tile := []config.Tile{motionTile([]string{"YARD"}, 120, 0)}

	if got := s.Resolve(tile, nil, nil)[0].Camera; got != "" {
		t.Errorf("nothing has ever moved, the screen should be dark: %q", got)
	}

	s.Apply(Message{Type: "motion", SN: "YARD"})
	if got := s.Resolve(tile, nil, nil)[0].Camera; got != "YARD" {
		t.Errorf("motion should light the screen, got %q", got)
	}

	off = 119 * time.Second
	if got := s.Resolve(tile, map[int]string{0: "YARD"}, nil)[0].Camera; got != "YARD" {
		t.Errorf("still inside the window, should stay lit, got %q", got)
	}

	off = 121 * time.Second
	if got := s.Resolve(tile, map[int]string{0: "YARD"}, nil)[0].Camera; got != "" {
		t.Errorf("past blank_after_seconds the screen should go dark, got %q", got)
	}
}

// Two cameras firing seconds apart must not make the tile flicker between them.
func TestDwellStopsTheTileStrobing(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "", "on_motion", "live"}, [4]string{"SOLAR", "", "on_motion", "live"}))
	tile := []config.Tile{motionTile(nil, 0, 10)}

	s.Apply(Message{Type: "motion", SN: "YARD"})
	switched := map[int]time.Time{0: base}
	showing := map[int]string{0: "YARD"}

	off = 2 * time.Second
	s.Apply(Message{Type: "motion", SN: "SOLAR"})
	if got := s.Resolve(tile, showing, switched)[0].Camera; got != "YARD" {
		t.Errorf("inside the dwell window the tile should hold still, got %q", got)
	}

	off = 12 * time.Second
	if got := s.Resolve(tile, showing, switched)[0].Camera; got != "SOLAR" {
		t.Errorf("past the dwell window it should switch, got %q", got)
	}
}

func TestWatchLimitsWhichCamerasCanTakeTheTile(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "", "on_motion", "idle"}, [4]string{"DOOR", "", "always", "live"}))
	tile := []config.Tile{motionTile([]string{"YARD"}, 0, 0)}

	s.Apply(Message{Type: "motion", SN: "DOOR"}) // not watched
	if got := s.Resolve(tile, nil, nil)[0].Camera; got != "" {
		t.Errorf("an unwatched camera must not take the tile, got %q", got)
	}
	s.Apply(Message{Type: "motion", SN: "YARD"})
	if got := s.Resolve(tile, nil, nil)[0].Camera; got != "YARD" {
		t.Errorf("a watched camera should, got %q", got)
	}
}

// A wired camera in the watch set is useful and costs nothing: it is already streaming, so the tile
// just switches URL and needs no hold.
func TestAWiredCameraCanTakeTheMotionTileWithoutAHold(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"GAR", "", "always", "live"}))
	s.Apply(Message{Type: "motion", SN: "GAR"})
	sel := s.Resolve([]config.Tile{motionTile([]string{"GAR"}, 0, 0)}, nil, nil)
	if sel[0].Camera != "GAR" {
		t.Fatalf("got %q", sel[0].Camera)
	}
	if sel[0].Hold {
		t.Error("an always-on camera is already streaming; asking for a hold would be pointless")
	}
}

func TestFixedTileShowsAStillWhileItsBatteryCameraSleeps(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "", "on_motion", "idle"}, [4]string{"GAR", "", "always", "live"}))
	tiles := []config.Tile{{Camera: "YARD"}, {Camera: "GAR"}}

	sel := s.Resolve(tiles, nil, nil)
	// Never the live URL: pointing a decoder at a sleeping camera's stream is what used to freeze a tile
	// on one frame. The retained still is served over HTTP and needs nothing woken up.
	if sel[0].Camera != "YARD" || sel[0].Content != ContentSnapshot {
		t.Errorf("a sleeping battery camera should show its last still, got %q/%q", sel[0].Camera, sel[0].Content)
	}
	if sel[1].Camera != "GAR" || sel[1].Content != ContentLive {
		t.Errorf("an always-on camera always shows live: %q/%q", sel[1].Camera, sel[1].Content)
	}

	if sel[0].Hold {
		t.Error("a fixed tile must not hold a battery camera awake for as long as the wall is powered on")
	}

	s.Apply(Message{Type: "streamState", SN: "YARD", State: "live"})
	sel = s.Resolve(tiles, nil, nil)
	if sel[0].Camera != "YARD" || sel[0].Content != ContentLive {
		t.Errorf("once the server says it is live the tile should swap to video, got %q/%q", sel[0].Camera, sel[0].Content)
	}
}

// The point of the whole feature: motion puts a picture on screen immediately, and the stream replaces it
// when it arrives a second or two later. Neither step leaves the tile black.
func TestMotionShowsTheStillThenTheStreamReplacesIt(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"YARD", "", "on_motion", "idle"}))
	tile := []config.Tile{motionTile([]string{"YARD"}, 120, 0)}

	if got := s.Resolve(tile, nil, nil)[0]; got.Content != ContentNone {
		t.Errorf("nothing has moved, the tile should be dark: %q", got.Content)
	}

	s.Apply(Message{Type: "motion", SN: "YARD"})
	if got := s.Resolve(tile, nil, nil)[0]; got.Camera != "YARD" || got.Content != ContentSnapshot {
		t.Fatalf("motion should put the still up at once, got %q/%q", got.Camera, got.Content)
	}

	// The server acknowledges it is waking the camera. Still a still — there are no frames yet.
	s.Apply(Message{Type: "streamState", SN: "YARD", State: "starting"})
	if got := s.Resolve(tile, map[int]string{0: "YARD"}, nil)[0]; got.Content != ContentSnapshot {
		t.Errorf("while starting there is no video yet, got %q", got.Content)
	}
	if cam, _ := s.Camera("YARD"); !cam.Starting || cam.Live {
		t.Errorf("starting should be tracked distinctly from live: %+v", cam)
	}

	s.Apply(Message{Type: "streamState", SN: "YARD", State: "live"})
	got := s.Resolve(tile, map[int]string{0: "YARD"}, nil)[0]
	if got.Content != ContentLive {
		t.Errorf("frames are flowing, the stream should replace the still, got %q", got.Content)
	}
	if !got.Hold {
		t.Error("the tile must keep asking while it watches, or the bounded hold lapses mid-view")
	}

	off = 121 * time.Second
	if got := s.Resolve(tile, map[int]string{0: "YARD"}, nil)[0]; got.Camera != "" || got.Content != ContentNone {
		t.Errorf("past blank_after the magic screen goes properly dark, not to a still: %q/%q", got.Camera, got.Content)
	}
}

// An always-on camera that briefly reports idle (a reconnect) must not blank its tile: it is coming back,
// and blanking would be a visible flap for something the supervisor already handles.
func TestAlwaysOnTileDoesNotBlankOnATransientIdle(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"GAR", "", "always", "live"}))
	s.Apply(Message{Type: "streamState", SN: "GAR", State: "idle"})
	if got := s.Resolve([]config.Tile{{Camera: "GAR"}}, nil, nil)[0].Camera; got != "GAR" {
		t.Errorf("an always-on tile should ride out a reconnect, got %q", got)
	}
}

func TestApplyReportsWhetherAnythingChanged(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(hello([4]string{"GAR", "", "always", "idle"}))

	if !s.Apply(Message{Type: "streamState", SN: "GAR", State: "live"}) {
		t.Error("going live is a change")
	}
	if s.Apply(Message{Type: "streamState", SN: "GAR", State: "live"}) {
		t.Error("the same state again is not a change, and should not trigger a re-render")
	}
	if s.Apply(Message{Type: "hold", SN: "GAR"}) {
		t.Error("a hold is informational for a tile; the streamState that follows is what matters")
	}
	if s.Apply(Message{Type: "motion"}) {
		t.Error("a motion event with no camera is not actionable")
	}
}

// A camera that has never pushed has no thumbnail. Rendering one anyway would aim a pipeline at a 404
// and leave the supervisor restarting it forever, so the tile stays dark until there is something.
func TestNoRetainedStillMeansADarkTileNotABrokenOne(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(Message{Type: "hello", Cameras: []HelloCamera{{SN: "YARD", Mode: "on_motion", State: "idle", Still: false}}})
	tile := []config.Tile{motionTile([]string{"YARD"}, 120, 0)}

	s.Apply(Message{Type: "motion", SN: "YARD"})
	got := s.Resolve(tile, nil, nil)[0]
	if got.Content != ContentNone {
		t.Errorf("no thumbnail exists, so there is nothing to render yet: %q", got.Content)
	}
	if !got.Hold {
		t.Error("it should still be waking the camera — the stream is the whole point")
	}

	// The next push delivers one.
	s.Apply(Message{Type: "motion", SN: "YARD", Still: true})
	if got := s.Resolve(tile, map[int]string{0: "YARD"}, nil)[0]; got.Content != ContentSnapshot {
		t.Errorf("once a thumbnail exists the tile should show it, got %q", got.Content)
	}
}

// go2rtc keys its streams by camera name. The wall must use the key the bridge reports rather than
// deriving its own, or a rename silently points a tile at a stream that does not exist.
func TestStreamKeyComesFromTheBridge(t *testing.T) {
	var off time.Duration
	s := storeAt(t, &off)
	s.Apply(Message{Type: "hello", Cameras: []HelloCamera{
		{SN: "T8214A", Name: "Front Door", Mode: "always", State: "live", StreamKey: "front_door"},
		{SN: "T8425B", Name: "Garage", Mode: "always", State: "live"}, // bridge kept the serial
	}})
	if got := s.StreamKeyFor("T8214A"); got != "front_door" {
		t.Errorf("should use the bridge's key, got %q", got)
	}
	if got := s.StreamKeyFor("T8425B"); got != "T8425B" {
		t.Errorf("no key reported means the bridge fell back to the serial too, got %q", got)
	}
	if got := s.StreamKeyFor("UNKNOWN"); got != "UNKNOWN" {
		t.Errorf("a camera we know nothing about falls back to its serial, got %q", got)
	}
}
