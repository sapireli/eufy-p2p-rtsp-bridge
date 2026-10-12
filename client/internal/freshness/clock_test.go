package freshness

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicClockRecordUsesMonotonicBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"monotonicUs":123456,"serverEpoch":"0102030405060708","scope":"bridge-ingress","captureClockValidated":false}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "clock")
	times := []uint64{80000000000, 80010000000}
	i := 0
	if err := Sample(context.Background(), server.URL, path, func() uint64 { v := times[i]; i++; return v }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 48 || string(data[:8]) != "EWCLK001" || binary.BigEndian.Uint64(data[16:]) != 123456 || binary.BigEndian.Uint64(data[24:]) != times[0] || binary.BigEndian.Uint64(data[40:]) != times[1]+5000000000 {
		t.Fatalf("bad mapping %x", data)
	}
}
func TestInvalidClockLeavesPreviousSampleIntact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"monotonicUs":123456,"serverEpoch":"bad","scope":"capture","captureClockValidated":true}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "clock")
	os.WriteFile(path, []byte("old atomic record"), 0600)
	if err := Sample(context.Background(), server.URL, path, func() uint64 { return 80000000000 }); err == nil {
		t.Fatal("accepted invalid clock")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old atomic record" {
		t.Fatal("corrupted preceding record")
	}
}
func TestCapabilitiesCannotClaimCaptureGuarantee(t *testing.T) {
	c := Capability{Version: 1, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://host/api/clock"}
	if !c.Supported() {
		t.Fatal("valid cap rejected")
	}
	c.CaptureClockValidated = true
	if c.Supported() {
		t.Fatal("unsupported capture guarantee accepted")
	}
}

func clockFixture(epoch byte, t0, rtt uint64) []byte {
	b := make([]byte, 48)
	copy(b, "EWCLK001")
	b[8] = epoch
	binary.BigEndian.PutUint64(b[24:], t0)
	binary.BigEndian.PutUint64(b[32:], t0+rtt)
	binary.BigEndian.PutUint64(b[40:], t0+rtt+5000000000)
	return b
}
func TestBestClockRejectsWorseRTTWithoutExtendingLease(t *testing.T) {
	old := clockFixture(1, 10000000000, 2000000)
	recent := clockFixture(1, 11000000000, 300000000)
	samples, best := selectSample([][]byte{old}, recent, 11300000000)
	if len(samples) != 2 || best[24] != old[24] || binary.BigEndian.Uint64(best[24:]) != 10000000000 {
		t.Fatal("worse RTT displaced valid accurate sample")
	}
	if binary.BigEndian.Uint64(best[40:]) != 15002000000 {
		t.Fatal("extended old sample lease")
	}
}
func TestClockSelectionReservesNextPollAndRequestBudget(t *testing.T) {
	old := clockFixture(1, 10000000000, 2000000)
	recent := clockFixture(1, 14000000000, 100000000)
	_, best := selectSample([][]byte{old}, recent, 14100000000)
	if binary.BigEndian.Uint64(best[24:]) != 14000000000 {
		t.Fatal("selected sample expiring before next maximum-duration poll")
	}
}
func TestClockSelectionDropsEpochAndExpiredSamples(t *testing.T) {
	old := clockFixture(1, 10000000000, 2000000)
	recent := clockFixture(2, 11000000000, 100000000)
	samples, best := selectSample([][]byte{old}, recent, 11100000000)
	if len(samples) != 1 || best[8] != 2 {
		t.Fatal("retained previous server epoch")
	}
	samples, best = selectSample(samples, nil, 17000000000)
	if len(samples) != 0 || best != nil {
		t.Fatal("retained expired mapping")
	}
}
func TestClockScoreIncludesRelativeRateUncertainty(t *testing.T) {
	old := clockFixture(1, 10000000000, 2000000)
	recent := clockFixture(1, 12000000000, 5000000)
	_, best := selectSample([][]byte{old}, recent, 12005000000)
	if binary.BigEndian.Uint64(best[24:]) != 12000000000 {
		t.Fatal("ignored older sample's1000ppm projected uncertainty")
	}
}

func TestObserveNeverEnforcesAndOldV1RemainsCompatible(t *testing.T) {
	c := Capability{Version: 1, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock"}
	if !c.Enforced() {
		t.Fatal("lost old v1 enforcement semantics")
	}
	c.Mode = "observe"
	if c.Supported() {
		t.Fatal("v1 observation cannot negotiate safely with older clients")
	}
	c.Version = 2
	if !c.Supported() || c.Enforced() {
		t.Fatal("v2 observation activated deadline enforcement")
	}
	c.Mode = "enforce"
	if !c.Enforced() {
		t.Fatal("explicit enforce ignored")
	}
	c.Mode = "unknown"
	if c.Supported() {
		t.Fatal("unknown mode accepted")
	}
}

func TestV2RequiresExplicitKnownMode(t *testing.T) {
	c := Capability{Version: 2, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock"}
	if c.Supported() {
		t.Fatal("v2 missing mode accepted")
	}
	c.Mode = "observe"
	if !c.Supported() || c.Enforced() {
		t.Fatal("v2 observation semantics")
	}
	c.Mode = "enforce"
	if !c.Enforced() {
		t.Fatal("v2 enforce rejected")
	}
	c.Mode = "unknown"
	if c.Supported() {
		t.Fatal("v2 unknown mode accepted")
	}
	c.Version = 3
	c.Mode = "enforce"
	if c.Supported() {
		t.Fatal("future capability accepted")
	}
}
