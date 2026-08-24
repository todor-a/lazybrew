<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# internal

## Purpose
All application packages. Dependency direction: ui → {brew, info, privileged}; info → brew; privileged → brew; logging is leaf-standalone.

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `brew/` | Homebrew command adapter: list/outdated/info/uses/sizes reads, argv safety, version distance (see `brew/AGENTS.md`) |
| `ui/` | Bubble Tea model/view: list table, sort, search, themes, settings, snapshot, operation queue (see `ui/AGENTS.md`) |
| `info/` | Info-pane loader (serialized fetch + cache) and `brew info` text curation (see `info/AGENTS.md`) |
| `privileged/` | Privileged uninstall/upgrade runner: helper re-exec, framed socket protocol, askpass, peer verification (see `privileged/AGENTS.md`) |
| `logging/` | slog-to-file setup; debug level for go-run builds (see `logging/AGENTS.md`) |
