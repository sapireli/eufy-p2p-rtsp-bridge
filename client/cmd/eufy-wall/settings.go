package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/wsclient"
)

const settingsUsage = `Manage shared bridge settings (changes affect every display).
Usage: eufy-wall settings [--bridge http://HOST:3000 | --config FILE] COMMAND

  show                              Show saved settings, revision and apply status
  cameras                           List camera names and serials
  patch '{"lan":{"force":true}}'     Merge settings (or use @patch.json)
  camera NAME FIELD JSON_VALUE      Change one camera override by name or serial
  export FILE                       Save a private bridge settings backup
  import FILE                       Replace settings from an exported backup
  restart                           Restart bridge to apply pending global settings

Camera fields include power_override, mode, dual_view, enabled, and quality.
Example: camera "Garage Doorbell" power_override '"always-on"'
Use null to remove an override. Rejected/conflicting updates are never retried.
Use --token-file FILE or EUFY_WALL_ADMIN_TOKEN for shared settings access.
Display layout, selected cameras and decoder priorities remain local.
`

// Settings management does not start any players or write the display config.
func runSettings(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	fs.SetOutput(errOut)
	bridge := fs.String("bridge", "", "bridge HTTP URL (bypasses local config)")
	tokenFile := fs.String("token-file", "", "file containing bridge settings admin token")
	cfg := fs.String("config", "/etc/eufy-wall.yaml", "display config used to locate the bridge")
	fs.Usage = func() { fmt.Fprint(errOut, settingsUsage) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cmd := fs.Args()
	if len(cmd) == 0 {
		fs.Usage()
		return fmt.Errorf("a settings command is required")
	}
	base, err := settingsBridge(*bridge, *cfg)
	if err != nil {
		return err
	}
	token, err := settingsAdminToken(*tokenFile)
	if err != nil {
		return err
	}
	c := settingsClient{base: base, token: token, http: &http.Client{Timeout: 30 * time.Second}}
	switch cmd[0] {
	case "show", "cameras":
		if len(cmd) != 1 {
			return fmt.Errorf("%s takes no arguments", cmd[0])
		}
		path := "/api/settings"
		if cmd[0] == "cameras" {
			path = "/api/cameras"
		}
		body, err := c.request(http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		return printJSON(out, body)
	case "restart":
		if len(cmd) != 1 {
			return fmt.Errorf("restart takes no arguments")
		}
		revision, err := c.revision()
		if err != nil {
			return err
		}
		body, err := c.request(http.MethodPost, "/api/settings/restart", map[string]any{"revision": revision})
		if err != nil {
			return err
		}
		return printJSON(out, body)
	case "patch":
		if len(cmd) != 2 {
			return fmt.Errorf("patch requires one JSON object or @file")
		}
		data := []byte(cmd[1])
		if strings.HasPrefix(cmd[1], "@") {
			data, err = os.ReadFile(strings.TrimPrefix(cmd[1], "@"))
			if err != nil {
				return err
			}
		}
		var patch map[string]json.RawMessage
		if err = json.Unmarshal(data, &patch); err != nil || patch == nil {
			return fmt.Errorf("patch must be a JSON object")
		}
		revision, err := c.revision()
		if err != nil {
			return err
		}
		body, err := c.request(http.MethodPatch, "/api/settings", map[string]any{"revision": revision, "settings": patch})
		if err != nil {
			return err
		}
		return printJSON(out, body)
	case "camera":
		if len(cmd) != 4 {
			return fmt.Errorf("camera requires NAME FIELD JSON_VALUE")
		}
		// Preserve types, including null, for server-side capability validation.
		var value json.RawMessage = []byte(cmd[3])
		if !json.Valid(value) {
			return fmt.Errorf("camera value must be JSON; quote strings, e.g. '\"split\"'")
		}
		allowed := map[string]bool{"power_override": true, "mode": true, "dual_view": true, "enabled": true, "quality": true, "hold_seconds": true, "codec": true, "transcode": true, "vaapi_decode": true, "name": true}
		if !allowed[cmd[2]] {
			return fmt.Errorf("unsupported camera field %q; use patch for other settings", cmd[2])
		}
		sn, err := c.camera(cmd[1])
		if err != nil {
			return err
		}
		revision, err := c.revision()
		if err != nil {
			return err
		}
		body, err := c.request(http.MethodPatch, "/api/settings", map[string]any{"revision": revision, "settings": map[string]any{"cameras": map[string]any{sn: map[string]any{cmd[2]: value}}}})
		if err != nil {
			return err
		}
		return printJSON(out, body)
	case "export":
		if len(cmd) != 2 || cmd[1] == "-" {
			return fmt.Errorf("export requires a new file path")
		}
		body, err := c.request(http.MethodGet, "/api/settings/export", nil)
		if err != nil {
			return err
		}
		// Do not overwrite an existing backup. Keep LAN addresses/private stream URLs private.
		f, err := os.OpenFile(cmd[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if err = printJSON(f, body); err != nil {
			f.Close()
			os.Remove(cmd[1])
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "Exported shared bridge settings to %s\n", cmd[1])
		return err
	case "import":
		if len(cmd) != 2 {
			return fmt.Errorf("import requires an exported file path")
		}
		data, err := os.ReadFile(cmd[1])
		if err != nil {
			return err
		}
		var backup map[string]json.RawMessage
		if err = json.Unmarshal(data, &backup); err != nil || backup == nil {
			return fmt.Errorf("backup must be a JSON object")
		}
		revision, err := c.revision()
		if err != nil {
			return err
		}
		body, err := c.request(http.MethodPost, "/api/settings/import", map[string]any{"revision": revision, "backup": backup})
		if err != nil {
			return err
		}
		return printJSON(out, body)
	default:
		return fmt.Errorf("unknown settings command %q; use settings --help", cmd[0])
	}
}

func settingsBridge(explicit, cfg string) (string, error) {
	if explicit != "" {
		u, err := url.Parse(explicit)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return "", fmt.Errorf("--bridge must be an HTTP(S) origin such as http://homebridge.lan:3000")
		}
		return strings.TrimRight(explicit, "/"), nil
	}
	c, err := config.Load(cfg)
	if err != nil {
		return "", err
	}
	rtsp := c.RTSPBase
	if rtsp == "auto" {
		rtsp, err = discoverBridge()
		if err != nil {
			return "", err
		}
	}
	base := wsclient.APIBase(rtsp)
	if base == "" {
		return "", fmt.Errorf("cannot derive bridge API; use --bridge")
	}
	return base, nil
}

type settingsClient struct {
	base  string
	token string
	http  *http.Client
}

func (c settingsClient) request(method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4*1024*1024 {
		return nil, fmt.Errorf("bridge response exceeds 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusConflict {
			return nil, fmt.Errorf("settings changed on another client; review settings show and repeat your update: %s", strings.TrimSpace(string(data)))
		}
		return nil, fmt.Errorf("bridge HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("bridge returned invalid JSON")
	}
	return data, nil
}
func (c settingsClient) revision() (json.RawMessage, error) {
	data, err := c.request(http.MethodGet, "/api/settings", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Revision json.RawMessage `json:"revision"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if len(response.Revision) == 0 || bytes.Equal(response.Revision, []byte("null")) {
		return nil, fmt.Errorf("bridge did not return a settings revision")
	}
	return response.Revision, nil
}
func (c settingsClient) camera(name string) (string, error) {
	data, err := c.request(http.MethodGet, "/api/cameras", nil)
	if err != nil {
		return "", err
	}
	var cameras []struct {
		SN   string `json:"sn"`
		Name string `json:"name"`
	}
	if err = json.Unmarshal(data, &cameras); err != nil {
		return "", err
	}
	for _, cam := range cameras {
		if cam.SN == name {
			return cam.SN, nil
		}
	}
	sn := ""
	for _, cam := range cameras {
		if strings.EqualFold(cam.Name, name) {
			if sn != "" {
				return "", fmt.Errorf("camera name %q is ambiguous; use its serial", name)
			}
			sn = cam.SN
		}
	}
	if sn == "" {
		return "", fmt.Errorf("camera %q not found; use settings cameras", name)
	}
	return sn, nil
}
func printJSON(out io.Writer, data []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err := out.Write(buf.Bytes())
	return err
}

// Shared by the management subcommand and the legacy view switch flag.
func settingsAdminToken(path string) (string, error) {
	token := strings.TrimSpace(os.Getenv("EUFY_WALL_ADMIN_TOKEN"))
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		token = strings.TrimSpace(string(data))
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", fmt.Errorf("admin token must be a single line")
	}
	return token, nil
}
