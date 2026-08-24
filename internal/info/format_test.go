package info

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lazybrew/internal/brew"
)

// Real `brew info --formula ast-grep` output.
const formulaRaw = `==> ast-grep: stable 0.45.1 (bottled), HEAD
Code searching, linting, rewriting
https://ast-grep.github.io/
Installed (on request)
From: https://github.com/Homebrew/homebrew-core/blob/HEAD/Formula/a/ast-grep.rb
License: MIT
==> Installed Versions
ast-grep 0.45.1 (14 files, 52.8MB) [Linked]
==> Options
--HEAD
	Install HEAD version
==> Analytics
install: 10,871 (30 days), 30,553 (90 days), 71,994 (365 days)
install-on-request: 10,841 (30 days), 30,484 (90 days), 71,779 (365 days)
build-error: 0 (30 days)`

// Real `brew info --cask firefox` output, trimmed after the Languages dump.
const caskRaw = `==> firefox (Mozilla Firefox): 154.0 (auto_updates)
Web browser
https://www.mozilla.org/firefox/
Installed (on request)
/opt/homebrew/Caskroom/firefox/153.0.4 (511.7MB)
  Installed using the internal formulae.brew.sh API on 2026-08-12 at 12:38:13
From: https://github.com/Homebrew/homebrew-cask/blob/HEAD/Casks/f/firefox.rb
==> Requirements
Required: macOS
==> Artifacts
Firefox.app (App)`

// Real `brew info --cask postman` output, trimmed after the Artifacts stanza.
// postman is the one cask `brew outdated --cask` reports on this machine.
const outdatedCaskRaw = `==> postman (Postman): 12.24.5 (auto_updates)
Collaboration platform for API development
https://www.postman.com/
Installed (on request)
/opt/homebrew/Caskroom/postman/12.24.4 (383MB)
  Installed using the internal formulae.brew.sh API on 2026-08-21 at 09:14:56
From: https://github.com/Homebrew/homebrew-cask/blob/HEAD/Casks/p/postman.rb
==> Requirements
Required: macOS >= 11
==> Artifacts
Postman.app (App)`

func TestFormatSaysOutdatedOnlyOnHomebrewsVerdict(t *testing.T) {
	tests := []struct {
		name     string
		pkg      brew.Package
		raw      string
		want     string
		unwanted string
	}{
		{
			name: "reported and a newer version parses",
			pkg:  brew.Package{Name: "postman", Kind: brew.Cask, Outdated: true},
			raw:  outdatedCaskRaw,
			want: "Version    12.24.4  (outdated, latest 12.24.5)",
		},
		{
			name:     "the same output unreported draws no conclusion",
			pkg:      brew.Package{Name: "postman", Kind: brew.Cask},
			raw:      outdatedCaskRaw,
			want:     "Version    12.24.4  (latest 12.24.5)",
			unwanted: "outdated",
		},
		{
			// A revision bump: Homebrew flags the package while both parsed
			// versions match, so the row must not read as up to date.
			name: "reported with no distinct newer version",
			pkg:  brew.Package{Name: "ast-grep", Kind: brew.Formula, Outdated: true},
			raw:  formulaRaw,
			want: "Version    0.45.1  (outdated)",
		},
		{
			// The latest carried in from `brew outdated` outranks the version
			// parsed from the info text: it is the same source as the verdict.
			name: "reported with the verdict's own latest version",
			pkg:  brew.Package{Name: "postman", Kind: brew.Cask, Outdated: true, LatestVersion: "12.24.6"},
			raw:  outdatedCaskRaw,
			want: "Version    12.24.4  (outdated, latest 12.24.6)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.pkg, tt.raw, Dependents{})
			if !strings.Contains(got, tt.want) {
				t.Fatalf("missing %q in:\n%s", tt.want, got)
			}
			if tt.unwanted != "" && strings.Contains(got, tt.unwanted) {
				t.Fatalf("contains %q in:\n%s", tt.unwanted, got)
			}
			if tt.pkg.Outdated && strings.Contains(got, "(up to date)") {
				t.Fatalf("claimed up to date for a package brew reports outdated:\n%s", got)
			}
		})
	}
}

func TestFormatMarksAPinnedFormula(t *testing.T) {
	pkg := brew.Package{Name: "ast-grep", Kind: brew.Formula, Pinned: true, Outdated: true, LatestVersion: "0.46.0"}
	got := Format(pkg, formulaRaw, Dependents{})
	if !strings.Contains(got, "Version    0.45.1  (pinned, outdated, latest 0.46.0)") {
		t.Fatalf("pinned version missing from:\n%s", got)
	}
}

func TestFormatFormulaKeepsDecisionFieldsAndDropsNoise(t *testing.T) {
	// OutdatedKnown: Homebrew was asked and reported this package as current, which
	// is what entitles the panel to say so.
	pkg := brew.Package{Name: "ast-grep", Kind: brew.Formula, OutdatedKnown: true}
	got := Format(pkg, formulaRaw, Dependents{Known: true})

	for _, want := range []string{
		"Code searching, linting, rewriting",
		"Version    0.45.1  (up to date)",
		"Size       52.8 MB, 14 files",
		"License    MIT",
		"Home       https://ast-grep.github.io/",
		"Needed by  nothing installed",
		"Safe to remove.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Analytics", "install-on-request", "From:", "--HEAD", "==>", "build-error"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("kept noise %q in:\n%s", unwanted, got)
		}
	}
	// Installed-on-request is a constant for every formula the list shows, so it
	// must not occupy a row.
	if strings.Contains(got, "on request") {
		t.Fatalf("rendered the constant on-request row:\n%s", got)
	}
}

func TestFormatCaskNamesTheNewerVersionWithoutCallingItOutdated(t *testing.T) {
	pkg := brew.Package{Name: "firefox", Kind: brew.Cask}
	got := Format(pkg, caskRaw, Dependents{})

	for _, want := range []string{
		"Web browser",
		"Version    153.0.4  (latest 154.0)",
		"Size       511.7 MB",
		"Home       https://www.mozilla.org/firefox/",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	// A cask has no dependent lookup, so it must claim neither dependents nor
	// safety, and Homebrew's own noise still goes.
	for _, unwanted := range []string{"Needed by", "Safe to remove", "outdated", "Requirements", "From:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("cask panel contains %q in:\n%s", unwanted, got)
		}
	}
}

func TestFormatReportsDependentsAndWithholdsSafetyWhenUnknown(t *testing.T) {
	pkg := brew.Package{Name: "gmp", Kind: brew.Formula}

	blocked := Format(pkg, formulaRaw, Dependents{Names: []string{"coreutils", "gcc"}, Known: true})
	if !strings.Contains(blocked, "Needed by  coreutils, gcc") {
		t.Fatalf("dependents row missing:\n%s", blocked)
	}
	if !strings.Contains(blocked, "Removing this breaks 2 installed formulae.") {
		t.Fatalf("expected a breakage verdict:\n%s", blocked)
	}
	if strings.Contains(blocked, "Safe to remove") {
		t.Fatalf("claimed safety despite dependents:\n%s", blocked)
	}

	many := Format(pkg, formulaRaw, Dependents{Names: []string{"a", "b", "c", "d", "e"}, Known: true})
	if !strings.Contains(many, "Needed by  a, b, c and 2 more") {
		t.Fatalf("long dependent list not summarised:\n%s", many)
	}

	// A failed lookup must read as absence of evidence, never as safety.
	unknown := Format(pkg, formulaRaw, Dependents{})
	if strings.Contains(unknown, "Safe to remove") || strings.Contains(unknown, "Needed by") {
		t.Fatalf("unknown dependents produced a claim:\n%s", unknown)
	}
}

func TestFormatFallsBackToRawWhenOutputIsUnrecognisable(t *testing.T) {
	pkg := brew.Package{Name: "mystery", Kind: brew.Formula}
	raw := "Error: No available formula with the name \"mystery\"."
	if got := Format(pkg, raw, Dependents{}); got != raw {
		t.Fatalf("fallback=%q, want the raw text %q", got, raw)
	}
}

func TestDetailsSkipsDependentsForCasksAndAbsorbsTheirFailure(t *testing.T) {
	usesCalls := 0
	load := Details(
		func(context.Context, brew.Package) (string, error) { return formulaRaw, nil },
		func(_ context.Context, pkg brew.Package) ([]string, error) {
			usesCalls++
			return nil, errors.New("brew uses exploded")
		},
	)

	cask, err := load(context.Background(), brew.Package{Name: "firefox", Kind: brew.Cask})
	if err != nil {
		t.Fatalf("cask load error: %v", err)
	}
	if usesCalls != 0 {
		t.Fatalf("ran the dependent lookup for a cask %d times, want 0", usesCalls)
	}
	if strings.Contains(cask, "Needed by") {
		t.Fatalf("cask panel claimed dependents:\n%s", cask)
	}

	formula, err := load(context.Background(), brew.Package{Name: "ast-grep", Kind: brew.Formula})
	if err != nil {
		t.Fatalf("a failed dependent lookup must not fail the load: %v", err)
	}
	if usesCalls != 1 {
		t.Fatalf("formula dependent lookups=%d, want 1", usesCalls)
	}
	if !strings.Contains(formula, "Size") {
		t.Fatalf("panel lost its details when dependents failed:\n%s", formula)
	}
	if strings.Contains(formula, "Safe to remove") {
		t.Fatalf("claimed safety after a failed dependent lookup:\n%s", formula)
	}
}

func TestDetailsPropagatesInfoFailure(t *testing.T) {
	load := Details(
		func(context.Context, brew.Package) (string, error) { return "", errors.New("boom") },
		func(context.Context, brew.Package) ([]string, error) { return nil, nil },
	)
	if _, err := load(context.Background(), brew.Package{Name: "x", Kind: brew.Formula}); err == nil {
		t.Fatal("expected the info failure to propagate")
	}
}

// A failed `brew outdated` read must not become a freshness assurance. This is
// the same restraint the dependents row already follows: absence of evidence is
// reported as absence, not as good news.
func TestFormatWithholdsFreshnessWhenTheOutdatedReadFailed(t *testing.T) {
	base := brew.Package{Name: "ast-grep", Kind: brew.Formula}

	asked := base
	asked.OutdatedKnown = true
	if got := Format(asked, formulaRaw, Dependents{Known: true}); !strings.Contains(got, "0.45.1  (up to date)") {
		t.Fatalf("a successful read should say so:\n%s", got)
	}

	// Same text, no verdict obtained.
	unknown := Format(base, formulaRaw, Dependents{Known: true})
	if strings.Contains(unknown, "up to date") {
		t.Fatalf("claimed freshness with no verdict obtained:\n%s", unknown)
	}
	if !strings.Contains(unknown, "Version    0.45.1") {
		t.Fatalf("dropped the installed version along with the verdict:\n%s", unknown)
	}

	// A newer version parsed from Homebrew's own text is independent evidence and
	// still stands without an outdated verdict.
	behind := brew.Package{Name: "firefox", Kind: brew.Cask}
	if got := Format(behind, caskRaw, Dependents{}); !strings.Contains(got, "153.0.4  (latest 154.0)") {
		t.Fatalf("text evidence of a newer version should survive:\n%s", got)
	}
}
