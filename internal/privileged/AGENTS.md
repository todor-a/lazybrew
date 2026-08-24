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
- Peer-chain walking cannot read setuid sudo the normal way: proc_pidinfo returns EPERM and KERN_PROCARGS2 EINVAL cross-euid, so the sudo link is acquired via the world-readable KERN_PROC_PID fallback (`acquireRestrictedProcess`) and its `-A` argv check is excused — only for a node kinfo proves is euid-0 on our own real uid, only at chain index 1. Do not widen either condition.
- Cleanup runs under a deadline (main gives 20s); phases must stay bounded.

### Testing Requirements
- privileged_test.go/protocol_test.go/peer_darwin_test.go cover lifecycle, framing, and peer checks; run the full suite — these tests take seconds by design.

## Dependencies

### Internal
- `internal/brew` (PrepareCommand output is the only thing executed).

### External
- cgo: Security + CoreFoundation frameworks (peer verification), libproc/sysctl.

### Security consequence of SUDO_ASKPASS surviving brew (accepted scope)
Everything brew runs inherits SUDO_ASKPASS - including formula/cask install and
postinstall scripts. Any such descendant running `sudo -A <anything>` satisfies
the peer chain (peer → sudo -A → … → the job's brew child, all in the tracked
pgid), pops the password dialog, and on approval runs as root. This matches
Homebrew's own trust model (approving an operation on a package already means
running its scripts): the peer check binds the password to THIS JOB'S PROCESS
TREE - the strongest binding available - not to one specific sudo invocation.
Recorded from the PR #36 security review; do not "fix" this by weakening the
chain check, and do not widen it either.
