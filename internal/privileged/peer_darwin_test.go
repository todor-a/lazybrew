package privileged

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDarwinProcessGroupInspectionFindsLiveMember(t *testing.T) {
	live, err := processGroupHasLiveMembers(syscall.Getpgrp())
	if err != nil {
		t.Fatal(err)
	}
	if !live {
		t.Fatal("current live process group reported gone")
	}
}

func TestDarwinProcessGroupInspectionReportsAcquisitionError(t *testing.T) {
	if _, err := processGroupHasLiveMembers(1); err == nil {
		t.Fatal("invalid process group inspection succeeded")
	}
}

func TestWaitGroupGoneDistinguishesKernelObservation(t *testing.T) {
	oldInspect := inspectTrackedGroup
	t.Cleanup(func() { inspectTrackedGroup = oldInspect })

	cases := []struct {
		name string
		live bool
		err  error
		want bool
	}{
		{name: "live member", live: true, want: false},
		{name: "reused live group", live: true, want: false},
		{name: "zombie-only group", want: true},
		{name: "empty group", want: true},
		{name: "acquisition failure", err: errors.New("sysctl failed"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inspectTrackedGroup = func(pgid int) (bool, error) {
				if pgid != 4242 {
					t.Fatalf("inspected pgid %d, want 4242", pgid)
				}
				return tc.live, tc.err
			}
			if got := waitGroupGone(4242, 0); got != tc.want {
				t.Fatalf("waitGroupGone returned %v, want %v", got, tc.want)
			}
		})
	}
}

func validPeerEvidence() peerEvidence {
	return peerEvidence{
		ExpectedUID:      501,
		PeerUID:          501,
		PeerPID:          10,
		ExpectedPath:     "/Applications/lazybrew",
		PeerLoadedPath:   "/Applications/lazybrew",
		ExpectedIdentity: []byte{1, 2, 3},
		PeerIdentity:     []byte{1, 2, 3},
		TrackedPID:       40,
		TrackedGroupID:   30,
		Chain: []processEvidence{
			{PID: 10, ParentPID: 20, GroupID: 30, LoadedPath: "/Applications/lazybrew"},
			{PID: 20, ParentPID: 40, GroupID: 30, LoadedPath: "/usr/bin/sudo", Argv: []string{"sudo", "-A", "prompt"}},
			{PID: 40, ParentPID: 1, GroupID: 30, LoadedPath: "/opt/homebrew/bin/brew"},
		},
	}
}

func cloneEvidence(src peerEvidence) peerEvidence {
	dst := src
	dst.ExpectedIdentity = append([]byte(nil), src.ExpectedIdentity...)
	dst.PeerIdentity = append([]byte(nil), src.PeerIdentity...)
	dst.Chain = append([]processEvidence(nil), src.Chain...)
	for i := range dst.Chain {
		dst.Chain[i].Argv = append([]string(nil), src.Chain[i].Argv...)
	}
	return dst
}

func TestVerifyPeerEvidence(t *testing.T) {
	if err := verifyPeerEvidence(validPeerEvidence()); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}

	cases := map[string]func(*peerEvidence){
		"uid":                   func(e *peerEvidence) { e.PeerUID++ },
		"pid":                   func(e *peerEvidence) { e.PeerPID = 0 },
		"loaded path":           func(e *peerEvidence) { e.PeerLoadedPath = "/replaced/lazybrew" },
		"chain path mismatch":   func(e *peerEvidence) { e.Chain[0].LoadedPath = "/replaced/lazybrew" },
		"code identity":         func(e *peerEvidence) { e.PeerIdentity[0]++ },
		"sudo path":             func(e *peerEvidence) { e.Chain[1].LoadedPath = "/tmp/sudo" },
		"sudo flag":             func(e *peerEvidence) { e.Chain[1].Argv = []string{"sudo", "-Ax"} },
		"peer group":            func(e *peerEvidence) { e.Chain[0].GroupID++ },
		"sudo group":            func(e *peerEvidence) { e.Chain[1].GroupID++ },
		"tracked group":         func(e *peerEvidence) { e.Chain[2].GroupID++ },
		"broken ancestry":       func(e *peerEvidence) { e.Chain[1].ParentPID = 99 },
		"loop":                  func(e *peerEvidence) { e.Chain[2].PID = e.Chain[0].PID },
		"missing tracked child": func(e *peerEvidence) { e.TrackedPID = 99 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			evidence := cloneEvidence(validPeerEvidence())
			mutate(&evidence)
			if err := verifyPeerEvidence(evidence); err == nil {
				t.Fatal("untrusted evidence accepted")
			}
		})
	}
}

func TestVerifierRejectsAmbiguousOrOverlongAncestry(t *testing.T) {
	evidence := validPeerEvidence()
	evidence.Chain = make([]processEvidence, 65)
	for i := range evidence.Chain {
		evidence.Chain[i] = processEvidence{PID: i + 10, ParentPID: i + 11, GroupID: 30, LoadedPath: "/x"}
	}
	evidence.Chain[0].PID = evidence.PeerPID
	evidence.Chain[0].LoadedPath = evidence.PeerLoadedPath
	evidence.Chain[1].LoadedPath = "/usr/bin/sudo"
	evidence.Chain[1].Argv = []string{"sudo", "-A"}
	evidence.TrackedPID = evidence.Chain[len(evidence.Chain)-1].PID
	if err := verifyPeerEvidence(evidence); err == nil {
		t.Fatal("overlong ancestry accepted")
	}
}

// A restricted sudo link - kernel refuses proc_pidinfo/argv against setuid
// root, so the node carries path, lineage, and pgid but no argv - must pass,
// and the excuse must stay scoped to exactly that link.
func TestVerifierAcceptsRestrictedSudoAndNothingElseRestricted(t *testing.T) {
	evidence := cloneEvidence(validPeerEvidence())
	evidence.Chain[1].Argv = nil
	evidence.Chain[1].Restricted = true
	if err := verifyPeerEvidence(evidence); err != nil {
		t.Fatalf("restricted sudo rejected: %v", err)
	}

	cases := map[string]func(*peerEvidence){
		// Without the Restricted mark, a missing -A stays fatal.
		"unrestricted sudo without argv": func(e *peerEvidence) { e.Chain[1].Argv = nil },
		// The excuse never applies to the peer or the tracked child.
		"restricted peer":          func(e *peerEvidence) { e.Chain[0].Restricted = true },
		"restricted tracked child": func(e *peerEvidence) { e.Chain[2].Restricted = true },
		// A restricted link still answers for its kernel-reported path.
		"restricted sudo with foreign path": func(e *peerEvidence) {
			e.Chain[1].Argv = nil
			e.Chain[1].Restricted = true
			e.Chain[1].LoadedPath = "/tmp/sudo"
		},
		// And for its process group.
		"restricted sudo outside the tracked group": func(e *peerEvidence) {
			e.Chain[1].Argv = nil
			e.Chain[1].Restricted = true
			e.Chain[1].GroupID++
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			evidence := cloneEvidence(validPeerEvidence())
			mutate(&evidence)
			if err := verifyPeerEvidence(evidence); err == nil {
				t.Fatal("untrusted evidence accepted")
			}
		})
	}
}

// The fallback predicate: only a setuid-root process carrying the caller's
// own real uid may be acquired restricted.
func TestRestrictedAcquisitionPredicate(t *testing.T) {
	cases := []struct {
		name             string
		euid, ruid, self int
		want             bool
	}{
		{name: "setuid sudo we launched", euid: 0, ruid: 502, self: 502, want: true},
		{name: "root process of another user", euid: 0, ruid: 501, self: 502, want: false},
		{name: "ordinary same-uid process", euid: 502, ruid: 502, self: 502, want: false},
		{name: "foreign non-root process", euid: 501, ruid: 501, self: 502, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := restrictedAcquisitionAllowed(tc.euid, tc.ruid, tc.self); got != tc.want {
				t.Fatalf("restrictedAcquisitionAllowed(%d,%d,%d)=%v, want %v", tc.euid, tc.ruid, tc.self, got, tc.want)
			}
		})
	}
}

// The live gate: this test process is not setuid root, so the restricted
// path must refuse it - the fallback can never widen acquisition for
// ordinary processes.
func TestRestrictedAcquisitionRefusesNonRootTargets(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, primary reads never fail")
	}
	if _, err := acquireRestrictedProcess(os.Getpid()); err == nil {
		t.Fatal("restricted acquisition accepted an ordinary same-uid process")
	}
}

// A node flipping between restricted and unrestricted between the first walk
// and the re-verification is a change of evidence, not the same process.
func TestSameProcessEvidenceDistinguishesRestricted(t *testing.T) {
	left := processEvidence{PID: 20, ParentPID: 40, GroupID: 30, LoadedPath: "/usr/bin/sudo", Restricted: true}
	right := left
	right.Restricted = false
	if sameProcessEvidence(left, right, false) {
		t.Fatal("restricted flip read as the same process")
	}
}

func TestDarwinPeerAcquisitionFailsClosedForDirectConnector(t *testing.T) {
	path := shortSocketPath(t, "peer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	uid, pid, err := socketPeer(server)
	if err != nil {
		t.Fatal(err)
	}
	if uid != os.Geteuid() || pid != os.Getpid() {
		t.Fatalf("kernel peer mismatch: uid=%d pid=%d", uid, pid)
	}
	expectedPath, err := resolvedExecutable()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := loadedCodeIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := acquirePeerEvidence(server, os.Getpid(), syscall.Getpgrp(), expectedPath, identity)
	if err != nil {
		t.Fatal(err)
	}
	loadedPath, err := filepath.EvalSymlinks(evidence.PeerLoadedPath)
	if err != nil {
		t.Fatal(err)
	}
	if loadedPath != expectedPath || !bytes.Equal(evidence.PeerIdentity, identity) {
		t.Fatal("loaded image evidence does not match the running process")
	}
	if err := verifyPeerEvidence(evidence); err == nil {
		t.Fatal("same-UID connector without sudo ancestry authenticated")
	}
}

func TestDarwinIdentityChild(t *testing.T) {
	if os.Getenv("LAZYBREW_IDENTITY_TEST_CHILD") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestDarwinLoadedIdentitySurvivesPathReplacement(t *testing.T) {
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	path := filepath.Join(t.TempDir(), "identity-child")
	destination, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(path, "-test.run=^TestDarwinIdentityChild$")
	cmd.Env = append(os.Environ(), "LAZYBREW_IDENTITY_TEST_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	before, err := loadedCodeIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	renamed := path + ".running"
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := loadedCodeIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("loaded identity followed the replaced pathname")
	}
}

func TestDarwinDistinctLoadedImagesHaveDistinctIdentityAndRejectSubstitution(t *testing.T) {
	ownIdentity, err := loadedCodeIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(ownIdentity)

	other := exec.Command("/bin/sleep", "30")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	running := true
	defer func() {
		if running {
			_ = other.Process.Kill()
			_ = other.Wait()
		}
	}()
	otherIdentity, err := loadedCodeIdentity(other.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(otherIdentity)
	if bytes.Equal(ownIdentity, otherIdentity) {
		t.Fatal("distinct loaded images reported the same dynamic code identity")
	}

	evidence := validPeerEvidence()
	evidence.ExpectedIdentity = append([]byte(nil), ownIdentity...)
	evidence.PeerIdentity = append([]byte(nil), otherIdentity...)
	if err := verifyPeerEvidence(evidence); err == nil {
		t.Fatal("substituted loaded image authenticated by pathname")
	}

	peer, err := acquireProcess(other.Process.Pid, false)
	if err != nil {
		t.Fatal(err)
	}
	monitorEvidence := peerEvidence{
		PeerPID:      other.Process.Pid,
		PeerIdentity: append([]byte(nil), otherIdentity...),
		Chain:        []processEvidence{peer},
	}
	if !peerRemainsAuthenticated(monitorEvidence) {
		t.Fatal("live authenticated image was reported dead")
	}
	if err := other.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := other.Wait(); err == nil {
		t.Fatal("killed image exited successfully")
	}
	running = false
	deadline := time.Now().Add(time.Second)
	for peerRemainsAuthenticated(monitorEvidence) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if peerRemainsAuthenticated(monitorEvidence) {
		t.Fatal("dead authenticated image remained live")
	}
}
