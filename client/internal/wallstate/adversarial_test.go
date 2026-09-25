package wallstate

import (
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func TestReconnectDoesNotForgetRecentMotion(t *testing.T) {
	var advance time.Duration
	s := NewAt(func() time.Time { return base.Add(advance) })
	hello := Message{Type: "hello", Cameras: []HelloCamera{{SN: "BAT", Mode: "on_motion", State: "idle", Still: true, Codec: "h265"}}}
	s.Apply(hello)
	s.Apply(Message{Type: "motion", SN: "BAT"})
	advance = 10 * time.Second
	s.Apply(hello)
	got := s.Resolve([]config.Tile{{Motion: "latest", Watch: []string{"BAT"}, BlankAfterSeconds: 30}}, nil, nil)[0]
	if got.Camera != "BAT" || !got.Hold || got.Content != ContentSnapshot {
		t.Fatalf("reconnect lost motion selection: %+v", got)
	}
	advance = 31 * time.Second
	if got := s.Resolve([]config.Tile{{Motion: "latest", Watch: []string{"BAT"}, BlankAfterSeconds: 30}}, nil, nil)[0]; got.Camera != "" {
		t.Fatalf("expired motion stayed live: %+v", got)
	}
}

func TestFixedDemandWithoutSnapshotStillRequestsStream(t *testing.T) {
	s := New()
	s.Apply(Message{Type: "hello", Cameras: []HelloCamera{{SN: "DEMAND", Mode: "on_demand", State: "idle", Still: false}}})
	got := s.Resolve([]config.Tile{{Camera: "DEMAND"}}, nil, nil)[0]
	if got.Camera != "DEMAND" || got.Content != ContentNone || !got.Hold {
		t.Fatalf("request stream without nonexistent JPEG: %+v", got)
	}
}
