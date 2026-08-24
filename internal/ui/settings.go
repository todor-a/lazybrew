package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
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
