package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type pendingClientApply struct {
	Backup       string `json:"backup"`
	BackupSHA256 string `json:"backupSHA256,omitempty"`
	HadPrevious  bool   `json:"hadPrevious"`
}

func recoverClientConfig(dest string) error {
	// ExecStartPre runs while `config apply` is restarting the unit. If apply still owns the lock,
	// the candidate is intentional and the pre-start hook must not wait for apply (which is itself
	// waiting for the restart). A crashed apply releases the lock, so recovery then restores backup.
	unlock, busy, err := tryLockClientConfig(dest)
	if err != nil {
		return err
	}
	if busy {
		return nil
	}
	defer unlock()
	return recoverClientConfigLocked(dest)
}

func recoverClientConfigLocked(dest string) error {
	return recoverClientConfigLockedWithOps(dest, atomicClientWrite, os.Link, os.Rename)
}

type clientConfigWrite func(string, []byte, os.FileMode, *syscall.Stat_t) error

func recoverClientConfigLockedWithOps(dest string, write clientConfigWrite, link, rename func(string, string) error) error {
	markerData, err := os.ReadFile(dest + ".pending")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var marker pendingClientApply
	if err := json.Unmarshal(markerData, &marker); err != nil {
		return fmt.Errorf("invalid pending marker: %w", err)
	}
	if marker.HadPrevious {
		if !strings.HasPrefix(marker.Backup, dest+".bak.") || filepath.Dir(marker.Backup) != filepath.Dir(dest) || filepath.Clean(marker.Backup) != marker.Backup {
			return errors.New("pending marker has invalid backup path")
		}
		old, err := os.ReadFile(marker.Backup)
		if errors.Is(err, os.ErrNotExist) && marker.BackupSHA256 != "" {
			// A low-space fallback may have moved the backup over the candidate before a crash.
			// The hash in the durable marker proves that retrying can finish without the backup name.
			active, readErr := os.ReadFile(dest)
			if readErr != nil || clientSHA256(active) != marker.BackupSHA256 {
				return fmt.Errorf("backup is absent and active config does not match the pending backup hash: %w", err)
			}
		} else {
			if err != nil {
				return fmt.Errorf("read backup: %w", err)
			}
			if marker.BackupSHA256 != "" && clientSHA256(old) != marker.BackupSHA256 {
				return errors.New("pending backup checksum does not match the original config")
			}
			info, err := os.Stat(marker.Backup)
			if err != nil {
				return fmt.Errorf("stat backup: %w", err)
			}
			mode := info.Mode().Perm()
			owner, _ := info.Sys().(*syscall.Stat_t)
			if err := write(dest, old, mode, owner); err != nil {
				if !errors.Is(err, syscall.ENOSPC) && !errors.Is(err, syscall.EDQUOT) {
					return fmt.Errorf("restore backup: %w", err)
				}
				if fallbackErr := restoreClientBackupWithoutCopy(dest, marker.Backup, link, rename); fallbackErr != nil {
					return fmt.Errorf("restore backup after %v: %w", err, fallbackErr)
				}
			}
		}
	} else if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(dest + ".pending"); err != nil {
		return err
	}
	if err := syncClientDir(filepath.Dir(dest)); err != nil {
		return err
	}
	if err := recordClientRollback(dest, "interrupted apply restored on service start"); err != nil {
		// The old config is in place and the marker is gone. ExecStartPre must allow
		// the wall to start even when the status report cannot be written.
		_, _ = fmt.Fprintf(os.Stderr, "eufy-wall: restored previous config, but rollback status could not be written: %v\n", err)
	}
	return nil
}

func restoreClientBackupWithoutCopy(dest, backup string, link, rename func(string, string) error) error {
	// A hard link preserves the dated backup without allocating another copy of its contents.
	// If even creating a directory entry fails, moving the backup itself restores the wall.
	staged := fmt.Sprintf("%s.restore-%d-%d", dest, os.Getpid(), time.Now().UnixNano())
	if err := link(backup, staged); err == nil {
		defer os.Remove(staged)
		if err := rename(staged, dest); err == nil {
			return syncClientDir(filepath.Dir(dest))
		}
	}
	if err := rename(backup, dest); err != nil {
		return err
	}
	return syncClientDir(filepath.Dir(dest))
}
