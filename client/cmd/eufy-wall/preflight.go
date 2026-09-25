package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
	"eufy-wall/internal/wsclient"
)

// preflightClientRemote checks the services and camera identities a new config will depend on before
// replacing a working wall. It only reads bridge endpoints and opens an RTSP TCP socket; a cold battery
// camera is never woken just to validate a config.
func preflightClientRemote(ctx context.Context, c *config.Config) ([]setupCamera, error) {
	bridgeURL := c.BridgeURL
	if bridgeURL == "" {
		legacy := wsclient.EventURL(c.RTSPBase)
		if legacy == "" {
			for _, tile := range c.Tiles {
				if tile.URL == "" {
					return nil, errors.New("cannot derive bridge_url; set it explicitly")
				}
			}
			return nil, nil // Legacy wall made entirely of independent RTSP URLs.
		}
		u, _ := url.Parse(legacy)
		if u.Scheme == "wss" {
			u.Scheme = "https"
		} else {
			u.Scheme = "http"
		}
		u.Path = ""
		bridgeURL = u.String()
	}
	base, err := url.Parse(bridgeURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("invalid bridge_url")
	}
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := *base
	healthURL.Path = "/healthz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("bridge health: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bridge health: HTTP %d", response.StatusCode)
	}
	var health struct {
		OK   bool `json:"ok"`
		Auth struct {
			State string `json:"state"`
		} `json:"auth"`
		Go2RTC string `json:"go2rtc"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&health); err != nil {
		return nil, fmt.Errorf("bridge health: %w", err)
	}
	if !health.OK || health.Auth.State != "ok" {
		return nil, fmt.Errorf("bridge is not ready (auth=%s)", health.Auth.State)
	}
	if health.Go2RTC != "running" {
		return nil, errors.New("bridge go2rtc process is not running")
	}
	cameras, err := fetchSetupCameras(ctx, bridgeURL, "")
	if err != nil {
		return nil, err
	}
	known := make(map[string]setupCamera, len(cameras))
	for _, camera := range cameras {
		known[camera.SN] = camera
	}
	for i, tile := range c.Tiles {
		if tile.Camera != "" {
			if _, ok := known[tile.Camera]; !ok {
				return nil, fmt.Errorf("tiles[%d].camera %q is not in bridge inventory", i, tile.Camera)
			}
		}
		for _, sn := range tile.Watch {
			if _, ok := known[sn]; !ok {
				return nil, fmt.Errorf("tiles[%d].watch camera %q is not in bridge inventory", i, sn)
			}
		}
	}
	if c.RTSPBase == "" {
		return cameras, nil
	} // URL-only legacy wall.
	rtsp, err := url.Parse(c.RTSPBase)
	if err != nil || (rtsp.Scheme != "rtsp" && rtsp.Scheme != "rtsps") || rtsp.Hostname() == "" || rtsp.User != nil || rtsp.RawQuery != "" || rtsp.Fragment != "" {
		return nil, errors.New("rtsp_base must be an RTSP(S) origin without credentials or query")
	}
	port := rtsp.Port()
	if port == "" {
		port = "554"
	}
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(rtsp.Hostname(), port))
	if err != nil {
		return nil, fmt.Errorf("RTSP endpoint %s is unreachable: %w", net.JoinHostPort(rtsp.Hostname(), port), err)
	}
	return cameras, conn.Close()
}

func codecPreflight(c *config.Config, cameras []setupCamera, caps pipeline.Caps, has func(string) bool) error {
	codecBySerial := make(map[string]string, len(cameras))
	for _, camera := range cameras {
		codecBySerial[camera.SN] = camera.Codec
	}
	for i, tile := range c.Tiles {
		serials := []string{tile.Camera}
		if tile.Motion != "" {
			serials = tile.Watch
			if len(serials) == 0 {
				for sn := range codecBySerial {
					serials = append(serials, sn)
				}
			}
		}
		for _, sn := range serials {
			codec := tile.Codec
			if reported := codecBySerial[sn]; reported != "" {
				codec = reported
			}
			if codec == "" {
				codec = "h264"
			}
			if codec != "h264" && codec != "h265" {
				return fmt.Errorf("tiles[%d]: bridge reported unsupported codec %q", i, codec)
			}
			name := caps.Element(codec)
			if name == "" || !has(name) {
				return fmt.Errorf("tiles[%d]: %s decoder for %s is unavailable", i, strings.ToUpper(caps.DecoderSummary()), codec)
			}
		}
	}
	return nil
}
