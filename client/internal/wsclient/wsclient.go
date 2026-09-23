// Package wsclient follows the bridge's /ws event channel.
//
// A wall display is expected to outlive the server it talks to: the bridge restarts, the network drops,
// the switch reboots. So this never gives up — it reconnects with backoff forever, and each new
// connection re-seeds from the server's hello snapshot, which is what lets a display that was
// disconnected during an event come back knowing what is already happening.
package wsclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"eufy-wall/internal/wallstate"
)

const (
	minBackoff = 1 * time.Second
	maxBackoff = 30 * time.Second
	// A read that never completes is indistinguishable from a healthy quiet wall, so a connection that
	// has produced nothing for this long is treated as dead. The server pings every 30s.
	readTimeout = 90 * time.Second
)

// EventURL turns an rtsp_base (or any http/rtsp URL of the bridge) into its /ws endpoint. The wall is
// already configured with where the bridge is; making the operator repeat it as a second URL would be
// one more thing to get wrong.
func EventURL(rtspBase string) string {
	u, err := url.Parse(strings.TrimSpace(rtspBase))
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	// The bridge serves RTSP on 8554 (go2rtc) and its HTTP/WS API on 3000.
	return "ws://" + host + ":3000/ws"
}

// StillURL is where the bridge serves a camera's last retained thumbnail. Derived from the same
// rtsp_base for the same reason as EventURL: one address to configure, not three.
func StillURL(rtspBase, sn string) string {
	u := EventURL(rtspBase)
	if u == "" {
		return ""
	}
	return strings.Replace(strings.Replace(u, "ws://", "http://", 1), "/ws", "/snapshot/"+sn, 1)
}

// Run follows the channel until ctx is cancelled, applying every message to `store` and calling
// `changed` whenever a tile could care. Store synchronizes updates with concurrent renderer reads.
func Run(ctx context.Context, endpoint string, store *wallstate.Store, changed func(), log func(string)) {
	backoff := minBackoff
	for ctx.Err() == nil {
		err := follow(ctx, endpoint, store, changed)
		if ctx.Err() != nil {
			return
		}
		log("events: " + reason(err) + "; reconnecting in " + backoff.String())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func reason(err error) string {
	if err == nil {
		return "disconnected"
	}
	return err.Error()
}

func follow(ctx context.Context, endpoint string, store *wallstate.Store, changed func()) error {
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDial()
	c, _, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{HTTPClient: &http.Client{}})
	if err != nil {
		return err
	}
	defer c.CloseNow()
	// The wall only reads; a frame larger than this is not something it knows how to act on.
	c.SetReadLimit(1 << 20)

	for {
		readCtx, cancel := context.WithTimeout(ctx, readTimeout)
		_, data, err := c.Read(readCtx)
		cancel()
		if err != nil {
			return err
		}
		var msg wallstate.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // a message this client does not understand is not a reason to drop the connection
		}
		if store.Apply(msg) && changed != nil {
			changed()
		}
	}
}
