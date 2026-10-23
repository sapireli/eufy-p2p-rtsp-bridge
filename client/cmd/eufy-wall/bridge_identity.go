package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func identityCachePath() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "eufy-wall", "bridge-identities.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "eufy-wall", "bridge-identities.json")
}
func safeIdentityParent(path string) error {
	if info, err := os.Lstat(filepath.Dir(path)); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("saved bridge identity directory is a symlink")
	}
	return nil
}
func clearBridgeIdentities(path string) error {
	if err := safeIdentityParent(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("saved bridge identity file is a symlink")
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readIdentityCache(path string) (map[string]string, error) {
	if err := safeIdentityParent(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("saved bridge identity unreadable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("saved bridge identity unreadable")
	}
	if len(data) > 65536 {
		return nil, fmt.Errorf("saved bridge identity invalid")
	}
	var ids map[string]string
	if json.Unmarshal(data, &ids) != nil || ids == nil {
		return nil, fmt.Errorf("saved bridge identity invalid")
	}
	return ids, nil
}
func retainBridgeIdentity(path, service, id string) error {
	if path == "" {
		return fmt.Errorf("bridge identity cache unavailable")
	}
	ids, err := readIdentityCache(path)
	if err != nil {
		return err
	}
	if known := ids[service]; known != "" && known != id {
		return fmt.Errorf("bridge identity changed; select intended bridge")
	}
	if id == "" {
		return nil
	}
	if ids[service] == id {
		return nil
	}
	if len(ids) >= 64 {
		return fmt.Errorf("bridge identity cache full; check setup")
	}
	ids[service] = id
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := safeIdentityParent(path); err != nil {
		return err
	}
	data, _ := json.Marshal(ids)
	f, err := os.CreateTemp(filepath.Dir(path), "bridge-id-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
