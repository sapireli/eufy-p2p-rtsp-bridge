package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func TestLaunchdPID(t *testing.T) {
	if got, err := launchdPID("state = running\n\tpid = 9606\njob state = running\n"); err != nil || got != 9606 {
		t.Fatalf("PID = %d, %v", got, err)
	}
	for _, report := range []string{"pid = 0", "pid = nope", "state = waiting"} {
		if _, err := launchdPID(report); err == nil {
			t.Fatalf("accepted %q", report)
		}
	}
}

func TestClientFramesHealthyRequiresFixedURLVideo(t *testing.T) {
	const yaml = "schema_version: 2\nbridge_url: http://127.0.0.1:3000\nrtsp_base: rtsp://127.0.0.1:8554\nlayout: custom\ncanvas: {cols: 32, rows: 32}\nscreen: {width: 640, height: 360}\ndecoder: software\nsink: window\ntiles:\n  - id: direct\n    url: rtsp://127.0.0.1:8554/test\n    rect: {x: 0, y: 0, w: 32, h: 32}\n"
	path := filepath.Join(t.TempDir(), "status.json")
	started := time.Now().Add(-time.Second).UTC()
	write := func(frames uint64, expected bool) error {
		now := time.Now().UTC()
		status := runtimeWallStatus{
			SchemaVersion: 1, PID: 77, StartedAt: started, UpdatedAt: now,
			Sink: "window", ConfigSHA256: clientSHA256([]byte(yaml)), OutputFrames: frames,
			OutputLastFrameAt: now,
			Tiles: map[string]runtimeTileStatus{"direct": {
				ExpectedLive: expected, DecodedFrames: frames, LastDecodedFrameAt: now,
				Generation: 1, State: "playing",
			}},
		}
		data, err := json.Marshal(status)
		if err != nil {
			return err
		}
		return atomicClientWrite(path, data, 0600, nil)
	}
	resolve := func(*config.Config) (string, error) { return "window", nil }
	for _, expected := range []bool{false, true} {
		if err := write(1, expected); err != nil {
			t.Fatal(err)
		}
		updated := make(chan error, 1)
		go func() {
			time.Sleep(200 * time.Millisecond)
			updated <- write(2, expected)
		}()
		err := clientFramesHealthyAt([]byte(yaml), 77, path, resolve)
		if writeErr := <-updated; writeErr != nil {
			t.Fatal(writeErr)
		}
		if expected && err != nil || !expected && (err == nil || !strings.Contains(err.Error(), "not receiving live video")) {
			t.Fatalf("expected_live=%v, health=%v", expected, err)
		}
	}
	if err := clientFramesHealthyAt([]byte(yaml), 77, path, func(*config.Config) (string, error) { return "planes", nil }); err != nil {
		t.Fatalf("plane service should use its own supervisor: %v", err)
	}
}

func TestClientHealthRequiresServiceAndFrameProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(path, []byte(validWallYAML), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	unhealthy := &fakeWallService{health: errors.New("service failed")}
	if err := clientHealthAt(path, &out, unhealthy); err == nil || unhealthy.frameChecks != 0 {
		t.Fatalf("checked frames without healthy service: %v checks=%d", err, unhealthy.frameChecks)
	}
	frozen := &fakeWallService{frames: errors.New("output frozen")}
	if err := clientHealthAt(path, &out, frozen); err == nil || !strings.Contains(err.Error(), "output frozen") || out.Len() != 0 {
		t.Fatalf("accepted frozen output: %v output=%q", err, out.String())
	}
	working := &fakeWallService{}
	if err := clientHealthAt(path, &out, working); err != nil || working.frameChecks != 1 || !strings.Contains(out.String(), "passed") {
		t.Fatalf("working health: %v checks=%d output=%q", err, working.frameChecks, out.String())
	}
	if err := clientHealthAt(filepath.Join(t.TempDir(), "absent"), &out, working); err == nil {
		t.Fatal("missing config passed health")
	}
}
