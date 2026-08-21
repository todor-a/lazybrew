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

func TestFormatFormulaKeepsDecisionFieldsAndDropsNoise(t *testing.T) {
	pkg := brew.Package{Name: "ast-grep", Kind: brew.Formula}
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
