package freshness

import "testing"

func TestSourceBindingIgnoresOnlyPacketSizeOverride(t *testing.T) {
	c := Capability{Version: 1, Scope: "bridge-ingress", MaxAgeMs: 500, ClockURL: "http://bridge/api/clock", StreamURL: "rtsp://bridge:8554/front?quality=high&transport=tcp"}
	for _, v := range []struct {
		url     string
		matches bool
	}{
		{"rtsp://bridge:8554/front?quality=high&transport=tcp", true},
		{"RTSP://BRIDGE:8554/front?transport=tcp&pkt_size=8192&quality=high", true},
		{"rtsp://other:8554/front?quality=high&transport=tcp", false},
		{"rtsp://192.0.2.1:8554/front?quality=high&transport=tcp", false},
		{"rtsp://bridge:8555/front?quality=high&transport=tcp", false},
		{"rtsp://bridge:8554/garage?quality=high&transport=tcp", false},
		{"rtsp://bridge:8554/front?quality=low&transport=tcp", false},
		{"rtsp://bridge:8554/front?quality=high&transport=tcp&another=value", false},
		{"rtsp://user@bridge:8554/front?quality=high&transport=tcp", false},
		{"rtsp://@bridge:8554/front?quality=high&transport=tcp", false},
	} {
		if got := c.BoundTo(v.url); got != v.matches {
			t.Errorf("BoundTo(%s)=%v want%v", v.url, got, v.matches)
		}
	}
	c.StreamURL = ""
	if c.BoundTo("rtsp://bridge:8554/front") {
		t.Fatal("inferred missing advertised URL")
	}
}
