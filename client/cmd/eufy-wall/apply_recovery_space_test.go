package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func pendingRecoveryFixture(t *testing.T) (dest, backup string) {
	t.Helper()
	dest = filepath.Join(t.TempDir(), "wall.yaml")
	backup = dest + ".bak.manual"
	if err := os.WriteFile(dest, []byte(otherWallYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte(validWallYAML), 0600); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(pendingClientApply{Backup: backup, BackupSHA256: clientSHA256([]byte(validWallYAML)), HadPrevious: true})
	if err := os.WriteFile(dest+".pending", marker, 0600); err != nil {
		t.Fatal(err)
	}
	return dest, backup
}

func TestRecoveryUsesBackupHardLinkWhenFullCopyHasNoSpace(t *testing.T) {
	dest, backup := pendingRecoveryFixture(t)
	backupInfo, _ := os.Stat(backup)
	failedCopy := func(string, []byte, os.FileMode, *syscall.Stat_t) error { return syscall.ENOSPC }
	if err := recoverClientConfigLockedWithOps(dest, failedCopy, os.Link, os.Rename); err != nil {
		t.Fatal(err)
	}
	activeInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(backupInfo, activeInfo) || activeInfo.Mode().Perm() != 0600 || activeInfo.Sys().(*syscall.Stat_t).Uid != backupInfo.Sys().(*syscall.Stat_t).Uid || activeInfo.Sys().(*syscall.Stat_t).Gid != backupInfo.Sys().(*syscall.Stat_t).Gid {
		t.Fatalf("hard-link restore lost backup inode/mode/owner: backup=%+v active=%+v", backupInfo, activeInfo)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("dated backup was consumed despite successful hard link: %v", err)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains: %v", err)
	}
	status, err := readClientStatus(dest)
	if err != nil || status.SHA256 != clientSHA256([]byte(validWallYAML)) || status.LastRollback == "" {
		t.Fatalf("rollback report missing after low-space restore: %+v %v", status, err)
	}
}

func TestRecoveryConsumesBackupOnlyWhenLinkAlsoHasNoSpace(t *testing.T) {
	dest, backup := pendingRecoveryFixture(t)
	failedCopy := func(string, []byte, os.FileMode, *syscall.Stat_t) error { return syscall.ENOSPC }
	failedLink := func(string, string) error { return syscall.ENOSPC }
	if err := recoverClientConfigLockedWithOps(dest, failedCopy, failedLink, os.Rename); err != nil {
		t.Fatal(err)
	}
	active, _ := os.ReadFile(dest)
	if string(active) != validWallYAML {
		t.Fatalf("old config not restored: %q", active)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("emergency backup move did not consume its old name: %v", err)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains: %v", err)
	}
	status, err := readClientStatus(dest)
	if err != nil || status.LastRollback == "" {
		t.Fatalf("emergency rollback report missing: %+v %v", status, err)
	}
}

func TestRecoveryRetryRecognizesAlreadyMovedBackup(t *testing.T) {
	dest, backup := pendingRecoveryFixture(t)
	if err := os.Rename(backup, dest); err != nil {
		t.Fatal(err)
	}
	// Simulate power loss after rename but before pending-marker removal.
	if err := recoverClientConfigLocked(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry left pending marker: %v", err)
	}
	active, _ := os.ReadFile(dest)
	if string(active) != validWallYAML {
		t.Fatalf("retry changed restored bytes: %q", active)
	}
	// A missing backup must not be treated as restored when the active candidate differs.
	dest, backup = pendingRecoveryFixture(t)
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := recoverClientConfigLocked(dest); err == nil {
		t.Fatal("missing backup and bad candidate were accepted")
	}
}

func TestRecoveryDoesNotBypassPermissionErrors(t *testing.T) {
	dest, backup := pendingRecoveryFixture(t)
	failedCopy := func(string, []byte, os.FileMode, *syscall.Stat_t) error { return syscall.EACCES }
	unexpected := func(string, string) error { t.Fatal("fallback ran after permission error"); return nil }
	if err := recoverClientConfigLockedWithOps(dest, failedCopy, unexpected, unexpected); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("permission failure was hidden: %v", err)
	}
	active, _ := os.ReadFile(dest)
	if string(active) != otherWallYAML {
		t.Fatal("non-space failure changed candidate unexpectedly")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup lost after non-space failure: %v", err)
	}
	if _, err := os.Stat(dest + ".pending"); err != nil {
		t.Fatalf("pending marker lost after non-space failure: %v", err)
	}
}

func TestRecoveryRejectsCorruptBackupBeforeReplacingCandidate(t *testing.T) {
	dest, backup := pendingRecoveryFixture(t)
	if err := os.WriteFile(backup, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverClientConfigLocked(dest); err == nil {
		t.Fatal("corrupt backup was restored")
	}
	active, _ := os.ReadFile(dest)
	if string(active) != otherWallYAML {
		t.Fatalf("corrupt backup replaced candidate: %q", active)
	}
	if _, err := os.Stat(dest + ".pending"); err != nil {
		t.Fatalf("pending marker was removed despite corrupt backup: %v", err)
	}
}

func TestInterruptedRecoveryStartsDespiteStatusWriteFailure(t *testing.T) {
	dest, _ := pendingRecoveryFixture(t)
	if err := os.Mkdir(dest+".apply-status.json", 0700); err != nil {
		t.Fatal(err)
	}
	if err := recoverClientConfig(dest); err != nil {
		t.Fatalf("status write prevented service pre-start recovery: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != validWallYAML {
		t.Fatalf("old config not restored: %q, %v", got, err)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains: %v", err)
	}
}

func TestRollbackRestartsDespiteStatusWriteFailure(t *testing.T) {
	dest, _ := pendingRecoveryFixture(t)
	if err := os.Mkdir(dest+".apply-status.json", 0700); err != nil {
		t.Fatal(err)
	}
	service := &fakeWallService{}
	err := rollbackClientConfig(dest, service, errors.New("candidate has no frames"))
	if err == nil || !strings.Contains(err.Error(), "rollback status failed") || !strings.Contains(err.Error(), "restored and healthy") {
		t.Fatalf("rollback report did not explain status failure after recovery: %v", err)
	}
	if service.restarts != 1 || service.healthChecks != 1 || service.frameChecks != 1 {
		t.Fatalf("restored wall was not verified: restarts=%d health=%d frames=%d", service.restarts, service.healthChecks, service.frameChecks)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != validWallYAML {
		t.Fatalf("old config not restored: %q, %v", got, err)
	}
	if _, err := os.Stat(dest + ".pending"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains: %v", err)
	}
}
