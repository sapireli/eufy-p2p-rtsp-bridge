package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modetestTwoOutputs = `Encoders:
id	crtc	type	possible crtcs	possible clones
31	50	TMDS	0x00000001	0x00000000
41	60	TMDS	0x00000002	0x00000000

Connectors:
id	encoder	status		name		size (mm)	modes	encoders
32	31	connected	HDMI-A-1	500x280	1	31
42	41	connected	HDMI-A-2	500x280	1	41

CRTCs:
id	fb	pos	size
50	0	(0,0)	(1920x1080)
60	0	(0,0)	(1920x1080)

Planes:
id	crtc	fb	CRTC x,y	x,y	gamma size	possible crtcs
71	0	0	0,0		0,0	0	0x00000001
72	0	0	0,0		0,0	0	0x00000002
73	0	0	0,0		0,0	0	0x00000003
 formats: NV12 XR24
`

func TestPlaneReachabilityRequiresCommonSelectedCRTC(t *testing.T) {
	if err := checkPlaneTopology(modetestTwoOutputs, "HDMI-A-2", []int{72, 73}); err != nil {
		t.Fatalf("compatible planes rejected: %v", err)
	}
	for _, tc := range []struct {
		name, output string
		planes       []int
		reason       string
	}{
		{"wrong CRTC", "HDMI-A-2", []int{71}, "cannot reach"},
		{"no common CRTC", "HDMI-A-2", []int{71, 72}, "cannot reach"},
		{"missing plane", "HDMI-A-2", []int{99}, "absent"},
		{"duplicate plane", "HDMI-A-2", []int{73, 73}, "more than once"},
		{"wrong output", "DP-1", []int{73}, "not connected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkPlaneTopology(modetestTwoOutputs, tc.output, tc.planes); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %v, want %q", err, tc.reason)
			}
		})
	}
	if err := checkPlaneTopology("modetest failed", "HDMI-A-1", []int{71}); err == nil {
		t.Fatal("unknown modetest format passed")
	}
}

func TestSelectedDRMCardRejectsDisconnectedOutput(t *testing.T) {
	root := t.TempDir()
	write := func(name, status string) {
		dir := filepath.Join(root, "sys/class/drm", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "modes"), []byte("1920x1080\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("card0-HDMI-A-1", "disconnected\n")
	write("card1-HDMI-A-2", "connected\n")
	if card, output, err := selectedDRMCard(root, ""); err != nil || card != "card1" || output != "HDMI-A-2" {
		t.Fatalf("auto DRM output = %s/%s, %v", card, output, err)
	}
	if _, _, err := selectedDRMCard(root, "HDMI-A-1"); err == nil {
		t.Fatal("disconnected named output accepted")
	}
}
