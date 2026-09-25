package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func TestClientInstancePathsAndNamesAreIsolated(t *testing.T) {
	defaultTarget, err := targetForPlatform("linux", "", "")
	if err != nil {
		t.Fatal(err)
	}
	east, err := targetForPlatform("linux", "", "east")
	if err != nil {
		t.Fatal(err)
	}
	west, err := targetForPlatform("linux", "", "west")
	if err != nil {
		t.Fatal(err)
	}
	if defaultTarget.ConfigPath != "/etc/eufy-wall.yaml" || defaultTarget.StatusPath != "/run/eufy-wall/status.json" || defaultTarget.Service != "eufy-wall" {
		t.Fatalf("legacy default paths changed: %+v", defaultTarget)
	}
	if east.ConfigPath != "/etc/eufy-wall-east.yaml" || east.StatusPath != "/run/eufy-wall-east/status.json" || east.Service != "eufy-wall@east" {
		t.Fatalf("named paths wrong: %+v", east)
	}
	for _, a := range []clientTarget{defaultTarget, east, west} {
		for _, b := range []clientTarget{defaultTarget, east, west} {
			if a.Name != b.Name && (a.ConfigPath == b.ConfigPath || a.StatusPath == b.StatusPath || a.Service == b.Service) {
				t.Fatalf("instances share a resource: %+v %+v", a, b)
			}
		}
	}
	mac, err := targetForPlatform("darwin", "/Users/test/Library/Application Support/eufy-wall", "")
	if err != nil || mac.Service != launchdWallLabel || !strings.HasSuffix(mac.ConfigPath, "config.yaml") {
		t.Fatalf("macOS default target %+v, %v", mac, err)
	}
	if _, err := targetForPlatform("darwin", "/tmp", "east"); err == nil {
		t.Fatal("named macOS instance accepted without display selection")
	}
	for _, name := range []string{"../west", "east west", "west@server", strings.Repeat("x", 65), "east%2fwest"} {
		if _, err := targetForPlatform("linux", "", name); err == nil {
			t.Errorf("unsafe instance name %q accepted", name)
		}
	}
}

func TestNamedRuntimeStatusCannotSatisfyAnotherInstance(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeStatus := func(name string, pid int, hash string) string {
		t.Helper()
		path := filepath.Join(dir, name, "status.json")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(runtimeWallStatus{
			SchemaVersion: 1, PID: pid, StartedAt: now.Add(-time.Minute), UpdatedAt: now,
			Sink: "compositor", ConfigSHA256: hash, OutputFrames: 20, OutputLastFrameAt: now,
			Tiles: map[string]runtimeTileStatus{"tile": {ExpectedLive: true, State: "playing", Generation: 1, DecodedFrames: 20, LastDecodedFrameAt: now}},
		})
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	eastPath := writeStatus("east", 101, "east-hash")
	westPath := writeStatus("west", 202, "west-hash")
	east, err := readRuntimeWallStatus(eastPath)
	if err != nil {
		t.Fatal(err)
	}
	west, err := readRuntimeWallStatus(westPath)
	if err != nil {
		t.Fatal(err)
	}
	if east.PID != 101 || west.PID != 202 || east.ConfigSHA256 == west.ConfigSHA256 {
		t.Fatalf("instance status paths collided: east=%+v west=%+v", east, west)
	}
	if err := runtimeProgress(east, west, 101, "east-hash", "compositor", []string{"tile"}, now); err == nil {
		t.Fatal("west status passed east PID/hash gate")
	}
}

func TestInstanceOptionTargetsTheWholeCommand(t *testing.T) {
	target, clean, err := commandTargetForPlatform("linux", "", []string{"config", "apply", "draft.yaml", "--instance", "east", "--json"})
	if err != nil || target.Service != "eufy-wall@east" || !reflect.DeepEqual(clean, []string{"config", "apply", "draft.yaml", "--json"}) {
		t.Fatalf("parsed target=%+v args=%v err=%v", target, clean, err)
	}
	for _, args := range [][]string{{"status", "--instance"}, {"status", "--instance", "../bad"}, {"status", "--instance", "east", "--instance", "west"}} {
		if _, _, err := commandTargetForPlatform("linux", "", args); err == nil {
			t.Errorf("accepted bad option %v", args)
		}
	}
}

func TestNamedCommandUsesItsConfigPath(t *testing.T) {
	target := clientTarget{Name: "right", ConfigPath: filepath.Join(t.TempDir(), "right.yaml"), StatusPath: filepath.Join(t.TempDir(), "right-status.json"), Service: "eufy-wall@right"}
	data := strings.Replace(string(config.Example()), "# output: HDMI-A-1", "output: HDMI-A-2", 1)
	if err := os.WriteFile(target.ConfigPath, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if handled, err := runCommandTarget([]string{"config", "validate", "-"}, strings.NewReader(data), &out, target); !handled || err != nil {
		t.Fatalf("named validate: handled=%v err=%v output=%q", handled, err, out.String())
	}
	out.Reset()
	if handled, err := runCommandTarget([]string{"status", "--json"}, strings.NewReader(""), &out, target); !handled || err != nil {
		t.Fatalf("named status: handled=%v err=%v", handled, err)
	}
	var report struct {
		ConfigPath string `json:"configPath"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.ConfigPath != target.ConfigPath {
		t.Fatalf("status used wrong config path: %q, %v", out.String(), err)
	}
	out.Reset()
	if handled, err := runCommandTarget([]string{"config", "recover"}, strings.NewReader(""), &out, target); !handled || err != nil {
		t.Fatalf("named recover: handled=%v err=%v", handled, err)
	}
}

func TestNamedDoctorRejectsImplicitOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "right.yaml")
	if err := os.WriteFile(path, config.Example(), 0600); err != nil {
		t.Fatal(err)
	}
	target := clientTarget{Name: "right", ConfigPath: path, StatusPath: filepath.Join(t.TempDir(), "status.json"), Service: "eufy-wall@right"}
	var out bytes.Buffer
	err := clientDoctorAtWithScreenForTarget(path, target, true, &out, func(string) (config.Screen, bool) {
		return config.Screen{Width: 1920, Height: 1080}, true
	})
	if err == nil {
		t.Fatal("doctor accepted a named config with no output")
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.ConfigValid || !strings.Contains(strings.Join(report.Problems, " "), "explicit output connector") {
		t.Fatalf("doctor gave false advice for named config: %+v", report)
	}
}

func TestNamedInstanceRequiresExplicitDisplayOutput(t *testing.T) {
	target, _ := targetForPlatform("linux", "", "east")
	c, err := config.Parse(config.Example())
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTargetOutput(target, c); err == nil {
		t.Fatal("named instance accepted an implicit first output")
	}
	c.Output = "HDMI-A-2"
	if err := validateTargetOutput(target, c); err != nil {
		t.Fatal(err)
	}
	e, err := newLayoutEditorForTarget("", target)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.change(func(c *config.Config) error { return editorMutation(c, []string{"output", "HDMI-A-2"}) }); err != nil {
		t.Fatal(err)
	}
	selected, err := e.config()
	if err != nil || selected.Output != "HDMI-A-2" || validateTargetOutput(target, selected) != nil {
		t.Fatalf("editor did not select named instance output: %+v, %v", selected, err)
	}
	for _, active := range []string{"/etc/eufy-wall.yaml", "/etc/eufy-wall-east.yaml", "/etc/eufy-wall-west.yaml"} {
		if err := e.save(active); err == nil || !strings.Contains(err.Error(), "active config") {
			t.Errorf("named editor could overwrite %q as a draft: %v", active, err)
		}
	}
	link := filepath.Join(t.TempDir(), "etc-link")
	if err := os.Symlink("/etc", link); err != nil {
		t.Fatal(err)
	}
	if err := e.save(filepath.Join(link, "eufy-wall-west.yaml")); err == nil {
		t.Fatal("named editor could overwrite another active config through a directory symlink")
	}
}

func TestSeparateInstanceApplyRollbackKeepsOtherConfigAndStatus(t *testing.T) {
	dir := t.TempDir()
	eastPath, westPath := filepath.Join(dir, "east.yaml"), filepath.Join(dir, "west.yaml")
	if err := os.WriteFile(eastPath, []byte(validWallYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(westPath, []byte(validWallYAML), 0600); err != nil {
		t.Fatal(err)
	}
	eastService := &fakeWallService{}
	if err := applyClientData(eastPath, []byte(otherWallYAML), eastService); err != nil {
		t.Fatal(err)
	}
	eastStatus, err := readClientStatus(eastPath)
	if err != nil {
		t.Fatal(err)
	}
	westService := &fakeWallService{health: errors.New("west display failed")}
	if err := applyClientData(westPath, []byte(otherWallYAML), westService); err == nil {
		t.Fatal("failed west apply was accepted")
	}
	eastBytes, _ := os.ReadFile(eastPath)
	westBytes, _ := os.ReadFile(westPath)
	eastAfter, _ := readClientStatus(eastPath)
	westAfter, _ := readClientStatus(westPath)
	if string(eastBytes) != otherWallYAML || eastAfter != eastStatus || eastService.restarts != 1 {
		t.Fatalf("west failure changed east: config=%q status=%+v restarts=%d", eastBytes, eastAfter, eastService.restarts)
	}
	if string(westBytes) != validWallYAML || westAfter.LastRollback == "" || westService.restarts != 2 {
		t.Fatalf("west rollback failed: config=%q status=%+v restarts=%d", westBytes, westAfter, westService.restarts)
	}
}
