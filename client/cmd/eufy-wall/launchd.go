package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const launchdWallLabel = "com.eufy.wall"

type launchdWallService struct{}

func launchdTarget() string { return fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdWallLabel) }

func launchdDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func launchdPlist() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdWallLabel+".plist"), nil
}

func launchctl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func (launchdWallService) Restart() error {
	if err := enableLaunchdWall(); err != nil {
		return err
	}
	if _, err := launchctl("print", launchdTarget()); err != nil {
		plist, pathErr := launchdPlist()
		if pathErr != nil {
			return pathErr
		}
		if _, err := os.Stat(plist); err != nil {
			return fmt.Errorf("launchd service is not installed at %s: %w", plist, err)
		}
		if _, err := launchctl("bootstrap", launchdDomain(), plist); err != nil {
			return fmt.Errorf("load launchd service in the logged-in user's GUI session: %w", err)
		}
	}
	_, err := launchctl("kickstart", "-k", launchdTarget())
	return err
}

func (launchdWallService) Stop() error {
	if _, err := launchctl("print", launchdTarget()); err == nil {
		if _, err := launchctl("bootout", launchdTarget()); err != nil {
			return err
		}
	}
	_, err := launchctl("disable", launchdTarget())
	return err
}

func (launchdWallService) Healthy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := launchdActive(); err == nil {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("eufy-wall launchd service is not running in the GUI session: %w", ctx.Err())
		}
	}
}

func launchdActive() error {
	b, err := launchctl("print", launchdTarget())
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), "state = running") {
		return fmt.Errorf("eufy-wall launchd service is not running")
	}
	return nil
}

func enableLaunchdWall() error {
	_, err := launchctl("enable", launchdTarget())
	return err
}
