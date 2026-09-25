package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"eufy-wall/internal/wsclient"
)

const holdRefreshInterval = 20 * time.Second
const holdRetryInterval = 5 * time.Second

var wallHoldOwner = func() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("wall:%s:%d", host, os.Getpid())
}()

type holdWorker struct {
	desired bool
	ttl     time.Duration
	changed chan struct{}
}

// holdCoordinator serializes requests for each camera. A late POST cannot resurrect a released
// hold, and a late DELETE cannot undo a newer POST. Different cameras remain independent.
type holdCoordinator struct {
	base    string
	request func(context.Context, string, string, string) error
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	workers map[string]*holdWorker
	closed  bool
	wg      sync.WaitGroup
	refresh time.Duration
	retry   time.Duration
}

func newHoldCoordinator(base string, request func(context.Context, string, string, string) error) *holdCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	return &holdCoordinator{base: base, request: request, ctx: ctx, cancel: cancel, workers: map[string]*holdWorker{}, refresh: holdRefreshInterval, retry: holdRetryInterval}
}

func (h *holdCoordinator) Update(wanted map[string]time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for sn, ttl := range wanted {
		if ttl <= 0 {
			continue
		}
		if h.workers[sn] == nil {
			w := &holdWorker{changed: make(chan struct{}, 1)}
			h.workers[sn] = w
			h.wg.Add(1)
			go h.run(sn, w)
		}
	}
	for sn, w := range h.workers {
		ttl := wanted[sn]
		want := ttl > 0
		if w.desired != want || w.ttl != ttl {
			w.desired = want
			w.ttl = ttl
			select {
			case w.changed <- struct{}{}:
			default:
			}
		}
	}
}

func (h *holdCoordinator) run(sn string, w *holdWorker) {
	defer h.wg.Done()
	attempted := false
	for {
		select {
		case <-w.changed:
		default:
		}
		h.mu.Lock()
		want, closed, ttl := w.desired, h.closed, w.ttl
		h.mu.Unlock()
		if h.ctx.Err() != nil || (closed && !want && !attempted) {
			return
		}
		if want {
			// Even a failed POST may have reached the server. A later release still sends DELETE.
			attempted = true
			err := h.call(http.MethodPost, sn, 5*time.Second)
			interval := h.refreshFor(ttl)
			if err != nil {
				interval = h.retry
			}
			h.wait(w, interval)
			continue
		}
		if attempted {
			if err := h.call(http.MethodDelete, sn, 3*time.Second); err != nil {
				h.wait(w, h.retry)
				continue
			}
			attempted = false
			continue
		}
		h.wait(w, 0)
	}
}

func (h *holdCoordinator) refreshFor(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return h.refresh
	}
	// The server owns the lifetime. Refresh before even a short user-configured hold expires.
	interval := min(h.refresh, ttl/2)
	return max(100*time.Millisecond, interval)
}

func (h *holdCoordinator) call(method, sn string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(h.ctx, timeout)
	defer cancel()
	if err := h.request(ctx, h.base, method, sn); err != nil {
		log.Printf("[wall] %s hold %s failed: %v", strings.ToLower(method), sn, err)
		return err
	}
	return nil
}

func (h *holdCoordinator) wait(w *holdWorker, interval time.Duration) {
	if interval == 0 {
		select {
		case <-w.changed:
		case <-h.ctx.Done():
		}
		return
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-w.changed:
	case <-timer.C:
	case <-h.ctx.Done():
	}
}

// Close allows an in-flight POST to finish before its DELETE and bounds shutdown if the LAN is down.
func (h *holdCoordinator) Close() {
	h.mu.Lock()
	h.closed = true
	for _, w := range h.workers {
		w.desired = false
		select {
		case w.changed <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(9 * time.Second):
		h.cancel()
		<-done
	}
	h.cancel()
}

func requestWallHold(ctx context.Context, base, method, sn string) error {
	u := wsclient.EventURL(base)
	if u == "" {
		return fmt.Errorf("invalid bridge URL %q", base)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	if parsed.Scheme == "wss" {
		parsed.Scheme = "https"
	} else {
		parsed.Scheme = "http"
	}
	parsed.Path = "/hold/" + sn
	parsed.RawQuery = url.Values{"owner": {wallHoldOwner}}.Encode()
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func holdRequest(ctx context.Context, base, method, sn string) {
	if err := requestWallHold(ctx, base, method, sn); err != nil {
		log.Printf("[wall] %s hold %s failed: %v", strings.ToLower(method), sn, err)
	}
}
