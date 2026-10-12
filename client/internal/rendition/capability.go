package rendition

import (
	"regexp"
	"strings"
)

var word = regexp.MustCompile(`[a-zA-Z0-9][a-zA-Z0-9.-]*`)

// ExplicitSinkSupport requires all three advertised SPS fields and exact values
// in ONE hardware decoder sink capability structure. Missing/generic fields and
// merely higher nominal levels are not evidence of support for this tuple.
func ExplicitSinkSupport(n Native, caps string) bool {
	if n.Profile == nil || n.Level == nil || n.Tier == nil {
		return false
	}
	profile := map[int]string{1: "main", 2: "main-10", 3: "main-still-picture"}[*n.Profile]
	level := map[int]string{30: "1", 60: "2", 63: "2.1", 90: "3", 93: "3.1", 120: "4", 123: "4.1", 150: "5", 153: "5.1", 156: "5.2", 180: "6", 183: "6.1", 186: "6.2"}[*n.Level]
	tier := map[int]string{0: "main", 1: "high"}[*n.Tier]
	if profile == "" || level == "" || tier == "" {
		return false
	}
	blocks := strings.Split(caps, "video/x-h265")
	for _, block := range blocks[1:] {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "video/") || strings.HasPrefix(trim, "audio/") {
				break
			}
			key, value, ok := strings.Cut(trim, ":")
			if ok && (key == "profile" || key == "level" || key == "tier") {
				fields[key] = value
			}
		}
		matches := func(field, value string) bool {
			if strings.ContainsAny(fields[field], "[]") {
				return false
			}
			for _, v := range word.FindAllString(fields[field], -1) {
				if v == value {
					return true
				}
			}
			return false
		}
		if matches("profile", profile) && matches("level", level) && matches("tier", tier) {
			return true
		}
	}
	return false
}
