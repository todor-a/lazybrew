package ui

import (
	"testing"

	"lazybrew/internal/brew"
)

func TestParseQuery(t *testing.T) {
	outdated := brew.Package{Name: "fzf", Outdated: true}
	fresh := brew.Package{Name: "fzf"}
	untrusted := brew.Package{Name: "acli", Untrusted: true}

	cases := []struct {
		name     string
		query    string
		text     string
		showDeps bool
		keep     []brew.Package
		drop     []brew.Package
	}{
		{name: "empty", query: ""},
		{name: "plain text", query: "docker compose", text: "docker compose"},
		{name: "outdated", query: "is:outdated", keep: []brew.Package{outdated}, drop: []brew.Package{fresh}},
		{name: "case-insensitive qualifier", query: "IS:Outdated", keep: []brew.Package{outdated}, drop: []brew.Package{fresh}},
		{name: "untrusted", query: "is:untrusted", keep: []brew.Package{untrusted}, drop: []brew.Package{fresh}},
		{name: "composed narrows to both", query: "is:outdated is:untrusted", drop: []brew.Package{outdated, untrusted}},
		{name: "qualifier plus text", query: "is:outdated fzf", text: "fzf", keep: []brew.Package{outdated}},
		// The fail-open rule: a typo'd qualifier is substring text, never a
		// filter that silently matches nothing.
		{name: "unknown qualifier stays text", query: "is:outdatd", text: "is:outdatd", keep: []brew.Package{fresh}},
		{name: "dep lifts the hide", query: "is:dep", showDeps: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseQuery(tc.query)
			if parsed.text != tc.text {
				t.Fatalf("text=%q, want %q", parsed.text, tc.text)
			}
			if parsed.showDeps != tc.showDeps {
				t.Fatalf("showDeps=%v, want %v", parsed.showDeps, tc.showDeps)
			}
			for _, pkg := range tc.keep {
				if !parsed.match(pkg) {
					t.Fatalf("%s dropped by %q", pkg.Name, tc.query)
				}
			}
			for _, pkg := range tc.drop {
				if parsed.match(pkg) {
					t.Fatalf("%s kept by %q", pkg.Name, tc.query)
				}
			}
		})
	}
}

func TestToggleQualifierRoundTrip(t *testing.T) {
	query, on := toggleQualifier("docker", "is:outdated")
	if query != "is:outdated docker" || !on {
		t.Fatalf("added: query=%q on=%v", query, on)
	}
	query, on = toggleQualifier(query, "is:outdated")
	if query != "docker" || on {
		t.Fatalf("removed: query=%q on=%v", query, on)
	}
	if query, on = toggleQualifier("", "is:dep"); query != "is:dep" || !on {
		t.Fatalf("empty add: query=%q on=%v", query, on)
	}
	// A hand-typed spelling toggles off the same as a key-written one.
	if query, _ = toggleQualifier("IS:DEP fzf", "is:dep"); query != "fzf" {
		t.Fatalf("case-folded removal: query=%q", query)
	}
}
