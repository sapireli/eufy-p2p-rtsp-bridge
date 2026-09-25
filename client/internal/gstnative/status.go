package gstnative

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const DefaultStatusPath = "/run/eufy-wall/status.json"

// Status is local process telemetry, not a control channel. Apply checks the PID, config digest,
// output progress, and live tile progress over time before accepting a restarted service.
type Status struct {
	SchemaVersion     int                   `json:"schema_version"`
	PID               int                   `json:"pid"`
	StartedAt         time.Time             `json:"started_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
	Sink              string                `json:"sink"`
	ConfigSHA256      string                `json:"config_sha256"`
	OutputFrames      uint64                `json:"output_frames"`
	OutputLastFrameAt *time.Time            `json:"output_last_frame_at,omitempty"`
	Tiles             map[string]TileStatus `json:"tiles"`
}

type TileStatus struct {
	ExpectedLive       bool       `json:"expected_live"`
	SourceKind         string     `json:"source_kind"`
	DecodedFrames      uint64     `json:"decoded_frames"`
	LastDecodedFrameAt *time.Time `json:"last_decoded_frame_at,omitempty"`
	Generation         uint64     `json:"generation"`
	State              string     `json:"state"`
	Error              string     `json:"error,omitempty"`
}

func HashConfig(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeStatus(path string, status Status) error {
	if path == "" {
		return errors.New("status path is empty")
	}
	_, digestErr := hex.DecodeString(status.ConfigSHA256)
	if status.SchemaVersion != 1 || status.PID <= 0 || status.Sink == "" ||
		len(status.ConfigSHA256) != 64 || digestErr != nil ||
		status.StartedAt.IsZero() || status.UpdatedAt.IsZero() || status.Tiles == nil {
		return errors.New("native compositor status is incomplete")
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > 1<<20 {
		return errors.New("native compositor status exceeds 1 MiB")
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".status-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0644); err != nil {
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
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync status directory: %w", err)
	}
	return nil
}
