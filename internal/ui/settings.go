package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"lazybrew/internal/brew"
)

// settings is the on-disk preference file. Persistence is best-effort in both
// directions: a missing, unreadable, or unrecognisable file means defaults,
// and a failed write means the choice lasts for the session — a preference is
// never worth an error dialog in a TUI.
type settings struct {
	// Schema points editors at the schema published in the repo, so
	// hand-editing gets completion and typo squiggles. A test pins the
	// published file's theme enum to the theme table, so the reference cannot
	// drift from what this binary accepts.
	Schema string `json:"$schema,omitempty"`
	Theme  string `json:"theme"`
	// OutdatedThreshold is how big a version jump must be before a package is
	// marked outdated: "any" (default), "minor", or "major". Version distance
	// rather than time because brew carries no per-version release dates
	// anywhere, so time has no local data source.
	OutdatedThreshold string `json:"outdatedThreshold"`
}

const schemaURL = "https://raw.githubusercontent.com/todor-a/lazybrew/main/settings.schema.json"

// settingsFile places the file in the given settings directory (~/lazybrew in
// main). An empty dir disables persistence, which is also how tests keep model
// construction off the real config.
func settingsFile(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "settings.json")
}

func loadSettings(path string) settings {
	var s settings
	if path == "" {
		return s
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

func saveSettings(path string, s settings) {
	if path == "" {
		return
	}
	s.Schema = schemaURL
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, append(raw, '\n'), 0o644)
}

// ensureSettings loads the settings and rewrites the file on startup, so a
// hand-written or pre-schema file gains the $schema reference and a canonical
// theme name. Best-effort like every other write.
func ensureSettings(path string) settings {
	s := loadSettings(path)
	if path == "" {
		return s
	}
	s.Theme = themes[themeIndexByName(s.Theme)].name
	s.OutdatedThreshold = thresholdByName(s.OutdatedThreshold).name()
	saveSettings(path, s)
	return s
}

// themeIndexByName resolves a saved theme name, falling back to the default
// for an unknown or empty one, so a renamed or removed theme degrades to the
// default instead of an out-of-range index.
func themeIndexByName(name string) int {
	for i, t := range themes {
		if t.name == name {
			return i
		}
	}
	return 0
}

// outdatedThreshold is the version distance the outdated mark requires. It is
// one truth applied everywhere the app says "outdated" — the ↑ freshness
// cell, the row's version arrow, the info pane's verdict line, and the `u`
// affordance — so no screen can contradict another.
type outdatedThreshold uint8

const (
	thresholdAny outdatedThreshold = iota
	thresholdMinor
	thresholdMajor
)

// thresholdNames doubles as the settings enum; a test pins the published
// schema to it exactly as the theme enum is pinned.
var thresholdNames = []string{"any", "minor", "major"}

// thresholdByName resolves a saved threshold, falling back to "any" for an
// unknown or empty one — the same degrade-to-default contract as themes, and
// the fail-open direction: the default hides nothing.
func thresholdByName(name string) outdatedThreshold {
	for i, n := range thresholdNames {
		if n == name {
			return outdatedThreshold(i)
		}
	}
	return thresholdAny
}

func (t outdatedThreshold) name() string { return thresholdNames[t] }

// allows reports whether a classified distance clears this threshold.
// DistanceUnknown orders above every real distance (see brew.Distance), so an
// unreadable version pair clears every threshold and is always marked.
func (t outdatedThreshold) allows(d brew.Distance) bool {
	switch t {
	case thresholdMajor:
		return d >= brew.DistanceMajor
	case thresholdMinor:
		return d >= brew.DistanceMinor
	}
	return true
}
