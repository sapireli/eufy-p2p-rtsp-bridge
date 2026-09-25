package detect

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CheckPlaneReachability checks the connector/encoder/CRTC/plane routing advertised by DRM.
// It is read-only: format, scaling, and visible pixels still need a display render test.
func CheckPlaneReachability(output string, ids []int) error {
	if len(ids) == 0 {
		return fmt.Errorf("planes: specify a DRM plane ID for each tile")
	}
	card, connector, err := selectedDRMCard("/", output)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "modetest", "-D", filepath.Join("/dev/dri", card), "-e", "-c", "-p").Output()
	if err != nil {
		return fmt.Errorf("query DRM plane routing with modetest: %w; install libdrm-tests and check video group access", err)
	}
	if len(b) > 4<<20 {
		return fmt.Errorf("modetest output exceeds 4 MiB")
	}
	return checkPlaneTopology(string(b), connector, ids)
}

func selectedDRMCard(fsRoot, output string) (card, connector string, err error) {
	pattern := "sys/class/drm/card*-HDMI-A-*/modes"
	if output != "" {
		pattern = "sys/class/drm/card*-" + output + "/modes"
	}
	paths, _ := filepath.Glob(filepath.Join(fsRoot, pattern))
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil || len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		dir := filepath.Dir(path)
		if status, readErr := os.ReadFile(filepath.Join(dir, "status")); readErr == nil && strings.TrimSpace(string(status)) != "connected" {
			continue
		}
		name := filepath.Base(dir)
		card, connector, _ = strings.Cut(name, "-")
		if card != "" && connector != "" {
			return card, connector, nil
		}
	}
	return "", "", fmt.Errorf("no connected DRM output %q with a mode; check the cable and output name", output)
}

type drmEncoder struct {
	crtc int
	mask uint64
}

type drmConnector struct {
	status   string
	encoder  int
	encoders []int
}

type drmTopology struct {
	crtcs      []int
	encoders   map[int]drmEncoder
	connectors map[string]drmConnector
	planes     map[int]uint64
}

func checkPlaneTopology(report, output string, ids []int) error {
	t, err := parseDRMTopology(report)
	if err != nil {
		return err
	}
	connector, ok := t.connectors[output]
	if !ok || connector.status != "connected" {
		return fmt.Errorf("DRM output %q is not connected according to modetest", output)
	}
	var allowed uint64
	if active, ok := t.encoders[connector.encoder]; ok && active.crtc > 0 {
		for i, crtc := range t.crtcs {
			if crtc == active.crtc {
				allowed = 1 << i
				break
			}
		}
	} else {
		for _, id := range connector.encoders {
			allowed |= t.encoders[id].mask
		}
	}
	if allowed == 0 {
		return fmt.Errorf("DRM output %q has no selectable CRTC", output)
	}
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("DRM plane %d is assigned more than once", id)
		}
		seen[id] = true
		mask, ok := t.planes[id]
		if !ok {
			return fmt.Errorf("DRM plane %d is absent from modetest output", id)
		}
		allowed &= mask
		if allowed == 0 {
			return fmt.Errorf("DRM plane %d cannot reach output %q on a common CRTC; select other plane IDs or use sink: compositor", id, output)
		}
	}
	return nil
}

func parseDRMTopology(report string) (drmTopology, error) {
	t := drmTopology{encoders: map[int]drmEncoder{}, connectors: map[string]drmConnector{}, planes: map[int]uint64{}}
	section := ""
	for _, line := range strings.Split(report, "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "Encoders:", "Connectors:", "CRTCs:", "Planes:":
			section = strings.TrimSuffix(line, ":")
			continue
		case "Frame buffers:":
			section = ""
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil || id <= 0 {
			continue
		}
		switch section {
		case "Encoders":
			if len(fields) < 5 {
				continue
			}
			crtc, e1 := strconv.Atoi(fields[1])
			mask, e2 := strconv.ParseUint(fields[3], 0, 64)
			if e1 == nil && e2 == nil {
				t.encoders[id] = drmEncoder{crtc: crtc, mask: mask}
			}
		case "Connectors":
			if len(fields) < 7 || (fields[2] != "connected" && fields[2] != "disconnected") {
				continue
			}
			encoder, e := strconv.Atoi(fields[1])
			if e != nil {
				continue
			}
			entry := drmConnector{status: fields[2], encoder: encoder}
			for _, value := range fields[6:] {
				for _, part := range strings.Split(value, ",") {
					if n, e := strconv.Atoi(part); e == nil {
						entry.encoders = append(entry.encoders, n)
					}
				}
			}
			t.connectors[fields[3]] = entry
		case "CRTCs":
			if len(fields) >= 4 && strings.HasPrefix(fields[2], "(") {
				t.crtcs = append(t.crtcs, id)
			}
		case "Planes":
			if len(fields) < 7 || !strings.Contains(fields[3], ",") {
				continue
			}
			if mask, e := strconv.ParseUint(fields[len(fields)-1], 0, 64); e == nil {
				t.planes[id] = mask
			}
		}
	}
	if len(t.crtcs) == 0 || len(t.connectors) == 0 || len(t.planes) == 0 {
		return t, fmt.Errorf("could not read connectors, CRTCs, and planes from modetest output")
	}
	return t, nil
}
