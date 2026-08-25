package ui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

	"lazybrew/internal/brew"
	"lazybrew/internal/info"
)

func TestSnapshotRoundTrip(t *testing.T) {
	path := snapshotFile(t.TempDir())
	sizes := brew.Sizes{Formula: map[string]int64{"alpha": 1024}, Total: 2048}
	saveSnapshot(path, snapshot{
		Lists: map[brew.Kind][]brew.Package{
			brew.Formula: {{
				Name: "alpha", Version: "1.0", Kind: brew.Formula, Outdated: true, OutdatedKnown: true, Pinned: true,
				Untrusted: true, FullName: "other/tap/alpha", Tap: "other/tap",
			}},
		},
		Sizes: &sizes,
	})

	loaded := loadSnapshot(path)
	formulae := loaded.Lists[brew.Formula]
	if len(formulae) != 1 || formulae[0].Name != "alpha" || !formulae[0].Outdated || !formulae[0].OutdatedKnown || !formulae[0].Pinned ||
		!formulae[0].Untrusted || formulae[0].FullName != "other/tap/alpha" || formulae[0].Tap != "other/tap" {
		t.Fatalf("loaded lists %+v", loaded.Lists)
	}
	if loaded.Sizes == nil || loaded.Sizes.Formula["alpha"] != 1024 || loaded.Sizes.Total != 2048 {
		t.Fatalf("loaded sizes %+v", loaded.Sizes)
	}
}

// A corrupt file, a foreign version, and disabled persistence all mean a cold
// boot, never an error and never half-decoded rows.
func TestSnapshotDegradesToCold(t *testing.T) {
	path := snapshotFile(t.TempDir())
	for _, raw := range []string{
		"not json",
		`{"version":2,"lists":{"formula":[{"Name":"alpha","Untrusted":true}]}}`,
		`{"version":99,"lists":{"cask":[{"Name":"x"}]}}`,
	} {
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := loadSnapshot(path); len(got.Lists) != 0 || got.Sizes != nil {
			t.Fatalf("raw %q loaded %+v", raw, got)
		}
	}
	if got := loadSnapshot(""); len(got.Lists) != 0 {
		t.Fatalf("disabled persistence loaded %+v", got)
	}
	saveSnapshot("", snapshot{}) // must be a no-op, not a panic
}

// A boot with a snapshot paints the previous session's rows before any brew
// call returns, reloads as a refresh, and persists the fresh result back.
func TestSeededBootPaintsSnapshotThenReloads(t *testing.T) {
	dir := t.TempDir()
	sizes := brew.Sizes{Formula: map[string]int64{"stale": 7}, Total: 7}
	saveSnapshot(snapshotFile(dir), snapshot{
		Lists: map[brew.Kind][]brew.Package{
			brew.Cask: {{Name: "Stale", Version: "1.0", Kind: brew.Cask}},
		},
		Sizes: &sizes,
	})
	homebrew := &fakeHomebrew{packages: map[brew.Kind][]brew.Package{
		brew.Cask: {{Name: "Fresh", Version: "2.0", Kind: brew.Cask}},
	}}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, dir)
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})

	command := m.Init()
	if m.sizes == nil || m.sizes.Total != 7 {
		t.Fatalf("seeded sizes %+v", m.sizes)
	}
	items := m.list.Items()
	if len(items) != 1 || items[0].(packageItem).packageValue.Name != "Stale" {
		t.Fatalf("seeded items %+v", items)
	}
	if !m.loading || m.loadPurpose != loadRefresh {
		t.Fatalf("loading %v purpose %v, want refreshing", m.loading, m.loadPurpose)
	}

	for _, message := range immediateMessages(command) {
		m.Update(message)
	}
	items = m.list.Items()
	if len(items) != 1 || items[0].(packageItem).packageValue.Name != "Fresh" {
		t.Fatalf("refreshed items %+v", items)
	}
	loaded := loadSnapshot(snapshotFile(dir))
	casks := loaded.Lists[brew.Cask]
	if len(casks) != 1 || casks[0].Name != "Fresh" {
		t.Fatalf("persisted lists %+v", loaded.Lists)
	}
}

// A cold boot stays exactly what it was: no items, "Loading" purpose.
func TestColdBootUnchangedWithoutSnapshot(t *testing.T) {
	homebrew := &fakeHomebrew{packages: map[brew.Kind][]brew.Package{}}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	m.Init()
	if len(m.list.Items()) != 0 || m.loadPurpose != loadStartup {
		t.Fatalf("cold boot items %d purpose %v", len(m.list.Items()), m.loadPurpose)
	}
}
