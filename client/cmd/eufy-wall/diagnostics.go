package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"eufy-wall/internal/config"
	"gopkg.in/yaml.v3"
)

type clientDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Remedy   string `json:"remedy"`
	Line     int    `json:"line,omitempty"`
}

var clientYAMLLine = regexp.MustCompile(`\bline (\d+)\b`)
var clientFieldPath = regexp.MustCompile(`^(?:config: )?((?:tiles\[\d+\](?:\.[a-z_]+)?|(?:screen|canvas|restart)\.[a-z_]+|[a-z_]+))\b`)
var clientUnknownField = regexp.MustCompile(`\bfield ([a-zA-Z_][a-zA-Z_0-9]*) not found\b`)

func diagnosticForClient(message string, operation string) clientDiagnostic {
	d := clientDiagnostic{Code: "CONFIG_INVALID", Severity: "error", Message: strings.SplitN(message, "\n", 2)[0], Remedy: "Correct the YAML and run eufy-wall config validate again."}
	if m := clientFieldPath.FindStringSubmatch(d.Message); len(m) > 1 {
		d.Path = m[1]
	}
	switch {
	case clientUnknownField.MatchString(message):
		d.Code = "CONFIG_UNSUPPORTED_KEY"
		d.Path = clientUnknownField.FindStringSubmatch(message)[1]
		d.Message = fmt.Sprintf("unsupported config key %q", d.Path)
		d.Remedy = "Correct or remove this key; compare with eufy-wall config example."
		if m := clientYAMLLine.FindStringSubmatch(message); len(m) > 1 {
			d.Line, _ = strconv.Atoi(m[1])
		}
	case strings.Contains(message, "yaml:"):
		d.Code, d.Path, d.Message, d.Remedy = "CONFIG_YAML_INVALID", "", "invalid YAML syntax", "Correct YAML syntax at the reported line; compare with eufy-wall config example."
		if m := clientYAMLLine.FindStringSubmatch(message); len(m) > 1 {
			d.Line, _ = strconv.Atoi(m[1])
		}
	case strings.Contains(message, "schema_version"):
		d.Code, d.Remedy = "CONFIG_SCHEMA_UNSUPPORTED", "Upgrade eufy-wall or migrate the config to schema version 2."
	case strings.Contains(message, "config not installed") || strings.Contains(message, "no such file"):
		d.Code, d.Path, d.Remedy = "CONFIG_FILE_UNREADABLE", "config", "Install a config with eufy-wall setup or provide a readable YAML file."
	case strings.Contains(message, "connected display"):
		d.Code, d.Path, d.Remedy = "DISPLAY_UNAVAILABLE", "output", "Connect the selected display or correct output in the YAML."
	case strings.Contains(message, "DRM plane") || strings.Contains(message, "sink=planes"):
		d.Code, d.Path, d.Remedy = "DRM_PLANE_UNUSABLE", "planes", "Choose plane IDs that reach the selected output, or use sink: compositor."
	case strings.Contains(message, "output") && strings.Contains(message, "DRM"):
		d.Code, d.Path, d.Remedy = "DRM_OUTPUT_UNAVAILABLE", "output", "Check the connected output name and DRM connector ID."
	case strings.Contains(message, "GStreamer version"):
		d.Code, d.Remedy = "GSTREAMER_VERSION_UNSUPPORTED", "Install GStreamer 1.20 or newer and rerun eufy-wall doctor."
	case strings.Contains(message, "GStreamer ") && strings.Contains(message, "is missing") || strings.Contains(message, "gst-launch"):
		d.Code, d.Remedy = "GSTREAMER_ELEMENT_MISSING", "Install the required GStreamer package and rerun eufy-wall doctor."
	case strings.Contains(message, "decoder") || strings.Contains(message, "GStreamer"):
		d.Code, d.Remedy = "DECODER_UNAVAILABLE", "Install the required GStreamer element or change the codec/decoder choice."
	case operation == "migrate" && strings.Contains(message, "legacy migration needs rtsp_base"):
		d.Code, d.Path, d.Remedy = "CONFIG_MIGRATION_NEEDS_RTSP_BASE", "rtsp_base", "Keep a URL-only offline file in the supported legacy format, or add a bridge-backed rtsp_base before migration."
	case strings.Contains(message, "bridge_url must be an http(s) origin") || strings.Contains(message, "cannot derive bridge_url"):
		d.Code, d.Path, d.Remedy = "BRIDGE_URL_INVALID", "bridge_url", "Use a credential-free HTTP(S) bridge origin without a path, query, or fragment."
	case strings.Contains(message, "bridge") && strings.Contains(message, "auth"):
		d.Code, d.Path, d.Remedy = "BRIDGE_AUTH_REQUIRED", "bridge_url", "Complete bridge login and retry."
	case strings.Contains(message, "bridge"):
		d.Code, d.Path, d.Remedy = "BRIDGE_UNREACHABLE", "bridge_url", "Check the bridge address and service health, then retry."
	case strings.Contains(message, "RTSP") || strings.Contains(message, "frame progress"):
		d.Code, d.Remedy = "RTSP_FRAMES_UNAVAILABLE", "Check the camera stream, codec, and network path, then run eufy-wall probe."
	}
	if operation == "apply" && d.Code == "CONFIG_INVALID" {
		d.Code = "CONFIG_APPLY_FAILED"
		d.Message = "configuration apply failed; inspect status for rollback details"
		d.Remedy = "Run eufy-wall status --json and review the retained backup before retrying."
	}
	if operation == "doctor" && d.Code == "CONFIG_INVALID" {
		d.Code, d.Remedy = "HOST_CHECK_FAILED", "Resolve the reported host check and rerun eufy-wall doctor."
	}
	return d
}

func diagnosticForClientInput(message, operation string, data []byte) clientDiagnostic {
	d := diagnosticForClient(message, operation)
	if d.Code == "CONFIG_UNSUPPORTED_KEY" && d.Line > 0 {
		if path := yamlFieldPath(data, d.Path, d.Line); path != "" {
			d.Path = path
			d.Message = fmt.Sprintf("unsupported config key %q", path)
		}
	}
	return d
}

func yamlFieldPath(data []byte, field string, line int) string {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return ""
	}
	var visit func(*yaml.Node, string) string
	visit = func(node *yaml.Node, prefix string) string {
		switch node.Kind {
		case yaml.DocumentNode:
			if len(node.Content) > 0 {
				return visit(node.Content[0], prefix)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				path := key.Value
				if prefix != "" {
					path = prefix + "." + path
				}
				if key.Value == field && key.Line == line {
					return path
				}
				if found := visit(value, path); found != "" {
					return found
				}
			}
		case yaml.SequenceNode:
			for i, child := range node.Content {
				if found := visit(child, fmt.Sprintf("%s[%d]", prefix, i)); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return visit(&root, "")
}

func emitClientJSON(out io.Writer, ok bool, diagnostic *clientDiagnostic) error {
	diagnostics := []clientDiagnostic{}
	if diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	}
	return json.NewEncoder(out).Encode(struct {
		OK          bool               `json:"ok"`
		Diagnostics []clientDiagnostic `json:"diagnostics"`
	}{OK: ok, Diagnostics: diagnostics})
}

func validateClientCommand(path string, in io.Reader, out io.Writer, jsonOutput bool) error {
	target, err := targetForInstance("")
	if err != nil {
		return err
	}
	return validateClientCommandTarget(path, in, out, jsonOutput, target)
}

func validateClientCommandTarget(path string, in io.Reader, out io.Writer, jsonOutput bool, target clientTarget) error {
	data, err := readClientInput(path, in)
	var c *config.Config
	if err == nil {
		c, err = config.Parse(data)
	}
	if err == nil {
		err = validateTargetOutput(target, c)
	}
	if err == nil {
		_, err = placeForValidation(c)
	}
	if !jsonOutput {
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "config valid")
		return err
	}
	if err != nil {
		d := diagnosticForClientInput(err.Error(), "validate", data)
		if writeErr := emitClientJSON(out, false, &d); writeErr != nil {
			return writeErr
		}
		return errors.New("configuration invalid; see JSON diagnostics")
	}
	return emitClientJSON(out, true, nil)
}

func applyClientCommand(path string, in io.Reader, out io.Writer, jsonOutput bool) error {
	target, err := targetForInstance("")
	if err != nil {
		return err
	}
	return applyClientCommandTarget(path, in, out, jsonOutput, target)
}

func applyClientCommandTarget(path string, in io.Reader, out io.Writer, jsonOutput bool, target clientTarget) error {
	if !jsonOutput {
		return applyClientConfigTarget(path, in, out, target)
	}
	data, err := readClientInput(path, in)
	if err == nil {
		err = applyClientConfigTarget("-", bytes.NewReader(data), io.Discard, target)
	}
	if err != nil {
		d := diagnosticForClientInput(err.Error(), "apply", data)
		if writeErr := emitClientJSON(out, false, &d); writeErr != nil {
			return writeErr
		}
		return errors.New("configuration apply failed; see JSON diagnostics")
	}
	return emitClientJSON(out, true, nil)
}
