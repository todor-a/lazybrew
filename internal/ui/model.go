package ui

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"lazybrew/internal/brew"
	"lazybrew/internal/info"
	"lazybrew/internal/privileged"
)

const (
	minimumWidth  = 32
	minimumHeight = 9
)

type mode uint8

const (
	modeNormal mode = iota
	modeSearch
	modeConfirm
	modeOperation
	modePassword
	modeQuitting
)

type loadPurpose uint8

const (
	loadStartup loadPurpose = iota
	loadSwitch
	loadRefresh
	loadAfterOperation
)

type cancelReason uint8

const (
	cancelNone cancelReason = iota
	cancelUser
	cancelTerminal
	cancelAuthentication
)

type packageItem struct{ packageValue brew.Package }

func (i packageItem) FilterValue() string {
	p := i.packageValue
	// The rendered tokens only. The kind word is gone from the row — the active
	// tab already names the kind — so it is gone from the filter too: a search
	// must not match every row by a word that is on none of them, and "dep"
	// stays because it is still the word on a dependency row.
	return p.Name + " " + p.Version + " " + strings.TrimSpace(depColumn(p))
}

type packageDelegate struct{}

func (packageDelegate) Height() int                                  { return 1 }
func (packageDelegate) Spacing() int                                 { return 0 }
func (packageDelegate) Update(tea.Msg, *list.Model) tea.Cmd          { return nil }
func (packageDelegate) Render(io.Writer, list.Model, int, list.Item) {}

// Supervisor owns cancellation and completion handles beyond the Tea run loop.
type Supervisor struct {
	mu sync.Mutex

	info *info.Loader

	closing           bool
	listCancel        context.CancelFunc
	listDone          <-chan struct{}
	sizesCancel       context.CancelFunc
	sizesDone         <-chan struct{}
	startCancel       context.CancelFunc
	startDone         <-chan struct{}
	job               privileged.Job
	jobDone           <-chan struct{}
	jobResultRecorded bool

	exitCode   int
	cleanupErr error
}

func (s *Supervisor) setList(cancel context.CancelFunc, done <-chan struct{}) {
	s.mu.Lock()
	closing := s.closing
	if !closing {
		s.listCancel, s.listDone = cancel, done
	}
	s.mu.Unlock()
	if closing {
		cancel()
	}
}

func (s *Supervisor) clearList(done <-chan struct{}) {
	s.mu.Lock()
	if s.listDone == done {
		s.listCancel, s.listDone = nil, nil
	}
	s.mu.Unlock()
}

func (s *Supervisor) setSizes(cancel context.CancelFunc, done <-chan struct{}) {
	s.mu.Lock()
	closing := s.closing
	if !closing {
		s.sizesCancel, s.sizesDone = cancel, done
	}
	s.mu.Unlock()
	if closing {
		cancel()
	}
}

func (s *Supervisor) clearSizes(done <-chan struct{}) {
	s.mu.Lock()
	if s.sizesDone == done {
		s.sizesCancel, s.sizesDone = nil, nil
	}
	s.mu.Unlock()
}

func (s *Supervisor) setStart(cancel context.CancelFunc, done <-chan struct{}) {
	s.mu.Lock()
	closing := s.closing
	if !closing {
		s.startCancel, s.startDone = cancel, done
	}
	s.mu.Unlock()
	if closing {
		cancel()
	}
}

func (s *Supervisor) clearStart(done <-chan struct{}) {
	s.mu.Lock()
	if s.startDone == done {
		s.startCancel, s.startDone = nil, nil
	}
	s.mu.Unlock()
}

func (s *Supervisor) setJob(job privileged.Job, done <-chan struct{}) {
	s.mu.Lock()
	s.job, s.jobDone = job, done
	s.jobResultRecorded = false
	closing := s.closing
	s.mu.Unlock()
	if closing {
		job.Cancel()
	}
}

func (s *Supervisor) finishJob(result privileged.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobResultRecorded {
		return
	}
	s.jobResultRecorded = true
	if result.CleanupErr != nil {
		s.cleanupErr = errors.Join(s.cleanupErr, result.CleanupErr)
	}
}

func (s *Supervisor) setExitCode(code int) {
	s.mu.Lock()
	s.exitCode = code
	s.mu.Unlock()
}

// ExitCode reports a signal-derived or cleanup-derived process result.
func (s *Supervisor) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// cancel synchronously stops new work and every currently owned operation.
func (s *Supervisor) cancel() {
	s.mu.Lock()
	s.closing = true
	listCancel := s.listCancel
	sizesCancel := s.sizesCancel
	startCancel := s.startCancel
	job := s.job
	s.mu.Unlock()

	if listCancel != nil {
		listCancel()
	}
	if sizesCancel != nil {
		sizesCancel()
	}
	if startCancel != nil {
		startCancel()
	}
	if s.info != nil {
		s.info.Cancel()
	}
	if job != nil {
		job.Cancel()
	}
}

// Cleanup idempotently cancels every owned operation and boundedly awaits its existing owner.
func (s *Supervisor) Cleanup(ctx context.Context) error {
	s.cancel()

	s.mu.Lock()
	listDone := s.listDone
	sizesDone := s.sizesDone
	startDone := s.startDone
	jobDone := s.jobDone
	s.mu.Unlock()

	waits := []<-chan struct{}{listDone, sizesDone, startDone, jobDone}
	if s.info != nil {
		waits = append(waits, s.info.Done())
	}
	s.rememberCleanup(awaitAll(ctx, waits...))

	// Start may have raced with cancellation and registered a real job.
	s.mu.Lock()
	racedJob, racedJobDone := s.job, s.jobDone
	s.mu.Unlock()
	if racedJob != nil && racedJobDone != jobDone {
		racedJob.Cancel()
		s.rememberCleanup(await(ctx, racedJobDone))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupErr
}

func awaitAll(ctx context.Context, waits ...<-chan struct{}) error {
	results := make(chan error, len(waits))
	count := 0
	for _, done := range waits {
		if done == nil {
			continue
		}
		count++
		go func(done <-chan struct{}) {
			results <- await(ctx, done)
		}(done)
	}

	var joined error
	for range count {
		joined = errors.Join(joined, <-results)
	}
	return joined
}

func await(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.New("cleanup did not complete: " + ctx.Err().Error())
	}
}

func (s *Supervisor) rememberCleanup(err error) {
	s.mu.Lock()
	s.cleanupErr = errors.Join(s.cleanupErr, err)
	s.mu.Unlock()
}

func (s *Supervisor) cleanupError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupErr
}

// SignalMsg lets main supervise SIGINT and SIGTERM through the same cleanup path.
type SignalMsg struct{ ExitCode int }

type listResultMsg struct {
	id       uint64
	kind     brew.Kind
	purpose  loadPurpose
	packages []brew.Package
	err      error
	done     <-chan struct{}
}

type sizesResultMsg struct {
	id    uint64
	sizes brew.Sizes
	err   error
	done  <-chan struct{}
}

type jobStartedMsg struct {
	id        uint64
	job       privileged.Job
	events    <-chan privileged.Event
	result    <-chan privileged.Result
	startDone <-chan struct{}
}

type jobStartFailedMsg struct {
	id        uint64
	err       error
	startDone <-chan struct{}
}

type jobEventMsg struct {
	id    uint64
	event privileged.Event
	open  bool
}

type jobResultMsg struct {
	id     uint64
	result privileged.Result
}

type model struct {
	homebrew   brew.Homebrew
	info       *info.Loader
	runner     privileged.Runner
	supervisor *Supervisor

	mode          mode
	kind          brew.Kind
	query         string
	width, height int
	contentRows   int
	themeIndex    int
	// outdatedMinimum is the settings threshold, loaded once at construction.
	// ponytail: changing it in the file takes effect next boot — the marks are
	// stamped into listCache at load time; re-stamping the retained lists live
	// is the upgrade if an in-app toggle ever exists.
	outdatedMinimum outdatedThreshold
	settingsPath    string
	snapshotPath    string
	monochrome      bool
	// isDark picks each adaptive color's variant. It defaults to true and is
	// corrected by the terminal's tea.BackgroundColorMsg reply: most terminals
	// are dark, and a wrong first frame recolors rather than breaks.
	isDark bool

	list     list.Model
	help     help.Model
	viewport viewport.Model
	spinner  spinner.Model
	password textinput.Model

	loading       bool
	loadPurpose   loadPurpose
	loadID        uint64
	loadSelection int
	listCancel    context.CancelFunc
	infoPending   bool
	listCache     map[brew.Kind][]brew.Package

	sizes        *brew.Sizes
	sizesID      uint64
	sizesCancel  context.CancelFunc
	sizesPending bool
	sortOrders   map[brew.Kind]sortOrder

	status       string
	priority     bool
	confirmation *brew.Package
	// confirmVerb is the verb the open confirmation dialog is asking about. It
	// is separate from verb because a dialog can open while a job runs, and
	// writing the dialog's verb over the running job's would retarget every
	// user-facing string of an operation already in flight.
	confirmVerb brew.Operation
	operation   *brew.Package
	// queue holds confirmed operations waiting for the running job to finish.
	// Entries carry their own immutable snapshot and verb, taken at
	// confirmation time exactly as a directly-started job's are. FIFO, run
	// strictly serially: brew does not support concurrent mutations, and the
	// job plumbing below is single-slot by design.
	queue []queuedOperation
	// verb is the running job's verb, captured when the job starts and as
	// immutable as its snapshot: every user-facing string and the argv both
	// derive from it, so a later selection change cannot retarget an in-flight
	// operation's wording.
	verb             brew.Operation
	operationID      uint64
	operationCancel  context.CancelFunc
	startPending     bool
	job              privileged.Job
	jobEvents        <-chan privileged.Event
	jobResult        <-chan privileged.Result
	jobPending       bool
	passwordRequest  privileged.RequestID
	passwordAttempts int
	cancelReason     cancelReason
	spinnerActive    bool
	quitExitCode     int
}

// New constructs the complete UI and the supervisor retained by main.
// settingsDir is where settings live (~/lazybrew in main); empty disables
// settings persistence.
func New(homebrew brew.Homebrew, loader *info.Loader, runner privileged.Runner, settingsDir string) (tea.Model, *Supervisor) {
	settingsPath := settingsFile(settingsDir)
	snapshotPath := snapshotFile(settingsDir)
	loaded := ensureSettings(settingsPath)
	items := list.New(nil, packageDelegate{}, 0, 0)
	items.SetShowTitle(false)
	items.SetShowFilter(false)
	items.SetShowStatusBar(false)
	items.SetShowPagination(false)
	items.SetShowHelp(false)
	items.InfiniteScrolling = false
	items.KeyMap = list.KeyMap{}
	items.Filter = substringFilter

	h := help.New()
	h.ShortSeparator = "  "
	h.Ellipsis = ""

	sp := spinner.New()
	sp.Spinner = spinner.Line

	m := &model{
		homebrew:        homebrew,
		info:            loader,
		runner:          runner,
		mode:            modeNormal,
		kind:            brew.Cask,
		list:            items,
		help:            h,
		spinner:         sp,
		password:        blankPasswordInput(),
		monochrome:      os.Getenv("NO_COLOR") != "",
		isDark:          true,
		settingsPath:    settingsPath,
		snapshotPath:    snapshotPath,
		themeIndex:      themeIndexByName(loaded.Theme),
		outdatedMinimum: thresholdByName(loaded.OutdatedThreshold),
		loading:         true,
		loadPurpose:     loadStartup,
		spinnerActive:   true,
		listCache:       make(map[brew.Kind][]brew.Package),
		sortOrders:      make(map[brew.Kind]sortOrder),
	}
	// Seed the session caches from the last run's snapshot so Init can paint
	// before the first brew call returns. The loads Init starts replace all of
	// it; nothing here is trusted beyond the first frames.
	if snap := loadSnapshot(snapshotPath); len(snap.Lists) > 0 {
		for kind, packages := range snap.Lists {
			m.listCache[kind] = packages
		}
		m.sizes = snap.Sizes
	}
	m.viewport = viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	m.viewport.SoftWrap = true
	m.viewport.FillHeight = true
	s := &Supervisor{info: loader}
	m.supervisor = s
	return m, s
}

func substringFilter(term string, targets []string) []list.Rank {
	term = strings.ToLower(term)
	matches := make([]list.Rank, 0, len(targets))
	for i, target := range targets {
		if strings.Contains(strings.ToLower(target), term) {
			matches = append(matches, list.Rank{Index: i})
		}
	}
	return matches
}

func (m *model) Init() tea.Cmd {
	// A seeded boot paints the previous session's inventory in the first frame
	// and reloads underneath it, so the startup load is a refresh in every
	// sense, including its status line. Showing the stale rows is safe: every
	// argv re-validates names, and brew itself is the authority for every verb.
	purpose, seedInfo := loadStartup, tea.Cmd(nil)
	if cached, ok := m.listCache[m.kind]; ok {
		m.setPackages(cached, 0)
		purpose = loadRefresh
		seedInfo = m.selectInfo()
	}
	return tea.Batch(m.startList(purpose, 0), m.startSizes(), m.spinner.Tick, tea.RequestBackgroundColor, seedInfo)
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		return m, m.resize(msg.Width, msg.Height)
	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		return m, nil
	case tea.ColorProfileMsg:
		profile := msg.Profile.String()
		m.monochrome = os.Getenv("NO_COLOR") != "" ||
			profile == "Unknown" || profile == "NoTTY" || profile == "Ascii"
		return m, nil
	case SignalMsg:
		if msg.ExitCode != 130 && msg.ExitCode != 143 {
			return m, nil
		}
		return m, m.beginQuit(msg.ExitCode)
	case info.Result:
		cmd := m.info.Complete(msg)
		m.infoPending = cmd != nil
		if m.mode == modeQuitting {
			return m, m.finishQuit()
		}
		m.syncInfo()
		return m, cmd
	case listResultMsg:
		return m, m.handleListResult(msg)
	case sizesResultMsg:
		return m, m.handleSizesResult(msg)
	case jobStartedMsg:
		return m, m.handleJobStarted(msg)
	case jobStartFailedMsg:
		return m, m.handleJobStartFailed(msg)
	case jobEventMsg:
		return m, m.handleJobEvent(msg)
	case jobResultMsg:
		return m, m.handleJobResult(msg)
	case spinner.TickMsg:
		if !m.spinnerActive {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.PasteMsg:
		if m.mode == modePassword {
			return m, nil
		}
	}

	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		return m, m.beginQuit(130)
	}
	if m.width < minimumWidth || m.height < minimumHeight {
		if key.String() == "q" || key.String() == "Q" {
			return m, m.beginQuit(0)
		}
		return m, nil
	}
	if m.mode == modeQuitting {
		return m, nil
	}
	if m.loading {
		if m.loadPurpose != loadAfterOperation && (key.String() == "q" || key.String() == "Q") {
			return m, m.beginQuit(0)
		}
		return m, nil
	}

	switch m.mode {
	case modeSearch:
		return m, m.updateSearch(key)
	case modeConfirm:
		return m, m.updateConfirmation(key)
	case modePassword:
		return m, m.updatePassword(key)
	case modeOperation:
		// A running job acts only on its immutable snapshot, so the list stays
		// browsable underneath it. updateNormal's jobRunning guards keep every
		// mutating or mode-leaving key dead; see jobRunning for the boundary.
		return m, m.updateNormal(key)
	default:
		return m, m.updateNormal(key)
	}
}

// operationWords carries the exact user-facing spellings for one operation.
// Written out rather than derived from the verb: "upgrade" gerunds to "upgrading"
// and "uninstall" to "uninstalling", so any suffix rule would be wrong for one of
// them, and every one of these strings is pinned by SPEC.
type operationWords struct {
	lower  string
	title  string
	gerund string
	past   string
}

func words(op brew.Operation) operationWords {
	if op == brew.Upgrade {
		return operationWords{lower: "upgrade", title: "Upgrade", gerund: "Upgrading", past: "Upgraded"}
	}
	return operationWords{lower: "uninstall", title: "Uninstall", gerund: "Uninstalling", past: "Uninstalled"}
}

func confirmTitle(op brew.Operation) string    { return "Confirm " + words(op).lower }
func cancelledStatus(op brew.Operation) string { return words(op).title + " cancelled" }
func tooSmallStatus(op brew.Operation) string {
	return "Terminal too small; " + words(op).lower + " cancelled"
}
func startFailedStatus(op brew.Operation) string { return "Could not start " + words(op).lower }
func progressStatus(op brew.Operation, name string) string {
	return words(op).gerund + " " + name + "..."
}
func doneStatus(op brew.Operation, name string) string { return words(op).past + " " + name }
func cleanupFailedStatus(op brew.Operation, err string) string {
	return words(op).title + " cleanup failed: " + err
}

// jobRunning reports the window between a confirmed start and the moment the
// job's ending reload lands, which is exactly when m.operation carries the
// immutable snapshot. Browsing is safe in that window - the job reads nothing
// from the list - but every key that could mutate the inventory, start a
// second job, reload underneath the running one, or leave modeOperation is
// dead. Leaving the mode matters as much as mutating: handleJobEvent cancels
// the job on any event that arrives outside modeOperation, so entering search
// mid-job would turn brew's password prompt into a cancelled operation. Dead
// keys here follow the loading windows' precedent: silent, with the footer
// naming the window browse-only.
func (m *model) jobRunning() bool { return m.operation != nil }

// queuedOperation is one confirmed, not-yet-started operation.
type queuedOperation struct {
	verb brew.Operation
	pkg  brew.Package
}

// pendingOperation reports whether the same verb is already running or queued
// for the same package. That request adds no work, only a duplicate
// destructive entry, so confirmOperation refuses it instead of double-queuing.
func (m *model) pendingOperation(op brew.Operation, pkg brew.Package) bool {
	if m.jobRunning() && m.verb == op && m.operation.Kind == pkg.Kind && m.operation.Name == pkg.Name {
		return true
	}
	for _, entry := range m.queue {
		if entry.verb == op && entry.pkg.Kind == pkg.Kind && entry.pkg.Name == pkg.Name {
			return true
		}
	}
	return false
}

// isQueued marks list rows; the running row carries the spinner instead.
func (m *model) isQueued(pkg brew.Package) bool {
	for _, entry := range m.queue {
		if entry.pkg.Kind == pkg.Kind && entry.pkg.Name == pkg.Name {
			return true
		}
	}
	return false
}

// dropQueue empties the queue and reports what it held. Every path that ends a
// job in anything but success calls it: destructive work must never continue
// past a failure or a cancel signal the user aimed at the run.
func (m *model) dropQueue() int {
	dropped := len(m.queue)
	m.queue = nil
	if dropped > 0 {
		slog.Debug("queue dropped", "entries", dropped)
	}
	return dropped
}

// queueDropSuffix names the collateral of a failed or cancelled job. Dropping
// queued entries silently would read as work done.
func queueDropSuffix(dropped int) string {
	if dropped == 0 {
		return ""
	}
	return " · " + strconv.Itoa(dropped) + " queued dropped"
}

func queuedStatus(op brew.Operation, name string) string {
	return "Queued " + words(op).lower + " " + name
}

func isSelfUninstall(op brew.Operation, pkg brew.Package) bool {
	return op == brew.Uninstall && pkg.Kind == brew.Cask && pkg.Name == "lazybrew"
}

func (m *model) updateNormal(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "up", "k":
		before := m.list.Index()
		m.list.CursorUp()
		if m.list.Index() != before {
			return m.selectInfo()
		}
	case "down", "j":
		before := m.list.Index()
		m.list.CursorDown()
		if m.list.Index() != before {
			return m.selectInfo()
		}
	case "/", "s", "S":
		if m.jobRunning() {
			return nil
		}
		m.mode = modeSearch
	case "tab":
		return m.switchKind()
	case "d", "D":
		return m.confirmOperation(brew.Uninstall)
	case "u", "U":
		return m.confirmOperation(brew.Upgrade)
	case "t", "T":
		m.themeIndex = (m.themeIndex + 1) % len(themes)
		m.status, m.priority = "Theme: "+themes[m.themeIndex].name, false
		saveSettings(m.settingsPath, settings{Theme: themes[m.themeIndex].name})
	case "a", "A":
		// The key edits the query's is:dep token: the query is the ONLY
		// filter state, so the key and a typed search can never disagree —
		// and esc, which clears the query, clears this too, by the same rule.
		// Casks have no dependency relation, so only the status changes
		// there. The token still toggles, so the key is never silently dead
		// and a later `tab` lands in the requested state.
		var shown bool
		m.query, shown = toggleQualifier(m.query, "is:dep")
		if m.kind != brew.Formula {
			m.status, m.priority = dependencyStatus(shown), false
			return nil
		}
		cmd, applied := m.reorder()
		if applied {
			m.status, m.priority = dependencyStatus(shown), false
		}
		return cmd
	case "f", "F":
		// The quick filter writes the query it stands for — is:outdated, at
		// the front of the search row — so one use teaches the syntax the
		// search footer names. It edits the query rather than a flag for the
		// same reason `a` does: one filter state, no second opinion.
		var on bool
		m.query, on = toggleQualifier(m.query, "is:outdated")
		cmd, applied := m.reorder()
		if applied {
			m.status, m.priority = filterStatus(on), false
		}
		return cmd
	case "o", "O":
		// Screen-aware: the key cycles the order of the list it is pressed
		// over and nothing else — flipping shared state from here would
		// re-order a screen the user is not looking at. Formulae cycle
		// name ↑ → size ↓ → size ↑ → name ↓; casks, unsized by design (see
		// brew.Sizes), cycle the two name orders only.
		m.sortOrders[m.kind] = m.sortOrders[m.kind].next(m.kind)
		cmd, applied := m.reorder()
		if applied {
			m.status, m.priority = m.sortOrders[m.kind].status(), false
		}
		return cmd
	case "r", "R":
		// A reload here would race the invalidate-and-reload that ends the job,
		// and the job's own completion refreshes everything anyway.
		if m.jobRunning() {
			return nil
		}
		selection := m.list.Index()
		sizesCmd := m.invalidateCaches()
		return tea.Batch(m.startList(loadRefresh, selection), sizesCmd, m.spinner.Tick)
	case "q", "Q":
		// ctrl+c stays the only quit while a job runs, exactly as it was when
		// this window swallowed every key.
		if m.jobRunning() {
			return nil
		}
		return m.beginQuit(0)
	}
	return nil
}

// switchKind toggles cask <-> formula. The query stays active, so the target
// list arrives already filtered.
//
// A kind already listed this session is served from listCache and renders in
// the same frame as the key press, starting no command at all. Map presence is
// the hit test, not slice length, so a kind with nothing installed is a hit too
// rather than re-shelling on every switch.
// confirmOperation opens the confirmation for one privileged verb. Both verbs
// share this path deliberately: the lowercase-y discipline, the immutable
// snapshot, and the fit check are the security-relevant parts and must never be
// re-derived per verb.
func (m *model) confirmOperation(op brew.Operation) tea.Cmd {
	selected := m.selectedPackage()
	if selected == nil {
		return nil
	}
	if op == brew.Upgrade && selected.Pinned {
		m.status, m.priority = selected.Name+" is pinned", false
		return nil
	}
	// An upgrade of a package Homebrew does not report as outdated would be a
	// no-op, so it starts nothing at all: no confirmation, no snapshot, no job.
	// The freshness cell is the affordance that says which rows this key acts on.
	if op == brew.Upgrade && !selected.Outdated {
		// A carried offer on an unmarked row means the outdated threshold
		// suppressed the mark (markOutdated stamps LatestVersion only on rows
		// brew named), so the refusal names the threshold instead of falsely
		// claiming the package is current. One truth: the key follows the
		// mark, and brew stays the executor either way.
		if selected.LatestVersion != "" && selected.LatestVersion != selected.Version {
			m.status, m.priority = selected.Name+": update below the outdated threshold", false
			return nil
		}
		m.status, m.priority = selected.Name+" is up to date", false
		return nil
	}
	// The same verb already running or queued for the same package is refused,
	// not double-queued: the job plumbing stays single-slot and the queue runs
	// strictly serially (brew does not support concurrent mutations), so the
	// duplicate could only run the verb a second time against a package the
	// first run already handled.
	if m.pendingOperation(op, *selected) {
		m.status, m.priority = selected.Name+" already queued", false
		return nil
	}
	if !m.confirmationFits(*selected) {
		m.status, m.priority = "Widen terminal to confirm", true
		return nil
	}
	snapshot := *selected
	m.confirmation = &snapshot
	m.confirmVerb = op
	m.mode = modeConfirm
	m.status, m.priority = confirmTitle(op), true
	return nil
}

func (m *model) switchKind() tea.Cmd {
	// Dead while a job runs: the reload that ends the job targets m.kind, so a
	// mid-job switch would land that reload on the other inventory and report
	// the verb's result against a list it never touched.
	if m.jobRunning() {
		return nil
	}
	m.kind = otherKind(m.kind)
	m.status, m.priority = "", false
	m.info.Select(nil)
	if cached, ok := m.listCache[m.kind]; ok {
		m.setPackages(cached, 0)
		return m.selectInfo()
	}
	m.setPackages(nil, 0)
	return tea.Batch(m.startList(loadSwitch, 0), m.spinner.Tick)
}

// invalidateCaches drops the info cache, the list cache, and the size map
// together. Every site that invalidates one must invalidate the others: anything
// that can change what `brew info` prints can also change what `brew list`
// prints and what the Cellar weighs.
//
// It returns the command that restarts the size pass, so the two call sites stay
// in lockstep by construction rather than by both remembering to do it.
func (m *model) invalidateCaches() tea.Cmd {
	m.info.Refresh(nil)
	clear(m.listCache)
	m.sizes = nil
	return m.startSizes()
}

func (m *model) updateSearch(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "enter":
		m.mode = modeNormal
		return nil
	case "tab":
		// Completion claims tab only while an incomplete is: token is under
		// the cursor: there, a kind switch would be a mode exit the user
		// visibly was not asking for mid-token. Every other query keeps tab's
		// established switch-kind meaning, and the trailing space finishes
		// the token so the next tab is a kind switch again. Rejoining on
		// single spaces normalizes machine-written text, the same contract
		// toggleQualifier documents.
		if completed := completeQualifier(m.query); completed != "" {
			fields := strings.Fields(m.query)
			fields[len(fields)-1] = completed
			m.query = strings.Join(fields, " ") + " "
			m.applyQuery(0)
			return m.selectInfo()
		}
		// The list result returns to normal mode anyway; leave search here so the
		// mode change is visible before the load rather than after it.
		m.mode = modeNormal
		return m.switchKind()
	case "esc":
		m.query = ""
		m.applyQuery(0)
		m.mode = modeNormal
		return m.selectInfo()
	case "backspace", "ctrl+h":
		if m.query == "" {
			return nil
		}
		runes := []rune(m.query)
		m.query = string(runes[:len(runes)-1])
		m.applyQuery(0)
		return m.selectInfo()
	case "delete":
		return nil
	}
	if text := key.Key().Text; text != "" {
		m.query += text
		m.applyQuery(0)
		return m.selectInfo()
	}
	return nil
}

func (m *model) updateConfirmation(key tea.KeyPressMsg) tea.Cmd {
	snapshot := m.confirmation
	m.confirmation = nil
	// The dialog returns to the window it opened over: plain browsing, or
	// browsing under a running job.
	resume := modeNormal
	if m.jobRunning() {
		resume = modeOperation
	}
	if key.Key().Text != "y" || snapshot == nil {
		m.mode = resume
		m.status, m.priority = cancelledStatus(m.confirmVerb), true
		return nil
	}
	if !m.confirmationFits(*snapshot) {
		m.mode = resume
		m.status, m.priority = tooSmallStatus(m.confirmVerb), true
		return nil
	}
	// Mid-job the confirmed entry queues instead of starting: one job at a
	// time is the invariant, the queue is how a second request waits its turn.
	if m.jobRunning() {
		m.queue = append(m.queue, queuedOperation{verb: m.confirmVerb, pkg: *snapshot})
		m.mode = modeOperation
		m.status, m.priority = queuedStatus(m.confirmVerb, snapshot.Name), true
		slog.Debug("job queued", "verb", m.confirmVerb.Verb(), "name", snapshot.Name, "queueLength", len(m.queue))
		return nil
	}
	m.verb = m.confirmVerb
	return m.startUninstall(*snapshot)
}

func (m *model) updatePassword(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "enter":
		value := m.password.Value()
		payload := []byte(value)
		request := m.passwordRequest
		m.wipePassword()
		m.mode = modeOperation
		err := m.job.RespondPassword(request, payload)
		for i := range payload {
			payload[i] = 0
		}
		value = ""
		if err != nil {
			m.job.Cancel()
			m.cancelReason = cancelAuthentication
			m.status, m.priority = "Administrator authentication failed", true
		}
		return nil
	case "esc":
		m.job.CancelPassword(m.passwordRequest)
		m.job.Cancel()
		m.wipePassword()
		m.mode = modeOperation
		m.cancelReason = cancelUser
		m.status, m.priority = "Cancelling "+m.operation.Name+"...", true
		return nil
	case "backspace", "ctrl+h":
		value := m.password.Value()
		runes := []rune(value)
		if len(runes) > 0 {
			m.password.SetValue(string(runes[:len(runes)-1]))
		}
		for i := range runes {
			runes[i] = 0
		}
		value = ""
		return nil
	case "delete":
		return nil
	}
	text := key.Key().Text
	if text == "" {
		return nil
	}
	value := m.password.Value()
	runes := []rune(value)
	runeCount := len(runes)
	byteCount := len(value)
	for _, r := range text {
		size := utf8.RuneLen(r)
		if runeCount == 256 || byteCount+size > 1024 {
			break
		}
		runes = append(runes, r)
		runeCount++
		byteCount += size
	}
	m.password.SetValue(string(runes))
	for i := range runes {
		runes[i] = 0
	}
	value = ""
	return nil
}

func (m *model) startUninstall(snapshot brew.Package) tea.Cmd {
	m.mode = modeOperation
	m.operation = &snapshot
	m.operationID++
	id := m.operationID
	m.passwordAttempts = 0
	m.cancelReason = cancelNone
	m.status, m.priority = progressStatus(m.verb, snapshot.Name), true
	m.spinnerActive = true
	// The privileged job never passes through runTool's command log, so its
	// lifecycle is logged here. Names and verbs only - never password bytes.
	slog.Debug("job started", "verb", m.verb.Verb(), "name", snapshot.Name, "id", id, "queued", len(m.queue))

	ctx, cancel := context.WithCancel(context.Background())
	m.operationCancel = cancel
	m.startPending = true
	startDone := make(chan struct{})
	m.supervisor.setStart(cancel, startDone)
	return tea.Batch(func() tea.Msg {
		job, err := m.runner.Start(ctx, m.verb, snapshot)
		if err != nil {
			close(startDone)
			return jobStartFailedMsg{id: id, err: err, startDone: startDone}
		}
		result := make(chan privileged.Result, 1)
		waitDone := make(chan struct{})
		m.supervisor.setJob(job, waitDone)
		go func() {
			terminal := job.Wait()
			m.supervisor.finishJob(terminal)
			result <- terminal
			close(result)
			close(waitDone)
		}()
		close(startDone)
		return jobStartedMsg{id: id, job: job, events: job.Events(), result: result, startDone: startDone}
	}, m.spinner.Tick)
}

func (m *model) handleJobStarted(msg jobStartedMsg) tea.Cmd {
	m.supervisor.clearStart(msg.startDone)
	if msg.id != m.operationID {
		msg.job.Cancel()
		return nil
	}
	m.startPending = false
	m.jobPending = true
	m.job, m.jobEvents, m.jobResult = msg.job, msg.events, msg.result
	if m.cancelReason != cancelNone || m.mode == modeQuitting {
		msg.job.Cancel()
	}
	if m.mode == modeQuitting {
		return waitJobResult(msg.id, msg.result)
	}
	return tea.Batch(waitJobEvent(msg.id, msg.events), waitJobResult(msg.id, msg.result))
}

func (m *model) handleJobStartFailed(msg jobStartFailedMsg) tea.Cmd {
	m.supervisor.clearStart(msg.startDone)
	if msg.id != m.operationID {
		return nil
	}
	m.startPending = false
	if m.mode == modeQuitting {
		return m.finishQuit()
	}
	m.spinnerActive = false
	if m.operationCancel != nil {
		m.operationCancel()
	}
	m.operation, m.operationCancel = nil, nil
	m.mode = modeNormal
	if m.cancelReason == cancelTerminal {
		m.status = tooSmallStatus(m.verb)
	} else if msg.err != nil {
		m.status = flattenStatus(msg.err.Error())
	} else {
		m.status = startFailedStatus(m.verb)
	}
	m.status += queueDropSuffix(m.dropQueue())
	m.priority = true
	return nil
}

func waitJobEvent(id uint64, events <-chan privileged.Event) tea.Cmd {
	return func() tea.Msg {
		event, open := <-events
		return jobEventMsg{id: id, event: event, open: open}
	}
}

func waitJobResult(id uint64, result <-chan privileged.Result) tea.Cmd {
	return func() tea.Msg { return jobResultMsg{id: id, result: <-result} }
}

func (m *model) handleJobEvent(msg jobEventMsg) tea.Cmd {
	if msg.id != m.operationID || m.mode == modeQuitting {
		return nil
	}
	if m.job == nil {
		return nil
	}
	if !msg.open {
		return nil
	}
	if msg.event.Type != privileged.PasswordRequested || (m.mode != modeOperation && m.mode != modeConfirm) || m.cancelReason != cancelNone {
		if m.mode == modePassword {
			m.wipePassword()
			m.mode = modeOperation
			m.status, m.priority = "Administrator authentication failed", true
			m.cancelReason = cancelAuthentication
		}
		m.job.Cancel()
		return waitJobEvent(msg.id, m.jobEvents)
	}
	// An auth prompt arriving under an open enqueue dialog takes the window:
	// sudo's prompt is time-boxed, the dialog is cheap to re-request. The
	// pending confirmation is dropped rather than silently enqueued - a
	// half-confirmed destructive entry must never survive a mode hijack.
	if m.mode == modeConfirm {
		m.confirmation = nil
		m.status, m.priority = cancelledStatus(m.confirmVerb), true
	}
	m.passwordAttempts++
	m.passwordRequest = msg.event.RequestID
	m.password = newPasswordInput(m.width)
	m.mode = modePassword
	focus := m.password.Focus()
	return tea.Batch(focus, waitJobEvent(msg.id, m.jobEvents))
}

func (m *model) handleJobResult(msg jobResultMsg) tea.Cmd {
	if msg.id != m.operationID {
		return nil
	}
	m.supervisor.finishJob(msg.result)
	m.wipePassword()
	m.spinnerActive = false
	m.job, m.jobEvents, m.jobResult = nil, nil, nil
	m.jobPending = false
	if m.operationCancel != nil {
		m.operationCancel()
	}
	m.operationCancel = nil
	if m.mode == modeQuitting {
		return m.finishQuit()
	}

	result := msg.result
	name := ""
	if m.operation != nil {
		name = m.operation.Name
	}
	slog.Debug("job finished",
		"verb", m.verb.Verb(), "name", name, "id", msg.id,
		"cancelled", result.Cancelled, "authFailed", result.AuthFailed,
		"authTimedOut", result.AuthTimedOut, "err", result.Err, "queued", len(m.queue))
	switch {
	case result.CleanupErr != nil:
		status := cleanupFailedStatus(m.verb, flattenStatus(result.CleanupErr.Error()))
		cmd := m.beginQuit(1)
		m.status, m.priority = status, true
		return cmd
	case result.AuthTimedOut:
		m.finishOperation("Administrator authentication timed out")
	case result.AuthFailed:
		m.finishOperation("Administrator authentication failed")
	case result.Cancelled:
		switch m.cancelReason {
		case cancelTerminal:
			m.finishOperation(tooSmallStatus(m.verb))
		case cancelAuthentication:
			m.finishOperation("Administrator authentication failed")
		default:
			m.finishOperation(cancelledStatus(m.verb))
		}
	case result.Err != nil:
		m.finishOperation(flattenStatus(result.Err.Error()))
	default:
		if m.operation != nil && isSelfUninstall(m.verb, *m.operation) {
			return m.beginQuit(0)
		}
		// One invalidate+reload for the whole run, when the queue drains: each
		// pop starts the next job directly, so the list stays browse-only and
		// deliberately stale mid-queue - the same contract a single job's
		// window has. A reload here would also be invalidated again by the
		// very next job.
		if len(m.queue) > 0 {
			next := m.queue[0]
			m.queue = m.queue[1:]
			m.verb = next.verb
			slog.Debug("job popped from queue", "verb", next.verb.Verb(), "name", next.pkg.Name, "remaining", len(m.queue))
			return m.startUninstall(next.pkg)
		}
		selection := m.list.Index()
		sizesCmd := m.invalidateCaches()
		m.mode = modeOperation
		m.spinnerActive = true
		return tea.Batch(m.startList(loadAfterOperation, selection), sizesCmd, m.spinner.Tick)
	}
	return nil
}

func (m *model) finishOperation(status string) {
	m.mode = modeNormal
	m.status, m.priority = status+queueDropSuffix(m.dropQueue()), true
	m.operation = nil
	m.cancelReason = cancelNone
}

func (m *model) startList(purpose loadPurpose, selection int) tea.Cmd {
	m.loading = true
	m.loadPurpose = purpose
	m.loadSelection = selection
	m.loadID++
	id, kind := m.loadID, m.kind
	m.spinnerActive = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.supervisor.setList(cancel, done)
	m.listCancel = cancel
	homebrew, threshold := m.homebrew, m.outdatedMinimum
	return func() tea.Msg {
		// Two concurrent reads, one command: the inventory and Homebrew's outdated
		// verdict for the same kind, joined before either local is read. The
		// annotated packages reach the existing message, cache, and renderers, so
		// a cached kind switch keeps its marks and nothing downstream changes.
		var (
			wg            sync.WaitGroup
			packages      []brew.Package
			listErr       error
			outdated      []brew.OutdatedPackage
			outdatedKnown bool
			untrusted     []brew.UntrustedPackage
		)
		wg.Add(3)
		go func() {
			defer wg.Done()
			packages, listErr = homebrew.List(ctx, kind)
		}()
		go func() {
			defer wg.Done()
			// A failed outdated read is absorbed, exactly as a failed dependent
			// lookup is: the list still loads and simply carries no marks. Absence
			// of evidence must never render as an assurance.
			if rows, err := homebrew.Outdated(ctx, kind); err == nil {
				outdated, outdatedKnown = rows, true
			}
		}()
		go func() {
			defer wg.Done()
			// Absorbed exactly like a failed outdated read, and this is also the
			// path a Homebrew without `brew trust` takes: the list still loads and
			// simply carries no trust marks.
			if names, err := homebrew.Untrusted(ctx, kind); err == nil {
				untrusted = names
			}
		}()
		wg.Wait()
		close(done)
		return listResultMsg{
			id:       id,
			kind:     kind,
			purpose:  purpose,
			packages: markUntrusted(markOutdated(packages, outdated, outdatedKnown, threshold), untrusted),
			err:      listErr,
			done:     done,
		}
	}
}

// markOutdated stamps Homebrew's verdict onto the inventory rows it names. The
// outdated set is matched by name against the visible rows only; for formulae it
// legitimately also names the dependency-only ones the list never shows, so its
// size and the list's are unrelated.
//
// A marked row also gains the versions the verdict came with: LatestVersion
// always, and Version only where the inventory left it blank — both `brew
// list` forms print bare names, so without this the row would show an arrow
// pointing from nothing.
func markOutdated(packages []brew.Package, outdated []brew.OutdatedPackage, known bool, threshold outdatedThreshold) []brew.Package {
	// Stamped on every row, not only the named ones, so the detail panel can tell
	// "Homebrew says this is current" from "Homebrew was never asked".
	for i := range packages {
		packages[i].OutdatedKnown = known
	}
	if len(outdated) == 0 {
		return packages
	}
	byName := make(map[string]brew.OutdatedPackage, len(outdated))
	for _, row := range outdated {
		byName[row.Name] = row
	}
	for i := range packages {
		row, ok := byName[packages[i].Name]
		if !ok {
			continue
		}
		// The versions ride along even when the threshold below suppresses
		// the mark: the info pane still truthfully says "latest X", and the
		// upgrade guard uses their presence to tell a suppressed row from a
		// genuinely current one.
		packages[i].LatestVersion = row.Latest
		if packages[i].Version == "" {
			packages[i].Version = row.Installed
		}
		// Classified over the verdict's own version pair, not the
		// inventory's, so the threshold judges exactly what brew compared.
		// An unreadable pair is DistanceUnknown, which clears every
		// threshold: a mark is never hidden behind a string we could not
		// read.
		if threshold.allows(brew.VersionDistance(row.Installed, row.Latest)) {
			packages[i].Outdated = true
		}
	}
	return packages
}

// markUntrusted stamps brew's trust verdict onto the inventory rows it names,
// matched by name against the rows exactly as markOutdated is. No Known twin:
// nothing anywhere renders an assurance of trust, so absence of evidence
// already renders as absence of the mark.
func markUntrusted(packages []brew.Package, identities []brew.UntrustedPackage) []brew.Package {
	if len(identities) == 0 {
		return packages
	}
	untrusted := make(map[string]brew.UntrustedPackage, len(identities))
	for _, identity := range identities {
		untrusted[identity.Name] = identity
	}
	for i := range packages {
		if identity, ok := untrusted[packages[i].Name]; ok {
			packages[i].Untrusted = true
			packages[i].FullName = identity.FullName
			packages[i].Tap = identity.Tap
		}
	}
	return packages
}

// startSizes measures the whole fleet in one pass, in its own command.
//
// It deliberately does not touch m.loading, m.spinnerActive, or the priority
// status: the list must render and be fully navigable before sizes arrive, and a
// size failure must never displace a message like `Uninstalled <name>`. The
// closure captures only the context and the id; every model mutation happens in
// Update when the typed result lands.
func (m *model) startSizes() tea.Cmd {
	// Cancel the pass this one supersedes before overwriting its handles. Without
	// this, r pressed twice inside the measurement window leaves the first du
	// running with its context leaked and its completion handle no longer
	// reachable from the supervisor, which can then neither cancel nor await it.
	if m.sizesCancel != nil {
		m.sizesCancel()
	}
	m.sizesID++
	id := m.sizesID
	m.sizesPending = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.supervisor.setSizes(cancel, done)
	m.sizesCancel = cancel
	homebrew := m.homebrew
	return func() tea.Msg {
		sizes, err := homebrew.Sizes(ctx)
		close(done)
		return sizesResultMsg{id: id, sizes: sizes, err: err, done: done}
	}
}

func (m *model) handleSizesResult(msg sizesResultMsg) tea.Cmd {
	m.supervisor.clearSizes(msg.done)
	if msg.id != m.sizesID {
		return nil
	}
	slog.Debug("sizes result", "formulae", len(msg.sizes.Formula), "totalKB", msg.sizes.Total, "err", msg.err)
	if m.sizesCancel != nil {
		m.sizesCancel()
	}
	m.sizesCancel = nil
	m.sizesPending = false
	if m.mode == modeQuitting {
		return m.finishQuit()
	}
	if msg.err != nil {
		// Ordinary, never priority, and only into an empty slot, so a failed
		// measurement cannot overwrite a real message. A failure caches nothing,
		// mirroring a failed list result.
		if m.status == "" {
			m.status, m.priority = flattenStatus(msg.err.Error()), false
		}
		return nil
	}
	sizes := msg.sizes
	m.sizes = &sizes
	m.persistSnapshot()
	// Sizes arriving is the moment a size sort requested earlier becomes
	// meaningful, so re-order from retention. ponytail: this resets selection to
	// the top, because the order changed underneath the cursor. It happens at
	// most once, only in the first seconds, and only if `o` was pressed before
	// the pass landed. Preserving the selected package by name is the fix if that
	// proves annoying.
	//
	// Only a running job is excluded. The list is browsable there, but the
	// reload that ends the job calls setPackages, which re-applies the sort
	// anyway, so re-ordering under a browsing cursor mid-job would move rows
	// twice for nothing. A confirmation or password dialog is a centered overlay over a list
	// the section 9 immutable snapshot already protects, so re-ordering underneath
	// it changes nothing that matters - and skipping it there left the request
	// dropped with no retry, so cancelling the dialog returned to a list in source
	// order while the status still claimed a size sort.
	if m.sortOrders[m.kind].bySize() && !m.loading && m.mode != modeOperation {
		if cached, ok := m.listCache[m.kind]; ok {
			m.setPackages(cached, 0)
			return m.selectInfo()
		}
	}
	return nil
}

func (m *model) handleListResult(msg listResultMsg) tea.Cmd {
	m.supervisor.clearList(msg.done)
	if msg.id != m.loadID {
		return nil
	}
	slog.Debug("list result", "kind", msg.kind, "rows", len(msg.packages), "err", msg.err)
	if m.listCancel != nil {
		m.listCancel()
	}
	m.listCancel = nil
	m.loading = false
	m.spinnerActive = false
	if m.mode == modeQuitting {
		return m.finishQuit()
	}
	if msg.purpose == loadAfterOperation && m.cancelReason == cancelTerminal {
		m.finishOperation(tooSmallStatus(m.verb))
		return m.selectInfo()
	}
	if msg.err != nil {
		m.setPackages(nil, 0)
		m.info.Select(nil)
		m.status, m.priority = flattenStatus(msg.err.Error()), false
		if msg.purpose == loadAfterOperation {
			m.finishOperation(m.status)
		}
		return nil
	}
	m.listCache[msg.kind] = msg.packages
	m.persistSnapshot()
	m.setPackages(msg.packages, m.loadSelection)
	if msg.purpose == loadAfterOperation {
		name := ""
		if m.operation != nil {
			name = m.operation.Name
		}
		m.mode = modeNormal
		m.operation = nil
		m.cancelReason = cancelNone
		m.status, m.priority = doneStatus(m.verb, name), true
	} else {
		m.mode = modeNormal
		m.status, m.priority = "", false
	}
	return m.selectInfo()
}

// setPackages is the only place the dependency filter and the size sort live, so
// no caller can disagree about what the list holds. Every path through it — a
// list result, a cached kind switch, a refresh, `d`, `o`, and a late size pass —
// gets the same transform.
//
// The retained slice is never mutated: rows are copied into a new slice before
// sorting, because listCache shares the backing array and sorting in place would
// corrupt retention and make `d` non-idempotent.
func (m *model) setPackages(packages []brew.Package, selection int) {
	parsed := parseQuery(m.query)
	visible := make([]brew.Package, 0, len(packages))
	for _, pkg := range packages {
		// The dependency rule and the is: predicates live here together, next
		// to the sort, so no caller can disagree about what the list holds.
		// The dependency default (hidden) is lifted by is:dep — the query
		// spelling of `a` — rather than narrowed by it; see parseQuery.
		if pkg.Dependency && !parsed.showDeps {
			continue
		}
		if !parsed.match(pkg) {
			continue
		}
		visible = append(visible, pkg)
	}
	switch order := m.sortOrders[m.kind]; {
	case order == sortNameDesc:
		slices.SortStableFunc(visible, func(a, b brew.Package) int {
			return cmp.Compare(b.Name, a.Name)
		})
	case order.bySize() && m.sizes != nil:
		asc := order == sortSizeAsc
		slices.SortStableFunc(visible, func(a, b brew.Package) int {
			aKB, aOK := m.sizes.KB(a.Kind, a.Name)
			bKB, bOK := m.sizes.KB(b.Kind, b.Name)
			// Unmeasured rows sink to the bottom in both directions: a row
			// with no number is not "the smallest", it is unknown, and the
			// ascending order must not lead with blanks.
			if aOK != bOK {
				if aOK {
					return -1
				}
				return 1
			}
			if asc {
				return cmp.Compare(aKB, bKB)
			}
			return cmp.Compare(bKB, aKB)
		})
	}

	items := make([]list.Item, len(visible))
	for i, pkg := range visible {
		items[i] = packageItem{packageValue: pkg}
	}
	m.list.SetItems(items)
	m.list.SetFilterText(parsed.text)
	m.clampSelection(selection)
}

// reorder re-runs setPackages against the retained list for the active kind. It
// starts no command: `d` and `o` are pure re-renders, instant in both directions.
// reorder re-renders the retained list under the current toggles, and reports
// whether it had a list to re-render. With none - after a failed load - the
// caller must not replace whatever the status already says, which is typically
// the load error, with a claim about an order the user cannot see.
// filterStatus reports what `f` just did to its own token, not the whole
// query: other qualifiers may remain, and the search row is the display for
// the full filter state.
func filterStatus(on bool) string {
	if on {
		return "Filter: outdated"
	}
	return "Filter: outdated off"
}

func dependencyStatus(shown bool) string {
	if shown {
		return "Dependencies: shown"
	}
	return "Dependencies: hidden"
}

// sortOrder is one screen's row order. The name orders exist on every screen;
// the size orders exist only where rows carry an honest size, which is the
// formula list (see brew.Sizes). The zero value is source order — the
// alphabetical order brew prints — so an untouched screen needs no map entry.
type sortOrder uint8

const (
	sortNameAsc sortOrder = iota
	sortSizeDesc
	sortSizeAsc
	sortNameDesc
)

// next advances one screen's cycle. Size-descending comes directly after the
// default because "what is eating my disk" is the question this screen exists
// to answer; the size steps are skipped where no row can carry a size.
func (o sortOrder) next(kind brew.Kind) sortOrder {
	if kind != brew.Formula {
		if o == sortNameAsc {
			return sortNameDesc
		}
		return sortNameAsc
	}
	switch o {
	case sortNameAsc:
		return sortSizeDesc
	case sortSizeDesc:
		return sortSizeAsc
	case sortSizeAsc:
		return sortNameDesc
	default:
		return sortNameAsc
	}
}

func (o sortOrder) bySize() bool { return o == sortSizeDesc || o == sortSizeAsc }

// status uses the same ↑/↓ glyphs the table head shows, so the transient
// message and the persistent cue teach the same vocabulary.
func (o sortOrder) status() string {
	switch o {
	case sortSizeDesc:
		return "Sort: size ↓"
	case sortSizeAsc:
		return "Sort: size ↑"
	case sortNameDesc:
		return "Sort: name ↓"
	default:
		return "Sort: name ↑"
	}
}

func (m *model) reorder() (tea.Cmd, bool) {
	cached, ok := m.listCache[m.kind]
	if !ok {
		return nil, false
	}
	m.setPackages(cached, 0)
	return m.selectInfo(), true
}

func (m *model) applyFilter(selection int) {
	m.list.SetFilterText(parseQuery(m.query).text)
	m.clampSelection(selection)
}

// applyQuery re-derives the visible rows after a query change. The is:
// predicates and the dependency rule live in setPackages, so the rebuild runs
// from the retained list; with nothing retained — a failed load — only the
// substring filter can apply, and the next successful load re-runs
// setPackages anyway.
func (m *model) applyQuery(selection int) {
	if cached, ok := m.listCache[m.kind]; ok {
		m.setPackages(cached, selection)
		return
	}
	m.applyFilter(selection)
}

func (m *model) clampSelection(selection int) {
	count := len(m.list.VisibleItems())
	if count == 0 {
		m.list.Select(0)
		return
	}
	if selection < 0 {
		selection = 0
	}
	if selection >= count {
		selection = count - 1
	}
	m.list.Select(selection)
}

func (m *model) selectedPackage() *brew.Package {
	item, ok := m.list.SelectedItem().(packageItem)
	if !ok {
		return nil
	}
	pkg := item.packageValue
	return &pkg
}

func (m *model) selectInfo() tea.Cmd {
	selected := m.selectedPackage()
	m.viewport.SetYOffset(0)
	cmd := m.info.Select(selected)
	if cmd != nil {
		m.infoPending = true
	}
	m.syncInfo()
	return cmd
}

func (m *model) syncInfo() { m.viewport.SetContent(m.info.Text()) }

func (m *model) resize(width, height int) tea.Cmd {
	m.width, m.height = width, height
	m.contentRows = max(0, height-7)
	paneWidth := max(0, width-2)
	if width >= 72 {
		paneWidth = max(0, splitColumn(width)-1)
	}
	selection := m.list.Index()
	// One row of the content area belongs to the table header, so the paginator
	// pages by what is actually drawn below it.
	m.list.SetSize(paneWidth, max(0, m.contentRows-1))
	m.clampSelection(selection)
	m.help.SetWidth(max(0, width-2))
	m.viewport.SetWidth(max(0, width-splitColumn(width)-2))
	m.viewport.SetHeight(max(0, m.contentRows-1))
	if m.mode == modePassword {
		m.password.SetWidth(max(1, min(40, width-16)))
	}

	if m.confirmation != nil && !m.confirmationFits(*m.confirmation) {
		m.confirmation = nil
		m.mode = modeNormal
		if m.jobRunning() {
			m.mode = modeOperation
		}
		m.status, m.priority = tooSmallStatus(m.confirmVerb), true
	}
	if m.operation != nil && !m.confirmationFits(*m.operation) && m.cancelReason == cancelNone {
		m.cancelReason = cancelTerminal
		if m.mode == modePassword && m.job != nil {
			m.job.CancelPassword(m.passwordRequest)
		}
		m.wipePassword()
		if m.job != nil {
			m.job.Cancel()
		}
		if m.operationCancel != nil {
			m.operationCancel()
		}
		if m.loading && m.loadPurpose == loadAfterOperation && m.listCancel != nil {
			m.listCancel()
		}
		m.mode = modeOperation
		m.status, m.priority = tooSmallStatus(m.verb), true
	}
	return nil
}

func (m *model) beginQuit(exitCode int) tea.Cmd {
	if m.mode == modeQuitting {
		if exitCode != 0 {
			m.quitExitCode = exitCode
			m.supervisor.setExitCode(exitCode)
		}
		return m.finishQuit()
	}
	if m.mode == modePassword {
		if m.job != nil {
			m.job.CancelPassword(m.passwordRequest)
		}
		m.wipePassword()
	}
	m.quitExitCode = exitCode
	m.supervisor.setExitCode(exitCode)
	m.mode = modeQuitting
	m.status, m.priority = "Quitting...", true
	m.spinnerActive = false
	m.supervisor.cancel()
	return m.finishQuit()
}

func (m *model) finishQuit() tea.Cmd {
	if m.mode != modeQuitting || m.loading || m.infoPending || m.sizesPending || m.startPending || m.jobPending {
		return nil
	}
	if m.supervisor.cleanupError() != nil {
		m.quitExitCode = 1
	}
	m.supervisor.setExitCode(m.quitExitCode)
	if m.quitExitCode == 130 {
		return func() tea.Msg { return tea.Interrupt() }
	}
	return tea.Quit
}

func blankPasswordInput() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.CharLimit = 256
	input.ShowSuggestions = false
	input.KeyMap = textinput.KeyMap{}
	input.Blur()
	return input
}

func newPasswordInput(width int) textinput.Model {
	input := blankPasswordInput()
	input.SetWidth(max(1, min(40, width-16)))
	return input
}

func (m *model) wipePassword() {
	m.password.Reset()
	m.password.Blur()
	m.password = blankPasswordInput()
	m.passwordRequest = privileged.RequestID{}
}

func otherKind(kind brew.Kind) brew.Kind {
	if kind == brew.Cask {
		return brew.Formula
	}
	return brew.Cask
}

// installedStatus is computed at render time from the live list rather than
// frozen into m.status at load time, so it cannot go stale behind a query that
// was typed, cleared, or carried across a kind switch. It reports nothing for
// an empty list; the list pane's own empty state already says that, and a
// failed load would otherwise read as "0 installed".
func (m *model) installedStatus() string {
	// The base is what the current dependency visibility would show with no
	// narrowing at all, so is:dep never yields "304 of 296": lifting the hide
	// grows the base, while predicates and text only shrink the shown count
	// within it.
	parsed := parseQuery(m.query)
	base := 0
	for _, pkg := range m.listCache[m.kind] {
		if pkg.Dependency && !parsed.showDeps {
			continue
		}
		base++
	}
	if base == 0 {
		return ""
	}
	// A cleared list — failed load, empty inventory — shows no count at all,
	// unless the query itself emptied it, where "0 of N match" is the honest
	// answer to what the user typed.
	if len(m.list.Items()) == 0 && !parsed.narrowing() {
		return ""
	}
	noun := "casks"
	if m.kind == brew.Formula {
		noun = "formulae"
	}
	if shown := len(m.list.VisibleItems()); shown != base {
		return strconv.Itoa(shown) + " of " + strconv.Itoa(base) + " " + noun + " match"
	}
	return strconv.Itoa(base) + " " + noun + " installed"
}

func flattenStatus(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}
