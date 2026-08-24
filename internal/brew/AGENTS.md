<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# internal/brew

## Purpose
The one Homebrew boundary. Read operations (list, outdated, untrusted, info, uses, sizes) behind the `Homebrew` interface, plus `PrepareCommand` — the ONLY argv builder for the privileged verbs. Every brew call runs with `HOMEBREW_NO_AUTO_UPDATE=1` so drawing a list never mutates the user's Homebrew.

## Key Files
| File | Description |
|------|-------------|
| `brew.go` | Package type (display fields explicitly outside identity), List (formula enumeration + concurrent dependency marker), Outdated (`--json=v2`, never `--greedy`), Untrusted (full-name × trust store), Info/Uses, name safety |
| `exec.go` | runTool: the single command runner (capture, WaitDelay, failure mapping, debug log per command); resolveBrew PATH walk; PrepareCommand |
| `sizes.go` | One `du -k -d 1` pass over the Cellar; casks deliberately unsized (their Caskroom numbers are not sizes) |
| `version.go` | VersionDistance/BumpOffset: dotted-segment + `_N` revision parsing; unparseable = DistanceUnknown which orders ABOVE every threshold (fail open) |

## For AI Agents

### Working In This Directory
- A second argv builder for a privileged verb must never exist; extend PrepareCommand.
- Names reaching an argv pass `safePackageName`; names used only as map keys are documented as such.
- Failed marker/annotation reads absorb (no marks); failed inventory reads fail the load. Keep that asymmetry.

### Testing Requirements
- Table-driven tests in *_test.go; parser tests use realistic Homebrew version strings (`1.3.19-stable`, `2026.07.27.00_1`).

### Common Patterns
- Concurrent paired reads (list + marker) joined before either local is read.
- Comments record measured numbers from the dev machine; keep them when editing nearby.

## Dependencies

### External
- Homebrew on PATH; /usr/bin/du (absolute, SIP-protected — never PATH-resolved).
