package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	"eufy-wall/internal/config"
)

type clientTarget struct {
	Name       string
	ConfigPath string
	StatusPath string
	Service    string
}

func validateTargetOutput(target clientTarget, c *config.Config) error {
	if target.Name != "" && c.Output == "" {
		return fmt.Errorf("instance %q needs an explicit output connector (for example output: HDMI-A-2)", target.Name)
	}
	return nil
}

func targetForInstance(name string) (clientTarget, error) {
	return targetForPlatform(runtime.GOOS, clientDataDir(), name)
}

func targetForPlatform(goos, dataDir, name string) (clientTarget, error) {
	if name != "" {
		if goos != "linux" {
			return clientTarget{}, errors.New("named display instances require Linux DRM outputs; macOS supports the main window only")
		}
		if len(name) > 64 {
			return clientTarget{}, errors.New("instance name must be 1-64 ASCII letters, digits, underscores, or hyphens")
		}
		for _, ch := range name {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
				return clientTarget{}, errors.New("instance name must be 1-64 ASCII letters, digits, underscores, or hyphens")
			}
		}
		return clientTarget{Name: name, ConfigPath: "/etc/eufy-wall-" + name + ".yaml", StatusPath: "/run/eufy-wall-" + name + "/status.json", Service: "eufy-wall@" + name}, nil
	}
	if goos == "darwin" {
		return clientTarget{ConfigPath: filepath.Join(dataDir, "config.yaml"), StatusPath: filepath.Join(dataDir, "status.json"), Service: launchdWallLabel}, nil
	}
	return clientTarget{ConfigPath: "/etc/eufy-wall.yaml", StatusPath: wallRuntimeStatusPath, Service: "eufy-wall"}, nil
}

func commandTarget(args []string) (clientTarget, []string, error) {
	return commandTargetForPlatform(runtime.GOOS, clientDataDir(), args)
}

func commandTargetForPlatform(goos, dataDir string, args []string) (clientTarget, []string, error) {
	name := ""
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] != "--instance" {
			clean = append(clean, args[i])
			continue
		}
		if name != "" || i+1 >= len(args) || args[i+1] == "" || args[i+1][0] == '-' {
			return clientTarget{}, nil, errors.New("--instance requires one name")
		}
		name = args[i+1]
		i++
	}
	target, err := targetForPlatform(goos, dataDir, name)
	if err != nil {
		return clientTarget{}, nil, fmt.Errorf("--instance: %w", err)
	}
	return target, clean, nil
}
