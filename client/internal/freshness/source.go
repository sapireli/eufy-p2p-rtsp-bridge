package freshness

import (
	"net/url"
	"strings"
)

// BoundTo compares the advertised media resource, not a serial or resolved IP.
// pkt_size is the documented client-only RTSP/TCP transport-size override.
// Every other query value, host/port, escaped path, credential and fragment
// remains part of the identity; hostname aliases are never resolved here.
func (c Capability) BoundTo(source string) bool {
	if !c.Supported() {
		return false
	}
	a, ok := sourceIdentity(c.StreamURL)
	if !ok {
		return false
	}
	b, ok := sourceIdentity(source)
	return ok && a == b
}
func sourceIdentity(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "rtsp") || u.Host == "" {
		return "", false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", false
	}
	q.Del("pkt_size")
	user := ""
	if u.User != nil {
		user = "present:" + u.User.String()
	}
	return strings.Join([]string{strings.ToLower(u.Scheme), strings.ToLower(u.Host), user, u.EscapedPath(), q.Encode(), u.Fragment}, "\x00"), true
}
