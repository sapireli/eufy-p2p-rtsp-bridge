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

type systemdWallService struct {
	name       string
	statusPath string
}

func (s systemdWallService) serviceName() string {
	if s.name != "" {
		return s.name
	}
	return "eufy-wall"
}

func (s systemdWallService) runtimeStatusPath() string {
	if s.statusPath != "" {
		return s.statusPath
	}
	return clientRuntimeStatusPath()
}

func (s systemdWallService) Restart() error { return systemctl("restart", s.serviceName()) }
func (s systemdWallService) Stop() error    { return systemctl("stop", s.serviceName()) }

func systemctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return nil
}

func (s systemdWallService) Healthy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
	defer cancel()
	baseline, _ := wallRestartCountFor(s.serviceName())
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := systemctl("is-active", "--quiet", s.serviceName()); err != nil {
			return fmt.Errorf("%s is not active: %w", s.serviceName(), err)
		}
		if n, err := wallRestartCountFor(s.serviceName()); err == nil && n > baseline {
			return fmt.Errorf("%s restarted %d time(s) during health check", s.serviceName(), n-baseline)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil
		}
	}
}

func wallRestartCount() (int, error) {
	return wallRestartCountFor("eufy-wall")
}

func wallRestartCountFor(name string) (int, error) {
	return wallRestartCountWithTimeout(name, 5*time.Second)
}

func wallRestartCountWithTimeout(name string, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "show", "--property=NRestarts", "--value", name)
	cmd.WaitDelay = timeout
	b, err := cmd.Output()
	if ctx.Err() != nil {
		return 0, fmt.Errorf("systemctl restart count for %s timed out: %w", name, ctx.Err())
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func applyClientConfig(path string, in io.Reader, out io.Writer) error {
	target, err := targetForInstance("")
	if err != nil {
		return err
	}
	return applyClientConfigTarget(path, in, out, target)
}

func applyClientConfigTarget(path string, in io.Reader, out io.Writer, target clientTarget) error {
	data, err := readClientInput(path, in)
	if err != nil {
		return err
	}
	c, err := config.Parse(data)
	if err != nil {
		return err
	}
	if err := validateTargetOutput(target, c); err != nil {
		return err
	}
	path, in = "-", strings.NewReader(string(data))
	if runtime.GOOS == "darwin" {
		if err := os.MkdirAll(filepath.Dir(target.ConfigPath), 0700); err != nil {
			return err
		}
		return applyClientConfigAt(path, in, out, target.ConfigPath, launchdWallService{}, true, enableLaunchdWall)
	}
	service := systemdWallService{name: target.Service, statusPath: target.StatusPath}
	return applyClientConfigAt(path, in, out, target.ConfigPath, service, true, func() error { return systemctl("enable", target.Service) })
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
		if err := detect.CheckNativeElements(caps.Sink, detect.HasElement); err != nil {
			return err
		}
		if err := detect.CheckNativeVersion(caps.Sink); err != nil {
			return err
		}
		if caps.Sink == "planes" || caps.Sink == "compositor" {
			if !detect.HasElement("kmssink") {
				return errors.New("GStreamer kmssink is missing")
			}
			if runtime.GOOS == "linux" {
				if _, err := detect.SelectedConnector("/", c.Output); err != nil {
					return err
				}
			}
		}
		if _, err := pipeline.Plans(c, placed, caps); err != nil {
			return err
		}
		if caps.Sink == "planes" {
			if err := detect.CheckPlaneReachability(c.Output, c.Planes[:len(c.Tiles)]); err != nil {
				return err
			}
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
	if hadPrevious {
		marker.BackupSHA256 = clientSHA256(old)
	}
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
	// Reporting may need a directory entry even after the config itself was restored.
	// Keep recovery moving if the disk is too full to write the status file.
	reportErr := recordClientRollback(dest, cause.Error())
	if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
		if stopErr := service.Stop(); stopErr != nil {
			return rollbackErrorWithReport(fmt.Errorf("new config failed (%v), previous config restored but service stop failed: %w", cause, stopErr), reportErr)
		}
		if reportErr != nil {
			return fmt.Errorf("new config failed (%v) and was removed, but rollback status failed: %w", cause, reportErr)
		}
		return fmt.Errorf("new config failed and was removed: %w", cause)
	}
	if err := service.Restart(); err != nil {
		return rollbackErrorWithReport(fmt.Errorf("new config failed (%v), previous config restored but restart failed: %w", cause, err), reportErr)
	}
	if err := service.Healthy(); err != nil {
		return rollbackErrorWithReport(fmt.Errorf("new config failed (%v), previous config restored but recovery health check failed: %w", cause, err), reportErr)
	}
	if frameService, ok := service.(clientFrameService); ok {
		restored, err := os.ReadFile(dest)
		if err != nil {
			return rollbackErrorWithReport(fmt.Errorf("new config failed (%v), previous config restored but its frame check could not read config: %w", cause, err), reportErr)
		}
		if err := frameService.FramesHealthy(restored); err != nil {
			return rollbackErrorWithReport(fmt.Errorf("new config failed (%v), previous config restored but frames did not recover: %w", cause, err), reportErr)
		}
	}
	if reportErr != nil {
		return fmt.Errorf("new config failed (%v), previous config restored and healthy but rollback status failed: %w", cause, reportErr)
	}
	return fmt.Errorf("new config failed; previous config restored: %w", cause)
}

func rollbackErrorWithReport(recoveryErr, reportErr error) error {
	if reportErr != nil {
		return fmt.Errorf("%w; rollback status also failed: %v", recoveryErr, reportErr)
	}
	return recoveryErr
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
