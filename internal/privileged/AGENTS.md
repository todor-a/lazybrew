<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-08-24 | Updated: 2026-08-24 -->

# internal/privileged

## Purpose
Runs the two mutating brew verbs with administrator authentication kept OUT of the UI process's trust domain: the lazybrew binary re-execs itself as a helper child, talks over a framed unix-socket protocol, and services sudo askpass password requests with a masked in-TUI dialog. SECURITY-CRITICAL package — read every comment before touching.

## Key Files
| File | Description |
|------|-------------|
| `privileged.go` | Runner/Job lifecycle: Start (one job at a time), Events/RespondPassword/CancelPassword/Cancel/Wait, worker supervision, bounded cleanup; DisableCoreDumps |
| `protocol.go` | Length-framed message protocol (request ids, password frames) over the socket |
| `peer_darwin.go` | cgo peer verification via macOS Security framework: the socket peer must be the expected process, not whoever dialed first |

## For AI Agents

### Working In This Directory
- Passwords: never logged, never cached, wiped after use; any change touching password bytes needs explicit review of every copy made.
- The helper re-entry (`RunHelperFromEnv`) is invoked from cmd/lazybrew before anything else. Detection is path-first: brew scrubs the LAZYBREW_* env markers, so SUDO_ASKPASS points at a per-job `lazybrew-askpass` symlink inside the private socket directory and the helper recognises itself by argv[0]; the env route survives for direct (unscrubbed) children and tests.
- One job at a time is a UI-level AND runner-level invariant; the ui queue serializes on top, it does not parallelize.
- Cleanup runs under a deadline (main gives 20s); phases must stay bounded.

### Testing Requirements
- privileged_test.go/protocol_test.go/peer_darwin_test.go cover lifecycle, framing, and peer checks; run the full suite — these tests take seconds by design.

## Dependencies

### Internal
- `internal/brew` (PrepareCommand output is the only thing executed).

### External
- cgo: Security + CoreFoundation frameworks (peer verification), libproc/sysctl.
