<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# internal/ui

## Purpose
The whole Bubble Tea application: model state machine (modes: normal/search/confirm/password/operation/quitting), table view with per-screen sort cycle, info pane, themes, settings + snapshot persistence, and the serial operation queue over the privileged runner.

## Key Files
| File | Description |
|------|-------------|
| `model.go` | Model struct, Update dispatch, list/sizes/info load lifecycles (id-guarded async results), sortOrder cycle, operation queue, outdated threshold stamping (markOutdated) |
| `view.go` | All rendering: table header + rows (freshness cell precedence: spinner > queued • > untrusted ! > outdated ↑), pane split (<72 cols = single pane; info capped at 46 past 96), footer, bump highlight |
| `keys.go` | Footer key help tables (display-only bindings; verb leads, key follows) |
| `theme.go` | True-color adaptive palettes (light/dark per color); canvas never painted |
| `settings.go` | Best-effort `~/lazybrew/settings.json` (theme, outdatedThreshold); schema-pinned enums |
| `snapshot.go` | Stale-while-revalidate boot snapshot (lists + sizes); versioned, best-effort, never trusted past first paint |

## For AI Agents

### Working In This Directory
- Async results carry ids (loadID/sizesID/operationID); stale results MUST be dropped by id guard — copy the existing pattern.
- setPackages is the only place filtering/sorting lives; never sort retained cache slices in place (shared backing arrays).
- The confirm flow is security-relevant: immutable snapshot, lowercase-y-only, confirmVerb vs verb separation. Do not re-derive per verb.
- Keys are never silently dead: blocked keys answer with a status or are absent from the footer.
- One freshness cell by design (ponytail comment names the upgrade path).

### Testing Requirements
- model_test.go drives Update with fake Homebrew/uninstaller/job; view_test.go strips rendered lines and pins exact strings (note: source uses \u escapes for glyphs).
- Startup flows via newTestModel/newFleetModel + immediateMessages.

### Common Patterns
- Status row: transient message + priority flag; errors ordinary, confirmations priority.
- Every user-facing operation string derives from operationWords.

## Dependencies

### Internal
- `internal/brew` (data + verbs), `internal/info` (pane loader), `internal/privileged` (jobs).
