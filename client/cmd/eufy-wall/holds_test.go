package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

func waitHoldEvent(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hold request")
		return ""
	}
}

func TestHoldCoordinatorSerializesDelayedPostAndRelease(t *testing.T) {
	started := make(chan string, 2)
	unblock := make(chan struct{})
	var mu sync.Mutex
	var calls []string
	h := newHoldCoordinator("bridge", func(_ context.Context, _, method, sn string) error {
		started <- method
		if method == http.MethodPost {
			<-unblock
		}
		mu.Lock()
		calls = append(calls, method+" "+sn)
		mu.Unlock()
		return nil
	})
	defer h.Close()
	h.Update(map[string]time.Duration{"A": 60 * time.Second})
	if got := waitHoldEvent(t, started); got != http.MethodPost {
		t.Fatalf("first request = %s", got)
	}
	h.Update(nil)
	select {
	case got := <-started:
		t.Fatalf("request %s overtook in-flight POST", got)
	default:
	}
	close(unblock)
	if got := waitHoldEvent(t, started); got != http.MethodDelete {
		t.Fatalf("second request = %s", got)
	}
	h.Close()
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(calls, []string{"POST A", "DELETE A"}) {
		t.Fatalf("hold order = %v", calls)
	}
}

func TestHoldCoordinatorRepostsAfterDelayedDelete(t *testing.T) {
	started := make(chan string, 4)
	unblockDelete := make(chan struct{})
	h := newHoldCoordinator("bridge", func(_ context.Context, _, method, _ string) error {
		started <- method
		if method == http.MethodDelete {
			<-unblockDelete
		}
		return nil
	})
	h.Update(map[string]time.Duration{"A": 60 * time.Second})
	if got := waitHoldEvent(t, started); got != http.MethodPost {
		t.Fatal(got)
	}
	h.Update(nil)
	if got := waitHoldEvent(t, started); got != http.MethodDelete {
		t.Fatal(got)
	}
	h.Update(map[string]time.Duration{"A": 60 * time.Second})
	close(unblockDelete)
	if got := waitHoldEvent(t, started); got != http.MethodPost {
		t.Fatalf("new hold did not follow DELETE: %s", got)
	}
	h.Close()
}

func TestHoldCoordinatorRetriesFailedPostAndDoesNotBlockOtherCameras(t *testing.T) {
	started := make(chan string, 8)
	unblockA := make(chan struct{})
	var mu sync.Mutex
	attempts := 0
	h := newHoldCoordinator("bridge", func(_ context.Context, _, method, sn string) error {
		if method == http.MethodPost && sn == "A" {
			<-unblockA
			mu.Lock()
			attempts++
			n := attempts
			mu.Unlock()
			started <- method + " " + sn
			if n == 1 {
				return errors.New("temporary LAN failure")
			}
			return nil
		}
		started <- method + " " + sn
		return nil
	})
	h.retry = 10 * time.Millisecond
	h.Update(map[string]time.Duration{"A": 60 * time.Second, "B": 60 * time.Second})
	if got := waitHoldEvent(t, started); got != "POST B" {
		t.Fatalf("blocked camera A delayed B: %s", got)
	}
	close(unblockA)
	if got := waitHoldEvent(t, started); got != "POST A" {
		t.Fatal(got)
	}
	if got := waitHoldEvent(t, started); got != "POST A" {
		t.Fatalf("failed hold was not retried: %s", got)
	}
	h.Close()
}

func TestHoldCoordinatorAdaptsWhenBridgeShortensHoldLifetime(t *testing.T) {
	requests := make(chan string, 4)
	h := newHoldCoordinator("bridge", func(_ context.Context, _, method, _ string) error {
		requests <- method
		return nil
	})
	if got := h.refreshFor(5 * time.Second); got != 2500*time.Millisecond {
		t.Fatalf("five-second server hold refreshes after %s", got)
	}
	h.Update(map[string]time.Duration{"A": 60 * time.Second})
	if got := waitHoldEvent(t, requests); got != http.MethodPost {
		t.Fatal(got)
	}
	h.Update(map[string]time.Duration{"A": 5 * time.Second})
	if got := waitHoldEvent(t, requests); got != http.MethodPost {
		t.Fatalf("shorter hold did not refresh immediately: %s", got)
	}
	h.Close()
}
