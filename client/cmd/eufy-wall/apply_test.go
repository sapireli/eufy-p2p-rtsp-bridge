package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const validWallYAML = "rtsp_base: rtsp://127.0.0.1:8554\nlayout: 1\ntiles:\n  - camera: CAM1\n"
const otherWallYAML = "rtsp_base: rtsp://127.0.0.1:8554\nlayout: 1\ntiles:\n  - camera: CAM2\n"

type fakeWallService struct {
	restarts     int
	stops        int
	health       error
	healthChecks int
	frames       error
	frameChecks  int
	restart      func() error
}

func (f *fakeWallService) Restart() error {
	f.restarts++
	if f.restart != nil {
		return f.restart()
	}
	return nil
}
func (f *fakeWallService) Healthy() error {
	f.healthChecks++
	if f.healthChecks > 1 {
		return nil
	}
	return f.health
}
func (f *fakeWallService) Stop() error {
	f.stops++
	return nil
}
func (f *fakeWallService) FramesHealthy(_ []byte) error {
	f.frameChecks++
	return f.frames
}

func TestApplyClientDataAndNoop(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest, []byte(validWallYAML), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(dest)
	originalOwner := before.Sys().(*syscall.Stat_t)
	f := &fakeWallService{}
	if err := applyClientData(dest, []byte(otherWallYAML), f); err != nil {
		t.Fatal(err)
	}
	if f.restarts != 1 {
		t.Fatalf("restarts=%d", f.restarts)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != otherWallYAML {
		t.Fatalf("active config = %q", got)
	}
	info, _ := os.Stat(dest)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	owner := info.Sys().(*syscall.Stat_t)
	if owner.Uid != originalOwner.Uid || owner.Gid != originalOwner.Gid {
		t.Fatalf("config ownership changed from %d:%d to %d:%d", originalOwner.Uid, originalOwner.Gid, owner.Uid, owner.Gid)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains: %v", err)
	}
	status, err := readClientStatus(dest)
	if err != nil || status.SHA256 != clientSHA256([]byte(otherWallYAML)) || status.AppliedAt.IsZero() {
		t.Fatalf("missing applied status: %+v, %v", status, err)
	}
	backups, err := filepath.Glob(dest + ".bak.*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backupInfo, _ := os.Stat(backups[0])
	backupOwner := backupInfo.Sys().(*syscall.Stat_t)
	if backupOwner.Uid != originalOwner.Uid || backupOwner.Gid != originalOwner.Gid {
		t.Fatalf("backup ownership changed to %d:%d", backupOwner.Uid, backupOwner.Gid)
	}
	if err := applyClientData(dest, []byte(otherWallYAML), f); err != nil {
		t.Fatal(err)
	}
	if f.restarts != 1 {
		t.Fatalf("unchanged apply restarted service: %d", f.restarts)
	}
}

func TestApplyClientDataRejectsBeforeMutationAndRollsBackOnHealthFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest, []byte(validWallYAML), 0644); err != nil {
		t.Fatal(err)
	}
	f := &fakeWallService{}
	if err := applyClientData(dest, []byte("tiles: []\n"), f); err == nil {
		t.Fatal("invalid config accepted")
	}
	if f.restarts != 0 {
		t.Fatal("invalid config restarted service")
	}
	f.health = errors.New("no frames")
	err := applyClientData(dest, []byte(otherWallYAML), f)
	if err == nil || !strings.Contains(err.Error(), "previous config restored") {
		t.Fatalf("rollback error=%v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != validWallYAML || f.restarts != 2 {
		t.Fatalf("rollback failed: config=%q restarts=%d", got, f.restarts)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains: %v", err)
	}
	status, err := readClientStatus(dest)
	if err != nil || status.LastRollback == "" || status.SHA256 != clientSHA256([]byte(validWallYAML)) {
		t.Fatalf("rollback not recorded: %+v, %v", status, err)
	}
}

func TestFirstInstallFailureRemovesCandidate(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	f := &fakeWallService{health: errors.New("display unavailable")}
	if err := applyClientData(dest, []byte(validWallYAML), f); err == nil {
		t.Fatal("expected apply failure")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) || f.stops != 1 {
		t.Fatalf("candidate retained or service running: stat=%v stops=%d", err, f.stops)
	}
}

func TestApplyRollsBackWhenServiceRunsButFramesDoNotAdvance(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest, []byte(validWallYAML), 0644); err != nil {
		t.Fatal(err)
	}
	f := &fakeWallService{frames: errors.New("live tile is frozen")}
	err := applyClientData(dest, []byte(otherWallYAML), f)
	if err == nil || !strings.Contains(err.Error(), "frame progress") {
		t.Fatalf("frozen candidate was accepted: %v", err)
	}
	got, readErr := os.ReadFile(dest)
	if readErr != nil || string(got) != validWallYAML || f.frameChecks != 1 || f.restarts != 2 {
		t.Fatalf("frozen apply did not restore prior config: data=%q read=%v checks=%d restarts=%d", got, readErr, f.frameChecks, f.restarts)
	}
}

func TestRecoverClientConfigAfterCrashAndDuringRestart(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest, []byte(validWallYAML), 0644); err != nil {
		t.Fatal(err)
	}
	f := &fakeWallService{}
	f.restart = func() error {
		// systemd ExecStartPre runs in the restart while apply still owns its lock. It must not
		// deadlock or replace the candidate being checked.
		return recoverClientConfig(dest)
	}
	if err := applyClientData(dest, []byte(otherWallYAML), f); err != nil {
		t.Fatalf("live apply recovery: %v", err)
	}
	backup := dest + ".bak.manual"
	if err := os.WriteFile(backup, []byte(validWallYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest+".pending", []byte(`{"backup":"`+backup+`","hadPrevious":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverClientConfig(dest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != validWallYAML {
		t.Fatalf("crash recovery restored %q", got)
	}
	if err := recoverClientConfig(dest); err != nil {
		t.Fatalf("repeat recovery should be harmless: %v", err)
	}
}

func TestRecoverRejectsForgedBackupPath(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	if err := os.WriteFile(dest+".pending", []byte(`{"backup":"/etc/shadow","hadPrevious":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverClientConfig(dest); err == nil {
		t.Fatal("forged backup path accepted")
	}
}

func TestTryLockClientConfigIsNonblocking(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	unlock, err := lockClientConfig(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	start := time.Now()
	_, busy, err := tryLockClientConfig(dest)
	if err != nil || !busy || time.Since(start) > time.Second {
		t.Fatalf("busy=%v err=%v elapsed=%s", busy, err, time.Since(start))
	}
}

func TestApplyEnablesServiceOnlyAfterHealthyConfig(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "wall.yaml")
	service := &fakeWallService{health: errors.New("no frames")}
	enabled := 0
	enable := func() error { enabled++; return nil }
	if err := applyClientConfigAt("-", strings.NewReader(validWallYAML), &bytes.Buffer{}, dest, service, false, enable); err == nil {
		t.Fatal("failed health check accepted")
	}
	if enabled != 0 {
		t.Fatal("service enabled before a healthy apply")
	}
	service.health = nil
	service.healthChecks = 0
	if err := applyClientConfigAt("-", strings.NewReader(validWallYAML), &bytes.Buffer{}, dest, service, false, enable); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("successful apply enabled %d times", enabled)
	}
}
