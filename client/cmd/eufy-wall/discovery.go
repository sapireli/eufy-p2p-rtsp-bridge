package main

import (
	"context"
	"encoding/json"
	"eufy-wall/internal/wsclient"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var resolvedIdentities sync.Map
var resolvedPublicIDs sync.Map

type bridgeCandidate struct{ instance, host, port, rtsp string }

func avahiCandidates(output string) []bridgeCandidate {
	var out []bridgeCandidate
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		f := strings.Split(line, ";")
		if len(f) < 10 || f[0] != "=" || f[4] != "_eufy-wall._tcp" {
			continue
		}
		port, nerr := strconv.Atoi(f[8])
		if nerr != nil || port < 1 || port > 65535 {
			continue
		}
		rtsp := ""
		for _, field := range f[9:] {
			if value, ok := strings.CutPrefix(strings.Trim(field, "\""), "rtsp="); ok {
				if n, e := strconv.Atoi(value); e == nil && n > 0 && n <= 65535 {
					rtsp = value
				}
			}
		}
		if rtsp == "" {
			continue
		}
		ip := net.ParseIP(f[7])
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		host := ip.String()
		if ip.IsLinkLocalUnicast() && ip.To4() == nil {
			host += "%" + f[1]
		}
		instance := f[3] + "." + f[4] + "." + f[5] + "|" + f[6]
		key := instance + "|" + host + "|" + f[8]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, bridgeCandidate{instance, host, f[8], rtsp})
	}
	return out
}
func (c bridgeCandidate) rtspURL() string {
	return (&url.URL{Scheme: "rtsp", Host: net.JoinHostPort(c.host, c.rtsp)}).String()
}
func (c bridgeCandidate) apiURL() string {
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(c.host, c.port), Path: "/api/cameras"}).String()
}
func parseAvahi(output string) (string, error) {
	all := avahiCandidates(output)
	if len(all) == 0 {
		return "", fmt.Errorf("no resolved bridge with valid HTTP/RTSP ports")
	}
	for _, c := range all {
		if c.port == "3000" {
			return c.rtspURL(), nil
		}
	}
	return "", fmt.Errorf("no bridge with HTTP port3000")
}
func discoverResolved(output string, probe func(string) error) (string, error) {
	return discoverResolvedPinned(output, "", probe)
}

func discoverResolvedPinned(output, expected string, probe func(string) error) (string, error) {
	all := avahiCandidates(output)
	if len(all) == 0 {
		return "", fmt.Errorf("no resolved bridge service; check bridge and network")
	}
	if expected != "" {
		filtered := all[:0]
		for _, candidate := range all {
			if candidate.instance == expected {
				filtered = append(filtered, candidate)
			}
		}
		all = filtered
		if len(all) == 0 {
			return "", fmt.Errorf("selected bridge service unavailable; check bridge and network")
		}
	}
	// Never select a different bridge merely because the first service has a dead
	// address. More than one instance requires an explicit bridge selection.
	identity := all[0].instance
	for _, c := range all {
		if c.instance != identity {
			return "", fmt.Errorf("multiple bridge services found; configure the intended bridge address")
		}
	}
	var failures []string

	for _, c := range all {
		if err := probe(c.apiURL()); err == nil {
			apiOrigin := strings.TrimSuffix(c.apiURL(), "/api/cameras")
			id, _ := resolvedPublicIDs.Load(apiOrigin)
			publicID, _ := id.(string)
			cachePath := identityCachePath()
			cache, cacheErr := readIdentityCache(cachePath)
			if cacheErr != nil {
				failures = append(failures, cacheErr.Error())
				continue
			}
			if cached := cache[c.instance]; cached != "" && cached != publicID {
				failures = append(failures, "bridge identity changed; check selected bridge")
				continue
			}
			if publicID != "" {
				if err := retainBridgeIdentity(cachePath, c.instance, publicID); err != nil {
					failures = append(failures, err.Error())
					continue
				}
			}

			api := c.apiURL()
			api = strings.TrimSuffix(api, "/api/cameras")
			wsclient.RegisterDiscoveredAPI(c.rtspURL(), api)
			resolvedIdentities.Store(c.rtspURL(), c.instance)
			return c.rtspURL(), nil
		} else {
			failures = append(failures, err.Error())
		}
	}
	return "", fmt.Errorf("bridge service found but API unreachable: %s", strings.Join(failures, "; "))
}
func probeBridge(endpoint string) error {
	return probeBridgeIdentity(endpoint, os.Getenv("EUFY_WALL_BRIDGE_ID"), strings.Split(os.Getenv("EUFY_WALL_EXPECTED_CAMERAS"), ","))
}
func probeBridgeIdentity(endpoint, expected string, known []string) error {
	client := &http.Client{Timeout: 1200 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("bridge redirect refused") }}
	resp, err := client.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bridge API HTTP%d", resp.StatusCode)
	}
	id := resp.Header.Get("X-Eufy-Wall-Bridge-Id")
	if expected != "" && id != expected {
		return fmt.Errorf("bridge identity changed; check selected bridge address")
	}
	if id != "" {
		if len(id) > 128 || strings.ContainsAny(id, " \r\n") {
			return fmt.Errorf("invalid bridge identity")
		}
		origin := strings.TrimSuffix(endpoint, "/api/cameras")
		resolvedPublicIDs.Store(origin, id)
		return nil
	}
	// Legacy compatibility needs configured-camera overlap, never a bare HTTP200.
	var cameras []struct {
		SN string `json:"sn"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cameras) != nil {
		return fmt.Errorf("bridge identity unavailable; configure intended bridge address")
	}
	for _, camera := range cameras {
		for _, sn := range known {
			if sn != "" && camera.SN == sn {
				return nil
			}
		}
	}
	return fmt.Errorf("bridge identity unavailable; configured cameras do not match")
}

func discoverBridgePinned(expected string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "avahi-browse", "--resolve", "--terminate", "--parsable", "_eufy-wall._tcp").Output()
	if err != nil {
		return "", err
	}
	return discoverResolvedPinned(string(out), expected, probeBridge)
}
func replacementBridge(current, identity string, probe func(string) error, discover func(string) (string, error)) (string, error) {
	if probe(wsclient.APIBase(current)+"/api/cameras") == nil {
		return "", nil
	}
	next, err := discover(identity)
	if err != nil {
		return "", err
	}
	if next == current {
		return "", nil
	}
	return next, nil
}

func initialDiscoveredBridge(identity, preferred, api string, probe func(string) error, discover func(string) (string, error)) (string, error) {
	if preferred != "" && api != "" && identity != "" && probe(api+"/api/cameras") == nil {
		wsclient.RegisterDiscoveredAPI(preferred, api)
		resolvedIdentities.Store(preferred, identity)
		return preferred, nil
	}
	return discover(identity)
}
func migrationAllowed(last string, now time.Time) bool {
	t, err := strconv.ParseInt(last, 10, 64)
	return err != nil || now.Unix()-t >= 30
}
