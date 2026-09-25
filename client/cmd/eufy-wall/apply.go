package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/detect"
	"eufy-wall/internal/pipeline"
)

var clientConfigPath = defaultClientConfigPath()

type clientService interface {
	Restart() error
	Healthy() error
	Stop() error
}

type clientFrameService interface {
	FramesHealthy([]byte) error
}

type systemdWallService struct{}

func (systemdWallService) Restart() error { return systemctl("restart", "eufy-wall") }
func (systemdWallService) Stop() error    { return systemctl("stop", "eufy-wall") }

func systemctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return nil
}

func (systemdWallService) Healthy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
	defer cancel()
	baseline, _ := wallRestartCount()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := systemctl("is-active", "--quiet", "eufy-wall"); err != nil {
			return fmt.Errorf("eufy-wall is not active: %w", err)
		}
		if n, err := wallRestartCount(); err == nil && n > baseline {
			return fmt.Errorf("eufy-wall restarted %d time(s) during health check", n-baseline)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil
		}
	}
}

func wallRestartCount() (int, error) {
	b, err := exec.Command("systemctl", "show", "--property=NRestarts", "--value", "eufy-wall").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

type pendingClientApply struct {
	Backup      string `json:"backup"`
	HadPrevious bool   `json:"hadPrevious"`
}

func applyClientConfig(path string, in io.Reader, out io.Writer) error {
	if runtime.GOOS == "darwin" {
		if err := os.MkdirAll(filepath.Dir(clientConfigPath), 0700); err != nil {
			return err
		}
		return applyClientConfigAt(path, in, out, clientConfigPath, launchdWallService{}, true, enableLaunchdWall)
	}
	return applyClientConfigAt(path, in, out, clientConfigPath, systemdWallService{}, true, func() error { return systemctl("enable", "eufy-wall") })
}

func applyClientConfigAt(path string, in io.Reader, out io.Writer, dest string, service clientService, host bool, enable func() error) error {
	b, err := readClientInput(path, in)
	if err != nil {
		return err
	}
	if len(b) > 4<<20 {
		return errors.New("config exceeds 4 MiB")
	}
	if err := validateClientConfig(b, host); err != nil {
		return err
	}
	if err := applyClientData(dest, b, service); err != nil {
		return err
	}
	if err := enable(); err != nil {
		return fmt.Errorf("config is applied and running, but automatic start could not be enabled: %w", err)
	}
	_, _ = fmt.Fprintln(out, "config applied and eufy-wall healthy")
	return nil
}

func validateClientConfig(b []byte, host bool) error {
	c, err := config.Parse(b)
	if err != nil {
		return err
	}
	if host && runtime.GOOS == "darwin" && c.Output != "" {
		return errors.New("macOS window sink opens on the main display; leave output empty")
	}
	if host && (c.Screen.Width == 0 || c.Screen.Height == 0) {
		screen, ok := detect.HostScreen("/", c.Output)
		if !ok {
			return fmt.Errorf("no connected screen mode found for output %q; set screen dimensions after verifying the output", c.Output)
		}
		c.Screen = screen
	}
	placed, err := placeForValidation(c)
	if err != nil {
		return err
	}
	if host {
		caps, err := detect.Resolve(c, detect.HasElement, detect.FileExists)
		if err != nil {
			return err
		}
		if !detect.HasElement("watchdog") {
			return errors.New("GStreamer watchdog is missing (install gstreamer1.0-plugins-bad)")
		}
		if caps.Sink == "planes" || caps.Sink == "compositor" {
			if !detect.HasElement("kmssink") {
				return errors.New("GStreamer kmssink is missing")
			}
		}
		if _, err := pipeline.Plans(c, placed, caps); err != nil {
			return err
		}
		cameras, err := preflightClientRemote(context.Background(), c)
		if err != nil {
			return err
		}
		if err := codecPreflight(c, cameras, caps.Decoder, detect.HasElement); err != nil {
			return err
		}
	}
	return nil
}

// applyClientData owns the config transaction. A pending marker is persisted before switching the
// active config, so `config recover` in the unit's ExecStartPre can undo an interrupted apply on boot.
func applyClientData(dest string, data []byte, service clientService) error {
	if err := validateClientConfig(data, false); err != nil {
		return err
	}
	unlock, err := lockClientConfig(dest)
	if err != nil {
		return err
	}
	defer unlock()
	if err := recoverClientConfigLocked(dest); err != nil {
		return err
	}
	old, err := os.ReadFile(dest)
	hadPrevious := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if hadPrevious && string(old) == string(data) {
		return nil
	}
	previousStatus, err := readClientStatus(dest)
	if err != nil {
		return err
	}
	mode := os.FileMode(0644)
	var owner *syscall.Stat_t
	if info, err := os.Stat(dest); err == nil {
		mode = info.Mode().Perm()
		owner, _ = info.Sys().(*syscall.Stat_t)
	}
	backup := ""
	if hadPrevious {
		backup = dest + ".bak." + time.Now().UTC().Format("20060102T150405.000000000Z")
		if err := atomicClientWrite(backup, old, mode, owner); err != nil {
			return fmt.Errorf("backup config: %w", err)
		}
	}
	marker := pendingClientApply{Backup: backup, HadPrevious: hadPrevious}
	markerData, _ := json.Marshal(marker)
	if err := atomicClientWrite(dest+".pending", markerData, 0600, nil); err != nil {
		return fmt.Errorf("write pending marker: %w", err)
	}
	if err := atomicClientWrite(dest, data, mode, owner); err != nil {
		_ = recoverClientConfigLocked(dest)
		return fmt.Errorf("activate config: %w", err)
	}
	if err := service.Restart(); err != nil {
		return rollbackClientConfig(dest, service, err)
	}
	if err := service.Healthy(); err != nil {
		return rollbackClientConfig(dest, service, err)
	}
	if frameService, ok := service.(clientFrameService); ok {
		if err := frameService.FramesHealthy(data); err != nil {
			return rollbackClientConfig(dest, service, fmt.Errorf("frame progress: %w", err))
		}
	}
	previousStatus.SHA256 = clientSHA256(data)
	previousStatus.AppliedAt = time.Now().UTC()
	previousStatus.Backup = backup
	if err := writeClientStatus(dest, previousStatus); err != nil {
		return rollbackClientConfig(dest, service, fmt.Errorf("record apply status: %w", err))
	}
	if err := os.Remove(dest + ".pending"); err != nil {
		return fmt.Errorf("applied, but could not clear pending marker: %w", err)
	}
	return syncClientDir(filepath.Dir(dest))
}

func rollbackClientConfig(dest string, service clientService, cause error) error {
	if err := recoverClientConfigLocked(dest); err != nil {
		return fmt.Errorf("new config failed (%v), rollback failed: %w", cause, err)
	}
	if err := recordClientRollback(dest, cause.Error()); err != nil {
		return fmt.Errorf("new config failed (%v), previous config restored but rollback status failed: %w", cause, err)
	}
	if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
		if stopErr := service.Stop(); stopErr != nil {
			return fmt.Errorf("new config failed (%v), previous config restored but service stop failed: %w", cause, stopErr)
		}
		return fmt.Errorf("new config failed and was removed: %w", cause)
	}
	if err := service.Restart(); err != nil {
		return fmt.Errorf("new config failed (%v), previous config restored but restart failed: %w", cause, err)
	}
	if err := service.Healthy(); err != nil {
		return fmt.Errorf("new config failed (%v), previous config restored but recovery health check failed: %w", cause, err)
	}
	if frameService, ok := service.(clientFrameService); ok {
		restored, err := os.ReadFile(dest)
		if err != nil {
			return fmt.Errorf("new config failed (%v), previous config restored but its frame check could not read config: %w", cause, err)
		}
		if err := frameService.FramesHealthy(restored); err != nil {
			return fmt.Errorf("new config failed (%v), previous config restored but frames did not recover: %w", cause, err)
		}
	}
	return fmt.Errorf("new config failed; previous config restored: %w", cause)
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
		if marker.Backup == "" || filepath.Dir(marker.Backup) != filepath.Dir(dest) {
			return errors.New("pending marker has invalid backup path")
		}
		old, err := os.ReadFile(marker.Backup)
		if err != nil {
			return fmt.Errorf("read backup: %w", err)
		}
		mode := os.FileMode(0644)
		var owner *syscall.Stat_t
		if info, err := os.Stat(marker.Backup); err == nil {
			mode = info.Mode().Perm()
			owner, _ = info.Sys().(*syscall.Stat_t)
		}
		if err := atomicClientWrite(dest, old, mode, owner); err != nil {
			return fmt.Errorf("restore backup: %w", err)
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
	return recordClientRollback(dest, "interrupted apply restored on service start")
}

func lockClientConfig(dest string) (func(), error) {
	lock, err := os.OpenFile(dest+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }, nil
}

func tryLockClientConfig(dest string) (func(), bool, error) {
	lock, err := os.OpenFile(dest+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }, false, nil
}

func atomicClientWrite(path string, data []byte, mode os.FileMode, owner *syscall.Stat_t) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".eufy-wall-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if owner != nil {
		current, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		uid, gid := current.Sys().(*syscall.Stat_t).Uid, current.Sys().(*syscall.Stat_t).Gid
		if uid != owner.Uid || gid != owner.Gid {
			if err := f.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
				f.Close()
				return fmt.Errorf("preserve config ownership: %w", err)
			}
		}
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncClientDir(dir)
}

func syncClientDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
