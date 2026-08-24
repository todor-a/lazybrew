package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazybrew/internal/brew"
	"lazybrew/internal/info"
)

func (m *model) View() tea.View {
	if m.width < minimumWidth || m.height < minimumHeight {
		view := tea.NewView("lazybrew: terminal too small (need 32x9)")
		view.AltScreen = true
		return view
	}

	base := m.baseView()
	content := base
	if m.mode == modeConfirm && m.confirmation != nil {
		content = centerLayer(base, m.confirmationModal(*m.confirmation), m.width, m.height)
	} else if m.mode == modePassword {
		content = centerLayer(base, m.passwordModal(), m.width, m.height)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m *model) baseView() string {
	interiorWidth := m.width - 2
	borderRole := m.currentTheme().border
	if m.mode == modeSearch {
		borderRole = m.currentTheme().search
	}
	rule := roleStyle(borderRole).Render(strings.Repeat("─", interiorWidth))
	lines := make([]string, 0, m.height-2)
	lines = append(lines, m.styledLine(m.headerLine(), interiorWidth, roleStyle(m.currentTheme().header)))
	lines = append(lines, rule)
	lines = append(lines, m.contentLines()...)
	lines = append(lines, rule)

	statusStyle := roleStyle(m.currentTheme().status)
	if m.mode == modeSearch {
		statusStyle = roleStyle(m.currentTheme().search).Bold(true)
	}
	lines = append(lines, m.styledLine(m.statusLine(), interiorWidth, statusStyle))
	lines = append(lines, m.footerLine(interiorWidth))

	border := borderStyle(borderRole)
	return border.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// headerLine carries the tab bar and, once measured, the fleet total right
// aligned against the interior edge.
//
// It goes here rather than in the status row because the status row already
// carries query, count, and errors and is the first thing clipped at 32 columns,
// while this row holds a 22-cell label. The value is bare, with no label, because
// 22 + 1 + 6 fits the 30-cell minimum interior and a labelled form does not; it
// uses the same glyphs as the row column, so it reads as that column's sum.
//
// It reports the whole fleet, not the visible subset: that is the number the
// question asks for, and it does not flicker as the query or the dependency
// toggle changes. It is omitted rather than clipped when it will not fit, so it
// can never disturb the tab bar's pinned cell slots.
func (m *model) headerLine() string {
	label := activeListLabel(m.kind)
	// The total is the Cellar's, so it belongs on the list it describes. Rendering
	// it over the cask list would put a formula figure above cask rows, with
	// nothing on screen saying which fleet it counted.
	if m.sizes == nil || m.kind != brew.Formula {
		return label
	}
	total := humanKB(m.sizes.Total)
	// The layout guarantees this fits: the minimum interior is 30 cells and the
	// tab bar plus a gap plus a six-cell total is 29.
	gap := m.width - 2 - lipgloss.Width(label) - lipgloss.Width(total)
	return label + strings.Repeat(" ", gap) + total
}

// rowSize is blank until the pass lands and for any package it did not measure,
// so a moved root or a Homebrew layout change renders an empty column rather
// than a wrong number.
func (m *model) rowSize(pkg brew.Package) string {
	if m.sizes == nil {
		return ""
	}
	kilobytes, ok := m.sizes.KB(pkg.Kind, pkg.Name)
	if !ok {
		return ""
	}
	return humanKB(kilobytes)
}

// humanKB spells a size the way Homebrew's own info output does, so the row
// column and the info pane agree. It never exceeds 6 cells below 10000 GB: the
// decimal is dropped from 100 GB up, where it would otherwise overflow.
func humanKB(kilobytes int64) string {
	switch {
	case kilobytes < 0:
		return ""
	case kilobytes < 1024:
		return strconv.FormatInt(kilobytes, 10) + "KB"
	case kilobytes < 1024*1024:
		return strconv.FormatInt((kilobytes+512)/1024, 10) + "MB"
	case kilobytes < 100*1024*1024:
		return strconv.FormatFloat(float64(kilobytes)/(1024*1024), 'f', 1, 64) + "GB"
	default:
		return strconv.FormatInt((kilobytes+512*1024)/(1024*1024), 10) + "GB"
	}
}

func padLeft(value string, width int) string {
	return strings.Repeat(" ", max(0, width-lipgloss.Width(value))) + value
}

// activeListLabel names both lists and brackets the active one. Brackets rather
// than a ">" marker: ">" is what the list uses for its selected row, and
// "Apps > Formulae" reads as a breadcrumb path instead of a choice between two
// tabs. Brackets need no color, so the cue survives monochrome themes without
// nesting styles inside the single-style header row.
//
// Each name keeps a fixed 8- and 12-cell slot with the bracket columns reserved
// on both sides, so switching swaps the brackets without shifting either name.
func activeListLabel(kind brew.Kind) string {
	if kind == brew.Cask {
		return "[ Apps ]    Formulae  "
	}
	return "  Apps    [ Formulae ]"
}

func (m *model) contentLines() []string {
	if m.width < 72 {
		return m.listLines(m.width-2, m.contentRows)
	}
	divider := m.width / 2
	leftWidth := divider - 1
	rightWidth := m.width - divider - 2
	left := m.listLines(leftWidth, m.contentRows)
	right := m.infoLines(rightWidth, m.contentRows)
	lines := make([]string, m.contentRows)
	for row := range lines {
		lines[row] = lipgloss.JoinHorizontal(lipgloss.Top, left[row], m.divider(), right[row])
	}
	return lines
}

func (m *model) divider() string {
	role := m.currentTheme().border
	if m.mode == modeSearch {
		role = m.currentTheme().search
	}
	return roleStyle(role).Render("│")
}

func (m *model) listLines(width, height int) []string {
	lines := make([]string, height)
	visible := m.list.VisibleItems()
	if len(visible) == 0 {
		empty := "No packages found"
		if m.query != "" {
			empty = "No matching packages"
		}
		if m.loading {
			// ponytail: the status row already owns the spinner and "Loading ...";
			// an empty-state here would contradict it mid-load.
			empty = ""
		}
		if height > 0 {
			lines[0] = fit(empty, width)
		}
		for i := 1; i < height; i++ {
			lines[i] = strings.Repeat(" ", width)
		}
		return lines
	}

	perPage := m.list.Paginator.PerPage
	if perPage < 1 {
		perPage = max(1, height)
	}
	start := m.list.Paginator.Page * perPage

	// Total pages is derived from the rows actually on screen rather than read
	// from the paginator, so a filter that has just shrunk the list cannot leave
	// the bar sized from stale bookkeeping.
	totalPages := (len(visible) + perPage - 1) / perPage
	bar := m.scrollbarColumn(height, totalPages, m.list.Paginator.Page)
	rowWidth := width
	if bar != nil {
		rowWidth = width - 1
	}

	for row := 0; row < height; row++ {
		index := start + row
		content := strings.Repeat(" ", max(rowWidth, 0))
		if index < len(visible) {
			if item, ok := visible[index].(packageItem); ok {
				content = m.packageLine(item.packageValue, index == m.list.Index(), rowWidth)
			}
		}
		if bar != nil {
			content += bar[row]
		}
		lines[row] = content
	}
	return lines
}

// scrollbarColumn renders the one-cell column that shows which slice of a longer
// list is on screen. It returns nil when everything fits, so the column costs no
// width in that case and a short list keeps the full pane for its rows.
//
// The list is paginated rather than continuously scrolled, so the thumb is sized
// and positioned by page. Deriving the offset from travel and page index — not
// from a proportion of rows — makes the thumb sit flush against the top on the
// first page and flush against the bottom on the last, with no rounding gap that
// would suggest there is more to see.
//
// Only the thumb is drawn; the track is blank. The column sits directly against
// the divider or the border, so a "│" track would render as "││" and read as a
// doubled border rather than as a scrollbar. A visible thumb is itself the signal
// that there is more list than fits, since the column is absent whenever
// everything does fit.
//
// The thumb carries the border role, so it needs no color of its own and reads
// the same under a monochrome theme as under a colored one.
func (m *model) scrollbarColumn(height, totalPages, page int) []string {
	if height <= 0 || totalPages <= 1 {
		return nil
	}
	thumbHeight := max(1, height/totalPages)
	thumbTop := 0
	if travel := height - thumbHeight; travel > 0 {
		thumbTop = travel * page / (totalPages - 1)
	}

	style := roleStyle(m.currentTheme().border)
	column := make([]string, height)
	for row := range column {
		glyph := " "
		if row >= thumbTop && row < thumbTop+thumbHeight {
			glyph = "█"
		}
		column[row] = style.Render(glyph)
	}
	return column
}

// originColumn repurposes the old kind column. In a single-kind list that column
// was a constant, so carrying `dep` there costs no width and turns a constant
// into the dependency marker. It is fixed-width per list rather than per row, so
// the name column cannot shift between rows. Text, not color, per the monochrome
// precedent the tab bar already sets.
func originColumn(pkg brew.Package) string {
	if pkg.Kind != brew.Formula {
		return string(pkg.Kind)
	}
	if pkg.Dependency {
		return "dep    "
	}
	return "formula"
}

// sizeColumnWidth is one space plus six right-aligned cells. It is reserved as
// soon as the name column can still hold its pinned minimum, and rendered blank
// until the size pass lands, so the name column never reflows when sizes arrive
// seconds after first paint.
const sizeColumnWidth = 7

func (m *model) packageLine(pkg brew.Package, selected bool, width int) string {
	marker := " "
	if selected {
		marker = ">"
	}
	// One fixed cell for Homebrew's outdated verdict, immediately after the
	// selection marker so it cannot be clipped away in the narrow layout where
	// the list is the only pane. A glyph rather than a color, for the same reason
	// the scrollbar thumb is one: the cue has to survive a monochrome theme. It
	// sits inside the row string, so it inherits the selected-row style.
	freshness := " "
	if pkg.Outdated {
		freshness = "\u2191"
	}
	origin := originColumn(pkg)
	// The size column is reserved only where a size can honestly be measured,
	// which is the formula list. See rowSize.
	sizeWidth := 0
	if m.kind == brew.Formula && width-5-lipgloss.Width(origin)-sizeColumnWidth >= 8 {
		sizeWidth = sizeColumnWidth
	}
	nameWidth := min(30, max(8, width-5-lipgloss.Width(origin)-sizeWidth))
	name := fit(pkg.Name, nameWidth)
	line := " " + marker + freshness + " " + name + " " + origin
	if pkg.Version != "" {
		line += " " + pkg.Version
	}
	if sizeWidth > 0 {
		line = fit(line, width-sizeWidth) + " " + padLeft(m.rowSize(pkg), sizeWidth-1)
	}
	line = fit(line, width)
	if !selected {
		return line
	}
	if m.monochrome {
		return lipgloss.NewStyle().Reverse(true).Bold(true).Render(line)
	}
	return roleStyle(m.currentTheme().selected).Bold(false).Render(line)
}

func (m *model) infoLines(width, height int) []string {
	lines := make([]string, height)
	selected := m.selectedPackage()
	if selected == nil {
		for i := range lines {
			lines[i] = strings.Repeat(" ", width)
		}
		// A list load clears the selection, so there is no package to head this
		// pane with and no details to fetch yet. Without this the pane is blank
		// for the whole load and only the status row shows anything happening.
		// An empty list that is not loading stays blank: the list pane's own
		// "No packages found" already covers it.
		if m.loading && height > 0 {
			lines[0] = fit(info.LoadingText, width)
		}
		return lines
	}
	if height > 0 {
		lines[0] = fit("Info: "+selected.Name, width)
	}
	if height == 1 {
		return lines
	}
	body := strings.Split(m.viewport.View(), "\n")
	for row := 1; row < height; row++ {
		if row-1 < len(body) {
			lines[row] = fit(body[row-1], width)
		} else {
			lines[row] = strings.Repeat(" ", width)
		}
	}
	return lines
}

func (m *model) statusLine() string {
	status := m.status
	if m.loading {
		switch m.loadPurpose {
		case loadRefresh:
			status = "Refreshing " + kindPlural(m.kind) + "..."
		case loadAfterUninstall:
			status = "Reloading " + kindPlural(m.kind) + "..."
		default:
			status = "Loading " + kindPlural(m.kind) + "..."
		}
	}
	if m.mode == modeSearch && !m.loading {
		return "Search: " + m.query + "_"
	}
	if m.mode == modeConfirm || m.mode == modePassword || m.mode == modeUninstall || m.mode == modeQuitting || m.loading || m.priority {
		if m.spinnerActive && (m.loading || m.mode == modeUninstall || m.mode == modePassword) {
			return m.spinner.View() + " " + status
		}
		return status
	}
	prefix := "Search [/ or s]: " + m.query
	if m.query == "" {
		prefix = "Search [/ or s]: —"
	}
	if count := m.installedStatus(); count != "" {
		prefix += " | " + count
	}
	if status != "" {
		return prefix + " | " + status
	}
	return prefix
}

func kindPlural(kind brew.Kind) string {
	if kind == brew.Cask {
		return "casks"
	}
	return "formulae"
}

func (m *model) footerLine(width int) string {
	keys := normalHelp
	switch {
	case m.mode == modeQuitting:
		keys = cleanupHelp
	case m.loading && m.loadPurpose == loadAfterUninstall:
		keys = progressHelp
	case m.loading:
		keys = loadingHelp
	case m.mode == modeConfirm:
		keys = confirmHelp
	case m.mode == modeUninstall || m.mode == modePassword:
		keys = progressHelp
	}
	h := m.help
	h.SetWidth(0)
	h.Styles = help.Styles{}
	return roleStyle(m.currentTheme().footer).Render(fit(h.View(keys), width))
}

func (m *model) styledLine(value string, width int, style lipgloss.Style) string {
	return style.Render(fit(value, width))
}

func fit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = lipgloss.NewStyle().MaxWidth(width).Render(value)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func (m *model) currentTheme() theme { return themes[m.themeIndex] }

func (m *model) modalStyle() lipgloss.Style {
	return borderStyle(m.currentTheme().border).Padding(0, 1)
}

func (m *model) confirmationModal(pkg brew.Package) string {
	lines := []string{
		roleStyle(m.currentTheme().header).Render("Confirm uninstall"),
		"Uninstall " + pkg.Name + "?",
		roleStyle(m.currentTheme().footer).Render("y: confirm  other: cancel"),
	}
	return m.modalStyle().Render(strings.Join(lines, "\n"))
}

func (m *model) passwordModal() string {
	body := "Homebrew requested administrator access."
	if m.passwordAttempts >= 2 {
		body = "Wrong password? Try again."
	}
	lines := []string{
		roleStyle(m.currentTheme().header).Render("Administrator password"),
		body,
		"Password: " + m.password.View(),
		roleStyle(m.currentTheme().footer).Render("Enter: submit  Esc: cancel"),
	}
	return m.modalStyle().Render(strings.Join(lines, "\n"))
}

func centerLayer(base, overlay string, width, height int) string {
	x := max(0, (width-lipgloss.Width(overlay))/2)
	y := max(0, (height-lipgloss.Height(overlay))/2)
	background := lipgloss.NewLayer(base).X(0).Y(0).Z(0)
	modal := lipgloss.NewLayer(overlay).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(background, modal).Render()
}

func (m *model) confirmationFits(pkg brew.Package) bool {
	if m.width < minimumWidth || m.height < minimumHeight {
		return false
	}
	confirmation := m.confirmationModal(pkg)
	passwordWidth := max(1, min(40, m.width-16))
	passwordLines := func(body string) string {
		return m.modalStyle().Render(strings.Join([]string{
			"Administrator password",
			body,
			"Password: " + strings.Repeat("•", passwordWidth+1),
			"Enter: submit  Esc: cancel",
		}, "\n"))
	}
	firstPassword := passwordLines("Homebrew requested administrator access.")
	retryPassword := passwordLines("Wrong password? Try again.")
	if lipgloss.Width(confirmation) > m.width || lipgloss.Height(confirmation) > m.height ||
		lipgloss.Width(firstPassword) > m.width || lipgloss.Height(firstPassword) > m.height ||
		lipgloss.Width(retryPassword) > m.width || lipgloss.Height(retryPassword) > m.height {
		return false
	}
	available := m.width - 2
	lines := []string{
		"Uninstalling " + pkg.Name + "...",
		"Cancelling " + pkg.Name + "...",
		"Loading casks...",
		"Loading formulae...",
		"Refreshing casks...",
		"Refreshing formulae...",
		"Reloading casks...",
		"Reloading formulae...",
	}
	for _, line := range lines {
		if lipgloss.Width(line) > available {
			return false
		}
	}
	return true
}

var _ help.KeyMap = footerKeys{}
