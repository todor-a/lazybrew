package uninstall

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"lazybrew/internal/brew"
)

func installFakeBrew(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "brew")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestStartSetupFailureIsMapped(t *testing.T) {
	old := createPrivateEndpoint
	createPrivateEndpoint = func() (*privateEndpoint, error) { return nil, errors.New("listener unavailable") }
	t.Cleanup(func() { createPrivateEndpoint = old })

	_, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err == nil || err.Error() != "Could not start uninstall: listener unavailable" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartPreparationFailureCleansEndpoint(t *testing.T) {
	oldPrepare := prepareUninstall
	var endpoint *privateEndpoint
	oldEndpoint := createPrivateEndpoint
	createPrivateEndpoint = func() (*privateEndpoint, error) {
		var err error
		endpoint, err = createEndpoint()
		return endpoint, err
	}
	prepareUninstall = func([]string, brew.Package) (brew.ResolvedCommand, error) {
		return brew.ResolvedCommand{}, errors.New("prepare failed")
	}
	t.Cleanup(func() {
		prepareUninstall = oldPrepare
		createPrivateEndpoint = oldEndpoint
	})

	_, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err == nil || err.Error() != "prepare failed" {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint == nil {
		t.Fatal("endpoint was not created")
	}
	if _, err := os.Lstat(endpoint.dirPath); !os.IsNotExist(err) {
		t.Fatal("partial endpoint remains after preparation failure")
	}
}

func TestStartEnvironmentAndChildFailuresCleanEndpoint(t *testing.T) {
	t.Run("environment", func(t *testing.T) {
		var endpoint *privateEndpoint
		oldEndpoint := createPrivateEndpoint
		createPrivateEndpoint = func() (*privateEndpoint, error) {
			var err error
			endpoint, err = createEndpoint()
			return endpoint, err
		}
		t.Cleanup(func() { createPrivateEndpoint = oldEndpoint })
		_, err := (&uninstaller{}).Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
		if err == nil || !strings.HasPrefix(err.Error(), "Could not start uninstall: ") {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Lstat(endpoint.dirPath); !os.IsNotExist(err) {
			t.Fatal("endpoint remains after environment failure")
		}
	})

	t.Run("child start", func(t *testing.T) {
		var endpoint *privateEndpoint
		oldEndpoint := createPrivateEndpoint
		oldPrepare := prepareUninstall
		createPrivateEndpoint = func() (*privateEndpoint, error) {
			var err error
			endpoint, err = createEndpoint()
			return endpoint, err
		}
		prepareUninstall = func([]string, brew.Package) (brew.ResolvedCommand, error) {
			return brew.ResolvedCommand{Path: "/definitely/missing/lazybrew-brew"}, nil
		}
		t.Cleanup(func() {
			createPrivateEndpoint = oldEndpoint
			prepareUninstall = oldPrepare
		})
		_, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
		if err == nil || err.Error() != "Homebrew is not installed or brew is not on PATH" {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Lstat(endpoint.dirPath); !os.IsNotExist(err) {
			t.Fatal("endpoint remains after child-start failure")
		}
	})
}

func TestJobSuccessFailureAndIdempotentWait(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		installFakeBrew(t, "exit 0")
		startedJob, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
		if err != nil {
			t.Fatal(err)
		}
		realJob := startedJob.(*job)
		result := startedJob.Wait()
		if result.Err != nil || result.CleanupErr != nil || result.Cancelled || result.AuthFailed || result.AuthTimedOut {
			t.Fatalf("unexpected result: %+v", result)
		}
		again := startedJob.Wait()
		if again.Err != nil || again.CleanupErr != nil {
			t.Fatalf("second Wait changed result: %+v", again)
		}
		if _, open := <-startedJob.Events(); open {
			t.Fatal("events channel remains open")
		}
		if err := syscall.Kill(realJob.childPID, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("direct child remains inspectable: %v", err)
		}
		select {
		case <-realJob.workersDone:
		default:
			t.Fatal("owned workers remain")
		}
		if _, err := realJob.endpoint.listener.AcceptUnix(); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("listener descriptor remains open: %v", err)
		}
		if _, err := os.Lstat(realJob.endpoint.dirPath); !os.IsNotExist(err) {
			t.Fatal("private endpoint remains")
		}
	})

	t.Run("command failure", func(t *testing.T) {
		installFakeBrew(t, "echo refused >&2\nexit 7")
		job, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Formula})
		if err != nil {
			t.Fatal(err)
		}
		result := job.Wait()
		if result.Err == nil || result.Err.Error() != "refused" || result.CleanupErr != nil {
			t.Fatalf("unexpected result: %+v", result)
		}
	})
}

func trustPeerForTest(t *testing.T) {
	t.Helper()
	oldAcquire, oldVerify, oldPeer := acquireEvidence, verifyEvidence, peerStillAuthenticated
	acquireEvidence = func(*net.UnixConn, int, int, string, []byte) (peerEvidence, error) {
		return peerEvidence{}, nil
	}
	verifyEvidence = func(peerEvidence) error { return nil }
	peerStillAuthenticated = func(peerEvidence) bool { return true }
	t.Cleanup(func() {
		acquireEvidence, verifyEvidence, peerStillAuthenticated = oldAcquire, oldVerify, oldPeer
	})
}

func receivePasswordEvent(t *testing.T, job Job) Event {
	t.Helper()
	select {
	case event := <-job.Events():
		if event.Type != PasswordRequested {
			t.Fatal("unexpected event type")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("password event was not emitted")
		return Event{}
	}
}

func TestAuthenticatedRequestsAreOnDemandAndFresh(t *testing.T) {
	trustPeerForTest(t)
	installFakeBrew(t, "/bin/sleep 30")
	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err != nil {
		t.Fatal(err)
	}
	realJob := started.(*job)
	realJob.identityErr = nil

	var previous RequestID
	for attempt, expected := range [][]byte{[]byte("first"), []byte{}} {
		response := make(chan []byte, 1)
		go func() {
			password, _ := helperExchange(realJob.endpoint.socketPath)
			response <- password
		}()
		event := receivePasswordEvent(t, started)
		if attempt > 0 && event.RequestID == previous {
			t.Fatal("retry reused a request ID")
		}
		previous = event.RequestID
		submitted := append([]byte(nil), expected...)
		if err := started.RespondPassword(event.RequestID, submitted); err != nil {
			t.Fatal(err)
		}
		for _, value := range submitted {
			if value != 0 {
				t.Fatal("submitted buffer was not wiped")
			}
		}
		got := <-response
		if !bytes.Equal(got, expected) {
			t.Fatal("helper received the wrong response")
		}
		wipe(got)
	}
	cancelled := make(chan error, 1)
	go func() {
		password, err := helperExchange(realJob.endpoint.socketPath)
		wipe(password)
		cancelled <- err
	}()
	event := receivePasswordEvent(t, started)
	if err := started.CancelPassword(event.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := <-cancelled; err == nil {
		t.Fatal("cancelled helper succeeded")
	}
	if result := started.Wait(); !result.Cancelled || result.CleanupErr != nil {
		t.Fatalf("unexpected terminal result: %+v", result)
	}
}

func TestAuthenticationRejectionAndTimeoutCancelJob(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		installFakeBrew(t, "/bin/sleep 30")
		oldAcquire, oldVerify := acquireEvidence, verifyEvidence
		acquireEvidence = func(*net.UnixConn, int, int, string, []byte) (peerEvidence, error) {
			return peerEvidence{}, nil
		}
		verifyEvidence = func(peerEvidence) error { return errAuthentication }
		t.Cleanup(func() { acquireEvidence, verifyEvidence = oldAcquire, oldVerify })
		started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
		if err != nil {
			t.Fatal(err)
		}
		started.(*job).identityErr = nil
		if password, err := helperExchange(started.(*job).endpoint.socketPath); err == nil || password != nil {
			wipe(password)
			t.Fatal("rejected helper received a password")
		}
		result := started.Wait()
		if !result.AuthFailed || result.CleanupErr != nil {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		trustPeerForTest(t)
		oldTimeout := authenticationTimeout
		authenticationTimeout = 20 * time.Millisecond
		t.Cleanup(func() { authenticationTimeout = oldTimeout })
		installFakeBrew(t, "/bin/sleep 30")
		started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
		if err != nil {
			t.Fatal(err)
		}
		realJob := started.(*job)
		realJob.identityErr = nil
		helperDone := make(chan error, 1)
		go func() {
			password, err := helperExchange(realJob.endpoint.socketPath)
			wipe(password)
			helperDone <- err
		}()
		_ = receivePasswordEvent(t, started)
		if err := <-helperDone; err == nil {
			t.Fatal("timed-out helper succeeded")
		}
		result := started.Wait()
		if !result.AuthTimedOut || result.CleanupErr != nil {
			t.Fatalf("unexpected result: %+v", result)
		}
	})
}

func TestJobCancelIsBoundedAndIdempotent(t *testing.T) {
	installFakeBrew(t, "trap '' TERM\nwhile :; do /bin/sleep 1; done")
	job, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	job.Cancel()
	job.Cancel()
	result := job.Wait()
	if !result.Cancelled || result.Err != nil || result.CleanupErr != nil {
		t.Fatalf("unexpected cancellation: %+v", result)
	}
	if time.Since(started) > 6*time.Second {
		t.Fatal("cancellation exceeded its bound")
	}
}

func TestCleanupAmbiguityIsExplicit(t *testing.T) {
	oldObserve := observeTrackedGroupGone
	observeTrackedGroupGone = func(int, time.Duration) bool { return false }
	t.Cleanup(func() { observeTrackedGroupGone = oldObserve })
	installFakeBrew(t, "/bin/sleep 30")
	job, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err != nil {
		t.Fatal(err)
	}
	job.Cancel()
	result := job.Wait()
	if result.CleanupErr == nil {
		t.Fatal("ambiguous process-group cleanup was reported as complete")
	}
}

func TestFatalCleanupIsBoundedWhenCompletionSignalsAreWithheld(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withhold func()
		want     string
	}{
		{
			name: "direct child Wait completion",
			withhold: func() {
				notifyChildDone = func(chan struct{}) {}
			},
			want: "direct child Wait did not complete before cleanup deadline",
		},
		{
			name: "worker completion",
			withhold: func() {
				notifyWorkersDone = func(chan struct{}) {}
			},
			want: "uninstall workers did not stop before cleanup deadline",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldTimeout := cleanupTimeout
			oldChildDone := notifyChildDone
			oldWorkersDone := notifyWorkersDone
			oldObserve := observeTrackedGroupGone
			cleanupTimeout = 100 * time.Millisecond
			observeTrackedGroupGone = func(int, time.Duration) bool { return true }
			tc.withhold()
			t.Cleanup(func() {
				cleanupTimeout = oldTimeout
				notifyChildDone = oldChildDone
				notifyWorkersDone = oldWorkersDone
				observeTrackedGroupGone = oldObserve
			})

			installFakeBrew(t, "trap '' TERM\nexec /bin/sleep 30")
			started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
			if err != nil {
				t.Fatal(err)
			}
			started.Cancel()
			resultDone := make(chan Result, 1)
			go func() { resultDone <- started.Wait() }()
			select {
			case result := <-resultDone:
				if result.CleanupErr == nil || !strings.Contains(result.CleanupErr.Error(), tc.want) {
					t.Fatalf("missing explicit fatal cleanup result: %+v", result)
				}
				if !strings.HasPrefix(result.CleanupErr.Error(), "fatal uninstall cleanup failure:") {
					t.Fatalf("cleanup failure is not marked fatal: %v", result.CleanupErr)
				}
			case <-time.After(time.Second):
				t.Fatal("Job.Wait exceeded its outer bound")
			}
		})
	}
}

func TestGroupSignalFailureIsFatalButDirectChildIsKilledAndWaited(t *testing.T) {
	oldSignal := signalTrackedGroup
	oldObserve := observeTrackedGroupGone
	oldWait := waitCommand
	var signals []syscall.Signal
	waitCalls := 0
	signalTrackedGroup = func(_ int, signal syscall.Signal) error {
		signals = append(signals, signal)
		return syscall.EPERM
	}
	observeTrackedGroupGone = func(int, time.Duration) bool { return false }
	waitCommand = func(cmd *exec.Cmd) error {
		waitCalls++
		return oldWait(cmd)
	}
	t.Cleanup(func() {
		signalTrackedGroup = oldSignal
		observeTrackedGroupGone = oldObserve
		waitCommand = oldWait
	})

	installFakeBrew(t, "trap '' TERM\nwhile :; do /bin/sleep 1; done")
	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Formula})
	if err != nil {
		t.Fatal(err)
	}
	realJob := started.(*job)
	started.Cancel()
	result := started.Wait()
	if result.CleanupErr == nil || !errors.Is(result.CleanupErr, syscall.EPERM) {
		t.Fatalf("signal ambiguity was not fatal: %+v", result)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v", signals)
	}
	if waitCalls != 1 {
		t.Fatalf("direct child Wait called %d times", waitCalls)
	}
	if err := syscall.Kill(realJob.childPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("direct child remains after fallback kill and Wait: %v", err)
	}
}

func TestRespondPasswordWritesDirectlyAndWipesCallerBuffer(t *testing.T) {
	path := shortSocketPath(t, "response.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	id := RequestID{1}
	r := &request{id: id, conn: server, done: make(chan struct{})}
	j := &job{ctx: context.Background(), active: r}
	password := []byte("not-retained")
	if err := j.RespondPassword(id, password); err != nil {
		t.Fatal(err)
	}
	if strings.Trim(string(password), "\x00") != "" {
		t.Fatal("caller password buffer was not wiped")
	}
	got, err := readFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(got.payload)
	if got.typ != messagePassword || got.id != id || string(got.payload) != "not-retained" {
		t.Fatal("wrong direct response")
	}
	if err := requireEOF(client); err != nil {
		t.Fatal("response was not terminated by EOF")
	}
}

func TestDisableCoreDumpsFailureIsRemembered(t *testing.T) {
	oldSetter := setCoreLimit
	coreState.mu.Lock()
	oldCalled, oldErr := coreState.called, coreState.err
	coreState.mu.Unlock()
	setCoreLimit = func() error { return errors.New("denied") }
	t.Cleanup(func() {
		setCoreLimit = oldSetter
		coreState.mu.Lock()
		coreState.called, coreState.err = oldCalled, oldErr
		coreState.mu.Unlock()
	})
	if DisableCoreDumps() == nil {
		t.Fatal("core-limit failure ignored")
	}
	u := New().(*uninstaller)
	if u.authenticationCapabilityError() == nil {
		t.Fatal("authentication remained enabled")
	}
}

func TestStartUsesCanonicalEnvironmentAndWaitDelay(t *testing.T) {
	installFakeBrew(t, "/bin/sleep 30")
	oldPrepare := prepareUninstall
	var captured []string
	prepareUninstall = func(env []string, pkg brew.Package) (brew.ResolvedCommand, error) {
		captured = append([]string(nil), env...)
		return brew.PrepareUninstall(env, pkg)
	}
	t.Cleanup(func() { prepareUninstall = oldPrepare })

	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err != nil {
		t.Fatal(err)
	}
	realJob := started.(*job)
	if realJob.cmd.WaitDelay != 2*time.Second {
		t.Fatalf("WaitDelay = %v", realJob.cmd.WaitDelay)
	}
	if realJob.cmd.Cancel == nil || realJob.processCancel == nil {
		t.Fatal("direct child is not owned by a dedicated CommandContext")
	}
	for _, key := range []string{sudoAskpassKey, askpassModeKey, askpassSocketKey} {
		if countKey(captured, key) != 1 {
			t.Fatalf("%s occurs %d times in preparation environment", key, countKey(captured, key))
		}
	}
	started.Cancel()
	if result := started.Wait(); !result.Cancelled || result.CleanupErr != nil {
		t.Fatalf("unexpected terminal result: %+v", result)
	}
}

func TestDirectChildWaitIsOwnedExactlyOnce(t *testing.T) {
	installFakeBrew(t, "exit 0")
	oldWait := waitCommand
	waitCalls := 0
	waitCommand = func(cmd *exec.Cmd) error {
		waitCalls++
		return oldWait(cmd)
	}
	t.Cleanup(func() { waitCommand = oldWait })

	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Formula})
	if err != nil {
		t.Fatal(err)
	}
	_ = started.Wait()
	_ = started.Wait()
	if waitCalls != 1 {
		t.Fatalf("direct child Wait called %d times", waitCalls)
	}
}

func TestCancelMethodsOnlyLatchAndHandlerOwnsTerminalFrame(t *testing.T) {
	endpoint, err := createEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.closeExact() })
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: endpoint.socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := endpoint.listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	id := RequestID{9}
	r := &request{id: id, conn: server, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ctx: ctx, cancel: cancel, endpoint: endpoint, active: r}

	started := time.Now()
	if err := j.CancelPassword(id); err != nil {
		t.Fatal(err)
	}
	j.Cancel()
	j.Cancel()
	if time.Since(started) > 50*time.Millisecond {
		t.Fatal("cancellation methods blocked")
	}
	if ctx.Err() == nil {
		t.Fatal("cancellation was not latched")
	}
	if err := client.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if _, err := client.Read(one[:]); err == nil {
		t.Fatal("CancelPassword wrote the terminal frame itself")
	}
	if !j.clearAndRespond(r, j.terminalResponse()) {
		t.Fatal("handler could not claim the active request")
	}
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	frame, err := readFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	if frame.typ != messageCancel || frame.id != id {
		t.Fatalf("unexpected terminal frame: %#v", frame)
	}
	if err := requireEOF(client); err != nil {
		t.Fatal("terminal response was not exact-once")
	}
}

func TestAuthenticatedPeerDeathFailsPromptly(t *testing.T) {
	installFakeBrew(t, "/bin/sleep 30")
	oldAcquire, oldVerify := acquireEvidence, verifyEvidence
	oldPeer, oldInterval := peerStillAuthenticated, peerCheckInterval
	acquireEvidence = func(*net.UnixConn, int, int, string, []byte) (peerEvidence, error) {
		return peerEvidence{}, nil
	}
	verifyEvidence = func(peerEvidence) error { return nil }
	peerStillAuthenticated = func(peerEvidence) bool { return false }
	peerCheckInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		acquireEvidence, verifyEvidence = oldAcquire, oldVerify
		peerStillAuthenticated, peerCheckInterval = oldPeer, oldInterval
	})

	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err != nil {
		t.Fatal(err)
	}
	realJob := started.(*job)
	realJob.identityErr = nil
	helperDone := make(chan error, 1)
	go func() {
		password, err := helperExchange(realJob.endpoint.socketPath)
		wipe(password)
		helperDone <- err
	}()
	_ = receivePasswordEvent(t, started)
	select {
	case err := <-helperDone:
		if err == nil {
			t.Fatal("dead authenticated helper received a password")
		}
	case <-time.After(time.Second):
		t.Fatal("peer death was not detected promptly")
	}
	if result := started.Wait(); !result.AuthFailed || result.CleanupErr != nil {
		t.Fatalf("unexpected terminal result: %+v", result)
	}
}

func TestInheritedDescriptorCannotHoldDirectWait(t *testing.T) {
	installFakeBrew(t, "(trap '' TERM; /bin/sleep 30) &\nexit 0")
	startedAt := time.Now()
	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Cask})
	if err != nil {
		t.Fatal(err)
	}
	result := started.Wait()
	if time.Since(startedAt) > 8*time.Second {
		t.Fatal("inherited descriptor exceeded the cleanup bound")
	}
	if result.Err == nil || !errors.Is(result.Err, exec.ErrWaitDelay) {
		t.Fatalf("descriptor retention was not reported: %+v", result)
	}
	if result.CleanupErr != nil {
		t.Fatalf("descriptor cleanup failed: %v", result.CleanupErr)
	}
}

func TestCleanupWaitsForBothMaximumProcessGroupPhases(t *testing.T) {
	oldSignal := signalTrackedGroup
	oldObserve := observeTrackedGroupGone
	oldKill := killDirectChild
	var signals []syscall.Signal
	killCalls := 0
	phaseCalls := make(chan time.Duration, 2)
	phaseRelease := make(chan bool)
	signalTrackedGroup = func(_ int, signal syscall.Signal) error {
		signals = append(signals, signal)
		return nil
	}
	observeTrackedGroupGone = func(_ int, phase time.Duration) bool {
		phaseCalls <- phase
		return <-phaseRelease
	}
	killDirectChild = func(cmd *exec.Cmd) error {
		killCalls++
		return oldKill(cmd)
	}
	t.Cleanup(func() {
		signalTrackedGroup = oldSignal
		observeTrackedGroupGone = oldObserve
		killDirectChild = oldKill
	})
	installFakeBrew(t, "exit 0")
	started, err := New().Start(context.Background(), brew.Package{Name: "safe", Kind: brew.Formula})
	if err != nil {
		t.Fatal(err)
	}
	resultDone := make(chan Result, 1)
	go func() { resultDone <- started.Wait() }()
	for phase := 1; phase <= 2; phase++ {
		select {
		case duration := <-phaseCalls:
			if duration != cleanupPhase {
				t.Fatalf("phase %d duration = %v", phase, duration)
			}
		case <-time.After(time.Second):
			t.Fatalf("phase %d did not start", phase)
		}
		select {
		case result := <-resultDone:
			t.Fatalf("job completed during phase %d: %+v", phase, result)
		default:
		}
		if killCalls != 0 {
			t.Fatalf("direct-child fallback ran during process-group phase %d", phase)
		}
		phaseRelease <- false
	}
	result := <-resultDone
	if result.CleanupErr == nil {
		t.Fatal("surviving group was not reported")
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v", signals)
	}
	if killCalls != 1 {
		t.Fatalf("direct-child fallback kill called %d times", killCalls)
	}
	if !strings.HasPrefix(result.CleanupErr.Error(), "fatal uninstall cleanup failure:") {
		t.Fatalf("cleanup failure is not marked fatal: %v", result.CleanupErr)
	}
}
