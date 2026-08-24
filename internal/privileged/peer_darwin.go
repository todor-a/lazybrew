package privileged

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <errno.h>
#include <libproc.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/proc.h>
#include <sys/socket.h>
#include <sys/sysctl.h>
#include <sys/un.h>
#include <sys/ucred.h>
#include <unistd.h>

static int lb_disable_core(void) {
	struct rlimit limit;
	limit.rlim_cur = 0;
	limit.rlim_max = 0;
	return setrlimit(RLIMIT_CORE, &limit);
}

static int lb_peer_cred(int fd, uid_t *uid, pid_t *pid) {
	struct xucred cred;
	socklen_t cred_len = sizeof(cred);
	pid_t peer_pid = 0;
	socklen_t pid_len = sizeof(peer_pid);
	if (getsockopt(fd, SOL_LOCAL, LOCAL_PEERCRED, &cred, &cred_len) != 0 ||
	    cred_len != sizeof(cred) || cred.cr_version != XUCRED_VERSION) return -1;
	if (getsockopt(fd, SOL_LOCAL, LOCAL_PEERPID, &peer_pid, &pid_len) != 0 ||
	    pid_len != sizeof(peer_pid) || peer_pid <= 1) return -1;
	*uid = cred.cr_uid;
	*pid = peer_pid;
	return 0;
}

static int lb_proc_info(pid_t pid, pid_t *ppid, pid_t *pgid, char *path, int path_cap) {
	struct proc_bsdinfo info;
	int got = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info));
	if (got != sizeof(info)) return -1;
	int path_len = proc_pidpath(pid, path, path_cap);
	if (path_len <= 0 || path_len >= path_cap) return -1;
	*ppid = (pid_t)info.pbi_ppid;
	*pgid = (pid_t)info.pbi_pgid;
	return path_len;
}

static int lb_proc_argv(pid_t pid, char *out, size_t out_cap) {
	int mib[3] = { CTL_KERN, KERN_PROCARGS2, pid };
	size_t size = 0;
	if (sysctl(mib, 3, NULL, &size, NULL, 0) != 0 || size < sizeof(int) || size > 1024 * 1024) return -1;
	char *buf = malloc(size);
	if (buf == NULL) return -1;
	if (sysctl(mib, 3, buf, &size, NULL, 0) != 0) { free(buf); return -1; }
	int argc = 0;
	memcpy(&argc, buf, sizeof(argc));
	if (argc < 0 || argc > 65536) { free(buf); return -1; }
	char *p = buf + sizeof(argc);
	char *end = buf + size;
	while (p < end && *p != '\0') p++;
	while (p < end && *p == '\0') p++;
	size_t used = 0;
	for (int i = 0; i < argc; i++) {
		if (p >= end) { free(buf); return -1; }
		char *nul = memchr(p, '\0', (size_t)(end - p));
		if (nul == NULL) { free(buf); return -1; }
		size_t len = (size_t)(nul - p);
		if (used + len + 1 > out_cap) { free(buf); return -1; }
		memcpy(out + used, p, len);
		used += len;
		out[used++] = '\0';
		p = nul + 1;
	}
	free(buf);
	return (int)used;
}

// lb_proc_restricted reads what the kernel still publishes about a process
// whose euid differs from the caller's: KERN_PROC_PID (ppid, pgid, euid,
// ruid) is world-readable, and proc_pidpath works cross-euid, while
// proc_pidinfo(PROC_PIDTBSDINFO) returns EPERM and KERN_PROCARGS2 returns
// EINVAL (probed against a live setuid /usr/bin/sudo on macOS 25.6).
static int lb_proc_restricted(pid_t pid, pid_t *ppid, pid_t *pgid, unsigned int *euid, unsigned int *ruid, char *path, int path_cap) {
	int mib[4] = { CTL_KERN, KERN_PROC, KERN_PROC_PID, pid };
	struct kinfo_proc info;
	size_t size = sizeof(info);
	memset(&info, 0, sizeof(info));
	if (sysctl(mib, 4, &info, &size, NULL, 0) != 0 || size == 0 || info.kp_proc.p_pid != pid) return -1;
	int path_len = proc_pidpath(pid, path, path_cap);
	if (path_len <= 0 || path_len >= path_cap) return -1;
	*ppid = info.kp_eproc.e_ppid;
	*pgid = info.kp_eproc.e_pgid;
	*euid = info.kp_eproc.e_ucred.cr_uid;
	*ruid = info.kp_eproc.e_pcred.p_ruid;
	return path_len;
}

static int lb_process_group_has_live_members(pid_t pgid, int *has_live) {
	if (pgid <= 1 || has_live == NULL) {
		errno = EINVAL;
		return -1;
	}
	int mib[4] = { CTL_KERN, KERN_PROC, KERN_PROC_PGRP, pgid };
	for (int attempt = 0; attempt < 4; attempt++) {
		size_t capacity = 0;
		if (sysctl(mib, 4, NULL, &capacity, NULL, 0) != 0) {
			if (errno == ESRCH) {
				*has_live = 0;
				return 0;
			}
			return -1;
		}
		if (capacity == 0) {
			*has_live = 0;
			return 0;
		}
		struct kinfo_proc *entries = malloc(capacity);
		if (entries == NULL) {
			errno = ENOMEM;
			return -1;
		}
		size_t used = capacity;
		if (sysctl(mib, 4, entries, &used, NULL, 0) != 0) {
			int saved_errno = errno;
			free(entries);
			if (saved_errno == ESRCH) {
				*has_live = 0;
				return 0;
			}
			if (saved_errno == ENOMEM) continue;
			errno = saved_errno;
			return -1;
		}
		if (used > capacity || used % sizeof(*entries) != 0) {
			free(entries);
			errno = EPROTO;
			return -1;
		}
		size_t count = used / sizeof(*entries);
		for (size_t i = 0; i < count; i++) {
			if (entries[i].kp_proc.p_stat != SZOMB) {
				free(entries);
				*has_live = 1;
				return 0;
			}
		}
		free(entries);
		*has_live = 0;
		return 0;
	}
	errno = EAGAIN;
	return -1;
}

static int lb_code_identity(pid_t pid, unsigned char *out, size_t out_cap) {
	SecCodeRef code = NULL;
	CFDictionaryRef attributes = NULL;
	CFDictionaryRef signing = NULL;
	CFNumberRef pid_number = NULL;
	OSStatus status;

	if (pid == getpid()) {
		status = SecCodeCopySelf(kSecCSDefaultFlags, &code);
	} else {
		pid_number = CFNumberCreate(NULL, kCFNumberIntType, &pid);
		if (pid_number == NULL) return -1;
		const void *keys[] = { kSecGuestAttributePid };
		const void *values[] = { pid_number };
		attributes = CFDictionaryCreate(NULL, keys, values, 1,
			&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
		if (attributes == NULL) { CFRelease(pid_number); return -1; }
		status = SecCodeCopyGuestWithAttributes(NULL, attributes, kSecCSDefaultFlags, &code);
	}
	if (status != errSecSuccess || code == NULL) goto fail;
	status = SecCodeCopySigningInformation(code, kSecCSSigningInformation | kSecCSDynamicInformation, &signing);
	if (status != errSecSuccess || signing == NULL) goto fail;
	CFTypeRef value = CFDictionaryGetValue(signing, kSecCodeInfoUnique);
	if (value == NULL || CFGetTypeID(value) != CFDataGetTypeID()) goto fail;
	CFIndex len = CFDataGetLength((CFDataRef)value);
	if (len <= 0 || (size_t)len > out_cap) goto fail;
	memcpy(out, CFDataGetBytePtr((CFDataRef)value), (size_t)len);
	if (signing) CFRelease(signing);
	if (code) CFRelease(code);
	if (attributes) CFRelease(attributes);
	if (pid_number) CFRelease(pid_number);
	return (int)len;
fail:
	if (signing) CFRelease(signing);
	if (code) CFRelease(code);
	if (attributes) CFRelease(attributes);
	if (pid_number) CFRelease(pid_number);
	return -1;
}
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"unsafe"
)

type processEvidence struct {
	PID        int
	ParentPID  int
	GroupID    int
	LoadedPath string
	Argv       []string
	// Restricted marks a node acquired through the kinfo fallback: a
	// setuid-root process (euid 0, real uid ours) whose proc_pidinfo and
	// argv the kernel refuses to a non-root caller. Its lineage, process
	// group, and executable path are still kernel-reported truth; only its
	// argv is unknowable. Settable exclusively by acquireRestrictedProcess.
	Restricted bool
}

type peerEvidence struct {
	ExpectedUID      int
	PeerUID          int
	PeerPID          int
	ExpectedPath     string
	PeerLoadedPath   string
	ExpectedIdentity []byte
	PeerIdentity     []byte
	TrackedPID       int
	TrackedGroupID   int
	Chain            []processEvidence
}

func disableCoreDumps() error {
	if C.lb_disable_core() != 0 {
		return errors.New("could not disable core dumps")
	}
	return nil
}

func processGroupHasLiveMembers(pgid int) (bool, error) {
	var live C.int
	if result, err := C.lb_process_group_has_live_members(C.pid_t(pgid), &live); result != 0 {
		return false, fmt.Errorf("could not inspect process group %d: %w", pgid, err)
	}
	if live != 0 && live != 1 {
		return false, errors.New("process group inspection returned invalid state")
	}
	return live == 1, nil
}

func loadedCodeIdentity(pid int) ([]byte, error) {
	buf := make([]byte, 64)
	n := int(C.lb_code_identity(C.pid_t(pid), (*C.uchar)(unsafe.Pointer(&buf[0])), C.size_t(len(buf))))
	if n <= 0 || n > len(buf) {
		wipe(buf)
		return nil, errors.New("loaded code identity unavailable")
	}
	identity := append([]byte(nil), buf[:n]...)
	wipe(buf)
	return identity, nil
}

func acquirePeerEvidence(conn *net.UnixConn, trackedPID, trackedGroupID int, expectedPath string, expectedIdentity []byte) (peerEvidence, error) {
	uid, pid, err := socketPeer(conn)
	if err != nil {
		return peerEvidence{}, err
	}
	peer, err := acquireProcess(pid, false)
	if err != nil {
		return peerEvidence{}, err
	}
	identity, err := loadedCodeIdentity(pid)
	if err != nil {
		return peerEvidence{}, err
	}
	evidence := peerEvidence{
		ExpectedUID: os.Geteuid(), PeerUID: uid, PeerPID: pid,
		ExpectedPath: expectedPath, PeerLoadedPath: peer.LoadedPath,
		ExpectedIdentity: append([]byte(nil), expectedIdentity...), PeerIdentity: identity,
		TrackedPID: trackedPID, TrackedGroupID: trackedGroupID,
		Chain: []processEvidence{peer},
	}
	seen := map[int]struct{}{pid: struct{}{}}
	current := peer
	for range 64 {
		if current.PID == trackedPID {
			uidAgain, pidAgain, credentialErr := socketPeer(conn)
			if credentialErr != nil || uidAgain != uid || pidAgain != pid {
				return evidence, errors.New("socket peer changed during verification")
			}
			peerAgain, acquireErr := acquireProcess(pid, false)
			if acquireErr != nil || !sameProcessEvidence(peer, peerAgain, false) {
				return evidence, errors.New("peer changed during verification")
			}
			identityAgain, identityErr := loadedCodeIdentity(pid)
			if identityErr != nil || !bytes.Equal(identity, identityAgain) {
				return evidence, errors.New("peer code identity changed during verification")
			}
			if len(evidence.Chain) > 1 {
				sudoAgain, sudoErr := acquireProcess(evidence.Chain[1].PID, true)
				if sudoErr != nil || !sameProcessEvidence(evidence.Chain[1], sudoAgain, true) {
					return evidence, errors.New("sudo changed during verification")
				}
			}
			trackedAgain, trackedErr := acquireProcess(trackedPID, false)
			if trackedErr != nil || !sameProcessEvidence(current, trackedAgain, false) {
				return evidence, errors.New("tracked child changed during verification")
			}
			return evidence, nil
		}
		if current.ParentPID <= 1 {
			return evidence, errors.New("tracked child absent from ancestry")
		}
		if _, exists := seen[current.ParentPID]; exists {
			return evidence, errors.New("process ancestry loop")
		}
		seen[current.ParentPID] = struct{}{}
		current, err = acquireProcess(current.ParentPID, len(evidence.Chain) == 1)
		if err != nil {
			return evidence, err
		}
		evidence.Chain = append(evidence.Chain, current)
	}
	return evidence, errors.New("process ancestry exceeds limit")
}
func sameProcessEvidence(left, right processEvidence, compareArgv bool) bool {
	if left.PID != right.PID || left.ParentPID != right.ParentPID || left.GroupID != right.GroupID || left.LoadedPath != right.LoadedPath || left.Restricted != right.Restricted {
		return false
	}
	if !compareArgv {
		return true
	}
	if len(left.Argv) != len(right.Argv) {
		return false
	}
	for index := range left.Argv {
		if left.Argv[index] != right.Argv[index] {
			return false
		}
	}
	return true
}

func peerRemainsAuthenticated(e peerEvidence) bool {
	if e.PeerPID <= 1 || len(e.Chain) == 0 || len(e.PeerIdentity) == 0 {
		return false
	}
	peer, err := acquireProcess(e.PeerPID, false)
	if err != nil || !sameProcessEvidence(e.Chain[0], peer, false) {
		return false
	}
	identity, err := loadedCodeIdentity(e.PeerPID)
	if err != nil {
		return false
	}
	defer wipe(identity)
	return bytes.Equal(identity, e.PeerIdentity)
}

func verifyPeerEvidence(e peerEvidence) error {
	if e.ExpectedUID < 0 || e.PeerUID != e.ExpectedUID || e.PeerPID <= 1 || e.TrackedPID <= 1 || e.TrackedGroupID <= 1 {
		return errAuthentication
	}
	if e.PeerLoadedPath == "" || e.PeerLoadedPath != e.ExpectedPath || len(e.ExpectedIdentity) == 0 || !bytes.Equal(e.PeerIdentity, e.ExpectedIdentity) {
		return errAuthentication
	}
	if len(e.Chain) < 3 || e.Chain[0].PID != e.PeerPID || e.Chain[0].LoadedPath != e.PeerLoadedPath || e.Chain[0].ParentPID != e.Chain[1].PID {
		return errAuthentication
	}
	sudo := e.Chain[1]
	if sudo.LoadedPath != "/usr/bin/sudo" {
		return errAuthentication
	}
	// The -A check proved sudo was invoked in askpass mode. For a Restricted
	// sudo the kernel refuses argv to non-root callers, so the check cannot
	// run - and it was corroboration, not the load-bearing bind: a sudo
	// without -A never execs an askpass at all, so the verified peer's
	// existence (this binary, exec'd via this job's private SUDO_ASKPASS
	// link, child of this sudo) already implies askpass mode. The password
	// stays bound to the job's process tree by everything that remains:
	// kernel-reported path /usr/bin/sudo, pgid membership in the tracked
	// group, and lineage terminating at the tracked brew child.
	if !sudo.Restricted && !containsArgument(sudo.Argv, "-A") {
		return errAuthentication
	}
	if e.Chain[0].GroupID != e.TrackedGroupID || sudo.GroupID != e.TrackedGroupID {
		return errAuthentication
	}
	seen := make(map[int]struct{}, len(e.Chain))
	foundTracked := false
	for index, proc := range e.Chain {
		if proc.PID <= 1 {
			return errAuthentication
		}
		if _, exists := seen[proc.PID]; exists {
			return errAuthentication
		}
		// Only the sudo link may be euid-restricted: the peer shares our
		// euid by the socket-credential check, and everything above sudo is
		// brew's own user-euid tree, so a second unreadable link is not a
		// legitimate shape.
		if proc.Restricted && index != 1 {
			return errAuthentication
		}
		seen[proc.PID] = struct{}{}
		if index > 0 && e.Chain[index-1].ParentPID != proc.PID {
			return errAuthentication
		}
		if proc.PID == e.TrackedPID {
			foundTracked = true
			if index != len(e.Chain)-1 {
				return errAuthentication
			}
		}
	}
	if !foundTracked || len(e.Chain) > 64 || e.Chain[len(e.Chain)-1].GroupID != e.TrackedGroupID {
		return errAuthentication
	}
	return nil
}

func socketPeer(conn *net.UnixConn) (int, int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var uid C.uid_t
	var pid C.pid_t
	var callErr error
	if err := raw.Control(func(fd uintptr) {
		if C.lb_peer_cred(C.int(fd), &uid, &pid) != 0 {
			callErr = errors.New("peer credentials unavailable")
		}
	}); err != nil {
		return 0, 0, err
	}
	if callErr != nil {
		return 0, 0, callErr
	}
	return int(uid), int(pid), nil
}

func acquireProcess(pid int, withArgv bool) (processEvidence, error) {
	path := make([]byte, C.PROC_PIDPATHINFO_MAXSIZE)
	var ppid C.pid_t
	var pgid C.pid_t
	n := int(C.lb_proc_info(C.pid_t(pid), &ppid, &pgid, (*C.char)(unsafe.Pointer(&path[0])), C.int(len(path))))
	if n <= 0 || n > len(path) {
		// proc_pidinfo is euid-gated: it returns EPERM against the setuid
		// /usr/bin/sudo that necessarily sits between the askpass helper and
		// the job's brew child, which made every real brew-invoked sudo fail
		// verification (field log: "process information unavailable"). The
		// kinfo fallback recovers exactly the fields the walk and the
		// verifier need - ppid, pgid, path - from kernel-published data, and
		// only for the one process shape that cannot be read the normal way.
		return acquireRestrictedProcess(pid)
	}
	result := processEvidence{PID: pid, ParentPID: int(ppid), GroupID: int(pgid), LoadedPath: string(path[:n])}
	if withArgv {
		argvBytes := make([]byte, 1024*1024)
		defer wipe(argvBytes)
		used := int(C.lb_proc_argv(C.pid_t(pid), (*C.char)(unsafe.Pointer(&argvBytes[0])), C.size_t(len(argvBytes))))
		if used <= 0 || used > len(argvBytes) {
			return processEvidence{}, errors.New("process argv unavailable")
		}
		for _, arg := range bytes.Split(argvBytes[:used], []byte{0}) {
			if len(arg) != 0 {
				result.Argv = append(result.Argv, string(arg))
			}
		}
	}
	return result, nil
}

// acquireRestrictedProcess is the euid-gated fallback for acquireProcess.
// It accepts only the setuid-sudo shape: kernel-reported euid 0 with the
// caller's own real uid, i.e. a setuid-root binary this user launched.
// Anything else keeps failing exactly as before - the fallback must never
// widen acquisition for ordinary same-euid processes, whose primary reads
// cannot legitimately fail. Argv is unreadable for such a node
// (KERN_PROCARGS2 returns EINVAL cross-euid), so the node carries none and
// is marked Restricted; the verifier decides what that absence may excuse.
func acquireRestrictedProcess(pid int) (processEvidence, error) {
	path := make([]byte, C.PROC_PIDPATHINFO_MAXSIZE)
	var ppid C.pid_t
	var pgid C.pid_t
	var euid C.uint
	var ruid C.uint
	n := int(C.lb_proc_restricted(C.pid_t(pid), &ppid, &pgid, &euid, &ruid, (*C.char)(unsafe.Pointer(&path[0])), C.int(len(path))))
	if n <= 0 || n > len(path) {
		return processEvidence{}, errors.New("process information unavailable")
	}
	if !restrictedAcquisitionAllowed(int(euid), int(ruid), os.Getuid()) {
		return processEvidence{}, errors.New("process information unavailable")
	}
	return processEvidence{
		PID: pid, ParentPID: int(ppid), GroupID: int(pgid),
		LoadedPath: string(path[:n]), Restricted: true,
	}, nil
}

// restrictedAcquisitionAllowed pins the narrowest predicate for the fallback:
// the target runs with root effective uid on top of the caller's real uid -
// the shape of a setuid system sudo the caller's own tree launched.
func restrictedAcquisitionAllowed(euid, ruid, selfUID int) bool {
	return euid == 0 && ruid == selfUID
}

func containsArgument(argv []string, target string) bool {
	for _, arg := range argv {
		if arg == target {
			return true
		}
	}
	return false
}
