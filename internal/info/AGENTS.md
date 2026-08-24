<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# internal/info

## Purpose
The info pane's data layer: a Loader that serializes `brew info` fetches (latest-pending-wins, generation-invalidated cache) and a Format pass that curates brew's install-oriented text into removal-oriented fields (version, size, license, home, dependents, removal verdict).

## Key Files
| File | Description |
|------|-------------|
| `loader.go` | Mutex-guarded Loader: one active command, one pending target, per-generation cache keyed (kind, name); Cancel/Done for shutdown |
| `format.go` | Parses `brew info` TEXT (not --json: JSON carries no installed size); unparseable output degrades to raw text, never blank |

## For AI Agents

### Working In This Directory
- The outdated verdict arrives ON the package value from `brew outdated` — Format must never form a second opinion from version strings.
- Generation counter is the invalidation mechanism; results from an old generation are discarded in Complete.
- Cache stores error text too (prevents refetch storms) — deliberate.

### Testing Requirements
- loader_test exercises the pending/active/generation races; format_test pins parsed fields against captured brew output.

## Dependencies

### Internal
- `internal/brew` (Package, load functions injected from main).
