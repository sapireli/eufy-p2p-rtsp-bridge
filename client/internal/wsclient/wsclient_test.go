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

func TestEveryLiveMotionNotifiesAfterSelectionWithoutHelloReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for _, msg := range []string{`{"type":"hello","cameras":[{"sn":"FRONT","state":"live"}]}`, `{"type":"motion","sn":"FRONT"}`, `{"type":"motion","sn":"FRONT"}`, `{"type":"motion","sn":""}`} {
			if c.Write(r.Context(), websocket.MessageText, []byte(msg)) != nil {
				return
			}
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var changed atomic.Int32
	got := make(chan int32, 4)
	go RunWithMotion(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), wallstate.New(), func() { changed.Add(1) }, func(sn string) {
		if sn != "FRONT" {
			got <- -1
			return
		}
		got <- changed.Load()
	}, func(string) {})
	for _, want := range []int32{2, 3} {
		select {
		case count := <-got:
			if count != want {
				t.Fatalf("motion before selection or hello replay: %d want %d", count, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("motion missing")
		}
	}
	select {
	case count := <-got:
		t.Fatalf("unexpected motion: %d", count)
	case <-time.After(30 * time.Millisecond):
	}
}

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

func TestDualStackOriginsPreservePortsSchemesAndBrackets(t *testing.T) {
	for _, tc := range []struct{ in, api, event string }{
		{"rtsp://[2001:db8::2]:8554", "http://[2001:db8::2]:3000", "ws://[2001:db8::2]:3000/ws"},
		{"rtsp://[fe80::2%25eth0]:8554", "http://[fe80::2%25eth0]:3000", "ws://[fe80::2%25eth0]:3000/ws"},
		{"http://[::1]:3100", "http://[::1]:3100", "ws://[::1]:3100/ws"},
		{"https://bridge.example:3443", "https://bridge.example:3443", "wss://bridge.example:3443/ws"},
		{"http://bridge.example", "http://bridge.example:80", "ws://bridge.example:80/ws"},
		{"http://192.0.2.1:3123", "http://192.0.2.1:3123", "ws://192.0.2.1:3123/ws"},
	} {
		if got := APIBase(tc.in); got != tc.api {
			t.Errorf("API %q got%q", tc.in, got)
		}
		if got := EventURL(tc.in); got != tc.event {
			t.Errorf("WS %q got%q", tc.in, got)
		}
		if got := StillURL(tc.in, "CAM"); got != tc.api+"/snapshot/CAM" {
			t.Errorf("snapshot %q", got)
		}
	}
}

func TestAuthenticationChallengeOutranksStaleReadyFlag(t *testing.T) {
	ready, starting := true, false
	for _, state := range []string{"require_2fa", "require_captcha", "reauth", "error"} {
		if got := authReport(state, &ready); got != "auth: "+state {
			t.Fatalf("masked %s: %s", state, got)
		}
	}
	if authReport("ok", &starting) != "auth: starting" || authReport("ok", &ready) != "auth: ready" {
		t.Fatal("ready/startupstateswrong")
	}
}
