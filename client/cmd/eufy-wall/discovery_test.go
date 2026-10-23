package main

import (
	"eufy-wall/internal/wsclient"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryFallsBackAddressWithinSameInstance(t *testing.T) {
	records := "=;eth0;IPv6;Bridge;_eufy-wall._tcp;local;bridge.local;2001:db8::1;3000;\"rtsp=8554\"\n=;eth0;IPv4;Bridge;_eufy-wall._tcp;local;bridge.local;192.0.2.5;3000;\"rtsp=8554\"\n"
	var probes []string
	got, err := discoverResolved(records, func(u string) error {
		probes = append(probes, u)
		if strings.Contains(u, "[2001:") {
			return fmt.Errorf("connection refused")
		}
		return nil
	})
	if err != nil || got != "rtsp://192.0.2.5:8554" || len(probes) != 2 {
		t.Fatalf("%q %v %v", got, err, probes)
	}
	got, err = discoverResolved(records, func(u string) error { return nil })
	if err != nil || got != "rtsp://[2001:db8::1]:8554" {
		t.Fatalf("healthyIPv6rejected %s %v", got, err)
	}
}
func TestDiscoveryNeverSilentlySelectsUnrelatedInstance(t *testing.T) {
	records := "=;eth0;IPv6;A;_eufy-wall._tcp;local;a.local;2001:db8::1;3000;\"rtsp=8554\"\n=;eth0;IPv4;B;_eufy-wall._tcp;local;b.local;192.0.2.5;3000;\"rtsp=8554\"\n"
	probes := 0
	_, err := discoverResolved(records, func(string) error { probes++; return nil })
	if err == nil || !strings.Contains(err.Error(), "multiple") || probes != 0 {
		t.Fatalf("err%v probes%d", err, probes)
	}
}
func TestDiscoveryRetainsAdvertisedHTTPPort(t *testing.T) {
	got, err := discoverResolved("=;eth0;IPv6;Custom;_eufy-wall._tcp;local;custom.local;2001:db8::6;3101;\"rtsp=8555\"\n", func(u string) error {
		if u != "http://[2001:db8::6]:3101/api/cameras" {
			t.Fatal(u)
		}
		return nil
	})
	if err != nil || wsclient.APIBase(got) != "http://[2001:db8::6]:3101" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRuntimeFamilyFailureRebindsAllOriginsToSameService(t *testing.T) {
	identity := "Pinned._eufy-wall._tcp.local|bridge.local"
	current := "rtsp://[2001:db8::10]:8554"
	records := "=;eth0;IPv6;Pinned;_eufy-wall._tcp;local;bridge.local;2001:db8::10;3000;\"rtsp=8554\"\n=;eth0;IPv4;Pinned;_eufy-wall._tcp;local;bridge.local;192.0.2.10;3000;\"rtsp=8554\"\n=;eth0;IPv4;Unrelated;_eufy-wall._tcp;local;other.local;192.0.2.99;3000;\"rtsp=8554\"\n"
	probe := func(u string) error {
		if strings.Contains(u, "[2001:") {
			return fmt.Errorf("connection refused")
		}
		return nil
	}
	next, err := replacementBridge(current, identity, probe, func(id string) (string, error) {
		if id != identity {
			t.Fatal(id)
		}
		return discoverResolvedPinned(records, id, probe)
	})
	if err != nil || next != "rtsp://192.0.2.10:8554" {
		t.Fatalf("%s %v", next, err)
	}
	if wsclient.APIBase(next) != "http://192.0.2.10:3000" || wsclient.EventURL(next) != "ws://192.0.2.10:3000/ws" || wsclient.StillURL(next, "CAM") != "http://192.0.2.10:3000/snapshot/CAM" {
		t.Fatal("origins not rebound")
	}
	got, err := initialDiscoveredBridge(identity, next, wsclient.APIBase(next), probe, func(string) (string, error) { t.Fatal("healthy verified endpoint discarded"); return "", nil })
	if err != nil || got != next {
		t.Fatal(got, err)
	}
	discoverCalls := 0
	got, err = replacementBridge(next, identity, probe, func(string) (string, error) { discoverCalls++; return "", nil })
	if err != nil || got != "" || discoverCalls != 0 {
		t.Fatal("healthy bridge replaced for isolated failure")
	}
}
func TestMigrationCooldownBoundsRepeatedEndpointReplacement(t *testing.T) {
	now := time.Unix(1000, 0)
	if migrationAllowed("990", now) || !migrationAllowed("970", now) || !migrationAllowed("", now) {
		t.Fatal("bad cooldown")
	}
}

func TestBridgeIdentityGuardsDHCPReuseAndLegacyOverlap(t *testing.T) {
	id := "public-bridge-A"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id != "" {
			w.Header().Set("X-Eufy-Wall-Bridge-Id", id)
		}
		w.Write([]byte(`[{"sn":"CAM"}]`))
	}))
	defer s.Close()
	endpoint := s.URL + "/api/cameras"
	if err := probeBridgeIdentity(endpoint, "public-bridge-A", nil); err != nil {
		t.Fatal(err)
	}
	id = "public-bridge-B"
	if probeBridgeIdentity(endpoint, "public-bridge-A", []string{"CAM"}) == nil {
		t.Fatal("DHCP-reused bridge accepted")
	}
	id = ""
	if probeBridgeIdentity(endpoint, "public-bridge-A", []string{"CAM"}) == nil {
		t.Fatal("known identity downgraded")
	}
	if probeBridgeIdentity(endpoint, "", []string{"CAM"}) != nil {
		t.Fatal("legacy configured camera rejected")
	}
	if probeBridgeIdentity(endpoint, "", nil) == nil {
		t.Fatal("bare HTTP200 accepted")
	}
}
