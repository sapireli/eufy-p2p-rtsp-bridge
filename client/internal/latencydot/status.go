// Package latencydot tracks conservative bridge-ingress age at successful output.
// It does not claim camera capture age or physical scanout verification.
package latencydot

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
)

type Color uint32

const (
	Green  Color = 0x22c55e
	Orange Color = 0xf59e0b
	Red    Color = 0xef4444
)
const (
	GreenLimit uint64 = 500000000
	RedLimit   uint64 = 1500000000
)

type sample struct {
	mono, upper uint64
	epoch       [8]byte
	valid       bool
}
type slot struct {
	camera         string
	live, measured bool
	demandAt       uint64
	output         sample
}
type Tracker struct {
	mu    sync.Mutex
	slots map[int]slot
	pids  map[string]uint64
	now   func() uint64
}

func New(now func() uint64) *Tracker {
	return &Tracker{slots: map[int]slot{}, pids: map[string]uint64{}, now: now}
}

// Select invalidates old pictures when a motion tile changes cameras/content.
func (t *Tracker) Select(index int, camera string, live, measured bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	old, known := t.slots[index]
	if !known || old.camera != camera || old.live != live || old.measured != measured {
		t.slots[index] = slot{camera: camera, live: live, measured: measured, demandAt: t.now()}
	}
}
func (t *Tracker) Observe(camera, line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if strings.HasPrefix(line, "started pid ") {
		pid, err := strconv.ParseUint(strings.TrimPrefix(line, "started pid "), 10, 64)
		if err != nil || pid == 0 {
			return
		}
		t.pids[camera] = pid
		for i, s := range t.slots {
			if s.camera == camera {
				s.output.valid = false
				t.slots[i] = s
			}
		}
		return
	}
	if !strings.HasPrefix(line, "eufy-output ") {
		return
	}
	fields := map[string]string{}
	for _, f := range strings.Fields(line) {
		k, v, ok := strings.Cut(f, "=")
		if ok {
			if _, duplicate := fields[k]; duplicate {
				return
			}
			fields[k] = v
		}
	}
	nums := map[string]uint64{}
	for _, key := range []string{"pid", "channel", "valid", "monoNs", "upperNs"} {
		n, err := strconv.ParseUint(fields[key], 10, 64)
		if err != nil {
			return
		}
		nums[key] = n
	}
	if nums["pid"] == 0 || nums["pid"] != t.pids[camera] || nums["channel"] == 0 || nums["channel"] > 1<<20 || nums["valid"] > 1 {
		return
	}
	index := int(nums["channel"] - 1)
	s, ok := t.slots[index]
	if !ok || !s.live || !s.measured || s.camera != camera {
		return
	}
	epoch, err := hex.DecodeString(fields["epoch"])
	if err != nil || len(epoch) != 8 {
		return
	}
	mono := nums["monoNs"]
	if mono == 0 || mono < s.demandAt || mono > t.now() || mono <= s.output.mono {
		return
	}
	next := sample{mono: mono, upper: nums["upperNs"], valid: nums["valid"] == 1}
	copy(next.epoch[:], epoch)
	// Newest invalid output must replace a preceding valid picture too.
	s.output = next
	t.slots[index] = s
}
func (t *Tracker) Colors(now uint64, clock []byte) map[int]Color {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[int]Color{}
	for i, s := range t.slots {
		out[i] = classify(s, now, clock)
	}
	return out
}
func classify(s slot, now uint64, clock []byte) Color {
	if !s.live || !s.measured {
		return Orange
	}
	o := s.output
	if o.mono == 0 {
		if now >= s.demandAt && now-s.demandAt > RedLimit {
			return Red
		}
		return Orange
	}
	if o.mono > now {
		return Orange
	}
	elapsed := now - o.mono
	if elapsed > RedLimit {
		return Red
	}
	if !o.valid || len(clock) != 48 || string(clock[:8]) != "EWCLK001" || !bytes.Equal(clock[8:16], o.epoch[:]) {
		return Orange
	}
	t0, t1, deadline := binary.BigEndian.Uint64(clock[24:32]), binary.BigEndian.Uint64(clock[32:40]), binary.BigEndian.Uint64(clock[40:48])
	if t0 > t1 || t1-t0 > 1000000000 || t1 > now || deadline <= now || deadline < t1 || deadline-t1 > 5000000000 {
		return Orange
	}
	if o.upper > RedLimit || elapsed > RedLimit-o.upper {
		return Red
	}
	if o.upper <= GreenLimit && elapsed <= GreenLimit-o.upper {
		return Green
	}
	return Orange
}
