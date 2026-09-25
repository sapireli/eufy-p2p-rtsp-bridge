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

type launchdWallService struct {
	run       func(...string) ([]byte, error)
	plistPath func() (string, error)
}

func (s launchdWallService) command(args ...string) ([]byte, error) {
	if s.run != nil {
		return s.run(args...)
	}
	return launchctl(args...)
}

func (s launchdWallService) plist() (string, error) {
	if s.plistPath != nil {
		return s.plistPath()
	}
	return launchdPlist()
}

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

func (s launchdWallService) Restart() error {
	if _, err := s.command("enable", launchdTarget()); err != nil {
		return err
	}
	if _, err := s.command("print", launchdTarget()); err != nil {
		plist, pathErr := s.plist()
		if pathErr != nil {
			return pathErr
		}
		if _, err := os.Stat(plist); err != nil {
			return fmt.Errorf("launchd service is not installed at %s: %w", plist, err)
		}
		if _, err := s.command("bootstrap", launchdDomain(), plist); err != nil {
			return fmt.Errorf("load launchd service in the logged-in user's GUI session: %w", err)
		}
	}
	_, err := s.command("kickstart", "-k", launchdTarget())
	return err
}

func (s launchdWallService) Stop() error {
	if _, err := s.command("print", launchdTarget()); err == nil {
		if _, err := s.command("bootout", launchdTarget()); err != nil {
			return err
		}
	}
	_, err := s.command("disable", launchdTarget())
	return err
}

func (s launchdWallService) Healthy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.active(); err == nil {
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
	return (launchdWallService{}).active()
}

func (s launchdWallService) active() error {
	b, err := s.command("print", launchdTarget())
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
