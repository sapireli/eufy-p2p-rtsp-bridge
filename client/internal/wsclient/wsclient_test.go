package wsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"eufy-wall/internal/wallstate"
)

func TestEventURLDerivesTheChannelFromRtspBase(t *testing.T) {
	// The wall already knows where the bridge is; making the operator repeat it would be one more
	// thing to get wrong.
	for _, tc := range []struct{ in, want string }{
		{"rtsp://192.168.1.10:8554", "ws://192.168.1.10:3000/ws"},
		{"rtsp://bridge.local:8554/", "ws://bridge.local:3000/ws"},
		{"http://192.168.1.10:3000", "ws://192.168.1.10:3000/ws"},
		{"", ""},
		{"not a url", ""},
	} {
		if got := EventURL(tc.in); got != tc.want {
			t.Errorf("EventURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A display is expected to outlive the bridge it talks to, so the interesting behaviour is that it
// keeps following across a disconnect and re-seeds from the new connection's hello.
func TestFollowsEventsAndReconnects(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		first := conns.Add(1) == 1
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","cameras":[{"sn":"YARD","mode":"on_motion","state":"idle"}]}`))
		if first {
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"motion","sn":"YARD"}`))
			c.CloseNow() // drop the first connection to force a reconnect
			return
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"streamState","sn":"YARD","state":"live"}`))
		<-ctx.Done()
	}))
	defer srv.Close()

	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	store := wallstate.New()
	changes := make(chan struct{}, 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, endpoint, store, func() { changes <- struct{}{} }, func(string) {})

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-changes:
			if store.IsLive("YARD") {
				if count := conns.Load(); count < 2 {
					t.Fatalf("should have reconnected, saw %d connections", count)
				}
				return // reconnected and re-seeded
			}
		case <-deadline:
			t.Fatalf("never saw YARD go live (connections=%d)", conns.Load())
		}
	}
}

func TestMalformedMessageDoesNotDropTheConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`not json at all`))
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","cameras":[{"sn":"GAR","mode":"always","state":"live"}]}`))
		<-ctx.Done()
	}))
	defer srv.Close()

	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	store := wallstate.New()
	changes := make(chan struct{}, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, endpoint, store, func() { changes <- struct{}{} }, func(string) {})

	select {
	case <-changes:
		if !store.IsLive("GAR") {
			t.Error("the message after the malformed one should still have been applied")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a malformed message should be skipped, not kill the connection")
	}
}

func TestStillURL(t *testing.T) {
	if got := StillURL("rtsp://192.168.1.50:8554", "T8210N1"); got != "http://192.168.1.50:3000/snapshot/T8210N1" {
		t.Errorf("got %q", got)
	}
	if got := StillURL("", "X"); got != "" {
		t.Errorf("no bridge configured should yield no URL, got %q", got)
	}
}

func TestExplicitControlURLKeepsPortAndTLS(t *testing.T) {
	if got := EventURL("https://bridge.local:7443/api"); got != "wss://bridge.local:7443/ws" {
		t.Fatalf("EventURL = %q", got)
	}
	if got := StillURL("https://bridge.local:7443/api", "CAM1"); got != "https://bridge.local:7443/snapshot/CAM1" {
		t.Fatalf("StillURL = %q", got)
	}
	if got := EventURL("rtsp://[2001:db8::1]:8554"); got != "ws://[2001:db8::1]:3000/ws" {
		t.Fatalf("IPv6 EventURL = %q", got)
	}
	if got := EventURL("ftp://bridge.local/file"); got != "" {
		t.Fatalf("unsupported scheme = %q", got)
	}
}

func TestReconnectBackoffResetsAfterAHealthyConnection(t *testing.T) {
	for _, tc := range []struct {
		current, uptime, delay, next time.Duration
	}{
		{16 * time.Second, stableConnection - time.Second, 16 * time.Second, 30 * time.Second},
		{16 * time.Second, stableConnection, minBackoff, 2 * time.Second},
		{maxBackoff, time.Second, maxBackoff, maxBackoff},
	} {
		delay, next := reconnectBackoff(tc.current, tc.uptime)
		if delay != tc.delay || next != tc.next {
			t.Errorf("reconnectBackoff(%s, %s) = %s, %s; want %s, %s", tc.current, tc.uptime, delay, next, tc.delay, tc.next)
		}
	}
	for i := 0; i < 100; i++ {
		if got := jitterBackoff(10 * time.Second); got < 8*time.Second || got > 10*time.Second {
			t.Fatalf("jitter exceeded bounded retry window: %s", got)
		}
	}
}
