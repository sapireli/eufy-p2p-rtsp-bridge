package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
)

func clientHealth(out io.Writer) error {
	data, err := readClientInput(clientConfigPath, nil)
	if err != nil {
		return err
	}
	var service interface {
		Healthy() error
		FramesHealthy([]byte) error
	} = systemdWallService{}
	if runtime.GOOS == "darwin" {
		service = launchdWallService{}
	}
	if err := service.Healthy(); err != nil {
		return err
	}
	if err := service.FramesHealthy(data); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "health check passed")
	return err
}

func (systemdWallService) FramesHealthy(data []byte) error {
	b, err := exec.Command("systemctl", "show", "--property=MainPID", "--value", "eufy-wall").Output()
	if err != nil {
		return fmt.Errorf("read eufy-wall PID: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("eufy-wall has no running PID")
	}
	return clientFramesHealthy(data, pid)
}

func (launchdWallService) FramesHealthy(data []byte) error {
	b, err := launchctl("print", launchdTarget())
	if err != nil {
		return err
	}
	pid, err := launchdPID(string(b))
	if err != nil {
		return err
	}
	return clientFramesHealthy(data, pid)
}

func launchdPID(report string) (int, error) {
	for _, line := range strings.Split(report, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "pid = ") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(line, "pid = "))
		if err == nil && pid > 0 {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("launchd service has no running PID")
}

func clientFramesHealthy(data []byte, pid int) error {
	c, err := config.Parse(data)
	if err != nil {
		return err
	}
	if c.Screen.Width == 0 || c.Screen.Height == 0 {
		if screen, ok := detect.HostScreen("/", c.Output); ok {
			c.Screen = screen
		}
	}
	caps, err := detect.Resolve(c, detect.HasElement, detect.FileExists)
	if err != nil {
		return err
	}
	if caps.Sink == "planes" {
		return nil // Planes still use independently supervised gst-launch processes.
	}
	ids := make([]string, 0, len(c.Tiles))
	for _, tile := range c.Tiles {
		ids = append(ids, tile.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
	defer cancel()
	if err := awaitRuntimeProgress(ctx, clientRuntimeStatusPath(), pid, clientSHA256(data), caps.Sink, ids, 500*time.Millisecond); err != nil {
		return err
	}
	status, err := readRuntimeWallStatus(clientRuntimeStatusPath())
	if err != nil {
		return err
	}
	for _, tile := range c.Tiles {
		if tile.URL != "" && tile.Motion == "" && !status.Tiles[tile.ID].ExpectedLive {
			return fmt.Errorf("fixed RTSP tile %s is not receiving live video", tile.ID)
		}
	}
	return nil
}
