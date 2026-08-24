package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"lazybrew/internal/brew"
	"lazybrew/internal/info"
	"lazybrew/internal/privileged"
)

type fakeHomebrew struct {
	packages     map[brew.Kind][]brew.Package
	outdated     map[brew.Kind][]brew.OutdatedPackage
	outdatedErr  error
	err          error
	listStarted  chan struct{}
	listCalls    map[brew.Kind]int
	dependents   map[string][]string
	sizes        brew.Sizes
	sizesErr     error
	sizesCalls   int
	sizesStarted chan struct{}
}

func (f *fakeHomebrew) Sizes(ctx context.Context) (brew.Sizes, error) {
	f.sizesCalls++
	// A real du fails on an already-cancelled context; so must the fake, or a
	// cancellation bug is invisible to every test that uses it.
	if err := ctx.Err(); err != nil {
		return brew.Sizes{}, err
	}
	if f.sizesStarted != nil {
		close(f.sizesStarted)
		<-ctx.Done()
		return brew.Sizes{}, ctx.Err()
	}
	if f.sizesErr != nil {
		return brew.Sizes{}, f.sizesErr
	}
	return f.sizes, nil
}

func (f *fakeHomebrew) List(ctx context.Context, kind brew.Kind) ([]brew.Package, error) {
	if f.listCalls == nil {
		f.listCalls = make(map[brew.Kind]int)
	}
	f.listCalls[kind]++
	if f.listStarted != nil {
		close(f.listStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	packages := f.packages[kind]
	copyOfPackages := append([]brew.Package(nil), packages...)
	return copyOfPackages, nil
}

func (f *fakeHomebrew) Outdated(_ context.Context, kind brew.Kind) ([]brew.OutdatedPackage, error) {
	if f.outdatedErr != nil {
		return nil, f.outdatedErr
	}
	return append([]brew.OutdatedPackage(nil), f.outdated[kind]...), nil
}

func (f *fakeHomebrew) Info(_ context.Context, pkg brew.Package) (string, error) {
	return "details for " + pkg.Name, nil
}

func (f *fakeHomebrew) Uses(_ context.Context, pkg brew.Package) ([]string, error) {
	if pkg.Kind != brew.Formula {
		return nil, errors.New("dependents are only defined for formulae")
	}
	return f.dependents[pkg.Name], nil
}

type fakeUninstaller struct {
	startedOps   []brew.Operation
	job          *fakeJob
	starts       int
	started      []brew.Package
	err          error
	startStarted chan struct{}
}

func (f *fakeUninstaller) Start(ctx context.Context, op brew.Operation, pkg brew.Package) (privileged.Job, error) {
	f.starts++
	f.started = append(f.started, pkg)
	f.startedOps = append(f.startedOps, op)
	if f.startStarted != nil {
		close(f.startStarted)
		<-ctx.Done()
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.job, nil
}

type fakeJob struct {
	events      chan privileged.Event
	result      chan privileged.Result
	once        sync.Once
	mu          sync.Mutex
	passwords   [][]byte
	passwordIDs []privileged.RequestID
	terminal    *privileged.Result
}

// setTerminal programs the result this job terminates with, including when it is
// cancelled. A real job asked to stop during cleanup still reports whatever
// actually happened, so a cancel does not overwrite a programmed terminal result.
func (j *fakeJob) setTerminal(result privileged.Result) {
	j.mu.Lock()
	j.terminal = &result
	j.mu.Unlock()
}

func newFakeJob() *fakeJob {
	return &fakeJob{
		events: make(chan privileged.Event, 4),
		result: make(chan privileged.Result, 1),
	}
}

func (j *fakeJob) Events() <-chan privileged.Event { return j.events }
func (j *fakeJob) RespondPassword(id privileged.RequestID, password []byte) error {
	j.mu.Lock()
	j.passwordIDs = append(j.passwordIDs, id)
	j.passwords = append(j.passwords, append([]byte(nil), password...))
	j.mu.Unlock()
	for i := range password {
		password[i] = 0
	}
	return nil
}
func (j *fakeJob) CancelPassword(privileged.RequestID) error { return nil }
func (j *fakeJob) Cancel() {
	j.mu.Lock()
	programmed := j.terminal
	j.mu.Unlock()
	if programmed != nil {
		j.finish(*programmed)
		return
	}
	j.finish(privileged.Result{Cancelled: true})
}
func (j *fakeJob) Wait() privileged.Result { return <-j.result }
func (j *fakeJob) finish(result privileged.Result) {
	j.once.Do(func() {
		j.result <- result
		close(j.result)
		close(j.events)
	})
}

func newTestModel(t *testing.T) (*model, *fakeUninstaller) {
	t.Helper()
	homebrew := &fakeHomebrew{
		packages: map[brew.Kind][]brew.Package{
			brew.Cask: {
				{Name: "Alpha", Version: "1.0", Kind: brew.Cask},
				{Name: "Beta", Version: "2.0", Kind: brew.Cask},
				{Name: "Gamma", Version: "3.0", Kind: brew.Cask},
			},
			brew.Formula: {
				{Name: "Alpha", Version: "1.0", Kind: brew.Formula},
				{Name: "Beta", Version: "2.0", Kind: brew.Formula},
				{Name: "Gamma", Version: "3.0", Kind: brew.Formula},
			},
		},
		// Sizes are formula-only; see brew.Sizes.
		sizes: brew.Sizes{
			Formula: map[string]int64{"Alpha": 1024, "Beta": 5 * 1024 * 1024, "Gamma": 512},
			Total:   9687960,
		},
	}
	job := newFakeJob()
	uninstaller := &fakeUninstaller{job: job}
	loader := info.New(homebrew.Info)
	root, _ := New(homebrew, loader, uninstaller, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	for _, message := range immediateMessages(m.Init()) {
		_, next := m.Update(message)
		if _, ok := message.(listResultMsg); ok && next != nil {
			result := next()
			if _, ok := result.(info.Result); ok {
				m.Update(result)
			}
		}
	}
	if m.loading {
		t.Fatal("startup list did not complete")
	}
	return m, uninstaller
}

func immediateMessages(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	message := command()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{message}
	}
	messages := make([]tea.Msg, 0, len(batch))
	for _, command := range batch {
		messages = append(messages, command())
	}
	return messages
}

func requireQuit(t *testing.T, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("expected tea.Quit, got nil")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("command did not return tea.QuitMsg")
	}
}

func requireQuittingState(t *testing.T, m *model) {
	t.Helper()
	if m.mode != modeQuitting || m.status != "Quitting..." || !m.priority {
		t.Fatalf("quitting state changed: mode=%v status=%q priority=%v", m.mode, m.status, m.priority)
	}
}

func textKey(value string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: value, Code: []rune(value)[0]})
}

func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func TestSearchIsModeFirstSubstringAndNonWrapping(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(textKey("/"))
	m.Update(textKey("E"))
	m.Update(textKey("t"))
	m.Update(textKey("A"))
	m.Update(textKey("q"))
	if m.mode != modeSearch || m.query != "EtAq" {
		t.Fatalf("search did not consume normal-mode keys: mode=%v query=%q", m.mode, m.query)
	}
	if got := len(m.list.VisibleItems()); got != 0 {
		t.Fatalf("filter matched %d rows, want 0", got)
	}
	m.Update(specialKey(tea.KeyBackspace))
	if got := m.selectedPackage(); got == nil || got.Name != "Beta" {
		t.Fatalf("case-insensitive substring selected %#v, want Beta", got)
	}
	m.Update(specialKey(tea.KeyDelete))
	if m.query != "EtA" {
		t.Fatalf("forward delete changed query to %q", m.query)
	}
	m.Update(specialKey(tea.KeyEscape))
	if m.mode != modeNormal || m.query != "" || m.list.Index() != 0 {
		t.Fatalf("escape did not clear/reset search: mode=%v query=%q index=%d", m.mode, m.query, m.list.Index())
	}
	m.Update(textKey("k"))
	if m.list.Index() != 0 {
		t.Fatalf("selection wrapped above first row to %d", m.list.Index())
	}
	m.Update(textKey("j"))
	m.Update(textKey("j"))
	m.Update(textKey("j"))
	if m.list.Index() != 2 {
		t.Fatalf("selection wrapped below final row to %d", m.list.Index())
	}
}

func TestConfirmationRequiresExactLowercaseYAndStartsAsynchronously(t *testing.T) {
	m, uninstaller := newTestModel(t)
	m.Update(textKey("d"))
	if m.mode != modeConfirm || m.confirmation == nil || m.confirmation.Name != "Alpha" {
		t.Fatalf("confirmation snapshot not opened: mode=%v snapshot=%#v", m.mode, m.confirmation)
	}
	m.Update(textKey("Y"))
	if m.mode != modeNormal || m.status != "Uninstall cancelled" || uninstaller.starts != 0 {
		t.Fatalf("uppercase Y did not cancel exactly: mode=%v status=%q starts=%d", m.mode, m.status, uninstaller.starts)
	}

	m.Update(textKey("d"))
	_, command := m.Update(textKey("y"))
	if uninstaller.starts != 0 {
		t.Fatal("Start ran inside Update")
	}
	if m.mode != modeOperation || m.status != "Uninstalling Alpha..." || !m.spinnerActive {
		t.Fatalf("start state not committed before command: mode=%v status=%q spinner=%v", m.mode, m.status, m.spinnerActive)
	}
	messages := immediateMessages(command)
	if uninstaller.starts != 1 {
		t.Fatalf("Start calls=%d, want 1 after command execution", uninstaller.starts)
	}
	for _, message := range messages {
		if started, ok := message.(jobStartedMsg); ok {
			m.Update(started)
		}
	}
	uninstaller.job.finish(privileged.Result{Err: errors.New("brew failed")})
}

func TestPasswordDropsPasteUsesFreshMaskedInputAndSubmitsDirectly(t *testing.T) {
	m, uninstaller := newTestModel(t)
	m.Update(textKey("d"))
	_, command := m.Update(textKey("y"))
	for _, message := range immediateMessages(command) {
		if started, ok := message.(jobStartedMsg); ok {
			m.Update(started)
		}
	}

	request := privileged.RequestID{1}
	m.Update(jobEventMsg{id: m.operationID, event: privileged.Event{Type: privileged.PasswordRequested, RequestID: request}, open: true})
	if m.mode != modePassword || m.password.Value() != "" || !m.password.Focused() {
		t.Fatalf("password request did not create fresh focused input")
	}
	m.Update(textKey("密"))
	m.Update(tea.PasteMsg{Content: "SECRET-PASTE"})
	m.Update(specialKey(tea.KeyDelete))
	if m.password.Value() != "密" {
		t.Fatalf("paste/delete changed password value, got %q", m.password.Value())
	}
	m.Update(specialKey(tea.KeyEnter))
	if m.mode != modeOperation || m.password.Value() != "" || m.password.Focused() {
		t.Fatal("submit did not immediately reset and blur password input")
	}
	uninstaller.job.mu.Lock()
	got := string(uninstaller.job.passwords[0])
	gotRequest := uninstaller.job.passwordIDs[0]
	uninstaller.job.mu.Unlock()
	if got != "密" || gotRequest != request || gotRequest == (privileged.RequestID{}) {
		t.Fatalf("submitted password=%q request=%x, want %q request=%x", got, gotRequest, "密", request)
	}

	m.Update(jobEventMsg{id: m.operationID, event: privileged.Event{Type: privileged.PasswordRequested, RequestID: privileged.RequestID{2}}, open: true})
	if m.password.Value() != "" || m.passwordAttempts != 2 {
		t.Fatalf("retry was not fresh: value=%q attempts=%d", m.password.Value(), m.passwordAttempts)
	}
	uninstaller.job.Cancel()
}

func TestLoadingModesAcceptOnlySpecifiedQuitKeys(t *testing.T) {
	m, _ := newTestModel(t)
	m.loading = true
	m.loadPurpose = loadRefresh
	m.Update(textKey("tab"))
	if m.kind != brew.Cask {
		t.Fatal("tab changed kind during refresh")
	}
	m.Update(textKey("q"))
	if m.mode != modeQuitting {
		t.Fatal("q did not enter supervised quitting during refresh")
	}

	m2, _ := newTestModel(t)
	m2.loading = true
	m2.loadPurpose = loadAfterOperation
	m2.mode = modeOperation
	m2.Update(textKey("q"))
	if m2.mode != modeOperation {
		t.Fatal("q escaped post-uninstall reload")
	}
}

func TestResizeCancelsUnsafeConfirmationAndActiveUninstall(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(textKey("d"))
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if m.mode != modeNormal || m.confirmation != nil || m.status != "Terminal too small; uninstall cancelled" {
		t.Fatalf("unsafe confirmation survived resize: mode=%v confirmation=%#v status=%q", m.mode, m.confirmation, m.status)
	}

	m2, uninstaller := newTestModel(t)
	m2.Update(textKey("d"))
	_, command := m2.Update(textKey("y"))
	for _, message := range immediateMessages(command) {
		if started, ok := message.(jobStartedMsg); ok {
			m2.Update(started)
		}
	}
	m2.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if m2.cancelReason != cancelTerminal || m2.status != "Terminal too small; uninstall cancelled" {
		t.Fatalf("unsafe uninstall was not cancelled: reason=%v status=%q", m2.cancelReason, m2.status)
	}
	uninstaller.job.Cancel()
}

func startFakeUninstall(t *testing.T, m *model, uninstaller *fakeUninstaller) {
	t.Helper()
	m.Update(textKey("d"))
	_, command := m.Update(textKey("y"))
	for _, message := range immediateMessages(command) {
		if started, ok := message.(jobStartedMsg); ok {
			m.Update(started)
		}
	}
	if m.job == nil || uninstaller.starts != 1 {
		t.Fatalf("uninstall did not start: job=%v starts=%d", m.job != nil, uninstaller.starts)
	}
}

// The job window is browse-only: cursor, info selection, and the pure toggles
// stay live while every key that could mutate, reload, quit, or leave
// modeOperation is dead, and the acted-upon row carries the spinner mark.
func TestJobWindowAllowsBrowsingAndBlocksMutation(t *testing.T) {
	m, uninstaller := newTestModel(t)
	startFakeUninstall(t, m, uninstaller)

	_, infoCommand := m.Update(textKey("j"))
	if m.list.Index() != 1 || infoCommand == nil {
		t.Fatalf("browsing dead during job: index=%d info=%v", m.list.Index(), infoCommand != nil)
	}
	m.Update(textKey("t"))
	if m.themeIndex != 1 {
		t.Fatal("theme cycle dead during job")
	}

	for _, key := range []tea.KeyPressMsg{textKey("d"), textKey("u"), textKey("r"), textKey("tab"), textKey("/"), textKey("s"), textKey("q")} {
		m.Update(key)
	}
	if m.mode != modeOperation || m.confirmation != nil || m.loading || m.kind != brew.Cask || uninstaller.starts != 1 {
		t.Fatalf("mutating key escaped the job window: mode=%v confirmation=%v loading=%v kind=%v starts=%d",
			m.mode, m.confirmation != nil, m.loading, m.kind, uninstaller.starts)
	}

	lines := strippedLines(m)
	if !strings.Contains(lines[m.height-2], "Uninstall in progress; browse only") || !strings.Contains(lines[m.height-2], "Theme: t") {
		t.Fatalf("job footer=%q", lines[m.height-2])
	}

	target := m.packageLine(*m.operation, false, 60)
	if !strings.Contains(target, m.spinner.View()) {
		t.Fatalf("acted-upon row %q missing spinner %q", target, m.spinner.View())
	}
	other := m.packageLine(brew.Package{Name: "Beta", Version: "2.0", Kind: brew.Cask}, false, 60)
	if strings.Contains(other, m.spinner.View()) {
		t.Fatalf("unrelated row %q carries the operation mark", other)
	}
	uninstaller.job.Cancel()
}

func TestUninstallStartAndTerminalFailuresRestoreControls(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		m, uninstaller := newTestModel(t)
		uninstaller.err = errors.New("setup failed")
		m.Update(textKey("d"))
		_, command := m.Update(textKey("y"))
		for _, message := range immediateMessages(command) {
			if failed, ok := message.(jobStartFailedMsg); ok {
				m.Update(failed)
			}
		}
		if m.mode != modeNormal || m.status != "setup failed" || m.operation != nil {
			t.Fatalf("start failure state: mode=%v status=%q operation=%#v", m.mode, m.status, m.operation)
		}
		m.Update(textKey("t"))
		if m.themeIndex != 1 {
			t.Fatal("normal controls remained disabled after start failure")
		}
	})

	tests := []struct {
		name   string
		result privileged.Result
		status string
	}{
		{name: "command", result: privileged.Result{Err: errors.New("brew failed")}, status: "brew failed"},
		{name: "authentication", result: privileged.Result{AuthFailed: true}, status: "Administrator authentication failed"},
		{name: "authentication timeout", result: privileged.Result{AuthTimedOut: true}, status: "Administrator authentication timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, uninstaller := newTestModel(t)
			startFakeUninstall(t, m, uninstaller)
			uninstaller.job.finish(tt.result)
			m.Update(jobResultMsg{id: m.operationID, result: tt.result})
			if m.mode != modeNormal || m.loading || m.status != tt.status || m.operation != nil {
				t.Fatalf("terminal failure state: mode=%v loading=%v status=%q operation=%#v", m.mode, m.loading, m.status, m.operation)
			}
			m.Update(textKey("t"))
			if m.themeIndex != 1 {
				t.Fatal("normal controls remained disabled after terminal failure")
			}
		})
	}
}

func TestUninstallSuccessWaitsForReloadAndKeepsTargetSnapshot(t *testing.T) {
	tests := []struct {
		name       string
		reloadErr  error
		wantStatus string
	}{
		{name: "reload success", wantStatus: "Uninstalled Alpha"},
		{name: "reload failure", reloadErr: errors.New("reload failed"), wantStatus: "reload failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, uninstaller := newTestModel(t)
			startFakeUninstall(t, m, uninstaller)
			homebrew := m.homebrew.(*fakeHomebrew)
			homebrew.packages[brew.Cask] = []brew.Package{{Name: "Beta", Version: "2.1", Kind: brew.Cask}}
			homebrew.err = tt.reloadErr

			uninstaller.job.finish(privileged.Result{})
			_, reload := m.Update(jobResultMsg{id: m.operationID, result: privileged.Result{}})
			if m.mode != modeOperation || !m.loading || m.loadPurpose != loadAfterOperation {
				t.Fatalf("success exposed before reload: mode=%v loading=%v purpose=%v", m.mode, m.loading, m.loadPurpose)
			}
			kind, theme, index, starts := m.kind, m.themeIndex, m.list.Index(), uninstaller.starts
			for _, key := range []tea.KeyPressMsg{textKey("q"), textKey("tab"), textKey("d"), textKey("j"), textKey("t")} {
				m.Update(key)
			}
			if m.kind != kind || m.themeIndex != theme || m.list.Index() != index || uninstaller.starts != starts || m.mode != modeOperation {
				t.Fatal("controls were active during post-uninstall reload")
			}

			var listMessage *listResultMsg
			for _, message := range immediateMessages(reload) {
				if result, ok := message.(listResultMsg); ok {
					resultCopy := result
					listMessage = &resultCopy
				}
			}
			if listMessage == nil {
				t.Fatal("reload command did not return a typed list result")
			}
			_, infoCommand := m.Update(*listMessage)
			if infoCommand != nil {
				if result, ok := infoCommand().(info.Result); ok {
					m.Update(result)
				}
			}
			if m.mode != modeNormal || m.loading || m.status != tt.wantStatus || m.operation != nil {
				t.Fatalf("reload terminal state: mode=%v loading=%v status=%q operation=%#v", m.mode, m.loading, m.status, m.operation)
			}
			if len(uninstaller.started) != 1 || uninstaller.started[0].Name != "Alpha" {
				t.Fatalf("uninstall target mutated: %#v", uninstaller.started)
			}
			if tt.reloadErr == nil {
				selected := m.selectedPackage()
				if selected == nil || selected.Name != "Beta" {
					t.Fatalf("reloaded inventory=%#v, want Beta", selected)
				}
			}
		})
	}
}

func TestQuitCancelsActiveAndPendingInfoBeforeCompletingResult(t *testing.T) {
	started := make(chan string, 2)
	loader := info.New(func(ctx context.Context, pkg brew.Package) (string, error) {
		started <- pkg.Name
		<-ctx.Done()
		return "", ctx.Err()
	})
	homebrew := &fakeHomebrew{}
	root, _ := New(homebrew, loader, &fakeUninstaller{}, t.TempDir())
	m := root.(*model)
	m.loading = false
	m.setPackages([]brew.Package{
		{Name: "Alpha", Kind: brew.Cask},
		{Name: "Beta", Kind: brew.Cask},
	}, 0)

	active := m.selectInfo()
	result := make(chan tea.Msg, 1)
	go func() { result <- active() }()
	select {
	case name := <-started:
		if name != "Alpha" {
			t.Fatalf("active info target=%q, want Alpha", name)
		}
	case <-time.After(time.Second):
		t.Fatal("active info did not start")
	}
	m.list.Select(1)
	if pending := m.selectInfo(); pending != nil {
		t.Fatal("pending info unexpectedly started beside active request")
	}

	_, quit := m.Update(textKey("q"))
	if m.mode != modeQuitting || quit != nil {
		t.Fatal("quit did not wait for the active typed info result")
	}
	var completed tea.Msg
	select {
	case completed = <-result:
	case <-time.After(time.Second):
		t.Fatal("active info did not observe synchronous cancellation")
	}
	_, quit = m.Update(completed)
	requireQuit(t, quit)
	requireQuittingState(t, m)
	select {
	case name := <-started:
		t.Fatalf("pending info started after quit for %q", name)
	default:
	}
	select {
	case <-loader.Done():
	case <-time.After(time.Second):
		t.Fatal("loader remained active after cancelled result")
	}
}

func TestQuitCancelsPendingInfoPromotedByRacingResult(t *testing.T) {
	started := make(chan string, 2)
	releaseAlpha := make(chan struct{})
	loader := info.New(func(ctx context.Context, pkg brew.Package) (string, error) {
		started <- pkg.Name
		if pkg.Name == "Alpha" {
			<-releaseAlpha
			return "alpha details", nil
		}
		<-ctx.Done()
		return "", ctx.Err()
	})
	root, _ := New(&fakeHomebrew{}, loader, &fakeUninstaller{}, t.TempDir())
	m := root.(*model)
	m.loading = false
	m.setPackages([]brew.Package{
		{Name: "Alpha", Kind: brew.Cask},
		{Name: "Beta", Kind: brew.Cask},
	}, 0)

	activeResult := make(chan tea.Msg, 1)
	active := m.selectInfo()
	go func() { activeResult <- active() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active info did not start")
	}
	m.list.Select(1)
	m.selectInfo()
	close(releaseAlpha)

	var completed tea.Msg
	select {
	case completed = <-activeResult:
	case <-time.After(time.Second):
		t.Fatal("active info did not complete")
	}
	_, promoted := m.Update(completed)
	if promoted == nil {
		t.Fatal("racing result did not promote pending info")
	}
	promotedResult := make(chan tea.Msg, 1)
	go func() { promotedResult <- promoted() }()
	select {
	case name := <-started:
		if name != "Beta" {
			t.Fatalf("promoted info target=%q, want Beta", name)
		}
	case <-time.After(time.Second):
		t.Fatal("promoted info did not start")
	}

	_, quit := m.Update(textKey("q"))
	if quit != nil {
		t.Fatal("quit did not wait for promoted typed info result")
	}
	var cancelled tea.Msg
	select {
	case cancelled = <-promotedResult:
	case <-time.After(time.Second):
		t.Fatal("promoted info did not observe quit cancellation")
	}
	_, quit = m.Update(cancelled)
	requireQuit(t, quit)
	requireQuittingState(t, m)
	select {
	case name := <-started:
		t.Fatalf("pending info restarted after quit for %q", name)
	default:
	}
}

func TestQuitWaitsForTypedListResult(t *testing.T) {
	m, _ := newTestModel(t)
	homebrew := m.homebrew.(*fakeHomebrew)
	homebrew.listStarted = make(chan struct{})
	result := make(chan tea.Msg, 1)
	go func() { result <- m.startList(loadRefresh, m.list.Index())() }()
	select {
	case <-homebrew.listStarted:
	case <-time.After(time.Second):
		t.Fatal("list request did not start")
	}

	_, quit := m.Update(textKey("q"))
	if quit != nil {
		t.Fatal("quit returned before typed list result")
	}
	var completed tea.Msg
	select {
	case completed = <-result:
	case <-time.After(time.Second):
		t.Fatal("list request did not observe synchronous cancellation")
	}
	_, quit = m.Update(completed)
	requireQuit(t, quit)
	requireQuittingState(t, m)
}

func TestQuitWaitsForTypedStartFailure(t *testing.T) {
	m, uninstaller := newTestModel(t)
	uninstaller.startStarted = make(chan struct{})
	uninstaller.err = errors.New("cancelled start")
	m.Update(textKey("d"))
	_, command := m.Update(textKey("y"))
	batch := command().(tea.BatchMsg)
	results := make(chan tea.Msg, len(batch))
	for _, command := range batch {
		go func(command tea.Cmd) { results <- command() }(command)
	}
	select {
	case <-uninstaller.startStarted:
	case <-time.After(time.Second):
		t.Fatal("uninstall start did not begin")
	}

	if _, quit := m.Update(SignalMsg{ExitCode: 143}); quit != nil {
		t.Fatal("quit returned before typed start failure")
	}
	for {
		message := <-results
		if failed, ok := message.(jobStartFailedMsg); ok {
			_, quit := m.Update(failed)
			requireQuit(t, quit)
			requireQuittingState(t, m)
			break
		}
	}
}

func TestQuitWaitsForRacedStartAndTypedJobResult(t *testing.T) {
	m, uninstaller := newTestModel(t)
	uninstaller.startStarted = make(chan struct{})
	m.Update(textKey("d"))
	_, command := m.Update(textKey("y"))
	batch := command().(tea.BatchMsg)
	results := make(chan tea.Msg, len(batch))
	for _, command := range batch {
		go func(command tea.Cmd) { results <- command() }(command)
	}
	select {
	case <-uninstaller.startStarted:
	case <-time.After(time.Second):
		t.Fatal("uninstall start did not begin")
	}

	if _, quit := m.Update(SignalMsg{ExitCode: 143}); quit != nil {
		t.Fatal("quit returned before typed start result")
	}
	var started jobStartedMsg
	for {
		message := <-results
		if value, ok := message.(jobStartedMsg); ok {
			started = value
			break
		}
	}
	_, waitResult := m.Update(started)
	if waitResult == nil {
		t.Fatal("raced successful start did not schedule its typed job result")
	}
	result, ok := waitResult().(jobResultMsg)
	if !ok {
		t.Fatal("raced job wait did not return jobResultMsg")
	}
	_, quit := m.Update(result)
	requireQuittingState(t, m)
	requireQuit(t, quit)
}

func TestQuitWaitsForTypedActiveJobResult(t *testing.T) {
	m, uninstaller := newTestModel(t)
	startFakeUninstall(t, m, uninstaller)
	if _, quit := m.Update(SignalMsg{ExitCode: 143}); quit != nil {
		t.Fatal("quit returned before active job result")
	}
	result, ok := waitJobResult(m.operationID, m.jobResult)().(jobResultMsg)
	if !ok {
		t.Fatal("active job wait did not return jobResultMsg")
	}
	_, quit := m.Update(result)
	requireQuittingState(t, m)
	requireQuit(t, quit)
}

func TestFatalCleanupAndSignalsSetSupervisedExitCodes(t *testing.T) {
	t.Run("fatal cleanup renders exact status", func(t *testing.T) {
		m, uninstaller := newTestModel(t)
		startFakeUninstall(t, m, uninstaller)
		result := privileged.Result{CleanupErr: errors.New("fatal uninstall cleanup failure:\nworkers remain")}
		uninstaller.job.finish(result)
		_, quit := m.Update(jobResultMsg{id: m.operationID, result: result})
		if m.mode != modeQuitting {
			t.Fatalf("mode=%v, want quitting", m.mode)
		}
		requireQuit(t, quit)
		if want := "Uninstall cleanup failed: fatal uninstall cleanup failure: workers remain"; m.status != want {
			t.Fatalf("status=%q, want %q", m.status, want)
		}
		if !m.priority || m.supervisor.ExitCode() != 1 {
			t.Fatal("cleanup failure was not retained as a priority fatal result")
		}
	})

	t.Run("fatal cleanup while quitting", func(t *testing.T) {
		m, uninstaller := newTestModel(t)
		startFakeUninstall(t, m, uninstaller)
		result := privileged.Result{CleanupErr: errors.New("fatal uninstall cleanup failure: workers remain")}
		// Programmed before the signal. Quitting cancels the job, and the waiter
		// goroutine records whatever the job terminated with; if that were a bare
		// Cancelled result it would take the once-only recording slot and the fatal
		// cleanup error below would be silently dropped, leaving the signal's exit
		// code in place. Both paths must see the one true terminal result, exactly
		// as they do in production.
		uninstaller.job.setTerminal(result)
		if _, quit := m.Update(SignalMsg{ExitCode: 143}); quit != nil {
			t.Fatal("quit returned before fatal typed job result")
		}
		uninstaller.job.finish(result)
		_, quit := m.Update(jobResultMsg{id: m.operationID, result: result})
		requireQuittingState(t, m)
		requireQuit(t, quit)
		if got := m.supervisor.ExitCode(); got != 1 {
			t.Fatalf("exit code=%d, want 1", got)
		}
	})

	t.Run("SIGTERM", func(t *testing.T) {
		m, _ := newTestModel(t)
		_, quit := m.Update(SignalMsg{ExitCode: 143})
		requireQuit(t, quit)
		if got := m.supervisor.ExitCode(); got != 143 {
			t.Fatalf("exit code=%d, want 143", got)
		}
	})

	t.Run("SIGINT", func(t *testing.T) {
		m, _ := newTestModel(t)
		_, interrupt := m.Update(SignalMsg{ExitCode: 130})
		if interrupt == nil {
			t.Fatal("SIGINT did not complete")
		}
		if _, ok := interrupt().(tea.InterruptMsg); !ok {
			t.Fatal("SIGINT did not return tea.InterruptMsg")
		}
		if got := m.supervisor.ExitCode(); got != 130 {
			t.Fatalf("exit code=%d, want 130", got)
		}
	})
}

type supervisorJob struct {
	cancelled chan struct{}
	once      sync.Once
}

func newSupervisorJob() *supervisorJob {
	return &supervisorJob{cancelled: make(chan struct{})}
}

func (*supervisorJob) Events() <-chan privileged.Event { return nil }
func (*supervisorJob) RespondPassword(privileged.RequestID, []byte) error {
	return nil
}
func (*supervisorJob) CancelPassword(privileged.RequestID) error { return nil }
func (j *supervisorJob) Cancel() {
	j.once.Do(func() { close(j.cancelled) })
}
func (*supervisorJob) Wait() privileged.Result { return privileged.Result{} }

func TestSupervisorCancelsEverythingBeforeConcurrentWaitAndAwaitsRacedJob(t *testing.T) {
	infoStarted, infoCancelled := make(chan struct{}), make(chan struct{})
	loader := info.New(func(ctx context.Context, _ brew.Package) (string, error) {
		close(infoStarted)
		<-ctx.Done()
		close(infoCancelled)
		return "", ctx.Err()
	})
	infoCommand := loader.Select(&brew.Package{Name: "Alpha", Kind: brew.Cask})
	go infoCommand()
	select {
	case <-infoStarted:
	case <-time.After(time.Second):
		t.Fatal("info request did not start")
	}

	supervisor := &Supervisor{info: loader}
	listCancelled, startCancelled := make(chan struct{}), make(chan struct{})
	listDone, startDone, jobDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var listOnce, startOnce sync.Once
	supervisor.setList(func() { listOnce.Do(func() { close(listCancelled) }) }, listDone)
	supervisor.setStart(func() { startOnce.Do(func() { close(startCancelled) }) }, startDone)
	job := newSupervisorJob()
	supervisor.setJob(job, jobDone)

	cleaned := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		cleaned <- supervisor.Cleanup(ctx)
	}()
	for name, cancelled := range map[string]<-chan struct{}{
		"list":  listCancelled,
		"info":  infoCancelled,
		"start": startCancelled,
		"job":   job.cancelled,
	} {
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatalf("%s was not cancelled before waits completed", name)
		}
	}

	raced := newSupervisorJob()
	racedDone := make(chan struct{})
	supervisor.setJob(raced, racedDone)
	select {
	case <-raced.cancelled:
	case <-time.After(time.Second):
		t.Fatal("job registered during cleanup was not cancelled")
	}
	close(listDone)
	close(startDone)
	close(jobDone)
	select {
	case err := <-cleaned:
		t.Fatalf("cleanup returned before raced job completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(racedDone)
	select {
	case err := <-cleaned:
		if err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not await raced job")
	}
}

func drainList(t *testing.T, m *model, command tea.Cmd) {
	t.Helper()
	for _, message := range immediateMessages(command) {
		_, next := m.Update(message)
		if _, ok := message.(listResultMsg); ok && next != nil {
			if result, ok := next().(info.Result); ok {
				m.Update(result)
			}
		}
	}
}

func TestOutdatedMarksRideTheListCacheAcrossASwitch(t *testing.T) {
	homebrew := &fakeHomebrew{
		packages: map[brew.Kind][]brew.Package{
			brew.Cask: {
				{Name: "Alpha", Version: "1.0", Kind: brew.Cask},
				{Name: "Beta", Version: "2.0", Kind: brew.Cask},
			},
			brew.Formula: {{Name: "zlib", Version: "1.3", Kind: brew.Formula}},
		},
		// "Beta" is the only visible cask reported; "ghostwriter" is a name the
		// inventory never shows, as `brew outdated --formula` legitimately reports
		// dependency-only formulae the list filters out.
		outdated: map[brew.Kind][]brew.OutdatedPackage{brew.Cask: {
			{Name: "Beta", Installed: "2.0", Latest: "2.1"},
			{Name: "ghostwriter", Installed: "1.0", Latest: "1.1"},
		}},
	}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	drainList(t, m, m.Init())

	marks := func() []bool {
		var got []bool
		for _, item := range m.list.Items() {
			got = append(got, item.(packageItem).packageValue.Outdated)
		}
		return got
	}
	if got := marks(); !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("startup marks=%v, want only Beta marked", got)
	}

	drainList(t, m, m.switchKind())
	if got := marks(); !slices.Equal(got, []bool{false}) {
		t.Fatalf("formula marks=%v, want none", got)
	}

	// The cached switch starts no command, so the marks must already be on the
	// retained slice rather than fetched again.
	drainList(t, m, m.switchKind())
	if got := homebrew.listCalls[brew.Cask]; got != 1 {
		t.Fatalf("cached switch re-shelled: cask list calls=%d, want 1", got)
	}
	if got := marks(); !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("cached marks=%v, want only Beta marked", got)
	}
}

func TestAFailedOutdatedReadStillLoadsAnUnmarkedList(t *testing.T) {
	homebrew := &fakeHomebrew{
		packages: map[brew.Kind][]brew.Package{
			brew.Cask: {{Name: "Alpha", Version: "1.0", Kind: brew.Cask}},
		},
		outdated:    map[brew.Kind][]brew.OutdatedPackage{brew.Cask: {{Name: "Alpha", Latest: "2.0"}}},
		outdatedErr: errors.New("brew outdated exploded"),
	}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	drainList(t, m, m.Init())

	if m.loading || m.status != "" {
		t.Fatalf("a failed outdated read broke the load: loading=%v status=%q", m.loading, m.status)
	}
	if got := len(m.list.Items()); got != 1 {
		t.Fatalf("list has %d items, want 1", got)
	}
	if m.list.Items()[0].(packageItem).packageValue.Outdated {
		t.Fatal("a failed outdated read produced a mark")
	}
	if _, ok := m.listCache[brew.Cask]; !ok {
		t.Fatal("a failed outdated read poisoned list retention")
	}
}

func TestKindSwitchServesCachedListWithoutReshelling(t *testing.T) {
	homebrew := &fakeHomebrew{packages: map[brew.Kind][]brew.Package{
		brew.Cask:    {{Name: "Alpha", Version: "1.0", Kind: brew.Cask}},
		brew.Formula: {{Name: "zlib", Version: "1.3", Kind: brew.Formula}},
	}}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	drainList(t, m, m.Init())
	if m.loading {
		t.Fatal("startup list did not complete")
	}
	if got := homebrew.listCalls[brew.Cask]; got != 1 {
		t.Fatalf("startup cask list calls=%d, want 1", got)
	}

	drainList(t, m, m.switchKind())
	if m.kind != brew.Formula || homebrew.listCalls[brew.Formula] != 1 {
		t.Fatalf("first switch: kind=%q formula calls=%d, want one miss", m.kind, homebrew.listCalls[brew.Formula])
	}

	command := m.switchKind()
	if m.loading {
		t.Fatal("cached switch entered a loading state")
	}
	if got := len(m.list.Items()); got != 1 {
		t.Fatalf("cached switch rendered %d items in the same frame, want 1", got)
	}
	if got := m.list.Items()[0].(packageItem).packageValue.Name; got != "Alpha" {
		t.Fatalf("cached switch showed %q, want the cached cask", got)
	}
	drainList(t, m, command)
	if got := homebrew.listCalls[brew.Cask]; got != 1 {
		t.Fatalf("cached switch re-shelled brew list: cask calls=%d, want 1", got)
	}

	drainList(t, m, m.switchKind())
	if got := homebrew.listCalls[brew.Formula]; got != 1 {
		t.Fatalf("switching back re-shelled brew list: formula calls=%d, want 1", got)
	}
}

func TestEmptyKindIsCachedRatherThanReshelledEverySwitch(t *testing.T) {
	homebrew := &fakeHomebrew{packages: map[brew.Kind][]brew.Package{
		brew.Cask: {{Name: "Alpha", Version: "1.0", Kind: brew.Cask}},
	}}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	drainList(t, m, m.Init())

	drainList(t, m, m.switchKind())
	if got := len(m.list.Items()); got != 0 {
		t.Fatalf("empty formula list has %d items, want 0", got)
	}
	drainList(t, m, m.switchKind())
	drainList(t, m, m.switchKind())
	if got := homebrew.listCalls[brew.Formula]; got != 1 {
		t.Fatalf("empty kind re-shelled on every switch: formula calls=%d, want 1", got)
	}
}

func TestRefreshDropsTheWholeListCache(t *testing.T) {
	homebrew := &fakeHomebrew{packages: map[brew.Kind][]brew.Package{
		brew.Cask:    {{Name: "Alpha", Version: "1.0", Kind: brew.Cask}},
		brew.Formula: {{Name: "zlib", Version: "1.3", Kind: brew.Formula}},
	}}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	drainList(t, m, m.Init())
	drainList(t, m, m.switchKind())
	drainList(t, m, m.switchKind())
	if len(m.listCache) != 2 {
		t.Fatalf("cache holds %d kinds before refresh, want 2", len(m.listCache))
	}

	drainList(t, m, m.updateNormal(tea.KeyPressMsg{Code: 'r'}))
	if _, ok := m.listCache[brew.Formula]; ok {
		t.Fatal("refresh left the inactive kind cached")
	}
	if _, ok := m.listCache[brew.Cask]; !ok {
		t.Fatal("refresh did not repopulate the refreshed kind")
	}
	drainList(t, m, m.switchKind())
	if got := homebrew.listCalls[brew.Formula]; got != 2 {
		t.Fatalf("switch after refresh served stale cache: formula calls=%d, want 2", got)
	}
}

// diskFleet is a formula inventory with dependency rows and a size for each, so
// the toggle, the sort, and the count are all exercised against one fixture.
func diskFleet() *fakeHomebrew {
	return &fakeHomebrew{
		packages: map[brew.Kind][]brew.Package{
			brew.Cask: {
				{Name: "Alpha", Kind: brew.Cask},
				{Name: "Beta", Kind: brew.Cask},
			},
			brew.Formula: {
				{Name: "awscli", Kind: brew.Formula},
				{Name: "gcc", Kind: brew.Formula, Dependency: true},
				{Name: "llvm@22", Kind: brew.Formula, Dependency: true},
				{Name: "vault", Kind: brew.Formula},
			},
		},
		sizes: brew.Sizes{
			Formula: map[string]int64{
				"awscli":  202752,
				"gcc":     488448,
				"llvm@22": 1550732,
				"vault":   519248,
			},
			Total: 9687960,
		},
	}
}

func newFleetModel(t *testing.T) (*model, *fakeHomebrew) {
	t.Helper()
	homebrew := diskFleet()
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	drainList(t, m, m.Init())
	if m.loading {
		t.Fatal("startup list did not complete")
	}
	return m, homebrew
}

func visibleNames(m *model) []string {
	items := m.list.VisibleItems()
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.(packageItem).packageValue.Name)
	}
	return names
}

func TestDependencyToggleServesRetentionAndStartsNoCommand(t *testing.T) {
	m, homebrew := newFleetModel(t)
	drainList(t, m, m.switchKind())
	if m.kind != brew.Formula {
		t.Fatalf("kind=%q, want formula", m.kind)
	}
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "vault"}) {
		t.Fatalf("startup formula rows=%q, want dependencies hidden", got)
	}
	if m.showDeps {
		t.Fatal("dependencies were shown at startup")
	}
	listCalls := homebrew.listCalls[brew.Formula]

	m.list.Select(1)
	m.updateNormal(textKey("a"))
	// Revealing rows resets the selection, so info retargets the new row 0.
	if got := m.selectedPackage(); got == nil || got.Name != "awscli" {
		t.Fatalf("selection after d=%#v, want the reset row 0", got)
	}
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "gcc", "llvm@22", "vault"}) {
		t.Fatalf("rows after d=%q, want every formula in source order", got)
	}
	if m.status != "Dependencies: shown" || m.priority {
		t.Fatalf("status=%q priority=%v, want an ordinary Dependencies: shown", m.status, m.priority)
	}

	m.updateNormal(textKey("A"))
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "vault"}) {
		t.Fatalf("rows after D=%q, want dependencies hidden again", got)
	}
	if m.status != "Dependencies: hidden" {
		t.Fatalf("status=%q, want Dependencies: hidden", m.status)
	}
	if got := homebrew.listCalls[brew.Formula]; got != listCalls {
		t.Fatalf("toggling re-shelled brew list: formula calls=%d, want %d", got, listCalls)
	}
	if m.loading {
		t.Fatal("toggling entered a loading state")
	}
}

// The retained slice is shared with listCache, so a sort that mutated it in place
// would corrupt retention and make the toggle non-idempotent.
func TestTogglingNeverMutatesTheRetainedList(t *testing.T) {
	m, _ := newFleetModel(t)
	drainList(t, m, m.switchKind())
	before := append([]brew.Package(nil), m.listCache[brew.Formula]...)

	m.updateNormal(textKey("a"))
	m.updateNormal(textKey("o"))
	m.updateNormal(textKey("o"))
	m.updateNormal(textKey("a"))

	if !slices.Equal(m.listCache[brew.Formula], before) {
		t.Fatalf("retention mutated to %#v, want %#v", m.listCache[brew.Formula], before)
	}
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "vault"}) {
		t.Fatalf("round trip left %q, want the original view", got)
	}
}

func TestDependencyToggleOnTheCaskListOnlyReportsStatus(t *testing.T) {
	m, _ := newFleetModel(t)
	m.list.Select(1)
	before := visibleNames(m)

	if command := m.updateNormal(textKey("a")); command != nil {
		t.Fatal("d re-targeted info on the cask list")
	}
	if got := visibleNames(m); !slices.Equal(got, before) {
		t.Fatalf("cask rows changed to %q, want %q", got, before)
	}
	if m.list.Index() != 1 {
		t.Fatalf("selection moved to %d, want it preserved on the cask list", m.list.Index())
	}
	if m.status != "Dependencies: shown" {
		t.Fatalf("status=%q, want the key to report itself rather than be silently dead", m.status)
	}
	drainList(t, m, m.switchKind())
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "gcc", "llvm@22", "vault"}) {
		t.Fatalf("formula rows after switching=%q, want the requested toggle state", got)
	}
}

func TestSizeSortOrdersLargestFirstAndBackToSourceOrder(t *testing.T) {
	m, _ := newFleetModel(t)
	drainList(t, m, m.switchKind())
	m.updateNormal(textKey("a"))

	m.updateNormal(textKey("o"))
	if m.status != "Sort: size" || m.priority {
		t.Fatalf("status=%q priority=%v, want an ordinary Sort: size", m.status, m.priority)
	}
	if got := visibleNames(m); !slices.Equal(got, []string{"llvm@22", "vault", "gcc", "awscli"}) {
		t.Fatalf("sorted rows=%q, want largest first", got)
	}

	m.updateNormal(textKey("O"))
	if m.status != "Sort: name" {
		t.Fatalf("status=%q, want Sort: name", m.status)
	}
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "gcc", "llvm@22", "vault"}) {
		t.Fatalf("unsorted rows=%q, want source order", got)
	}
}

func TestQueryFilterPreservesTheSizeOrder(t *testing.T) {
	m, _ := newFleetModel(t)
	drainList(t, m, m.switchKind())
	m.updateNormal(textKey("a"))
	m.updateNormal(textKey("o"))

	// "c" matches awscli and gcc only, by name. Source order is
	// awscli then gcc, so the filtered result proves the size order survived.
	m.query = "c"
	m.applyFilter(0)
	if got := visibleNames(m); !slices.Equal(got, []string{"gcc", "awscli"}) {
		t.Fatalf("filtered rows=%q, want the sorted order preserved", got)
	}
}

func TestSizeSortAppliesWhenTheMeasurementLandsLate(t *testing.T) {
	m, _ := newFleetModel(t)
	drainList(t, m, m.switchKind())
	m.updateNormal(textKey("a"))
	m.sizes = nil

	m.updateNormal(textKey("o"))
	if !m.sortBySize {
		t.Fatal("o did not record the requested order")
	}
	if got := visibleNames(m); !slices.Equal(got, []string{"awscli", "gcc", "llvm@22", "vault"}) {
		t.Fatalf("rows without a measurement=%q, want source order left alone", got)
	}

	m.Update(sizesResultMsg{id: m.sizesID, sizes: diskFleet().sizes})
	if got := visibleNames(m); !slices.Equal(got, []string{"llvm@22", "vault", "gcc", "awscli"}) {
		t.Fatalf("rows after the measurement landed=%q, want largest first", got)
	}
}

func TestInstalledCountFollowsTheDependencyToggle(t *testing.T) {
	m, _ := newFleetModel(t)
	drainList(t, m, m.switchKind())
	if got := m.installedStatus(); got != "2 formulae installed" {
		t.Fatalf("count with dependencies hidden=%q", got)
	}
	m.updateNormal(textKey("a"))
	if got := m.installedStatus(); got != "4 formulae installed" {
		t.Fatalf("count with dependencies shown=%q", got)
	}
}

// The list must be usable before the measurement lands, and the measurement must
// not drag the list into a loading mode or start the spinner.
func TestListIsFullyNavigableBeforeSizesLand(t *testing.T) {
	homebrew := diskFleet()
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})

	var deferred []tea.Msg
	for _, message := range immediateMessages(m.Init()) {
		if _, ok := message.(sizesResultMsg); ok {
			deferred = append(deferred, message)
			continue
		}
		_, next := m.Update(message)
		if _, ok := message.(listResultMsg); ok && next != nil {
			if result, ok := next().(info.Result); ok {
				m.Update(result)
			}
		}
	}

	if !m.sizesPending || m.sizes != nil {
		t.Fatalf("expected an in-flight measurement: pending=%v sizes=%v", m.sizesPending, m.sizes)
	}
	if m.loading || m.spinnerActive {
		t.Fatalf("the measurement gated the list: loading=%v spinner=%v", m.loading, m.spinnerActive)
	}
	if got := len(m.list.VisibleItems()); got != 2 {
		t.Fatalf("rendered %d rows before sizes landed, want 2", got)
	}
	m.Update(textKey("j"))
	if m.list.Index() != 1 {
		t.Fatalf("selection=%d, want j to move before sizes landed", m.list.Index())
	}
	if got := strippedLines(m)[4]; !strings.Contains(got, "Alpha") {
		t.Fatalf("row before sizes landed=%q, want it rendered", got)
	}

	for _, message := range deferred {
		m.Update(message)
	}
	if m.sizes == nil || m.sizesPending {
		t.Fatal("the landed measurement was not retained")
	}
	if m.spinnerActive {
		t.Fatal("the measurement started the spinner on completion")
	}
}

func TestSizeFailureNeverDisplacesARealStatus(t *testing.T) {
	m, _ := newFleetModel(t)
	m.status, m.priority = "Uninstalled Alpha", true

	measured := m.sizes
	m.Update(sizesResultMsg{id: m.sizesID, err: errors.New("du: permission denied")})
	if m.status != "Uninstalled Alpha" || !m.priority {
		t.Fatalf("status=%q priority=%v, want the priority message untouched", m.status, m.priority)
	}
	// A failure caches nothing of its own and does not discard what was measured.
	if m.sizes != measured {
		t.Fatalf("sizes=%v, want the last good measurement kept", m.sizes)
	}

	m.sizes = nil
	m.status, m.priority = "", false
	m.Update(sizesResultMsg{id: m.sizesID, err: errors.New("du: permission\ndenied")})
	if m.status != "du: permission denied" || m.priority {
		t.Fatalf("status=%q priority=%v, want the flattened failure in the empty slot", m.status, m.priority)
	}
	if m.sizes != nil {
		t.Fatal("a failed measurement was cached")
	}
}

func TestStaleSizeResultIsIgnored(t *testing.T) {
	m, _ := newFleetModel(t)
	m.Update(sizesResultMsg{id: m.sizesID - 1, sizes: brew.Sizes{Total: 42}})
	if m.sizes.Total == 42 {
		t.Fatal("a superseded measurement was applied")
	}
}

func TestBothCacheInvalidationSitesDropSizesAndRemeasure(t *testing.T) {
	t.Run("refresh", func(t *testing.T) {
		m, homebrew := newFleetModel(t)
		calls := homebrew.sizesCalls
		if m.sizes == nil {
			t.Fatal("startup left no measurement to invalidate")
		}
		command := m.updateNormal(textKey("r"))
		if m.sizes != nil {
			t.Fatal("refresh kept a stale measurement")
		}
		drainList(t, m, command)
		if got := homebrew.sizesCalls; got != calls+1 {
			t.Fatalf("size calls=%d, want %d after refresh", got, calls+1)
		}
	})

	t.Run("committed uninstall", func(t *testing.T) {
		m, uninstaller := newTestModel(t)
		homebrew := m.homebrew.(*fakeHomebrew)
		calls := homebrew.sizesCalls
		startFakeUninstall(t, m, uninstaller)
		uninstaller.job.finish(privileged.Result{})
		_, reload := m.Update(jobResultMsg{id: m.operationID, result: privileged.Result{}})
		if m.sizes != nil {
			t.Fatal("a committed uninstall kept a stale measurement")
		}
		drainList(t, m, reload)
		if got := homebrew.sizesCalls; got != calls+1 {
			t.Fatalf("size calls=%d, want %d after a committed uninstall", got, calls+1)
		}
	})

	t.Run("a kind switch is not an invalidation site", func(t *testing.T) {
		m, homebrew := newFleetModel(t)
		calls := homebrew.sizesCalls
		drainList(t, m, m.switchKind())
		drainList(t, m, m.switchKind())
		m.updateNormal(textKey("a"))
		m.updateNormal(textKey("o"))
		if m.sizes == nil {
			t.Fatal("a kind switch or a toggle dropped the measurement")
		}
		if got := homebrew.sizesCalls; got != calls {
			t.Fatalf("size calls=%d, want %d: only refresh and uninstall remeasure", got, calls)
		}
	})
}

func TestQuitWaitsForTypedSizeResult(t *testing.T) {
	m, _ := newTestModel(t)
	homebrew := m.homebrew.(*fakeHomebrew)
	started := make(chan struct{})
	homebrew.sizesStarted = started
	result := make(chan tea.Msg, 1)
	go func() { result <- m.startSizes()() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("size pass did not start")
	}

	_, quit := m.Update(textKey("q"))
	if quit != nil {
		t.Fatal("quit returned before the typed size result")
	}
	var completed tea.Msg
	select {
	case completed = <-result:
	case <-time.After(time.Second):
		t.Fatal("size pass did not observe synchronous cancellation")
	}
	_, quit = m.Update(completed)
	requireQuit(t, quit)
	requireQuittingState(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.supervisor.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestNewKeysAreInertOutsideNormalMode(t *testing.T) {
	for _, pressed := range []string{"d", "D", "o", "O"} {
		t.Run("search consumes "+pressed, func(t *testing.T) {
			m, _ := newFleetModel(t)
			m.Update(textKey("/"))
			m.Update(textKey(pressed))
			if m.query != pressed {
				t.Fatalf("query=%q, want the key typed as text", m.query)
			}
			if m.showDeps || m.sortBySize {
				t.Fatalf("search mode applied the key: deps=%v sort=%v", m.showDeps, m.sortBySize)
			}
		})

		t.Run("confirmation cancels on "+pressed, func(t *testing.T) {
			m, _ := newFleetModel(t)
			m.Update(textKey("d"))
			m.Update(textKey(pressed))
			if m.mode != modeNormal || m.status != "Uninstall cancelled" {
				t.Fatalf("mode=%v status=%q, want a cancelled confirmation", m.mode, m.status)
			}
			if m.showDeps || m.sortBySize {
				t.Fatalf("confirmation applied the key: deps=%v sort=%v", m.showDeps, m.sortBySize)
			}
		})

		t.Run("loading ignores "+pressed, func(t *testing.T) {
			m, _ := newFleetModel(t)
			m.loading = true
			m.loadPurpose = loadRefresh
			m.Update(textKey(pressed))
			if m.showDeps || m.sortBySize {
				t.Fatalf("a loading list applied the key: deps=%v sort=%v", m.showDeps, m.sortBySize)
			}
		})

		t.Run("quitting ignores "+pressed, func(t *testing.T) {
			m, _ := newFleetModel(t)
			m.mode = modeQuitting
			m.Update(textKey(pressed))
			if m.showDeps || m.sortBySize {
				t.Fatalf("quitting applied the key: deps=%v sort=%v", m.showDeps, m.sortBySize)
			}
		})
	}
}

// A superseded size pass must be cancelled, not abandoned. Otherwise a second
// refresh inside the measurement window leaves the first du running with its
// context leaked and its completion handle unreachable from the supervisor.
func TestASupersededSizePassIsCancelled(t *testing.T) {
	homebrew := &fakeHomebrew{
		packages: map[brew.Kind][]brew.Package{
			brew.Cask: {{Name: "Alpha", Version: "1.0", Kind: brew.Cask}},
		},
	}
	root, _ := New(homebrew, info.New(homebrew.Info), &fakeUninstaller{job: newFakeJob()}, t.TempDir())
	m := root.(*model)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})

	first := m.startSizes()
	if m.sizesCancel == nil {
		t.Fatal("the first pass registered no cancel function")
	}

	second := m.startSizes()
	if m.sizesCancel == nil {
		t.Fatal("the second pass registered no cancel function")
	}
	// The first pass's context must already be done; the second must not be.
	firstCtxDone := func() bool {
		msg, _ := first().(sizesResultMsg)
		return msg.err != nil
	}()
	if !firstCtxDone {
		t.Fatal("the superseded pass was not cancelled")
	}
	if msg, ok := second().(sizesResultMsg); !ok || msg.err != nil {
		t.Fatalf("the current pass was cancelled too: %#v", msg)
	}
}

// The search field carries exactly the words on the row: the name, the
// version, and "dep" for a dependency. The kind word is on no row — the tab
// names it — so no row may be reachable through it.
func TestSearchMatchesTheRenderedTokensOnly(t *testing.T) {
	item := packageItem{packageValue: brew.Package{
		Name: "llvm@22", Version: "22.1", Kind: brew.Formula, Dependency: true,
	}}
	value := strings.ToLower(item.FilterValue())
	for _, want := range []string{"llvm@22", "22.1", "dep"} {
		if !strings.Contains(value, want) {
			t.Fatalf("FilterValue()=%q, want it to contain %q", item.FilterValue(), want)
		}
	}
	if strings.Contains(value, "formula") {
		t.Fatalf("FilterValue()=%q, want no kind word", item.FilterValue())
	}

	onRequest := packageItem{packageValue: brew.Package{Name: "vault", Kind: brew.Formula}}
	if strings.Contains(strings.ToLower(onRequest.FilterValue()), "dep") {
		t.Fatalf("an on-request formula matched %q: %q", "dep", onRequest.FilterValue())
	}
}

// A size measurement landing while a confirmation dialog is open must still be
// applied. Skipping it dropped the request with no retry, so cancelling the
// dialog returned to a list in source order while the status claimed a size sort.
func TestASizeSortRequestSurvivesAConfirmationDialog(t *testing.T) {
	m, homebrew := newFleetModel(t)
	switchTo(t, m)

	landed := *m.sizes
	m.sizes = nil
	m.Update(textKey("o"))
	if !m.sortBySize {
		t.Fatal("o did not request a size sort")
	}

	// Open the confirmation, then let the measurement land underneath it.
	m.Update(textKey("d"))
	if m.mode != modeConfirm {
		t.Fatalf("mode=%v, want confirm", m.mode)
	}
	m.Update(sizesResultMsg{id: m.sizesID, sizes: landed})

	// Cancel the dialog and read the order actually on screen.
	m.Update(textKey("n"))
	if m.mode != modeNormal {
		t.Fatalf("mode=%v after cancel, want normal", m.mode)
	}
	names := visibleNames(m)
	if len(names) < 2 {
		t.Fatalf("only %d rows visible", len(names))
	}
	first, _ := homebrew.sizes.KB(brew.Formula, names[0])
	second, _ := homebrew.sizes.KB(brew.Formula, names[1])
	if first < second {
		t.Fatalf("list is not sorted by size after the dialog: %q (%d then %d)", names[:2], first, second)
	}
}

// With no retained list, a toggle must not replace a real status - typically the
// load error - with a claim about an order the user cannot see.
func TestAToggleWithNoRetainedListKeepsTheExistingStatus(t *testing.T) {
	m, _ := newTestModel(t)
	m.setPackages(nil, 0)
	delete(m.listCache, m.kind)
	m.status, m.priority = "brew exploded", false

	m.Update(textKey("o"))
	if m.status != "brew exploded" {
		t.Fatalf("status=%q, want the existing error preserved", m.status)
	}
	if !m.sortBySize {
		t.Fatal("the preference should still be recorded for the next load")
	}
}

// An upgrade of a package Homebrew does not report as outdated would be a no-op,
// so it must not reach the privileged machinery at all: no confirmation, no
// snapshot, no job. This is the guard that keeps a destructive-adjacent path
// unreachable for an operation with nothing to do.
func TestUpgradeStartsNothingForAPackageThatIsUpToDate(t *testing.T) {
	m, uninstaller := newTestModel(t)
	selected := m.selectedPackage()
	if selected == nil || selected.Outdated {
		t.Fatalf("fixture must start on a package that is not outdated: %#v", selected)
	}

	if cmd := m.updateNormal(textKey("u")); cmd != nil {
		t.Fatal("upgrade returned a command for a package that is up to date")
	}
	if m.mode != modeNormal {
		t.Fatalf("mode=%v, want normal", m.mode)
	}
	if m.confirmation != nil {
		t.Fatalf("an immutable snapshot was taken: %#v", m.confirmation)
	}
	if uninstaller.starts != 0 {
		t.Fatalf("starts=%d, want the privileged path never reached", uninstaller.starts)
	}
	if want := selected.Name + " is up to date"; m.status != want || m.priority {
		t.Fatalf("status=%q priority=%v, want %q as an ordinary status", m.status, m.priority, want)
	}
}

// The two verbs share one confirmation path, and the verb travels with the
// snapshot so every string and the argv agree on which operation is running.
func TestBothVerbsShareTheConfirmationPathAndCarryTheirOwnWords(t *testing.T) {
	for _, tt := range []struct {
		name     string
		key      string
		outdated bool
		op       brew.Operation
		title    string
		prompt   string
		progress string
	}{
		{"uninstall", "d", false, brew.Uninstall, "Confirm uninstall", "Uninstall Alpha?", "Uninstalling Alpha..."},
		{"upgrade", "u", true, brew.Upgrade, "Confirm upgrade", "Upgrade Alpha?", "Upgrading Alpha..."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, uninstaller := newTestModel(t)
			if tt.outdated {
				// Mark the selected row so the upgrade guard lets it through.
				items := m.list.Items()
				pkg := items[0].(packageItem).packageValue
				pkg.Outdated = true
				m.setPackages([]brew.Package{pkg}, 0)
			}

			if cmd := m.updateNormal(textKey(tt.key)); cmd != nil {
				t.Fatalf("confirmation returned a command before y: %v", cmd)
			}
			if m.mode != modeConfirm || m.confirmation == nil {
				t.Fatalf("no confirmation opened: mode=%v snapshot=%v", m.mode, m.confirmation)
			}
			if m.verb != tt.op {
				t.Fatalf("verb=%v, want %v", m.verb, tt.op)
			}
			if m.status != tt.title {
				t.Fatalf("status=%q, want %q", m.status, tt.title)
			}
			if got := ansiSequence.ReplaceAllString(m.confirmationModal(*m.confirmation), ""); !strings.Contains(got, tt.prompt) {
				t.Fatalf("modal=%q, want it to contain %q", got, tt.prompt)
			}

			// Confirm, and check the verb reached both the status and the runner.
			_, cmd := m.Update(textKey("y"))
			for _, message := range immediateMessages(cmd) {
				if started, ok := message.(jobStartedMsg); ok {
					m.Update(started)
				}
			}
			if m.status != tt.progress {
				t.Fatalf("progress status=%q, want %q", m.status, tt.progress)
			}
			if len(uninstaller.startedOps) != 1 || uninstaller.startedOps[0] != tt.op {
				t.Fatalf("runner received %v, want exactly [%v]", uninstaller.startedOps, tt.op)
			}
			if got := ansiSequence.ReplaceAllString(m.footerLine(m.width-2), ""); !strings.Contains(got, words(tt.op).title+" in progress;") {
				t.Fatalf("progress footer=%q, want the running verb named", got)
			}
		})
	}
}
