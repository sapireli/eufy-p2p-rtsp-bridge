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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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
var discoveredOrigins sync.Map

// RegisterDiscoveredAPI binds both address families and the advertised HTTP port to this resolved bridge.
func RegisterDiscoveredAPI(rtspBase, apiBase string) { discoveredOrigins.Store(rtspBase, apiBase) }

func EventURL(rtspBase string) string {
	if api, ok := discoveredOrigins.Load(rtspBase); ok {
		rtspBase = api.(string)
	}
	u, err := url.Parse(strings.TrimSpace(rtspBase))
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}

	port := "3000"
	scheme := "ws"
	switch u.Scheme {
	case "http", "https", "ws", "wss":
		port = "80"
		if u.Scheme == "https" || u.Scheme == "wss" {
			port = "443"
		}
		if u.Port() != "" {
			port = u.Port()
		}
	case "rtsp", "rtsps":
	default:
		return ""
	}
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "wss"
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port), Path: "/ws"}).String()
}

// StillURL is where the bridge serves a camera's last retained thumbnail. Derived from the same
// rtsp_base for the same reason as EventURL: one address to configure, not three.
func StillURL(rtspBase, sn string) string {
	u := EventURL(rtspBase)
	if u == "" {
		return ""
	}
	parsed, _ := url.Parse(u)
	if parsed.Scheme == "wss" {
		parsed.Scheme = "https"
	} else {
		parsed.Scheme = "http"
	}
	parsed.Path = "/snapshot/" + sn
	return parsed.String()
}

// APIBase is the bridge HTTP endpoint paired with the configured RTSP service.
func APIBase(rtspBase string) string {
	endpoint := EventURL(rtspBase)
	if endpoint == "" {
		return ""
	}
	parsed, _ := url.Parse(endpoint)
	if parsed.Scheme == "wss" {
		parsed.Scheme = "https"
	} else {
		parsed.Scheme = "http"
	}
	parsed.Path = ""
	return parsed.String()
}

// Run follows the channel until ctx is cancelled, applying every message to `store` and calling
// `changed` whenever a tile could care. Store synchronizes updates with concurrent renderer reads.
func Run(ctx context.Context, endpoint string, store *wallstate.Store, changed func(), log func(string)) {
	RunWithMotion(ctx, endpoint, store, changed, nil, log)
}

// RunWithMotion delivers each live motion notification after selection changes
// have been applied. Hello snapshots never replay an old motion flash.
func RunWithMotion(ctx context.Context, endpoint string, store *wallstate.Store, changed func(), motion func(string), log func(string)) {
	backoff := minBackoff
	for ctx.Err() == nil {
		err := follow(ctx, endpoint, store, changed, motion, log)
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

func follow(ctx context.Context, endpoint string, store *wallstate.Store, changed func(), motion func(string), report func(string)) error {
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDial()
	c, _, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{HTTPClient: &http.Client{}})
	if err != nil {
		return err
	}
	defer c.CloseNow()
	report("events: connected")
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
		if msg.Type == "hello" && os.Getenv("EUFY_WALL_BRIDGE_ID") != "" && msg.BridgeID != os.Getenv("EUFY_WALL_BRIDGE_ID") {
			return fmt.Errorf("bridge identity changed; select intended bridge address")
		}
		if (msg.Type == "hello" || msg.Type == "auth") && (msg.Auth != nil || msg.Ready != nil) {
			state := ""
			if msg.Auth != nil {
				state = msg.Auth.State
			}
			report(authReport(state, msg.Ready))
		}

		if store.Apply(msg) {
			if changed != nil {
				changed()
			}
			if msg.Type == "motion" && msg.SN != "" && motion != nil && ctx.Err() == nil {
				motion(msg.SN)
			}
		}
	}
}

// Account challenges outrank an earlier ready flag retained during reauthentication.
func authReport(state string, ready *bool) string {
	if state != "" && state != "ok" {
		return "auth: " + state
	}
	if ready != nil && !*ready {
		return "auth: starting"
	}
	return "auth: ready"
}
