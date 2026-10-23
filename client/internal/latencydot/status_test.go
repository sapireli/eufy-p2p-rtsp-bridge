package latencydot

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clockRecord(now uint64) []byte {
	c := make([]byte, 48)
	copy(c, "EWCLK001")
	copy(c[8:], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	binary.BigEndian.PutUint64(c[24:], now-20000000)
	binary.BigEndian.PutUint64(c[32:], now-10000000)
	binary.BigEndian.PutUint64(c[40:], now+4000000000)
	return c
}
func output(pid, channel, valid int, mono, upper uint64) string {
	return fmt.Sprintf("eufy-output pid=%d channel=%d valid=%d monoNs=%d upperNs=%d epoch=0102030405060708", pid, channel, valid, mono, upper)
}
func TestOutputAgeIncludesTimeSinceSinkReturn(t *testing.T) {
	now := uint64(10000000000)
	tr := New(func() uint64 { return now })
	tr.Select(0, "A", true, true)
	tr.Observe("A", "started pid 10")
	tr.Observe("A", output(10, 1, 1, now, 100000000))
	c := clockRecord(now)
	for _, tc := range []struct {
		elapsed uint64
		want    Color
	}{{0, Green}, {400000000, Green}, {400000001, Orange}, {1400000000, Orange}, {1400000001, Red}} {
		if got := tr.Colors(now+tc.elapsed, c)[0]; got != tc.want {
			t.Fatalf("elapsed%d got%x want%x", tc.elapsed, got, tc.want)
		}
	}
}
func TestInvalidLatestOutputReplacesGreenAndSilenceTurnsRed(t *testing.T) {
	now := uint64(10000000000)
	tr := New(func() uint64 { return now })
	tr.Select(0, "A", true, true)
	tr.Observe("A", "started pid 10")
	tr.Observe("A", output(10, 1, 1, now, 100000000))
	now += 100000000
	tr.Observe("A", output(10, 1, 0, now, 0))
	if tr.Colors(now, clockRecord(now))[0] != Orange {
		t.Fatal("invalid output retained green")
	}
	if tr.Colors(now+RedLimit+1, nil)[0] != Red {
		t.Fatal("known silence hidden by missing clock")
	}
}
func TestMalformedFutureOldProcessAndPreDemandOutputCannotGreen(t *testing.T) {
	now := uint64(10000000000)
	tr := New(func() uint64 { return now })
	tr.Select(0, "A", true, true)
	tr.Observe("A", "started pid 11")
	for _, line := range []string{output(10, 1, 1, now, 1), output(11, 1, 1, now+1, 1), strings.Replace(output(11, 1, 1, now, 1), "upperNs=1 ", "", 1), strings.Replace(output(11, 1, 1, now, 1), "upperNs=1", "upperNs=oops", 1)} {
		tr.Observe("A", line)
		if tr.Colors(now, clockRecord(now))[0] != Orange {
			t.Fatalf("malformed green: %s", line)
		}
	}
	tr.Observe("A", output(11, 1, 1, now, 1))
	now += 100000000
	tr.Observe("A", output(11, 1, 0, now, 0))
	tr.Observe("A", output(11, 1, 1, now-50000000, 1))
	if tr.Colors(now, clockRecord(now))[0] != Orange {
		t.Fatal("old sample overwrote latest invalid output")
	}
	now += 100000000
	tr.Select(0, "A", false, true)
	tr.Select(0, "A", true, true)
	tr.Observe("A", output(11, 1, 1, now-1, 1))
	if tr.Colors(now, clockRecord(now))[0] != Orange {
		t.Fatal("previous demand sample accepted")
	}
	tr.Observe("A", output(11, 1, 1, now, 1))
	if tr.Colors(now, clockRecord(now))[0] != Green {
		t.Fatal("same current PID lost after demand reset")
	}
}
func TestClockFailuresCannotGreen(t *testing.T) {
	now := uint64(10000000000)
	tr := New(func() uint64 { return now })
	tr.Select(0, "A", true, true)
	tr.Observe("A", "started pid 10")
	tr.Observe("A", output(10, 1, 1, now, 1))
	for _, change := range []func([]byte){func(c []byte) { c[8]++ }, func(c []byte) { binary.BigEndian.PutUint64(c[40:], now) }, func(c []byte) { binary.BigEndian.PutUint64(c[24:], now) }, func(c []byte) { binary.BigEndian.PutUint64(c[40:], now+10000000000) }} {
		c := clockRecord(now)
		change(c)
		if tr.Colors(now, c)[0] != Orange {
			t.Fatal("invalid clock green")
		}
	}
	if tr.Colors(now, nil)[0] != Orange {
		t.Fatal("missing clock green")
	}
}
func TestStartupRetriesDoNotHideOutageAndUnsupportedStaysUnknown(t *testing.T) {
	now := uint64(10000000000)
	tr := New(func() uint64 { return now })
	tr.Select(0, "A", true, true)
	tr.Select(1, "B", true, false)
	now += RedLimit + 1
	tr.Observe("A", "started pid 10")
	tr.Select(0, "A", true, true)
	got := tr.Colors(now, nil)
	if got[0] != Red || got[1] != Orange {
		t.Fatalf("wrong startup/retry/unsupported: %v", got)
	}
	tr.Select(0, "A", false, true)
	if tr.Colors(now, nil)[0] != Orange {
		t.Fatal("idle not unknown")
	}
}
func TestUnknownImageIsPrivateAndVisible(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dot.png")
	if err := WriteUnknownImage(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := WriteUnknownImage(p); err == nil {
		t.Fatal("clobbered image")
	}
}

func TestRetryRetainsActualOutageAgeWithoutOldGreen(t *testing.T) {
	now := uint64(10000000000)
	tr := New(func() uint64 { return now })
	tr.Select(0, "A", true, true)
	tr.Observe("A", "started pid 10")
	now += 10000000000
	tr.Observe("A", output(10, 1, 1, now, 100000000))
	now += 100000000
	tr.Observe("A", "started pid 11")
	if tr.Colors(now, clockRecord(now))[0] != Orange {
		t.Fatal("retry should invalidate old green but not invent long outage")
	}
	if tr.Colors(now+RedLimit, nil)[0] != Red {
		t.Fatal("retry erased output outage age")
	}
	tr.Observe("A", output(10, 1, 1, now, 1))
	if tr.Colors(now, clockRecord(now))[0] != Orange {
		t.Fatal("old PID accepted")
	}
}
