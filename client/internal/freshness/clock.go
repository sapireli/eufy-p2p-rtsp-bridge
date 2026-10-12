// Package freshness distributes conservative bridge-monotonic clock mappings.
package freshness

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Capability struct {
	Mode                  string `json:"mode"` // Absent v1 mode retains enforce compatibility.
	StreamURL             string `json:"-"`    // Actual advertised RTSP source; never inferred by camera serial.
	Version               int    `json:"version"`
	Scope                 string `json:"scope"`
	MaxAgeMs              int    `json:"maxAgeMs"`
	ClockURL              string `json:"clockUrl"`
	CaptureClockValidated bool   `json:"captureClockValidated"`
}

func (c Capability) Supported() bool {
	versionMode := (c.Version == 1 && (c.Mode == "" || c.Mode == "enforce")) || (c.Version == 2 && (c.Mode == "observe" || c.Mode == "enforce"))
	return versionMode && c.Scope == "bridge-ingress" && c.MaxAgeMs > 0 && c.MaxAgeMs <= 65535 && !c.CaptureClockValidated && c.ClockURL != ""
}

// Enforced is deliberately opt-in for mode=observe; absent mode preserves v1 behavior.
func (c Capability) Enforced() bool { return c.Supported() && c.Mode != "observe" }

// The 48-byte clock record uses BE integers: magic EWCLK001, epoch[8], serverUs,
// local request-start ns, local response-end ns, local validity deadline ns.
// Readers get an entire atomic rename or the preceding record, never half a sample.
func Sample(ctx context.Context, endpoint, path string, now func() uint64) error {
	b, err := fetchSample(ctx, endpoint, now)
	if err != nil {
		return err
	}
	return writeSample(path, b)
}
func fetchSample(ctx context.Context, endpoint string, now func() uint64) ([]byte, error) {
	t0 := now()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("clock HTTP %d", resp.StatusCode)
	}
	var v struct {
		MonotonicUs           uint64 `json:"monotonicUs"`
		ServerEpoch           string `json:"serverEpoch"`
		Scope                 string `json:"scope"`
		CaptureClockValidated bool   `json:"captureClockValidated"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	t1 := now()
	epoch, err := hex.DecodeString(v.ServerEpoch)
	if err != nil || len(epoch) != 8 || v.Scope != "bridge-ingress" || v.CaptureClockValidated || t1 < t0 || t1-t0 > 1000000000 {
		return nil, fmt.Errorf("invalid bridge clock sample")
	}
	b := make([]byte, 48)
	copy(b, "EWCLK001")
	copy(b[8:], epoch)
	binary.BigEndian.PutUint64(b[16:], v.MonotonicUs)
	binary.BigEndian.PutUint64(b[24:], t0)
	binary.BigEndian.PutUint64(b[32:], t1)
	binary.BigEndian.PutUint64(b[40:], t1+5000000000)
	return b, nil
}
func writeSample(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "eufy-clock-record-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// selectSample keeps at most16 recent samples, discarding old epochs. A selected
// record keeps its original5s lease. Prefer >=2.1s remaining to cover the1s poll,
// maximum1s HTTP request and100ms scheduler allowance without a healthy-clock gap.
func selectSample(samples [][]byte, newSample []byte, now uint64) ([][]byte, []byte) {
	if newSample != nil {
		if len(samples) > 0 && !bytes.Equal(samples[0][8:16], newSample[8:16]) {
			samples = nil
		}
		samples = append(samples, newSample)
	}
	kept := samples[:0]
	for _, v := range samples {
		if len(v) == 48 && binary.BigEndian.Uint64(v[40:]) > now {
			kept = append(kept, v)
		}
	}
	samples = kept
	if len(samples) > 16 {
		samples = samples[len(samples)-16:]
	}
	var best []byte
	bestScore := uint64(^uint64(0))
	reserved := false
	for _, v := range samples {
		t0 := binary.BigEndian.Uint64(v[24:])
		t1 := binary.BigEndian.Uint64(v[32:])
		lease := binary.BigEndian.Uint64(v[40:]) - now
		hasReserve := lease >= 2100000000
		if reserved && !hasReserve {
			continue
		}
		if hasReserve && !reserved {
			reserved = true
			best = nil
			bestScore = ^uint64(0)
		}
		age := uint64(0)
		if now > t1 {
			age = now - t1
		}
		score := t1 - t0 + 2*((age+999)/1000) //1000ppm relative rate bound.
		if score < bestScore {
			bestScore = score
			best = v
		}
	}
	return samples, best
}
func Poll(ctx context.Context, endpoint, path string, log func(string)) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var samples [][]byte
	for {
		sample, err := fetchSample(ctx, endpoint, monotonicNs)
		if err != nil {
			log(err.Error())
		}
		var best []byte
		samples, best = selectSample(samples, sample, monotonicNs())
		if best != nil {
			if err := writeSample(path, best); err != nil {
				log(err.Error())
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
