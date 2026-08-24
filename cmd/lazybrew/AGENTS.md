<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# cmd/lazybrew

## Purpose
Process entrypoint: wires the brew adapter, info loader, privileged runner, settings dir (~/lazybrew) and logger, then runs the Bubble Tea program with its own signal handling (SIGINT=130, SIGTERM=143) and a supervised cleanup window on exit.

## Key Files
| File | Description |
|------|-------------|
| `main.go` | run() wiring; privileged helper re-entry check runs FIRST (the same binary re-execs as the helper); cgo isatty gate refuses non-interactive stdin/stdout |

## For AI Agents

### Working In This Directory
- `privileged.RunHelperFromEnv()` must stay the first act of run(): the binary doubles as the privileged helper child.
- Exit codes are contractual: 130/143 pass through; cleanup failure joins errors to stderr as a single line.
- No tests exist here (cgo main); keep logic minimal and push anything testable into internal packages.

## Dependencies

### Internal
- `internal/ui` (root model), `internal/brew`, `internal/info`, `internal/privileged`, `internal/logging`.
