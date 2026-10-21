package supervisor

import (
	"regexp"
	"strings"
)

// Only a fatal bus message owned by a hardware decoder can select software.
// Generic not-negotiated messages can originate in any part of the pipeline;
// corrupted media and watchdog/source errors are not decoder initialization failures.
var hardwareErrorOwner = regexp.MustCompile(`(?i)/Gst(?:V4l2(?:SL)?|Va)H26[45]Dec:[^/:]+$`)

func hardwareInitializationError(line string) bool {
	if hardwareResourceError(line) {
		return true
	}
	if !strings.HasPrefix(line, "ERROR: from element ") {
		return false
	}
	owner, message, ok := strings.Cut(strings.TrimPrefix(line, "ERROR: from element "), ": ")
	if !ok || !hardwareErrorOwner.MatchString(owner) {
		return false
	}
	for _, known := range []string{
		"Failed to allocate required memory.",
		"Failed to start decoding thread.",
		"Failed to start decoder",
		"Could not initialize supporting library.",
		"Could not open device '",
		"Cannot identify device '",
	} {
		if strings.Contains(message, known) {
			return true
		}
	}
	return strings.Contains(message, "Decoder on device ") && strings.Contains(message, " has no supported input format")
}

// Reclaim hardware only when the decoder's fatal error identifies exhausted or
// busy resources. Unsupported formats and generic initialization failures do
// not establish that displacing a working decoder could help.
func hardwareResourceError(line string) bool {
	if !strings.HasPrefix(line, "ERROR: from element ") {
		return false
	}
	owner, message, ok := strings.Cut(strings.TrimPrefix(line, "ERROR: from element "), ": ")
	if !ok || !hardwareErrorOwner.MatchString(owner) {
		return false
	}
	message = strings.ToLower(message)
	for _, known := range []string{"failed to allocate required memory", "device or resource busy", "resource exhausted", "resource exhaustion", "no space left on device", "too many open", "insufficient resources"} {
		if strings.Contains(message, known) {
			return true
		}
	}
	return false
}
