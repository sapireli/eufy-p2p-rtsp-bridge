package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const wallRuntimeStatusPath = "/run/eufy-wall/status.json"

type runtimeTileStatus struct {
	ExpectedLive       bool      `json:"expected_live"`
	DecodedFrames      uint64    `json:"decoded_frames"`
	LastDecodedFrameAt time.Time `json:"last_decoded_frame_at"`
	Generation         uint64    `json:"generation"`
	State              string    `json:"state"`
	Error              string    `json:"error"`
}

type runtimeWallStatus struct {
	SchemaVersion     int                          `json:"schema_version"`
	PID               int                          `json:"pid"`
	StartedAt         time.Time                    `json:"started_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`
	Sink              string                       `json:"sink"`
	ConfigSHA256      string                       `json:"config_sha256"`
	OutputFrames      uint64                       `json:"output_frames"`
	OutputLastFrameAt time.Time                    `json:"output_last_frame_at"`
	Tiles             map[string]runtimeTileStatus `json:"tiles"`
}

func readRuntimeWallStatus(path string) (runtimeWallStatus, error) {
	f, err := os.Open(path)
	if err != nil {
		return runtimeWallStatus{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return runtimeWallStatus{}, err
	}
	if len(b) > 1<<20 {
		return runtimeWallStatus{}, errors.New("runtime status exceeds 1 MiB")
	}
	var status runtimeWallStatus
	if err := json.Unmarshal(b, &status); err != nil {
		return runtimeWallStatus{}, fmt.Errorf("runtime status JSON: %w", err)
	}
	return status, nil
}

// runtimeProgress compares two samples from the same service generation. A live tile must decode new
// buffers; a black or still tile only needs the compositor output to keep advancing.
func runtimeProgress(before, after runtimeWallStatus, pid int, hash, sink string, ids []string, now time.Time) error {
	if after.SchemaVersion != 1 || after.PID != pid || after.Sink != sink || after.ConfigSHA256 != hash {
		return errors.New("runtime status does not match the active service, sink, and config")
	}
	if after.StartedAt.IsZero() || after.UpdatedAt.IsZero() || after.UpdatedAt.After(now.Add(2*time.Second)) || now.Sub(after.UpdatedAt) > 5*time.Second {
		return errors.New("runtime status is stale or has an invalid clock")
	}
	if after.OutputLastFrameAt.IsZero() || after.OutputLastFrameAt.After(now.Add(2*time.Second)) || now.Sub(after.OutputLastFrameAt) > 5*time.Second {
		return errors.New("display output has no recent frame")
	}
	if before.PID != after.PID || !before.StartedAt.Equal(after.StartedAt) || before.ConfigSHA256 != after.ConfigSHA256 || before.Sink != after.Sink {
		return errors.New("waiting for two samples from the same renderer")
	}
	if !after.UpdatedAt.After(before.UpdatedAt) || after.OutputFrames <= before.OutputFrames {
		return errors.New("display output frame counter did not advance")
	}
	for _, id := range ids {
		tile, ok := after.Tiles[id]
		if !ok {
			return fmt.Errorf("tile %s is absent from runtime status", id)
		}
		if tile.State == "error" || tile.State == "retrying" || tile.State == "stalled" {
			return fmt.Errorf("tile %s is %s: %s", id, tile.State, tile.Error)
		}
		if !tile.ExpectedLive {
			continue
		}
		if tile.State != "playing" {
			return fmt.Errorf("tile %s is not playing (state=%s)", id, tile.State)
		}
		previous, ok := before.Tiles[id]
		if !ok || !previous.ExpectedLive || previous.Generation != tile.Generation || tile.DecodedFrames <= previous.DecodedFrames {
			return fmt.Errorf("tile %s has no new decoded frames", id)
		}
		if tile.LastDecodedFrameAt.IsZero() || tile.LastDecodedFrameAt.After(now.Add(2*time.Second)) || now.Sub(tile.LastDecodedFrameAt) > 5*time.Second {
			return fmt.Errorf("tile %s last decoded frame is stale", id)
		}
	}
	return nil
}

func awaitRuntimeProgress(ctx context.Context, path string, pid int, hash, sink string, ids []string, poll time.Duration) error {
	var before runtimeWallStatus
	var lastErr error
	timer := time.NewTicker(poll)
	defer timer.Stop()
	for {
		after, err := readRuntimeWallStatus(path)
		if err == nil {
			err = runtimeProgress(before, after, pid, hash, sink, ids, time.Now())
			if err == nil {
				return nil
			}
			if after.SchemaVersion == 1 && after.PID == pid && after.ConfigSHA256 == hash && after.Sink == sink {
				before = after
			}
		}
		lastErr = err
		select {
		case <-timer.C:
		case <-ctx.Done():
			return fmt.Errorf("no verified display and live-tile frame progress: %v: %w", lastErr, ctx.Err())
		}
	}
}
