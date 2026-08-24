package brew

import "testing"

func TestVersionDistance(t *testing.T) {
	cases := []struct {
		name              string
		installed, latest string
		want              Distance
	}{
		{"major", "1.0.1", "2.0.0", DistanceMajor},
		{"minor", "1.0.1", "1.2.0", DistanceMinor},
		{"patch", "3.0.11", "3.0.13", DistancePatch},
		{"deep patch", "2026.07.27.00", "2026.07.27.01", DistancePatch},
		{"revision only is patch", "1.18.1", "1.18.1_1", DistancePatch},
		{"revision bump is patch", "1.18.1_1", "1.18.1_2", DistancePatch},
		{"equal", "1.18.1_1", "1.18.1_1", DistanceNone},
		{"missing trailing segment reads as zero", "1.2", "1.2.0", DistanceNone},
		{"date versions differ on the year, so major", "20260426", "20270101", DistanceMajor},
		{"date dotted minor", "2026.8.2", "2026.9.0", DistanceMinor},
		// Fail open: anything unreadable on either side must never be
		// classified below a threshold.
		{"suffixed installed fails open", "1.3.19-stable", "1.4.0", DistanceUnknown},
		{"suffixed latest fails open", "1.4.0", "1.4.1-beta", DistanceUnknown},
		{"empty installed fails open", "", "1.0.0", DistanceUnknown},
		{"empty latest fails open", "1.0.0", "", DistanceUnknown},
		{"bad revision fails open", "1.0.0_x", "1.0.1", DistanceUnknown},
	}
	for _, tc := range cases {
		if got := VersionDistance(tc.installed, tc.latest); got != tc.want {
			t.Errorf("%s: VersionDistance(%q, %q)=%d, want %d", tc.name, tc.installed, tc.latest, got, tc.want)
		}
	}
}

func TestBumpOffset(t *testing.T) {
	cases := []struct {
		name              string
		installed, latest string
		want              int
	}{
		{"major bolds everything", "1.0.1", "2.0.0", 0},
		{"minor bolds from the second segment", "1.0.1", "1.2.0", 2},
		{"patch bolds the tail", "3.0.11", "3.0.13", 4},
		{"leading zeros keep their true positions", "2026.07.27.00", "2026.07.28.00", 8},
		{"revision-only bolds from the underscore", "1.18.1", "1.18.1_1", 6},
		{"equal has no highlight", "1.2.3", "1.2.3", -1},
		{"unreadable pair has no highlight", "1.3.19-stable", "1.4.0", -1},
		{"difference missing from a shorter latest has no highlight", "1.2.3", "1.2", -1},
	}
	for _, tc := range cases {
		if got := BumpOffset(tc.installed, tc.latest); got != tc.want {
			t.Errorf("%s: BumpOffset(%q, %q)=%d, want %d", tc.name, tc.installed, tc.latest, got, tc.want)
		}
	}
}
