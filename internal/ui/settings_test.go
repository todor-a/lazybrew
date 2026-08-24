package ui

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// The schema published in the repo (referenced by every written settings file
// via its raw URL) must accept exactly the themes this binary ships.
func TestPublishedSchemaMatchesThemeTable(t *testing.T) {
	raw, err := os.ReadFile("../../settings.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Theme struct {
				Enum []string `json:"enum"`
			} `json:"theme"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(themes))
	for i, theme := range themes {
		names[i] = theme.name
	}
	if !slices.Equal(schema.Properties.Theme.Enum, names) {
		t.Fatalf("schema enum %v, themes %v", schema.Properties.Theme.Enum, names)
	}
}

func TestThemeChoicePersistsAcrossRuns(t *testing.T) {
	m, _ := newTestModel(t)
	m.settingsPath = settingsFile(t.TempDir())

	m.Update(textKey("t"))
	saved := loadSettings(m.settingsPath)
	if saved.Theme != themes[m.themeIndex].name {
		t.Fatalf("saved theme %q, want %q", saved.Theme, themes[m.themeIndex].name)
	}
	if got := themeIndexByName(saved.Theme); got != m.themeIndex {
		t.Fatalf("restored theme index %d, want %d", got, m.themeIndex)
	}

	// An unknown saved name and a corrupt file both degrade to the default.
	if err := os.WriteFile(m.settingsPath, []byte(`{"theme":"NoSuchTheme"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := themeIndexByName(loadSettings(m.settingsPath).Theme); got != 0 {
		t.Fatalf("unknown theme restored index %d, want 0", got)
	}
	if err := os.WriteFile(m.settingsPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := themeIndexByName(loadSettings(m.settingsPath).Theme); got != 0 {
		t.Fatalf("corrupt file restored index %d, want 0", got)
	}

	// An empty path (persistence disabled) must not panic on either side.
	m.settingsPath = ""
	m.Update(textKey("t"))
	if got := themeIndexByName(loadSettings("").Theme); got != 0 {
		t.Fatalf("disabled persistence restored index %d, want 0", got)
	}
}
