package info

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"lazybrew/internal/brew"
)

// Homebrew's own `brew info` output is written for a terminal reader scanning a
// package they are about to install: provenance, build options, and download
// analytics. This pane serves someone deciding whether to remove a package they
// already have, so it keeps the fields that answer that question and drops the
// rest.
//
// The fields are parsed out of the same `brew info` text the pane used to print
// verbatim, rather than from `brew info --json=v2`, for one reason: the JSON
// carries no installed size, and size is the field this screen exists for. Any
// line that cannot be parsed simply does not appear, and if the output is
// unrecognisable enough that neither a description nor a size is found, Format
// returns the raw text unchanged. A Homebrew output change therefore degrades to
// the old behaviour rather than to a blank pane.
//
// The one field not parsed from that text is the outdated verdict: it arrives on
// the package value from `brew outdated`, so exactly one component decides the
// word and the pane cannot form a second opinion from a version mismatch.

// sizeInParentheses matches Homebrew's parenthesised size groups: a formula's
// "(14 files, 52.8MB)" and a cask's "(511.7MB)".
var sizeInParentheses = regexp.MustCompile(`\(([^()]*?[\d.]+\s?[KMGT]?B)\)`)

// caskroomVersion matches the installed-version segment of a cask's Caskroom
// path line, "/opt/homebrew/Caskroom/firefox/153.0.4 (511.7MB)".
var caskroomVersion = regexp.MustCompile(`/Caskroom/[^/]+/([^/\s]+)`)

// Dependents carries the result of a `brew uses --installed` lookup. Known
// distinguishes "we asked and nothing depends on this" from "we never got an
// answer", so a failed lookup can never render as a safe-to-remove verdict.
type Dependents struct {
	Names []string
	Known bool
}

// Format renders the info pane body for pkg from Homebrew's raw `brew info`
// text and, for formulae, its dependents.
func Format(pkg brew.Package, raw string, dependents Dependents) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	description, descriptionRow := describe(lines)
	size := findSize(lines, descriptionRow)
	if description == "" && size == "" {
		return strings.TrimRight(raw, "\n")
	}

	var out []string
	if description != "" {
		out = append(out, description, "")
	}

	rows := [][2]string{}
	if version := versionRow(pkg, lines); version != "" {
		rows = append(rows, [2]string{"Version", version})
	}
	if size != "" {
		rows = append(rows, [2]string{"Size", size})
	}
	// Only surfaced when it is not the default. Every formula the list shows was
	// installed on request by construction, so an "on request" row would be a
	// constant; a package pulled in as a dependency is the case worth flagging.
	if !strings.Contains(raw, "(on request)") && strings.Contains(raw, "Installed") {
		rows = append(rows, [2]string{"Installed", "as a dependency"})
	}
	if license := fieldAfter(lines, "License:"); license != "" {
		rows = append(rows, [2]string{"License", license})
	}
	if home := findHomepage(lines); home != "" {
		rows = append(rows, [2]string{"Home", home})
	}
	if row := dependentsRow(pkg, dependents); row != "" {
		rows = append(rows, [2]string{"Needed by", row})
	}
	out = append(out, alignRows(rows)...)

	if verdict := verdict(pkg, dependents); verdict != "" {
		out = append(out, "", verdict)
	}
	return strings.Join(out, "\n")
}

// describe returns the one-line summary Homebrew prints directly beneath its
// first "==>" heading, and the row it was found on.
func describe(lines []string) (string, int) {
	for i, line := range lines {
		if !strings.HasPrefix(line, "==>") || i+1 >= len(lines) {
			continue
		}
		candidate := strings.TrimSpace(lines[i+1])
		if candidate == "" || strings.HasPrefix(candidate, "==>") || strings.HasPrefix(candidate, "http") {
			return "", -1
		}
		return candidate, i + 1
	}
	return "", -1
}

// findSize reads the parenthesised size group. Only the description row is
// skipped, so a package described with a size in parentheses cannot be mistaken
// for one; every other row stays eligible, including a formula's size line
// directly beneath its "==> Installed Versions" heading.
func findSize(lines []string, descriptionRow int) string {
	for i, line := range lines {
		if i == descriptionRow {
			continue
		}
		if match := sizeInParentheses.FindStringSubmatch(line); match != nil {
			return humanizeSize(strings.TrimSpace(match[1]))
		}
	}
	return ""
}

// humanizeSize turns Homebrew's "14 files, 52.8MB" into "52.8 MB, 14 files",
// leading with the number the reader came for.
func humanizeSize(value string) string {
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 2 && strings.HasSuffix(parts[0], "files") {
		return spaceBeforeUnit(parts[1]) + ", " + parts[0]
	}
	return spaceBeforeUnit(value)
}

var sizeUnit = regexp.MustCompile(`^([\d.]+)\s*([KMGT]?B)$`)

func spaceBeforeUnit(value string) string {
	if match := sizeUnit.FindStringSubmatch(value); match != nil {
		return match[1] + " " + match[2]
	}
	return value
}

// versionRow reports the installed version, and names the newer available one
// when they differ.
//
// The word "outdated" comes from pkg.Outdated — Homebrew's own `brew outdated`
// verdict, carried in on the package value — and never from a version mismatch:
// an auto-updating cask legitimately sits behind Homebrew's version without
// being out of date. A mismatch alone therefore still renders as `(latest X)`
// with no conclusion drawn, and `(up to date)` can never render for a package
// Homebrew reports as outdated. The bare `(outdated)` fallback covers a revision
// bump, where Homebrew flags the package but the two parsed versions match.
func versionRow(pkg brew.Package, lines []string) string {
	installed := installedVersion(pkg, lines)
	if installed == "" {
		return ""
	}
	// The offered version prefers the `brew outdated` value carried on the
	// package — the same source as the Outdated verdict itself — and falls back
	// to the version parsed from the info text, which is the only source for a
	// package Homebrew does not report as outdated.
	latest := pkg.LatestVersion
	if latest == "" {
		latest = latestVersion(lines)
	}
	newer := latest != "" && latest != installed
	var state []string
	if pkg.Pinned {
		state = append(state, "pinned")
	}
	if pkg.Outdated {
		state = append(state, "outdated")
	}
	if newer {
		state = append(state, "latest "+latest)
	}
	if len(state) == 0 && !pkg.OutdatedKnown {
		// No verdict was obtained and the text shows nothing newer, so nothing is
		// known about freshness. Say nothing rather than assure, the same way a
		// failed dependent lookup withholds its row instead of claiming safety.
		return installed
	}
	if len(state) == 0 {
		state = append(state, "up to date")
	}
	return installed + "  (" + strings.Join(state, ", ") + ")"
}

func installedVersion(pkg brew.Package, lines []string) string {
	if pkg.Kind == brew.Cask {
		for _, line := range lines {
			if match := caskroomVersion.FindStringSubmatch(line); match != nil {
				return match[1]
			}
		}
		return ""
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "==> Installed Versions") || i+1 >= len(lines) {
			continue
		}
		// "ast-grep 0.45.1 (14 files, 52.8MB) [Linked]"
		if fields := strings.Fields(lines[i+1]); len(fields) >= 2 {
			return fields[1]
		}
	}
	return ""
}

// latestVersion reads the version Homebrew offers from its heading line:
// "==> ast-grep: stable 0.45.1 (bottled), HEAD" or
// "==> firefox (Mozilla Firefox): 154.0 (auto_updates)".
func latestVersion(lines []string) string {
	for _, line := range lines {
		if !strings.HasPrefix(line, "==>") {
			continue
		}
		colon := strings.LastIndex(line, ": ")
		if colon < 0 {
			return ""
		}
		fields := strings.Fields(line[colon+2:])
		if len(fields) == 0 {
			return ""
		}
		version := fields[0]
		if version == "stable" && len(fields) > 1 {
			version = fields[1]
		}
		return strings.TrimSuffix(version, ",")
	}
	return ""
}

func findHomepage(lines []string) string {
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "http") {
			return trimmed
		}
	}
	return ""
}

func fieldAfter(lines []string, prefix string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func dependentsRow(pkg brew.Package, dependents Dependents) string {
	if pkg.Kind != brew.Formula || !dependents.Known {
		return ""
	}
	if len(dependents.Names) == 0 {
		return "nothing installed"
	}
	if len(dependents.Names) <= 3 {
		return strings.Join(dependents.Names, ", ")
	}
	return strings.Join(dependents.Names[:3], ", ") +
		" and " + strconv.Itoa(len(dependents.Names)-3) + " more"
}

// verdict states the removal consequence only where it was actually established.
// A cask, or a formula whose dependent lookup failed, gets no verdict at all
// rather than an unearned reassurance.
func verdict(pkg brew.Package, dependents Dependents) string {
	if pkg.Kind != brew.Formula || !dependents.Known {
		return ""
	}
	if len(dependents.Names) == 0 {
		return "Safe to remove."
	}
	if len(dependents.Names) == 1 {
		return "Removing this breaks 1 installed formula."
	}
	return "Removing this breaks " + strconv.Itoa(len(dependents.Names)) + " installed formulae."
}

// labelWidth is the widest label any panel uses. It is fixed rather than
// measured per panel so the value column lands on the same cell for every
// package: measuring per panel would shift the columns as the selection moves
// between packages whose panels carry different rows.
const labelWidth = len("Needed by")

func alignRows(rows [][2]string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[0]+strings.Repeat(" ", max(labelWidth-len(row[0]), 0)+2)+row[1])
	}
	return out
}

// InfoFunc and UsesFunc are the two Homebrew reads a detail panel needs.
type InfoFunc func(context.Context, brew.Package) (string, error)
type UsesFunc func(context.Context, brew.Package) ([]string, error)

// Details composes the two reads into one LoadFunc, so the existing single-slot
// cache keeps caching one finished panel per package and needs no changes.
//
// The dependent lookup runs for formulae only, and its failure is absorbed: a
// panel without a "Needed by" row is useful, whereas failing the whole load
// would leave the pane showing an error for a package whose details loaded fine.
func Details(loadInfo InfoFunc, loadUses UsesFunc) LoadFunc {
	return func(ctx context.Context, pkg brew.Package) (string, error) {
		if pkg.Untrusted && pkg.FullName != "" && pkg.Tap != "" {
			return untrustedDetails(pkg), nil
		}
		raw, err := loadInfo(ctx, pkg)
		if err != nil {
			return "", err
		}
		var dependents Dependents
		if pkg.Kind == brew.Formula {
			if names, err := loadUses(ctx, pkg); err == nil {
				dependents = Dependents{Names: names, Known: true}
			}
		}
		return Format(pkg, raw, dependents), nil
	}
}

func untrustedDetails(pkg brew.Package) string {
	kind := string(pkg.Kind)
	rows := [][2]string{
		{"Source", pkg.Tap},
		{"Package", pkg.FullName},
	}
	lines := []string{"Untrusted " + kind, ""}
	lines = append(lines, alignRows(rows)...)
	lines = append(lines,
		"Homebrew will not load or upgrade it.",
		"",
		"Trust permits this "+kind+"'s current and future Ruby definition to run as your user. Other tap items stay untrusted.",
		"",
		"Trust: brew trust --"+kind+" "+pkg.FullName,
		"Undo: brew untrust --"+kind+" "+pkg.FullName,
	)
	return strings.Join(lines, "\n")
}
