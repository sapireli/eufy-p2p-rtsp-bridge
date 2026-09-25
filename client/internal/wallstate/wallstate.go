// Package wallstate tracks what the server has told us about each camera, and decides what each tile
// should be showing right now.
//
// The wall is driven by events, not polling: by the time a poll noticed motion, the thing that moved
// would be gone. The server pushes `motion`, `hold` and `streamState` over /ws; this turns that stream
// into the one question a renderer asks — for each tile, which camera (if any) should be on screen?
package wallstate

import (
	"sort"
	"sync"
	"time"

	"eufy-wall/internal/config"
)

// Camera is what we know about one camera.
type Camera struct {
	SN          string
	Name        string
	Mode        string  // always | on_motion | on_demand
	HoldSeconds float64 // bridge-configured hold lifetime; refresh before it expires
	Live        bool    // the server says there is video to show right now
	Starting    bool    // waking: a stream is being established but no frames yet
	HasStill    bool    // the bridge holds a thumbnail for this camera at /snapshot/<sn>
	StreamKey   string  // the bridge's go2rtc stream key (its name, slugged) — what the RTSP URL uses
	Codec       string  // h264 | h265, as reported by the bridge for this camera
	LastMotion  time.Time
}

// Store is the client's view of the wall. Event updates and renderer reads may run concurrently.
type Store struct {
	mu   sync.RWMutex
	cams map[string]*Camera
	now  func() time.Time
}

func New() *Store { return &Store{cams: map[string]*Camera{}, now: time.Now} }

// NewAt is New with a fixed clock, for tests.
func NewAt(now func() time.Time) *Store { return &Store{cams: map[string]*Camera{}, now: now} }

func (s *Store) get(sn string) *Camera {
	c, ok := s.cams[sn]
	if !ok {
		c = &Camera{SN: sn}
		s.cams[sn] = c
	}
	return c
}

// HelloCamera is one camera in the server's hello snapshot.
type HelloCamera struct {
	SN           string  `json:"sn"`
	Name         string  `json:"name"`
	Mode         string  `json:"mode"`
	HoldSeconds  float64 `json:"holdSeconds"`
	State        string  `json:"state"`
	Still        bool    `json:"still"`
	StreamKey    string  `json:"streamKey"`
	Codec        string  `json:"codec"`
	LastMotionAt int64   `json:"lastMotionAt"`
}

// Message is one /ws frame. Only the fields the wall acts on are decoded.
type Message struct {
	Type    string        `json:"type"`
	At      int64         `json:"at"`
	SN      string        `json:"sn"`
	State   string        `json:"state"`
	Event   string        `json:"event"`
	Still   bool          `json:"still"`
	Codec   string        `json:"codec"`
	Cameras []HelloCamera `json:"cameras"`
}

// Apply folds one message in. Returns whether anything a tile could notice changed.
func (s *Store) Apply(m Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch m.Type {
	case "hello":
		// The snapshot a joining client is sent, so a wall that connects mid-event knows what is already
		// happening rather than waiting for the next event.
		previous := s.cams
		s.cams = map[string]*Camera{}
		for _, c := range m.Cameras {
			cam := &Camera{SN: c.SN, Name: c.Name, Mode: c.Mode, HoldSeconds: c.HoldSeconds, Live: c.State == "live", Starting: c.State == "starting", HasStill: c.Still, StreamKey: c.StreamKey, Codec: c.Codec}
			if c.LastMotionAt > 0 && m.At >= c.LastMotionAt {
				// Use the age in the server snapshot, not its wall clock, so skew between the bridge and
				// display cannot extend a battery camera's motion window indefinitely.
				ageMs := min(m.At-c.LastMotionAt, int64((7*24*time.Hour)/time.Millisecond))
				cam.LastMotion = s.now().Add(-time.Duration(ageMs) * time.Millisecond)
			}
			if old := previous[c.SN]; old != nil {
				if cam.LastMotion.IsZero() {
					cam.LastMotion = old.LastMotion
				}
				if cam.Codec == "" {
					cam.Codec = old.Codec
				}
			}
			s.cams[c.SN] = cam
		}
		return true
	case "motion":
		if m.SN == "" {
			return false
		}
		c := s.get(m.SN)
		c.LastMotion = s.now()
		// The push that carried this event also carried the thumbnail, so a camera with no still before
		// the event usually has one now.
		c.HasStill = c.HasStill || m.Still
		return true
	case "streamState":
		if m.SN == "" {
			return false
		}
		c := s.get(m.SN)
		live, starting := m.State == "live", m.State == "starting"
		codecChanged := m.Codec != "" && m.Codec != c.Codec
		if c.Live == live && c.Starting == starting && !codecChanged {
			return false
		}
		c.Live, c.Starting = live, starting
		if m.Codec != "" {
			c.Codec = m.Codec
		}
		return true
	}
	return false // `hold` is informational for a tile; the streamState that follows is what matters
}

func (s *Store) Camera(sn string) (Camera, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.camera(sn)
}

func (s *Store) camera(sn string) (Camera, bool) {
	c, ok := s.cams[sn]
	if !ok {
		return Camera{}, false
	}
	return *c, true
}

func (s *Store) IsLive(sn string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isLive(sn)
}

func (s *Store) HoldSecondsFor(sn string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cam := s.cams[sn]; cam != nil && cam.HoldSeconds > 0 {
		return cam.HoldSeconds
	}
	return 60 // compatibility with a bridge that predates holdSeconds in hello
}

func (s *Store) isLive(sn string) bool {
	c, ok := s.cams[sn]
	return ok && c.Live
}

// Known lists every camera the server has mentioned, in a stable order.
func (s *Store) Known() []Camera {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.known()
}

func (s *Store) known() []Camera {
	out := make([]Camera, 0, len(s.cams))
	for _, c := range s.cams {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SN < out[j].SN })
	return out
}

// Content is what a tile renders.
const (
	ContentNone     = ""         // nothing: a dark tile
	ContentLive     = "live"     // the RTSP stream
	ContentSnapshot = "snapshot" // the camera's last retained still
)

// Selection is what one tile should show. Camera empty means: show nothing.
type Selection struct {
	TileIndex int
	Camera    string
	// Content is live once the server says there are frames, and the retained still before that — which
	// is what puts a picture on screen during the second or two a battery camera takes to wake, instead
	// of a black rectangle that looks the same as a broken camera.
	Content string
	// Hold is true when this tile is actively asking for the camera to be streaming, and stays true
	// while it is — a hold is bounded on the server, so a tile that stopped asking once the picture
	// arrived would watch it die mid-view.
	//
	// Motion tiles and fixed on_demand tiles ask for a bounded, refreshed hold. Fixed on_motion tiles
	// never ask, so battery cameras continue sleeping between events.
	Hold bool
}

// dwellUntil is when a motion tile that switched at `since` is allowed to switch again.
func dwellUntil(since time.Time, t config.Tile) time.Time {
	if t.DwellSeconds <= 0 {
		return since
	}
	return since.Add(time.Duration(t.DwellSeconds) * time.Second)
}

// Resolve decides what every tile shows.
//
// `current` is what each tile is showing now (by tile index), and `switchedAt` when it last changed;
// both are needed so a dwell time can hold a tile still rather than letting it strobe between two
// cameras that fired together.
func (s *Store) Resolve(tiles []config.Tile, current map[int]string, switchedAt map[int]time.Time) []Selection {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	out := make([]Selection, 0, len(tiles))
	for i, t := range tiles {
		if t.Motion == "" {
			// A fixed tile shows its camera whenever the server says there is video. A battery camera
			// therefore blanks while asleep instead of showing a dead RTSP URL, and lights up on its own
			// when motion wakes it.
			sel := Selection{TileIndex: i, Camera: t.Camera, Content: ContentLive}
			if t.Camera != "" && !s.isLive(t.Camera) {
				if cam, ok := s.camera(t.Camera); ok && cam.Mode != "always" {
					// Its camera is asleep or waking: show the last still rather than nothing. An
					// always-on camera is exempt — a brief idle there is a reconnect, and swapping to a
					// still and back would be a visible flap.
					sel.Content = s.contentFor(t.Camera)
				}
			}
			if cam, ok := s.camera(t.Camera); ok && cam.Mode == "on_demand" {
				sel.Hold = true
			}
			out = append(out, sel)
			continue
		}
		out = append(out, s.resolveMotion(i, t, current[i], switchedAt[i], now))
	}
	return out
}

func (s *Store) resolveMotion(i int, t config.Tile, showing string, since time.Time, now time.Time) Selection {
	watch := t.Watch
	if len(watch) == 0 {
		for _, c := range s.known() {
			watch = append(watch, c.SN)
		}
	}

	best, bestAt := "", time.Time{}
	for _, sn := range watch {
		c, ok := s.cams[sn]
		if !ok || c.LastMotion.IsZero() {
			continue
		}
		if c.LastMotion.After(bestAt) {
			best, bestAt = sn, c.LastMotion
		}
	}

	// Nothing has moved recently enough: this is what makes a screen that turns ON for motion rather
	// than one permanently showing the last thing that moved.
	if t.BlankAfterSeconds > 0 && (bestAt.IsZero() || now.Sub(bestAt) > time.Duration(t.BlankAfterSeconds)*time.Second) {
		return Selection{TileIndex: i, Camera: ""}
	}
	if best == "" {
		return Selection{TileIndex: i, Camera: showing, Content: s.contentFor(showing), Hold: s.holdFor(showing)}
	}
	// Hold still if we switched recently and the tile is already showing something valid.
	if showing != "" && best != showing && now.Before(dwellUntil(since, t)) {
		best = showing
	}
	return Selection{TileIndex: i, Camera: best, Content: s.contentFor(best), Hold: s.holdFor(best)}
}

// contentFor is the whole "snapshot first, stream replaces it" rule: show the still the moment a camera
// is chosen, and swap to video only once the server says frames are flowing.
func (s *Store) contentFor(sn string) string {
	if sn == "" {
		return ContentNone
	}
	if s.isLive(sn) {
		return ContentLive
	}
	if c, ok := s.cams[sn]; ok && c.HasStill {
		return ContentSnapshot
	}
	return ContentNone // nothing retained to show; a dark tile beats a pipeline restarting on a 404
}

// holdFor reports whether this wall has to keep `sn` streaming itself. An always-on camera is streaming
// for everyone already, so a hold on it would mean nothing.
func (s *Store) holdFor(sn string) bool {
	if sn == "" {
		return false
	}
	c, ok := s.cams[sn]
	return !ok || c.Mode != "always"
}

// StreamKeyFor is the RTSP path segment for a camera: the bridge's key when it has told us one, and the
// serial otherwise — which is also what the bridge falls back to for a camera whose name it cannot use.
func (s *Store) StreamKeyFor(sn string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if c, ok := s.cams[sn]; ok && c.StreamKey != "" {
		return c.StreamKey
	}
	return sn
}

// CodecFor returns the camera's current codec. An empty value means the bridge has not reported one;
// the caller may then use a local tile hint or the transcoding bridge's H.264 default.
func (s *Store) CodecFor(sn string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.cams[sn]; ok {
		return c.Codec
	}
	return ""
}
