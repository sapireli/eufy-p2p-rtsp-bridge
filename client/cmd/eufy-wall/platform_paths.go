package main

import (
	"os"
	"path/filepath"
	"runtime"
)

func clientDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "eufy-wall")
}

func defaultClientConfigPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(clientDataDir(), "config.yaml")
	}
	return "/etc/eufy-wall.yaml"
}

func clientRuntimeStatusPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(clientDataDir(), "status.json")
	}
	return wallRuntimeStatusPath
}
