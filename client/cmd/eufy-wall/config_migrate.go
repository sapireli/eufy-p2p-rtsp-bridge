package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"eufy-wall/internal/config"
)

type clientMigrationReport struct {
	OK                  bool   `json:"ok"`
	SourceSchemaVersion int    `json:"sourceSchemaVersion"`
	CandidateSchema     int    `json:"candidateSchemaVersion"`
	CandidateFile       string `json:"candidateFile,omitempty"`
	Diff                string `json:"diff"`
	Next                string `json:"next"`
}

func migrateClientCommand(args []string, in io.Reader, out io.Writer, target clientTarget) error {
	if len(args) == 0 {
		return errors.New("usage: eufy-wall config migrate <legacy-file|-> [--output candidate.yaml] [--json]")
	}
	source, output, jsonOutput := args[0], "", false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--output":
			if output != "" || i+1 >= len(args) || args[i+1] == "" || args[i+1][0] == '-' {
				return errors.New("--output requires one candidate file path")
			}
			output = args[i+1]
			i++
		case "--json":
			if jsonOutput {
				return errors.New("--json may be supplied once")
			}
			jsonOutput = true
		default:
			return fmt.Errorf("unknown migration option %q", args[i])
		}
	}
	data, err := readClientInput(source, in)
	if err != nil {
		return migrationError(out, jsonOutput, err)
	}
	candidate, diff, err := migrateLegacyForEditor(data)
	if err != nil {
		return migrationError(out, jsonOutput, err)
	}
	converted, err := config.Parse(candidate)
	if err != nil {
		return migrationError(out, jsonOutput, err)
	}
	if err := validateTargetOutput(target, converted); err != nil {
		return migrationError(out, jsonOutput, err)
	}
	if output != "" {
		if err := writeClientMigrationCandidate(output, candidate, target); err != nil {
			return migrationError(out, jsonOutput, err)
		}
	}
	next := "Review the diff, then rerun with --output candidate.yaml; no active config was changed."
	if output != "" {
		next = fmt.Sprintf("Run eufy-wall config validate %s, review it, then eufy-wall config apply %s; apply backs up and can roll back the active config.", output, output)
	}
	report := clientMigrationReport{OK: true, SourceSchemaVersion: 1, CandidateSchema: 2, CandidateFile: output, Diff: diff, Next: next}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	_, err = fmt.Fprintf(out, "Legacy migration preview (source and active config untouched):\n%s\n%s\n", diff, next)
	return err
}

func migrationError(out io.Writer, jsonOutput bool, cause error) error {
	if !jsonOutput {
		return cause
	}
	d := diagnosticForClient(cause.Error(), "migrate")
	if err := emitClientJSON(out, false, &d); err != nil {
		return err
	}
	return cause
}

// A hard link publishes a fully synced draft only if the requested name does not already exist.
// The active config remains under config apply's lock, backup, and rollback transaction.
func writeClientMigrationCandidate(path string, data []byte, target clientTarget) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if abs == target.ConfigPath || isInstalledClientConfig(abs) {
		return errors.New("candidate output cannot be an active client config; use config apply after review")
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && (resolved == target.ConfigPath || isInstalledClientConfig(resolved)) {
		return errors.New("candidate output points to an active client config")
	}
	dir := filepath.Dir(abs)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && isInstalledClientConfig(filepath.Join(resolved, filepath.Base(abs))) {
		return errors.New("candidate output points to an installed client config")
	}
	tmp, err := os.CreateTemp(dir, ".eufy-wall-migrate-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmp.Name(), abs); err != nil {
		return fmt.Errorf("candidate file already exists or cannot be created: %w", err)
	}
	return syncClientDir(dir)
}
