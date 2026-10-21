package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
	"eufy-wall/internal/pipeline"
)

// A single simulated decoder resource lets real child processes prove teardown
// precedes hardware retry, without depending on a particular GPU's capacity.
func TestHardwarePriorityReclaimsOnlyEligibleTile(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		requesterPinned, victimPinned bool
		requesterRank, victimRank     int
		failure, victimPool           string
		explicit                      bool
		reclaim                       bool
		nativeFallback                bool
	}{
		{name: "higher rank", requesterRank: 2, victimRank: 1, reclaim: true},
		{name: "native fallback uses compatible factory", requesterRank: 2, victimRank: 1, reclaim: true, nativeFallback: true},
		{name: "pinned requester", requesterPinned: true, victimRank: 100, reclaim: true},
		{name: "pinned victim", requesterRank: 2, victimPinned: true},
		{name: "equal rank"},
		{name: "lower rank", requesterRank: 1, victimRank: 2},
		{name: "network", requesterRank: 2, failure: "ERROR: from element /GstPipeline:p/GstRTSPSrc:src: Could not read from resource."},
		{name: "corrupt media", requesterRank: 2, failure: "ERROR: from element /GstPipeline:p/GstV4l2H264Dec:decoder: Failed to process frame."},
		{name: "unsupported format", requesterRank: 2, failure: "ERROR: from element /GstPipeline:p/GstV4l2H264Dec:decoder: Decoder on device /dev/video10 has no supported input format"},
		{name: "other pool", requesterRank: 2, victimPool: "va"},
		{name: "explicit decoder", requesterRank: 2, explicit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "player")
			lock, events := filepath.Join(dir, "decoder"), filepath.Join(dir, "events")
			code := `#!/bin/sh
mode="$1"
lock="$2"
events="$3"
if [ "$mode" = native ]; then
 printf '%s\n' "$mode-failed" >> "$events"
 echo 'ERROR: native rendition unavailable' >&2
 exit 1
fi
if [ "$mode" = low-software ] || [ "$mode" = high-software ]; then
 printf '%s\n' "$mode" >> "$events"
 exec sleep 30
fi
if ! mkdir "$lock" 2>/dev/null; then
 printf '%s\n' "$mode-failed" >> "$events"
 printf '%s\n' "$4" >&2
 exit 1
fi
printf '%s\n' "$mode-acquired" >> "$events"
trap 'rmdir "$lock"; printf "%s\n" "$mode-released" >> "$events"; exit 0' TERM INT
while :; do sleep 0.02; done
`
			if err := os.WriteFile(bin, []byte(code), 0700); err != nil {
				t.Fatal(err)
			}
			failure := tc.failure
			if failure == "" {
				failure = "ERROR: from element /GstPipeline:p/GstV4l2H264Dec:decoder: Failed to allocate required memory."
			}
			args := func(mode string) []string { return []string{mode, lock, events, failure} }
			pool := tc.victimPool
			if pool == "" {
				pool = "v4l2"
			}
			low := pipeline.Plan{Name: "low", Args: args("low"), SoftwareArgs: args("low-software"), HardwareDecoder: pool, HardwarePinned: tc.victimPinned, HardwarePriority: tc.victimRank}
			high := pipeline.Plan{Name: "high", Args: args("high"), SoftwareArgs: args("high-software"), HardwareDecoder: "v4l2", HardwarePinned: tc.requesterPinned, HardwarePriority: tc.requesterRank}
			if tc.nativeFallback {
				low.Args = args("native")
				low.FallbackArgs = args("low")
				low.HardwareDecoder = "v4l2h265dec"
				low.FallbackHardwareDecoder = pool
			}
			if tc.explicit {
				low.SoftwareArgs = nil
				high.SoftwareArgs = nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			r := &recorder{}
			m := NewManager(bin, config.Restart{}, r.log)
			defer func() { cancel(); m.Stop() }()
			read := func() string { b, _ := os.ReadFile(events); return string(b) }
			m.Update(ctx, []pipeline.Plan{low})
			waitFor(t, func() bool { return strings.Contains(read(), "low-acquired") }, "low hardware allocation")
			// Identical refresh after compatible selection must not relaunch native.
			m.Update(ctx, []pipeline.Plan{low})
			m.Update(ctx, []pipeline.Plan{low, high})
			if tc.reclaim {
				waitFor(t, func() bool {
					return strings.Contains(read(), "high-acquired") && strings.Contains(read(), "low-software")
				}, "hardware resource handoff")
				got := read()
				if strings.Index(got, "low-released") > strings.Index(got, "high-acquired") {
					t.Fatalf("retried before release: %s", got)
				}
				m.Update(ctx, []pipeline.Plan{low, high})
				wantLowStarts := 2
				if tc.nativeFallback {
					wantLowStarts = 3
				}
				if r.countWith("low: started") != wantLowStarts || r.countWith("high: started") != 2 {
					t.Fatalf("identical update reset allocation: %s", read())
				}
			} else {
				waitFor(t, func() bool { return r.countWith("high: started") >= 3 || strings.Contains(read(), "high-software") }, "requester retries without displacing sibling")
				if strings.Contains(read(), "low-released") || r.countWith("low: started") != 1 {
					t.Fatalf("ineligible sibling displaced: %s", read())
				}
			}
		})
	}
}

// A manager reservation can race a decoder's own initialization fallback. The
// demotion must be acknowledged without replacing the software args with nil.
func TestDemotionConcurrentWithOwnSoftwareFallback(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "player")
	events := filepath.Join(dir, "events")
	code := `#!/bin/sh
printf '%s\n' "$1" >> "$2"
if [ "$1" = hardware ]; then
 echo 'ERROR: from element /GstPipeline:p/GstV4l2H264Dec:decoder: Could not initialize supporting library.' >&2
 exit 1
fi
exec sleep 30
`
	if err := os.WriteFile(bin, []byte(code), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	demote := make(chan chan struct{}, 1)
	ack := make(chan struct{})
	done := make(chan struct{})
	control := &decoderControl{demote: demote, onSoftware: func() { demote <- ack }}
	go func() {
		defer close(done)
		_ = runWithDecoderControl(ctx, bin, []string{"hardware", events}, nil, []string{"software", events}, config.Restart{}, func(string) {}, control)
	}()
	select {
	case <-ack:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("concurrent demotion not acknowledged")
	}
	waitFor(t, func() bool { b, _ := os.ReadFile(events); return strings.Contains(string(b), "software") }, "software playback")
	cancel()
	<-done
	b, _ := os.ReadFile(events)
	if strings.TrimSpace(string(b)) != "hardware\nsoftware" {
		t.Fatalf("software args/latch lost: %q", b)
	}
}
