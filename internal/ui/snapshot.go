package ui

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"lazybrew/internal/brew"
)

// snapshot is the on-disk copy of the last successful list and size reads,
// written after every successful load and read back at startup so the first
// frame shows the previous session's inventory instead of an empty pane. It is
// never trusted as current: the startup loads still run and replace it, so a
// stale, corrupt, or hand-edited file costs at most one repaint. Names read
// from it pass through the same argv validation as names read from brew
// (safePackageName at every argv builder), so this file adds no command
// surface.
//
// Best-effort in both directions, exactly like settings: a missing or
// unreadable file means a cold boot, and a failed write means the next boot is
// cold. Info text is deliberately not persisted — its validity is not captured
// by (kind, name), so an honest cache would have to revalidate on every
// select, which is the fetch it was meant to save.
type snapshot struct {
	// Version gates decoding: a snapshot written by a binary with a different
	// row shape is discarded whole rather than half-decoded into wrong rows.
	Version int                          `json:"version"`
	Lists   map[brew.Kind][]brew.Package `json:"lists"`
	Sizes   *brew.Sizes                  `json:"sizes,omitempty"`
}

const snapshotVersion = 1

// snapshotFile places the file next to settings. An empty dir disables
// persistence, which is also how tests keep model construction off the real
// file.
func snapshotFile(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "snapshot.json")
}

func loadSnapshot(path string) snapshot {
	var s snapshot
	if path == "" {
		return s
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("snapshot not read", "path", path, "err", err)
		return s
	}
	if json.Unmarshal(raw, &s) != nil || s.Version != snapshotVersion {
		slog.Debug("snapshot discarded", "path", path, "version", s.Version)
		return snapshot{}
	}
	slog.Debug("snapshot loaded",
		"casks", len(s.Lists[brew.Cask]), "formulae", len(s.Lists[brew.Formula]), "sized", s.Sizes != nil)
	return s
}

func saveSnapshot(path string, s snapshot) {
	if path == "" {
		return
	}
	s.Version = snapshotVersion
	raw, err := json.Marshal(s)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, append(raw, '\n'), 0o644)
	slog.Debug("snapshot saved",
		"casks", len(s.Lists[brew.Cask]), "formulae", len(s.Lists[brew.Formula]), "sized", s.Sizes != nil)
}

// persistSnapshot writes the retained caches after a successful load. Only
// call sites holding fresh data call it; a failed load persists nothing,
// mirroring the in-memory caches it copies.
func (m *model) persistSnapshot() {
	saveSnapshot(m.snapshotPath, snapshot{Lists: m.listCache, Sizes: m.sizes})
}
