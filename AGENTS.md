<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# lazybrew

## Purpose
A macOS-only Bubble Tea TUI for inspecting and safely uninstalling/upgrading Homebrew packages, inspired by lazygit. Read paths shell out to `brew` (never constructing shell strings); the two mutating verbs run through a privileged helper with strict confirmation. Startup paints instantly from an on-disk snapshot and revalidates underneath.

## Key Files
| File | Description |
|------|-------------|
| `go.mod` / `go.sum` | Go 1.27 module; deps are bubbletea/bubbles/lipgloss v2 only |
| `SPEC.md` | The authoritative behavioral spec — many UI strings and behaviors are pinned to it by tests |
| `README.md` | User-facing pitch: demo GIF, features, keys, safety |
| `settings.schema.json` | JSON schema for `~/lazybrew/settings.json`; enums are pinned to code tables by tests |
| `.goreleaser.yaml` | Release build: macOS arm64/amd64 archives + Homebrew cask generation |
| `release-please-config.json` | Conventional-commit driven release PRs |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `cmd/` | Binary entrypoint (see `cmd/AGENTS.md`) |
| `internal/` | All application packages (see `internal/AGENTS.md`) |
| `.github/` | CI workflows (pr.yml, release.yml), vhs demo tapes, README assets |

## For AI Agents

### Working In This Directory
- Every change ships as a PR off `main`; branch names must not contain slashes (use hyphens).
- Conventional commits, lowercase after the type; PR bodies use `## What` / `## Test plan`.
- Comment style is dense and explains *why*, never what; deliberate simplifications carry a `// ponytail:` marker naming the ceiling and the upgrade path. Match this or the diff reads foreign.
- Absence of evidence never renders as assurance: failed reads absorb into "no marks", never into claims.

### Testing Requirements
- `gofmt -l`, `go build ./...`, `go vet ./...`, `go test ./...` all clean before any commit.
- Many tests pin exact user-facing strings; changing copy means updating the pin deliberately, not deleting it.

### Common Patterns
- Security boundary: any value reaching an argv is validated (`safePackageName` and friends); display data is explicitly documented as "not identity, no argv reads it".
- Best-effort persistence: settings/snapshot writes never produce error dialogs.

## Dependencies

### External
- charm.land/bubbletea/v2, bubbles/v2, lipgloss/v2 — TUI framework, widgets, styling.
- Homebrew and /usr/bin/du at runtime; cgo for isatty and macOS Security framework.

<!-- MANUAL: Any manually added notes below this line are preserved on regeneration -->
