package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/layout"
)

// runCommand handles the operator commands built into the renderer. Legacy flag invocations are
// dispatched by main and still start the wall as before.
func runCommand(args []string, in io.Reader, out io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "setup":
		return true, setupWall(context.Background(), args[1:], in, out)
	case "config":
		if len(args) < 2 {
			return true, errors.New("usage: eufy-wall config validate|apply|recover|example ...")
		}
		switch args[1] {
		case "explain":
			if len(args) > 3 {
				return true, errors.New("usage: eufy-wall config explain [field]")
			}
			fields := map[string]string{
				"schema_version": "2 uses strict field validation and stable tile IDs; omit for legacy YAML",
				"bridge_url":     "HTTP(S) control origin for events, inventory, stills, and holds",
				"rtsp_base":      "RTSP origin for camera video, separate from bridge_url",
				"layout":         "custom needs explicit nonoverlapping rects on a 1..32 canvas; presets remain available",
				"tiles":          "stable id, camera or motion source, and rect for custom layouts",
				"decoder":        "auto, v4l2, va, or software; actual codec must be installed on this host",
				"sink":           "auto, planes, compositor, or window; planes need one verified ID per tile",
			}
			if len(args) == 3 {
				value, ok := fields[args[2]]
				if !ok {
					return true, fmt.Errorf("unknown config field %q; see docs/config-client.md", args[2])
				}
				_, err := fmt.Fprintf(out, "%s: %s\n", args[2], value)
				return true, err
			}
			for _, key := range []string{"schema_version", "bridge_url", "rtsp_base", "layout", "tiles", "decoder", "sink"} {
				_, _ = fmt.Fprintf(out, "%s: %s\n", key, fields[key])
			}
			return true, nil
		case "validate":
			if len(args) != 3 {
				return true, errors.New("usage: eufy-wall config validate <file|->")
			}
			c, err := parseClientInput(args[2], in)
			if err != nil {
				return true, err
			}
			if _, err := placeForValidation(c); err != nil {
				return true, err
			}
			_, _ = fmt.Fprintln(out, "config valid")
			return true, nil
		case "example":
			if len(args) != 2 {
				return true, errors.New("usage: eufy-wall config example")
			}
			_, err := out.Write(config.Example())
			return true, err
		case "apply":
			if len(args) != 3 {
				return true, errors.New("usage: eufy-wall config apply <file|->")
			}
			return true, applyClientConfig(args[2], in, out)
		case "recover":
			if len(args) != 2 {
				return true, errors.New("usage: eufy-wall config recover")
			}
			return true, recoverClientConfig(clientConfigPath)
		default:
			return true, fmt.Errorf("unknown config command %q", args[1])
		}
	case "layout":
		if len(args) >= 2 && args[1] == "edit" {
			if len(args) > 3 {
				return true, errors.New("usage: eufy-wall layout edit [file]")
			}
			path := ""
			if len(args) == 3 {
				path = args[2]
			}
			return true, editLayout(path, in, out)
		}
		if len(args) < 3 || args[1] != "preview" {
			return true, errors.New("usage: eufy-wall layout preview <file|-> [--png path] [--display] [--width n --height n]")
		}
		opts := PreviewOptions{}
		for i := 3; i < len(args); i++ {
			switch args[i] {
			case "--png":
				if i+1 >= len(args) {
					return true, errors.New("--png requires a path")
				}
				i++
				opts.PNGPath = args[i]
			case "--display":
				opts.Display = true
			case "--width", "--height":
				if i+1 >= len(args) {
					return true, fmt.Errorf("%s requires a number", args[i])
				}
				value, err := strconv.Atoi(args[i+1])
				if err != nil {
					return true, err
				}
				if args[i] == "--width" {
					opts.Width = value
				} else {
					opts.Height = value
				}
				i++
			default:
				return true, fmt.Errorf("unknown preview option %q", args[i])
			}
		}
		if (opts.Width == 0) != (opts.Height == 0) {
			return true, errors.New("--width and --height must be provided together")
		}
		return true, previewLayout(args[2], in, out, opts)
	case "doctor":
		if len(args) > 2 || len(args) == 2 && args[1] != "--json" {
			return true, errors.New("usage: eufy-wall doctor [--json]")
		}
		return true, clientDoctor(len(args) == 2, out)
	case "status":
		if len(args) > 2 || len(args) == 2 && args[1] != "--json" {
			return true, errors.New("usage: eufy-wall status [--json]")
		}
		return true, showClientStatus(clientConfigPath, len(args) == 2, out, func() error { return systemctl("is-active", "--quiet", "eufy-wall") })
	case "help":
		_, _ = io.WriteString(out, clientHelp)
		return true, nil
	default:
		if strings.HasPrefix(args[0], "-") {
			return false, nil
		}
		return true, fmt.Errorf("unknown command %q; run eufy-wall help", args[0])
	}
}

const clientHelp = "eufy-wall setup and renderer\n\nCommands:\n  setup [--answers file] [--output draft.yaml]\n  config validate <file|->\n  config apply <file|->\n  config recover\n  config example\n  config explain [field]\n  layout edit [file]\n  layout preview <file|-> [--png path] [--display]\n  doctor [--json]\n  status [--json]\n\nLegacy renderer flags: -config, -dry-run, -print-layout\n"

func readClientInput(path string, in io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(io.LimitReader(in, (4<<20)+1))
	}
	return os.ReadFile(path)
}

func parseClientInput(path string, in io.Reader) (*config.Config, error) {
	b, err := readClientInput(path, in)
	if err != nil {
		return nil, err
	}
	if len(b) > 4<<20 {
		return nil, errors.New("config exceeds 4 MiB")
	}
	c, err := config.Parse(b)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func placeForValidation(c *config.Config) ([]layout.Placed, error) {
	screen := c.Screen
	if screen.Width == 0 || screen.Height == 0 {
		screen = config.Screen{Width: 1920, Height: 1080}
	}
	return layout.Place(c, screen)
}

type doctorReport struct {
	Screen      config.Screen `json:"screen"`
	Decoder     string        `json:"decoder,omitempty"`
	Sink        string        `json:"sink,omitempty"`
	Watchdog    bool          `json:"watchdog"`
	GstLaunch   bool          `json:"gstLaunch"`
	BridgeReady bool          `json:"bridgeReady"`
	ConfigValid bool          `json:"configValid"`
	Problems    []string      `json:"problems"`
}

func clientDoctor(jsonOutput bool, out io.Writer) error {
	return clientDoctorAt(clientConfigPath, jsonOutput, out)
}

func clientDoctorAt(path string, jsonOutput bool, out io.Writer) error {
	report := doctorReport{Problems: []string{}}
	if s, ok := detect.Screen("/"); ok {
		report.Screen = s
	} else {
		report.Problems = append(report.Problems, "no connected HDMI output found")
	}
	report.Watchdog = detect.HasElement("watchdog")
	if !report.Watchdog {
		report.Problems = append(report.Problems, "GStreamer watchdog is missing (install gstreamer1.0-plugins-bad)")
	}
	_, err := os.Stat(path)
	if err == nil {
		c, parseErr := config.Load(path)
		if parseErr != nil {
			report.Problems = append(report.Problems, parseErr.Error())
		} else {
			report.ConfigValid = true
			if _, placeErr := placeForValidation(c); placeErr != nil {
				report.ConfigValid = false
				report.Problems = append(report.Problems, placeErr.Error())
			}
			if caps, resolveErr := detect.Resolve(c, detect.HasElement, detect.FileExists); resolveErr != nil {
				report.Problems = append(report.Problems, resolveErr.Error())
			} else {
				report.Decoder, report.Sink = caps.Decoder, caps.Sink
				if cameras, preflightErr := preflightClientRemote(context.Background(), c); preflightErr != nil {
					report.Problems = append(report.Problems, preflightErr.Error())
				} else if codecErr := codecPreflight(c, cameras, caps.Decoder, detect.HasElement); codecErr != nil {
					report.Problems = append(report.Problems, codecErr.Error())
				} else {
					report.BridgeReady = true
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		report.Problems = append(report.Problems, err.Error())
	} else {
		report.Problems = append(report.Problems, "config not installed at "+path)
	}
	if _, err := exec.LookPath("gst-launch-1.0"); err == nil {
		report.GstLaunch = true
	} else {
		report.Problems = append(report.Problems, "gst-launch-1.0 is missing")
	}
	if jsonOutput {
		if err := json.NewEncoder(out).Encode(report); err != nil {
			return err
		}
		if len(report.Problems) > 0 {
			return errors.New("doctor found problems")
		}
		return nil
	}
	_, _ = fmt.Fprintf(out, "screen: %dx%d\ndecoder: %s\nsink: %s\nwatchdog: %v\ngst-launch: %v\nconfig valid: %v\nbridge ready: %v\n", report.Screen.Width, report.Screen.Height, report.Decoder, report.Sink, report.Watchdog, report.GstLaunch, report.ConfigValid, report.BridgeReady)
	for _, p := range report.Problems {
		_, _ = fmt.Fprintf(out, "problem: %s\n", p)
	}
	if len(report.Problems) > 0 {
		return errors.New("doctor found problems")
	}
	return nil
}
