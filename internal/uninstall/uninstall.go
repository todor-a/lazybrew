package uninstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"lazybrew/internal/brew"
)

const (
	askpassModeKey   = "LAZYBREW_ASKPASS_MODE"
	askpassSocketKey = "LAZYBREW_ASKPASS_SOCKET"
	sudoAskpassKey   = "SUDO_ASKPASS"

	protocolTimeout = 2 * time.Second
	passwordTimeout = 5 * time.Minute
	cleanupPhase    = 2 * time.Second
	cleanupLimit    = 8 * time.Second
)

type RequestID [16]byte

type EventType uint8

const PasswordRequested EventType = 1

type Event struct {
	Type      EventType
	RequestID RequestID
}

type Result struct {
	Err          error
	CleanupErr   error
	Cancelled    bool
	AuthFailed   bool
	AuthTimedOut bool
}

type Job interface {
	Events() <-chan Event
	RespondPassword(RequestID, []byte) error
	CancelPassword(RequestID) error
	Cancel()
	Wait() Result
}

type Uninstaller interface {
	Start(context.Context, brew.Package) (Job, error)
}

type uninstaller struct {
	executable  string
	identity    []byte
	identityErr error
}

var (
	createPrivateEndpoint   = createEndpoint
	prepareUninstall        = brew.PrepareUninstall
	acquireEvidence         = acquirePeerEvidence
	verifyEvidence          = verifyPeerEvidence
	peerStillAuthenticated  = peerRemainsAuthenticated
	authenticationTimeout   = passwordTimeout
	peerCheckInterval       = 50 * time.Millisecond
	setCoreLimit            = disableCoreDumps
	signalTrackedGroup      = signalGroup
	observeTrackedGroupGone = waitGroupGone
	observeKnownGone        = knownProcessesGone
	waitCommand             = func(cmd *exec.Cmd) error { return cmd.Wait() }
	killDirectChild         = func(cmd *exec.Cmd) error { return cmd.Process.Kill() }
	notifyChildDone         = func(done chan struct{}) { close(done) }
	notifyWorkersDone       = func(done chan struct{}) { close(done) }
	cleanupTimeout          = cleanupLimit
)

// DisableCoreDumps must be called by normal startup before any TTY handling.
// A failure is also remembered by New and causes future authentication to fail closed.
func DisableCoreDumps() error {
	err := setCoreLimit()
	coreState.mu.Lock()
	coreState.called = true
	coreState.err = err
	coreState.mu.Unlock()
	return err
}

var coreState struct {
	mu     sync.Mutex
	called bool
	err    error
}

func New() Uninstaller {
	path, err := resolvedExecutable()
	if err != nil {
		return &uninstaller{identityErr: err}
	}
	identity, identityErr := loadedCodeIdentity(os.Getpid())
	return &uninstaller{executable: path, identity: identity, identityErr: identityErr}
}

func (u *uninstaller) Start(parent context.Context, pkg brew.Package) (Job, error) {
	endpoint, err := createPrivateEndpoint()
	if err != nil {
		return nil, fmt.Errorf("Could not start uninstall: %w", err)
	}

	baseEnv := os.Environ()
	env, err := canonicalEnvironment(baseEnv, u.executable, endpoint.socketPath)
	if err != nil {
		cleanupErr := endpoint.closeExact()
		return nil, fmt.Errorf("Could not start uninstall: %w", errors.Join(err, cleanupErr))
	}
	prepared, err := prepareUninstall(env, pkg)
	if err != nil {
		return nil, errors.Join(err, endpoint.closeExact())
	}

	ctx, cancel := context.WithCancel(parent)
	processCtx, processCancel := context.WithCancel(context.WithoutCancel(parent))
	j := &job{
		ctx:           ctx,
		cancel:        cancel,
		processCancel: processCancel,
		endpoint:      endpoint,
		events:        make(chan Event),
		done:          make(chan struct{}),
		childDone:     make(chan struct{}),
		workersDone:   make(chan struct{}),
		acceptReady:   make(chan struct{}),
		processReady:  make(chan struct{}),
		executable:    u.executable,
		identity:      append([]byte(nil), u.identity...),
		identityErr:   u.authenticationCapabilityError(),
	}
	j.cmd = exec.CommandContext(processCtx, prepared.Path, prepared.Args...)
	j.cmd.Env = env
	j.cmd.Stdout = &j.stdout
	j.cmd.Stderr = &j.stderr
	j.cmd.WaitDelay = 2 * time.Second
	j.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	j.startWorker(j.acceptLoop)
	<-j.acceptReady
	if err := j.cmd.Start(); err != nil {
		cancel()
		processCancel()
		_ = endpoint.listener.Close()
		close(j.processReady)
		j.armWorkers()
		var cleanupErr error
		if !waitBefore(j.workersDone, time.Now().Add(cleanupTimeout)) {
			cleanupErr = errors.New("fatal uninstall cleanup failure: workers did not stop before cleanup deadline")
		}
		return nil, errors.Join(brew.MapCommandFailure(err, nil, nil), endpoint.closeExact(), cleanupErr)
	}
	j.childPID = j.cmd.Process.Pid
	j.pgid = j.childPID
	close(j.processReady)

	j.startWorker(j.waitChild)
	j.armWorkers()
	go j.finish()
	return j, nil
}

func (u *uninstaller) authenticationCapabilityError() error {
	if u.identityErr != nil || u.executable == "" || len(u.identity) == 0 {
		return errAuthentication
	}
	coreState.mu.Lock()
	defer coreState.mu.Unlock()
	if !coreState.called || coreState.err != nil {
		return errAuthentication
	}
	return nil
}

type job struct {
	ctx           context.Context
	cancel        context.CancelFunc
	processCancel context.CancelFunc
	endpoint      *privateEndpoint
	cmd           *exec.Cmd
	stdout        bytes.Buffer
	stderr        bytes.Buffer

	childPID    int
	pgid        int
	executable  string
	identity    []byte
	identityErr error

	events       chan Event
	done         chan struct{}
	childDone    chan struct{}
	workersDone  chan struct{}
	acceptReady  chan struct{}
	processReady chan struct{}

	workerMu          sync.Mutex
	workerCount       int
	workersArmed      bool
	workersDoneClosed bool

	mu              sync.Mutex
	active          *request
	live            *net.UnixConn
	knownPIDs       map[int]struct{}
	cancelled       bool
	authFailed      bool
	authTimedOut    bool
	resultFinalized bool
	result          Result
	cleanupOnce     sync.Once
}

type request struct {
	id   RequestID
	conn *net.UnixConn
	done chan struct{}
	once sync.Once
}

func (j *job) Events() <-chan Event { return j.events }

func (j *job) RespondPassword(id RequestID, password []byte) error {
	defer wipe(password)
	if len(password) > maxPasswordBytes || !utf8.Valid(password) {
		return errRequestUnavailable
	}
	j.mu.Lock()
	r := j.active
	if r == nil || r.id != id || j.cancelled || j.ctx.Err() != nil {
		j.mu.Unlock()
		return errRequestUnavailable
	}
	j.active = nil
	j.mu.Unlock()
	err := r.respond(messagePassword, password)
	j.mu.Lock()
	if j.live == r.conn {
		j.live = nil
	}
	j.mu.Unlock()
	if err != nil {
		j.failAuthentication(false)
	}
	return err
}

func (j *job) CancelPassword(id RequestID) error {
	j.mu.Lock()
	r := j.active
	if r == nil || r.id != id {
		j.mu.Unlock()
		return errRequestUnavailable
	}
	j.cancelled = true
	j.mu.Unlock()
	j.cancel()
	_ = j.endpoint.listener.Close()
	return nil
}

func (j *job) Cancel() {
	j.mu.Lock()
	j.cancelled = true
	j.mu.Unlock()
	j.cancel()
	_ = j.endpoint.listener.Close()
}

func (j *job) Wait() Result {
	<-j.done
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.result
}

func (j *job) startWorker(run func()) {
	j.workerMu.Lock()
	if j.workersDoneClosed {
		j.workerMu.Unlock()
		panic("uninstall worker started after completion")
	}
	j.workerCount++
	j.workerMu.Unlock()
	go func() {
		defer j.finishWorker()
		run()
	}()
}

func (j *job) armWorkers() {
	j.workerMu.Lock()
	j.workersArmed = true
	j.closeWorkersDoneLocked()
	j.workerMu.Unlock()
}

func (j *job) finishWorker() {
	j.workerMu.Lock()
	j.workerCount--
	j.closeWorkersDoneLocked()
	j.workerMu.Unlock()
}

func (j *job) closeWorkersDoneLocked() {
	if j.workersArmed && j.workerCount == 0 && !j.workersDoneClosed {
		j.workersDoneClosed = true
		notifyWorkersDone(j.workersDone)
	}
}

func (j *job) waitChild() {
	err := waitCommand(j.cmd)
	j.mu.Lock()
	if !j.resultFinalized {
		j.result.Err = brew.MapCommandFailure(err, j.stdout.Bytes(), j.stderr.Bytes())
	}
	j.mu.Unlock()
	notifyChildDone(j.childDone)
}

func (j *job) finish() {
	select {
	case <-j.childDone:
	case <-j.ctx.Done():
		j.mu.Lock()
		if !j.authFailed && !j.authTimedOut {
			j.cancelled = true
		}
		j.mu.Unlock()
	}
	workersStopped := false
	j.cleanupOnce.Do(func() {
		var cleanupErr error
		cleanupErr, workersStopped = j.cleanup()
		j.mu.Lock()
		if j.cancelled || j.authFailed || j.authTimedOut {
			j.result.Err = nil
		}
		j.result.Cancelled = j.cancelled && !j.authFailed && !j.authTimedOut
		j.result.AuthFailed = j.authFailed
		j.result.AuthTimedOut = j.authTimedOut
		j.result.CleanupErr = cleanupErr
		j.resultFinalized = true
		j.mu.Unlock()
	})
	if workersStopped {
		close(j.events)
	}
	close(j.done)
}

func (j *job) cleanup() (error, bool) {
	deadline := time.Now().Add(cleanupTimeout)
	j.cancel()
	_ = j.endpoint.listener.Close()

	var errs []error
	if err := signalTrackedGroup(j.pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		errs = append(errs, fmt.Errorf("SIGTERM process-group signal failed: %w", err))
	}
	groupGone := false
	if phase := cleanupPhaseBefore(deadline); phase > 0 {
		groupGone = observeTrackedGroupGone(j.pgid, phase)
	}
	if !groupGone {
		if err := signalTrackedGroup(j.pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("SIGKILL process-group signal failed: %w", err))
		}
		if phase := cleanupPhaseBefore(deadline); phase <= 0 || !observeTrackedGroupGone(j.pgid, phase) {
			errs = append(errs, errors.New("process group still present after SIGKILL"))
		}
	}

	if err := killDirectChild(j.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		errs = append(errs, fmt.Errorf("direct child kill could not be confirmed: %w", err))
	}
	j.processCancel()
	if !waitBefore(j.childDone, deadline) {
		errs = append(errs, errors.New("direct child Wait did not complete before cleanup deadline"))
	}
	workersStopped := waitBefore(j.workersDone, deadline)
	if !workersStopped {
		errs = append(errs, errors.New("uninstall workers did not stop before cleanup deadline"))
	}

	j.mu.Lock()
	known := make([]int, 0, len(j.knownPIDs))
	for pid := range j.knownPIDs {
		known = append(known, pid)
	}
	j.mu.Unlock()
	if err := observeKnownGone(known); err != nil {
		errs = append(errs, fmt.Errorf("known descendant cleanup could not be confirmed: %w", err))
	}
	if err := j.endpoint.closeExact(); err != nil {
		errs = append(errs, fmt.Errorf("private endpoint cleanup failed: %w", err))
	}
	if len(errs) == 0 {
		return nil, workersStopped
	}
	return fmt.Errorf("fatal uninstall cleanup failure: %w", errors.Join(errs...)), workersStopped
}

func cleanupPhaseBefore(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining < cleanupPhase {
		return remaining
	}
	return cleanupPhase
}

func waitBefore(done <-chan struct{}, deadline time.Time) bool {
	select {
	case <-done:
		return true
	default:
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (j *job) acceptLoop() {
	close(j.acceptReady)
	for {
		conn, err := j.endpoint.listener.AcceptUnix()
		if err != nil {
			return
		}
		j.mu.Lock()
		busy := j.live != nil || j.active != nil
		if !busy {
			j.live = conn
		}
		j.mu.Unlock()
		if busy {
			_ = conn.Close()
			j.failAuthentication(false)
			continue
		}
		j.startWorker(func() { j.handleConnection(conn) })
	}
}

func (j *job) handleConnection(conn *net.UnixConn) {
	defer func() {
		_ = conn.Close()
		j.mu.Lock()
		if j.live == conn {
			j.live = nil
		}
		j.mu.Unlock()
	}()
	id, err := readRequest(conn)
	if err != nil {
		j.failAuthentication(false)
		return
	}
	<-j.processReady
	if j.childPID <= 1 || j.identityErr != nil {
		_ = writeTerminal(conn, id, messageError, nil)
		j.failAuthentication(false)
		return
	}
	evidence, err := acquireEvidence(conn, j.childPID, j.pgid, j.executable, j.identity)
	if err != nil || verifyEvidence(evidence) != nil {
		_ = writeTerminal(conn, id, messageError, nil)
		j.failAuthentication(false)
		return
	}

	r := &request{id: id, conn: conn, done: make(chan struct{})}
	j.mu.Lock()
	if j.cancelled || j.active != nil {
		typ := j.terminalResponseLocked()
		j.mu.Unlock()
		_ = r.respond(typ, nil)
		return
	}
	j.active = r
	if j.knownPIDs == nil {
		j.knownPIDs = make(map[int]struct{})
	}
	for _, p := range evidence.Chain {
		j.knownPIDs[p.PID] = struct{}{}
	}
	j.mu.Unlock()

	select {
	case j.events <- Event{Type: PasswordRequested, RequestID: id}:
	case <-j.ctx.Done():
		j.clearAndRespond(r, j.terminalResponse())
		return
	}

	timer := time.NewTimer(authenticationTimeout)
	ticker := time.NewTicker(peerCheckInterval)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			if j.clearAndRespond(r, messageError) {
				j.failAuthentication(true)
			}
			return
		case <-ticker.C:
			if !peerStillAuthenticated(evidence) && j.clearAndRespond(r, messageError) {
				j.failAuthentication(false)
				return
			}
		case <-j.ctx.Done():
			j.clearAndRespond(r, j.terminalResponse())
			return
		case <-r.done:
			return
		}
	}
}

func (j *job) terminalResponse() byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.terminalResponseLocked()
}

func (j *job) terminalResponseLocked() byte {
	if j.authFailed || j.authTimedOut {
		return messageError
	}
	return messageCancel
}

func (j *job) clearAndRespond(r *request, typ byte) bool {
	j.mu.Lock()
	owned := j.active == r
	if owned {
		j.active = nil
	}
	j.mu.Unlock()
	if owned {
		_ = r.respond(typ, nil)
	}
	return owned
}

func (j *job) failAuthentication(timeout bool) {
	j.mu.Lock()
	if timeout {
		j.authTimedOut = true
	} else {
		j.authFailed = true
	}
	j.cancelled = true
	j.mu.Unlock()
	j.cancel()
	_ = j.endpoint.listener.Close()
}

func (r *request) respond(typ byte, payload []byte) error {
	var err error
	r.once.Do(func() {
		err = writeTerminal(r.conn, r.id, typ, payload)
		_ = r.conn.Close()
		close(r.done)
	})
	return err
}

func canonicalEnvironment(base []string, executable, socketPath string) ([]string, error) {
	if executable == "" || !filepath.IsAbs(executable) || socketPath == "" || !filepath.IsAbs(socketPath) {
		return nil, errors.New("invalid askpass routing metadata")
	}
	env := make([]string, 0, len(base)+3)
	for _, entry := range base {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		switch key {
		case sudoAskpassKey, askpassModeKey, askpassSocketKey:
			continue
		default:
			env = append(env, entry)
		}
	}
	return append(env,
		sudoAskpassKey+"="+executable,
		askpassModeKey+"=1",
		askpassSocketKey+"="+socketPath,
	), nil
}

func resolvedExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 1 {
		return errors.New("invalid process group")
	}
	return syscall.Kill(-pgid, sig)
}

func waitGroupGone(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if err != nil {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func knownProcessesGone(pids []int) error {
	for _, pid := range pids {
		if pid <= 1 {
			return errors.New("invalid known descendant")
		}
		err := syscall.Kill(pid, 0)
		if err == nil {
			return errors.New("known descendant still present")
		}
		if !errors.Is(err, syscall.ESRCH) {
			return errors.New("known descendant could not be inspected")
		}
	}
	return nil
}

func wipe(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}

var (
	errAuthentication     = errors.New("administrator authentication failed")
	errRequestUnavailable = errors.New("password request is unavailable")
)
