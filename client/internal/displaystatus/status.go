// Package displaystatus keeps bounded actionable captions, never raw private stream URLs.
package displaystatus

import (
	"strconv"
	"strings"
	"sync"
)

const (
	DNS        = "DNS FAILED - CHECK BRIDGE ADDRESS"
	Refused    = "BRIDGE UNREACHABLE - CHECK SERVER"
	Discovery  = "DISCOVERY FAILED - CHECK BRIDGE AND NETWORK"
	Stream     = "STREAM FAILED - CHECK CAMERA"
	Missing    = "STREAM NOT FOUND - CHECK CAMERA"
	Connecting = "CONNECTING - RETRYING STREAM"
)

func Classify(message string) string {
	m := strings.ToLower(message)
	switch {
	case strings.Contains(m, "identity changed"):
		return "BRIDGE ID CHANGED - CHECK ADDRESS"
	case strings.Contains(m, "bridge identity"):
		return "BRIDGE ID CHECK FAILED - CHECK SETUP"
	case m == "auth: starting":
		return "BRIDGE STARTING - PLEASE WAIT"
	case m == "auth: ready":
		return ""
	case strings.Contains(m, "auth: require_2fa"):
		return "LOGIN CODE NEEDED - OPEN BRIDGE SETUP"
	case strings.Contains(m, "auth: require_captcha"):
		return "LOGIN CAPTCHA - OPEN BRIDGE SETUP"
	case strings.Contains(m, "auth: reauth") || strings.Contains(m, "auth: error"):
		return "BRIDGE LOGIN FAILED - OPEN SETUP"
	case strings.HasPrefix(m, "auth:"):
		return "BRIDGE LOGIN PENDING - OPEN SETUP"
	case strings.Contains(m, "multiple bridge"):
		return "MULTIPLE BRIDGES - SET BRIDGE ADDRESS"
	case strings.Contains(m, "resolv") || strings.Contains(m, "no such host") || strings.Contains(m, "lookup "):
		return DNS
	case strings.Contains(m, "refused") || strings.Contains(m, "unreachable") || strings.Contains(m, "timed out") || strings.Contains(m, "timeout"):
		return Refused
	case strings.Contains(m, "avahi") && strings.Contains(m, "not found"):
		return "DISCOVERY UNAVAILABLE - INSTALL AVAHI"
	case strings.Contains(m, "discovery") || strings.Contains(m, "avahi") || strings.Contains(m, "no resolved bridge"):
		return Discovery
	case strings.Contains(m, "404") || strings.Contains(m, "not found"):
		return Missing
	default:
		return Stream
	}
}

type Store struct {
	mu       sync.Mutex
	bridge   string
	renderer string
	cameras  map[string]string
	slots    map[int]string
}

func New() *Store { return &Store{cameras: map[string]string{}, slots: map[int]string{}} }
func (s *Store) Bridge(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == "" {
		s.bridge = ""
	} else {
		s.bridge = Classify(err)
	}
}
func (s *Store) Observe(camera, line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(line, "eufytile tile") {
		parts := strings.Fields(line)
		if len(parts) > 1 {
			index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(parts[1], "tile"), ":"))
			if err == nil {
				camera = s.slots[index]
			}
		}
	}
	switch {
	case strings.HasPrefix(line, "eufy-output ") || strings.Contains(line, ": output-ready"):
		delete(s.cameras, camera)
		if camera != "wall" {
			s.renderer = ""
		}
	case strings.Contains(line, "ERROR:") || strings.Contains(line, "error:") || strings.HasPrefix(line, "start failed:") || strings.Contains(line, "decoder stopped") || strings.Contains(line, ": invalid decoder pipeline:") || strings.Contains(line, ": pipeline requires appsink") || strings.Contains(line, ": invalid configuration:"):
		if camera == "wall" {
			s.renderer = "DISPLAY FAILED - CHECK VIDEO OUTPUT"
		} else {
			s.cameras[camera] = Classify(line)
		}
	}
}
func (s *Store) Message(camera string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bridge != "" {
		return s.bridge
	}
	return s.cameras[camera]
}

func (s *Store) Select(index int, camera string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slots[index] = camera
}
func (s *Store) Captions() map[int]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int]string{}
	for i, camera := range s.slots {
		if s.bridge != "" {
			out[i] = s.bridge
		} else if s.renderer != "" {
			out[i] = s.renderer
		} else {
			out[i] = s.cameras[camera]
		}
	}
	return out
}
