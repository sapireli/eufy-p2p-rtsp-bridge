package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/layout"
	"gopkg.in/yaml.v3"
)

type setupAnswers struct {
	BridgeURL     string   `yaml:"bridge_url"`
	RTSPBase      string   `yaml:"rtsp_base"`
	Output        string   `yaml:"output"`
	InventoryFile string   `yaml:"inventory_file"`
	Cameras       []string `yaml:"cameras"`
	Template      string   `yaml:"template"`
	Decoder       string   `yaml:"decoder"`
	Sink          string   `yaml:"sink"`
	Planes        []int    `yaml:"planes"`
	ProbeStreams  bool     `yaml:"probe_streams"`
}

type setupCamera struct {
	SN        string `json:"sn"`
	Name      string `json:"name"`
	Codec     string `json:"codec"`
	Mode      string `json:"mode"`
	Powered   bool   `json:"powered"`
	StreamKey string `json:"streamKey"`
}

type setupTile struct {
	ID                string      `yaml:"id"`
	Camera            string      `yaml:"camera,omitempty"`
	Motion            string      `yaml:"motion,omitempty"`
	Watch             []string    `yaml:"watch,omitempty"`
	BlankAfterSeconds int         `yaml:"blank_after_seconds,omitempty"`
	Codec             string      `yaml:"codec,omitempty"`
	Rect              config.Rect `yaml:"rect"`
}

type setupFile struct {
	SchemaVersion int           `yaml:"schema_version"`
	BridgeURL     string        `yaml:"bridge_url"`
	RTSPBase      string        `yaml:"rtsp_base"`
	Output        string        `yaml:"output,omitempty"`
	Decoder       string        `yaml:"decoder,omitempty"`
	Sink          string        `yaml:"sink,omitempty"`
	Planes        []int         `yaml:"planes,omitempty"`
	Layout        string        `yaml:"layout"`
	Canvas        config.Canvas `yaml:"canvas"`
	Tiles         []setupTile   `yaml:"tiles"`
}

func fetchSetupCameras(ctx context.Context, bridgeURL, inventoryFile string) ([]setupCamera, error) {
	var data []byte
	if inventoryFile != "" {
		b, err := os.ReadFile(inventoryFile)
		if err != nil {
			return nil, err
		}
		data = b
	} else {
		origin, err := url.Parse(bridgeURL)
		if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") {
			return nil, errors.New("bridge_url must be an HTTP(S) origin")
		}
		origin.Path = "/api/cameras"
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			return nil, fmt.Errorf("camera inventory: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("camera inventory: HTTP %d", response.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		if err != nil {
			return nil, err
		}
	}
	if len(data) > 1<<20 {
		return nil, errors.New("camera inventory exceeds 1 MiB")
	}
	var cameras []setupCamera
	if inventoryFile == "" {
		if err := json.Unmarshal(data, &cameras); err != nil {
			return nil, fmt.Errorf("camera inventory: %w", err)
		}
	} else {
		var exported struct {
			SchemaVersion int           `json:"schema_version"`
			Cameras       []setupCamera `json:"cameras"`
		}
		if err := json.Unmarshal(data, &exported); err != nil {
			return nil, fmt.Errorf("camera inventory: %w", err)
		}
		if exported.SchemaVersion != 1 {
			return nil, fmt.Errorf("camera inventory schema_version %d is unsupported", exported.SchemaVersion)
		}
		cameras = exported.Cameras
	}
	if len(cameras) == 0 {
		return nil, errors.New("no cameras found in inventory")
	}
	seen := make(map[string]bool, len(cameras))
	for _, camera := range cameras {
		if camera.SN == "" || seen[camera.SN] {
			return nil, errors.New("camera inventory has a missing or duplicate serial")
		}
		seen[camera.SN] = true
	}
	return cameras, nil
}

func setupYAML(a setupAnswers, available []setupCamera) ([]byte, error) {
	return setupYAMLForOS(runtime.GOOS, a, available)
}

func setupYAMLForOS(goos string, a setupAnswers, available []setupCamera) ([]byte, error) {
	if goos == "darwin" && a.Output != "" {
		return nil, errors.New("macOS window sink opens on the main display; leave output empty")
	}
	if goos == "darwin" && (a.Sink != "" && a.Sink != "auto" && a.Sink != "window" || len(a.Planes) > 0) {
		return nil, errors.New("macOS setup uses a window sink; leave planes empty")
	}
	if len(a.Cameras) == 0 {
		return nil, errors.New("choose at least one camera")
	}
	bySerial := make(map[string]setupCamera, len(available))
	for _, c := range available {
		bySerial[c.SN] = c
	}
	selected := make([]setupCamera, 0, len(a.Cameras))
	seen := map[string]bool{}
	for _, sn := range a.Cameras {
		camera, ok := bySerial[sn]
		if !ok {
			return nil, fmt.Errorf("camera %q is absent from inventory", sn)
		}
		if seen[sn] {
			return nil, fmt.Errorf("camera %q was selected twice", sn)
		}
		seen[sn] = true
		selected = append(selected, camera)
	}
	template := a.Template
	if template == "" {
		if len(selected) == 1 {
			template = "one"
		} else {
			template = "split"
		}
	}
	geometry, err := layout.StarterTemplate(template)
	if err != nil {
		return nil, err
	}
	switch template {
	case "one":
		if len(selected) != 1 {
			return nil, errors.New("one template needs exactly one camera")
		}
	case "split":
		if len(selected) != 2 {
			return nil, errors.New("split template needs exactly two cameras")
		}
	case "four":
		if len(selected) < 1 || len(selected) > 4 {
			return nil, errors.New("four template accepts one to four cameras")
		}
	case "1+5", "one-plus-five":
		if len(selected) != 6 {
			return nil, errors.New("1+5 template needs exactly six cameras")
		}
	case "motion":
	}
	file := setupFile{SchemaVersion: 2, BridgeURL: a.BridgeURL, RTSPBase: a.RTSPBase, Output: a.Output, Decoder: a.Decoder, Sink: a.Sink, Planes: a.Planes, Layout: "custom", Canvas: config.Canvas{Cols: 32, Rows: 32}}
	if template == "motion" {
		file.Tiles = []setupTile{{ID: geometry[0].ID, Motion: "latest", Watch: a.Cameras, BlankAfterSeconds: 90, Rect: geometry[0].Rect}}
	} else {
		for i, camera := range selected {
			tile := setupTile{ID: geometry[i].ID, Camera: camera.SN, Rect: geometry[i].Rect}
			if camera.Codec == "h265" {
				tile.Codec = "h265"
			}
			file.Tiles = append(file.Tiles, tile)
		}
	}
	if (a.Sink == "planes" || (a.Sink == "" || a.Sink == "auto") && len(a.Planes) > 0) && len(a.Planes) < len(file.Tiles) {
		return nil, fmt.Errorf("setup: %d tiles need at least %d plane IDs; use sink: compositor or list one plane per tile", len(file.Tiles), len(file.Tiles))
	}
	b, err := yaml.Marshal(file)
	if err != nil {
		return nil, err
	}
	if err := validateClientConfig(b, false); err != nil {
		return nil, err
	}
	return b, nil
}

func setupWall(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	answersPath, outputPath := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--answers", "--output":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a file", args[i])
			}
			if args[i] == "--answers" {
				answersPath = args[i+1]
			} else {
				outputPath = args[i+1]
			}
			i++
		default:
			return fmt.Errorf("unknown setup option %q", args[i])
		}
	}
	var a setupAnswers
	if answersPath != "" {
		data, err := os.ReadFile(answersPath)
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return errors.New("setup answers exceed 1 MiB")
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&a); err != nil {
			return fmt.Errorf("setup answers: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return errors.New("setup answers: only one YAML document is allowed")
		}
	} else {
		reader := bufio.NewReader(in)
		ask := func(label, fallback string) (string, error) {
			_, _ = fmt.Fprintf(out, "%s [%s]: ", label, fallback)
			line, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			if err == io.EOF && line == "" {
				return "", errors.New("setup input ended before completion")
			}
			if value := strings.TrimSpace(line); value != "" {
				return value, nil
			}
			return fallback, nil
		}
		var err error
		a.BridgeURL, err = ask("Bridge HTTP URL", "http://127.0.0.1:3000")
		if err != nil {
			return err
		}
		parsed, err := url.Parse(a.BridgeURL)
		if err != nil {
			return err
		}
		a.RTSPBase, err = ask("RTSP base", "rtsp://"+parsed.Hostname()+":8554")
		if err != nil {
			return err
		}
		if runtime.GOOS == "darwin" {
			_, _ = fmt.Fprintln(out, "Display: main macOS desktop (window sink)")
		} else {
			a.Output, err = ask("Display output (blank for first HDMI)", "")
			if err != nil {
				return err
			}
		}
		cameras, err := fetchSetupCameras(ctx, a.BridgeURL, "")
		if err != nil {
			return err
		}
		for _, camera := range cameras {
			_, _ = fmt.Fprintf(out, "  %s  %s  mode=%s codec=%s powered=%v\n", camera.SN, camera.Name, camera.Mode, camera.Codec, camera.Powered)
		}
		list, err := ask("Camera serials (comma separated)", "")
		if err != nil {
			return err
		}
		for _, sn := range strings.Split(list, ",") {
			if trimmed := strings.TrimSpace(sn); trimmed != "" {
				a.Cameras = append(a.Cameras, trimmed)
			}
		}
		a.Template, err = ask("Template (one/split/four/1+5/motion)", "split")
		if err != nil {
			return err
		}
		if a.Template == "split" && len(a.Cameras) == 1 {
			a.Template = "one"
		}
		_, _ = fmt.Fprintf(out, "Bridge: %s\nRTSP: %s\nOutput: %s\nCameras: %s\nTemplate: %s\n", a.BridgeURL, a.RTSPBase, a.Output, strings.Join(a.Cameras, ", "), a.Template)
		probeAnswer, err := ask("Decode two frames from each selected camera before applying? (yes/no)", "no")
		if err != nil {
			return err
		}
		if probeAnswer != "yes" && probeAnswer != "no" {
			return errors.New("probe choice must be yes or no")
		}
		a.ProbeStreams = probeAnswer == "yes"
		if outputPath == "" {
			confirmed, err := ask("Apply this config and restart eufy-wall? (yes/no)", "no")
			if err != nil {
				return err
			}
			if confirmed != "yes" {
				return errors.New("setup cancelled; no config changed")
			}
		}
		return finishSetup(ctx, a, cameras, outputPath, out, probeFrames)
	}
	if a.BridgeURL == "" || a.RTSPBase == "" {
		return errors.New("bridge_url and rtsp_base are required in setup answers")
	}
	cameras, err := fetchSetupCameras(ctx, a.BridgeURL, a.InventoryFile)
	if err != nil {
		return err
	}
	return finishSetup(ctx, a, cameras, outputPath, out, probeFrames)
}

func finishSetup(ctx context.Context, a setupAnswers, cameras []setupCamera, outputPath string, out io.Writer, probe func(context.Context, *config.Config, string, io.Writer) error) error {
	b, err := setupYAML(a, cameras)
	if err != nil {
		return err
	}
	if a.ProbeStreams {
		c, err := config.Parse(b)
		if err != nil {
			return err
		}
		for _, serial := range a.Cameras {
			if err := probe(ctx, c, serial, out); err != nil {
				return fmt.Errorf("probe %s failed; no config applied: %w", serial, err)
			}
		}
	}
	if outputPath != "" {
		if _, err := os.Stat(outputPath); err == nil {
			return fmt.Errorf("draft %q already exists", outputPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := atomicClientWrite(outputPath, b, 0600, nil); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "draft saved: %s\n", outputPath)
		return nil
	}
	return applyClientConfig("-", bytes.NewReader(b), out)
}
