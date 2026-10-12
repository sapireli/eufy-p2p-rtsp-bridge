package rendition

import "testing"

func ptr(n int) *int { return &n }
func TestExplicitHEVCProfileLevelTierTuple(t *testing.T) {
	n := Native{Profile: ptr(1), Level: ptr(150), Tier: ptr(1)}
	for _, tc := range []struct {
		name, caps string
		want       bool
	}{
		{"exact", "video/x-h265\n profile: main\n level: 5\n tier: high", true},
		{"explicit lists", "video/x-h265\n profile: { main, main-10 }\n level: { 5, 5.1 }\n tier: { main, high }", true},
		{"no level", "video/x-h265\n profile: main\n tier: high", false},
		{"no tier", "video/x-h265\n profile: main\n level: 5", false},
		{"higher main tier", "video/x-h265\n profile: main\n level: 6.2\n tier: main", false},
		{"main tier only", "video/x-h265\n profile: main\n level: 5\n tier: main", false},
		{"fields from different structures", "video/x-h265\n profile: main\n level: 5\nvideo/x-h265\n profile: main-10\n tier: high", false},
		{"another codec fields", "video/x-h265\n profile: main\nvideo/x-h264\n level: 5\n tier: high", false},
		{"range isn't measured tuple", "video/x-h265\n profile: main\n level: [ 1, 6.2 ]\n tier: high", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ExplicitSinkSupport(n, tc.caps) != tc.want {
				t.Fatal("incorrect tuple qualification")
			}
		})
	}
	n.Tier = nil
	if ExplicitSinkSupport(n, "video/x-h265\n profile: main\n level: 5\n tier: main") {
		t.Fatal("missing tier interpreted as zero")
	}
	n.Tier = ptr(0)
	if !ExplicitSinkSupport(n, "video/x-h265\n profile: main\n level: 5\n tier: main") {
		t.Fatal("explicit main tier rejected")
	}
}
