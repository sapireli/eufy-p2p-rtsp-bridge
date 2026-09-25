package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type clientApplyStatus struct {
	SHA256       string    `json:"sha256,omitempty"`
	AppliedAt    time.Time `json:"appliedAt,omitempty"`
	Backup       string    `json:"backup,omitempty"`
	LastRollback string    `json:"lastRollback,omitempty"`
	RollbackAt   time.Time `json:"rollbackAt,omitempty"`
}

func clientSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readClientStatus(dest string) (clientApplyStatus, error) {
	b, err := os.ReadFile(dest + ".apply-status.json")
	if errors.Is(err, os.ErrNotExist) {
		return clientApplyStatus{}, nil
	}
	if err != nil {
		return clientApplyStatus{}, err
	}
	var status clientApplyStatus
	if err := json.Unmarshal(b, &status); err != nil {
		return status, fmt.Errorf("invalid apply status: %w", err)
	}
	return status, nil
}

func writeClientStatus(dest string, status clientApplyStatus) error {
	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return atomicClientWrite(dest+".apply-status.json", append(b, '\n'), 0644, nil)
}

func recordClientRollback(dest, reason string) error {
	status, err := readClientStatus(dest)
	if err != nil {
		return err
	}
	status.LastRollback = reason
	status.RollbackAt = time.Now().UTC()
	if data, err := os.ReadFile(dest); err == nil {
		status.SHA256 = clientSHA256(data)
	} else if errors.Is(err, os.ErrNotExist) {
		status.SHA256 = ""
	} else {
		return err
	}
	return writeClientStatus(dest, status)
}

func showClientStatus(dest string, jsonOutput bool, out io.Writer, active func() error) error {
	status, err := readClientStatus(dest)
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(dest)
	serviceErr := active()
	report := struct {
		ConfigPath string            `json:"configPath"`
		Exists     bool              `json:"exists"`
		Active     bool              `json:"active"`
		SHA256     string            `json:"sha256,omitempty"`
		LastApply  clientApplyStatus `json:"lastApply"`
	}{ConfigPath: dest, Exists: readErr == nil, Active: serviceErr == nil, LastApply: status}
	if readErr == nil {
		report.SHA256 = clientSHA256(data)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	_, err = fmt.Fprintf(out, "config: %s (exists=%v, sha256=%s)\nservice active: %v\nlast applied: %s\nlast backup: %s\nlast rollback: %s\n", dest, report.Exists, report.SHA256, report.Active, status.AppliedAt.Format(time.RFC3339), status.Backup, status.LastRollback)
	return err
}
