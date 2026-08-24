<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# internal/logging

## Purpose
Routes the process-wide slog default to `~/lazybrew/lazybrew.log` as JSON lines. A TUI owns stdout/stderr, so a failed open degrades to io.Discard — never a stream write that would corrupt the frame. Debug level when the executable is a `go run` build (path contains /go-build), info otherwise.

## Key Files
| File | Description |
|------|-------------|
| `logging.go` | Setup (level pick, open/append, 1MB startup truncation), isGoRun |

## For AI Agents

### Working In This Directory
- Never add a stderr/stdout fallback. Discard is the correct degradation.
- Log lines are analysis data: keep them structured (key-value), one debug line per external command lives in internal/brew/exec.go, not here.

### Testing Requirements
- logging_test.go: isGoRun cases + file smoke test. Do not assert the level in tests — the test binary itself runs from a go-build path.
