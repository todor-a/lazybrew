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
	rule := m.roleStyle(borderRole).Render(strings.Repeat("─", interiorWidth))
	lines := make([]string, 0, m.height-2)
	lines = append(lines, fitStyled(m.headerRow(), interiorWidth, lipgloss.NewStyle()))
	lines = append(lines, rule)
	lines = append(lines, m.contentLines()...)
	lines = append(lines, rule)

	statusStyle := m.roleStyle(m.currentTheme().status)
	if m.mode == modeSearch {
		statusStyle = m.roleStyle(m.currentTheme().search).Bold(true)
	}
	lines = append(lines, m.styledLine(m.statusLine(), interiorWidth, statusStyle))
	lines = append(lines, m.footerLine(interiorWidth))

	border := m.borderStyle(borderRole)
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

// headerRow is headerLine with the roles painted on: the active tab carries the
// header accent, everything else is faint. The brackets stay the load-bearing
// cue — the styling only echoes them — so the row still reads on a monochrome
// theme. Cell layout is identical to headerLine's, which the tests pin.
func (m *model) headerRow() string {
	apps, formulae := tabSlots(m.kind)
	active := m.roleStyle(m.currentTheme().header).Bold(true)
	faint := lipgloss.NewStyle().Faint(true)
	appsStyle, formulaeStyle := faint, active
	if m.kind == brew.Cask {
		appsStyle, formulaeStyle = active, faint
	}
	row := appsStyle.Render(apps) + "  " + formulaeStyle.Render(formulae)
	if m.sizes == nil || m.kind != brew.Formula {
		return row
	}
	total := humanKB(m.sizes.Total)
	gap := m.width - 2 - 22 - lipgloss.Width(total)
	return row + strings.Repeat(" ", gap) + faint.Render(total)
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
	apps, formulae := tabSlots(kind)
	return apps + "  " + formulae
}

func tabSlots(kind brew.Kind) (apps, formulae string) {
	if kind == brew.Cask {
		return "[ Apps ]", "  Formulae  "
	}
	return "  Apps  ", "[ Formulae ]"
}

func (m *model) contentLines() []string {
	// The table header owns the first content row, so the list draws one row
	// fewer. resize sizes the paginator from the same figure, so the page size
	// and the rows actually drawn can never disagree.
	listRows := max(0, m.contentRows-1)
	if m.width < 72 {
		return append([]string{m.listHeader(m.width - 2)}, m.listLines(m.width-2, listRows)...)
	}
	divider := splitColumn(m.width)
	leftWidth := divider - 1
	rightWidth := m.width - divider - 2
	left := append([]string{m.listHeader(leftWidth)}, m.listLines(leftWidth, listRows)...)
	right := m.infoLines(rightWidth, m.contentRows)
	lines := make([]string, m.contentRows)
	for row := range lines {
		lines[row] = lipgloss.JoinHorizontal(lipgloss.Top, left[row], m.divider(), right[row])
	}
	return lines
}

// listHeader is the one-row table head above the list. It repeats
// packageLine's cell math — prefix, name, dep, size — so every heading sits
// exactly over its column, and it is chrome: faint like the borders, so it
// never competes with the rows it labels. Headings exist only for columns the
// active list actually renders, which is why the cask list shows neither Dep
// nor Size.
//
// The sort cue is a glyph on the ordered column, not a color, per the
// monochrome precedent the freshness cell sets: ▲ ascending, ▼ descending, on
// whichever column `o`'s cycle has ordered. It follows exactly the condition
// setPackages applies the size sort under (requested by `o` AND sizes have
// landed AND rows that can carry a size), so the cue can never claim an order
// the rows on screen do not have.
func (m *model) listHeader(width int) string {
	dep := ""
	sizeWidth := 0
	if m.kind == brew.Formula {
		dep = "Dep"
		if width-5-len(dep)-sizeColumnWidth >= 8 {
			sizeWidth = sizeColumnWidth
		}
	}
	nameWidth := min(30, max(8, width-5-len(dep)-sizeWidth))
	order := m.sortOrders[m.kind]
	sizeSorted := order.bySize() && m.sizes != nil && m.kind == brew.Formula
	nameLabel := "Name"
	if !sizeSorted {
		if order == sortNameDesc {
			nameLabel += " ▼"
		} else {
			nameLabel += " ▲"
		}
	}
	line := "    " + fit(nameLabel, nameWidth)
	if dep != "" {
		line += " " + dep
	}
	// The version column is the unreserved tail the rows clip from the right,
	// so in a pane too narrow to show a whole version its heading is omitted
	// rather than clipped: "Vers" over a four-cell sliver would label noise.
	if lipgloss.Width(line)+len(" Version") <= width-sizeWidth {
		line += " Version"
	}
	if sizeWidth > 0 {
		sizeLabel := "Size"
		if sizeSorted {
			if order == sortSizeAsc {
				sizeLabel += " ▲"
			} else {
				sizeLabel += " ▼"
			}
		}
		line = fit(line, width-sizeWidth) + " " + padLeft(sizeLabel, sizeWidth-1)
	}
	return lipgloss.NewStyle().Faint(true).Render(fit(line, width))
}

// splitColumn caps the info pane at 46 interior cells. Below 96 columns the
// split stays at half; past that the surplus goes to the list, because brew
// descriptions wrap fine at 46 cells while the list keeps gaining columns for
// long names. The resize path sizes the list and viewport from the same figure.
func splitColumn(width int) int {
	return max(width/2, width-48)
}

func (m *model) divider() string {
	role := m.currentTheme().border
	if m.mode == modeSearch {
		role = m.currentTheme().search
	}
	return m.roleStyle(role).Render("│")
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

	style := m.roleStyle(m.currentTheme().border)
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

// depColumn is the three-cell dependency marker. The kind word that used to
// share this column is gone: in a single-kind list it was a constant, and the
// active tab already names the kind for every row, so it carried no
// information and only cost name-column width. The dep marker is real per-row
// signal and keeps a fixed-width slot on the formula list — blank when not a
// dependency — so the version column cannot shift between rows. Text, not
// color, per the monochrome precedent the tab bar already sets. A cask has no
// dependency relation, so the cask list drops the column entirely.
func depColumn(pkg brew.Package) string {
	if pkg.Kind != brew.Formula {
		return ""
	}
	if pkg.Dependency {
		return "dep"
	}
	return "   "
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
	// Untrusted claims the same cell, and wins it: brew refuses to even load
	// such a package's definition, so the refusal explains every other failure
	// on the row (info, upgrade) and is the state the user must clear first,
	// with brew's own info-pane error spelling out the `brew trust` remedy.
	// ponytail: one shared cell, so a row both outdated and untrusted shows
	// only "!" while the info pane's version row still says outdated; a second
	// fixed cell is the upgrade if both cues must coexist.
	if pkg.Untrusted {
		freshness = "!"
	}
	// A queued row carries a bullet until its turn comes: confirmed work the
	// user cannot see waiting would look like work forgotten. Same shared
	// cell, same ponytail ceiling as above.
	if m.isQueued(pkg) {
		freshness = "•"
	}
	// The acted-upon row carries the operation's spinner in the freshness cell
	// for as long as the job runs, so a user browsing elsewhere still sees which
	// row the verb is acting on. The cell is reused rather than added: the mark
	// displaces the outdated arrow — and the untrusted and queued marks — on
	// that one row only, and the layout cannot shift when the job ends.
	if m.operation != nil && m.operation.Kind == pkg.Kind && m.operation.Name == pkg.Name {
		freshness = m.spinner.View()
	}
	dep := depColumn(pkg)
	// The size column is reserved only where a size can honestly be measured,
	// which is the formula list. See rowSize.
	sizeWidth := 0
	if m.kind == brew.Formula && width-5-lipgloss.Width(dep)-sizeColumnWidth >= 8 {
		sizeWidth = sizeColumnWidth
	}
	nameWidth := min(30, max(8, width-5-lipgloss.Width(dep)-sizeWidth))
	name := fit(pkg.Name, nameWidth)
	line := " " + marker + freshness + " " + name
	if dep != "" {
		line += " " + dep
	}
	if pkg.Version != "" {
		line += " " + pkg.Version
		// The offered version renders only on rows Homebrew's verdict marked, so
		// the arrow can never claim an upgrade the ↑ cell does not. It rides the
		// same end-of-line clipping as the version, so a narrow pane loses the
		// detail, never the columns.
		if pkg.Outdated && pkg.LatestVersion != "" && pkg.LatestVersion != pkg.Version {
			line += " \u2192 " + m.bumpHighlighted(pkg, selected)
		}
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
	return m.roleStyle(m.currentTheme().selected).Bold(true).Render(line)
}

// bumpHighlighted emboldens the offered version from the first differing
// segment onward (1.0.1 → 1.**2.0**), so the eye lands on what actually
// moved. Unselected colored rows only: the selected row already renders
// whole-line bold under its background, and styling a segment inside it would
// end that background at the inner reset — the same composition rule the
// two-tone footer documents — while monochrome drops styling entirely. Both
// omissions cost nothing because bold is enhancement only; the arrow text is
// the carrier of meaning. An unreadable version pair gets no highlight, plain
// text, mirroring the parser's fail-open contract.
// ponytail: the info pane keeps its plain "(outdated, latest X)" wording;
// style it only if that panel ever grows styled text at all.
func (m *model) bumpHighlighted(pkg brew.Package, selected bool) string {
	latest := pkg.LatestVersion
	if selected || m.monochrome {
		return latest
	}
	offset := brew.BumpOffset(pkg.Version, latest)
	if offset < 0 {
		return latest
	}
	return latest[:offset] + lipgloss.NewStyle().Bold(true).Render(latest[offset:])
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
	m.overlayQueue(lines, width, height)
	return lines
}

// overlayQueue claims the info pane's bottom rows while a job runs: the
// running entry first, then the queue in run order, so the whole run is
// readable in one place while the status row only ever shows the latest event.
//
// ponytail: the block overwrites the pane's tail - the info text keeps
// rendering at full height underneath and its last rows are hidden while the
// block is up. Fine for a transient window; shortening the viewport for the
// block's lifetime is the upgrade if the hidden tail ever matters.
func (m *model) overlayQueue(lines []string, width, height int) {
	if m.operation == nil || height < 4 {
		return
	}
	rows := []string{"Queue", m.spinner.View() + " " + words(m.verb).gerund + " " + m.operation.Name + "..."}
	for _, entry := range m.queue {
		rows = append(rows, "queued · "+words(entry.verb).lower+" "+entry.pkg.Name)
	}
	// At most half the pane; the overflow row keeps the hidden count honest.
	limit := max(2, height/2)
	if len(rows) > limit {
		hidden := len(rows) - (limit - 1)
		rows = append(rows[:limit-1], "… +"+strconv.Itoa(hidden)+" more queued")
	}
	start := height - len(rows)
	if start >= 2 {
		lines[start-1] = strings.Repeat(" ", width)
	}
	for i, row := range rows {
		lines[start+i] = fit(row, width)
	}
}

func (m *model) statusLine() string {
	status := m.status
	if m.loading {
		switch m.loadPurpose {
		case loadRefresh:
			status = "Refreshing " + kindPlural(m.kind) + "..."
		case loadAfterOperation:
			status = "Reloading " + kindPlural(m.kind) + "..."
		default:
			status = "Loading " + kindPlural(m.kind) + "..."
		}
	}
	if m.mode == modeSearch && !m.loading {
		return "Search: " + m.query + "_"
	}
	if m.mode == modeConfirm || m.mode == modePassword || m.mode == modeOperation || m.mode == modeQuitting || m.loading || m.priority {
		if m.spinnerActive && (m.loading || m.mode == modeOperation || m.mode == modePassword) {
			return m.spinner.View() + " " + status
		}
		return status
	}
	// The footer already teaches "/" — a permanent "Search: —" placeholder here
	// only restated it. The prefix appears when a filter is actually narrowing
	// the list, which is the one moment it carries information.
	parts := make([]string, 0, 3)
	if m.query != "" {
		parts = append(parts, "Search: "+m.query)
	}
	if count := m.installedStatus(); count != "" {
		parts = append(parts, count)
	}
	if status != "" {
		parts = append(parts, status)
	}
	return strings.Join(parts, " · ")
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
	case m.loading && m.loadPurpose == loadAfterOperation:
		keys = progressHelp(m.verb)
	case m.loading:
		keys = loadingHelp
	case m.mode == modeSearch:
		keys = searchHelp
	case m.mode == modeConfirm:
		keys = confirmHelp
	case m.mode == modePassword:
		keys = progressHelp(m.verb)
	case m.mode == modeOperation:
		keys = operationHelp(m.verb)
	}
	// Two-tone, and every segment carries the full footer role rather than relying
	// on an enclosing style. A single outer Render over pre-styled segments would
	// end its own background at the first inner reset, which is visible on any
	// theme whose footer has a background.
	footer := m.currentTheme().footer
	h := m.help
	h.SetWidth(0)
	h.ShortSeparator = " · "
	// The verb (in the key slot, see keys.go) goes faint and the keystroke keeps
	// the accent, so the row reads as dim prose punctuated by bright keys instead
	// of a solid colored bar.
	h.Styles = help.Styles{
		ShortKey:       m.roleStyle(footer).Faint(true),
		ShortDesc:      m.roleStyle(footer).Bold(true),
		ShortSeparator: m.roleStyle(footer).Faint(true),
	}
	return fitStyled(h.View(keys), width, m.roleStyle(footer))
}

func (m *model) styledLine(value string, width int, style lipgloss.Style) string {
	return style.Render(fit(value, width))
}

// fitStyled clips to width and pads with styled blanks, so a themed background
// reaches the end of the row even though the content arrives pre-styled and
// cannot be wrapped in one enclosing style.
func fitStyled(value string, width int, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	value = lipgloss.NewStyle().MaxWidth(width).Render(value)
	if pad := width - lipgloss.Width(value); pad > 0 {
		value += style.Render(strings.Repeat(" ", pad))
	}
	return value
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
	return m.borderStyle(m.currentTheme().border).Padding(0, 1)
}

func otherOperation(op brew.Operation) brew.Operation {
	if op == brew.Upgrade {
		return brew.Uninstall
	}
	return brew.Upgrade
}

func (m *model) confirmationModal(pkg brew.Package) string {
	return m.confirmationModalFor(m.confirmVerb, pkg)
}

func (m *model) confirmationModalFor(op brew.Operation, pkg brew.Package) string {
	lines := []string{
		m.roleStyle(m.currentTheme().header).Render(confirmTitle(op)),
		words(op).title + " " + pkg.Name + "?",
		m.roleStyle(m.currentTheme().footer).Render("y: confirm  other: cancel"),
	}
	return m.modalStyle().Render(strings.Join(lines, "\n"))
}

func (m *model) passwordModal() string {
	body := "Homebrew requested administrator access."
	if m.passwordAttempts >= 2 {
		body = "Wrong password? Try again."
	}
	lines := []string{
		m.roleStyle(m.currentTheme().header).Render("Administrator password"),
		body,
		"Password: " + m.password.View(),
		m.roleStyle(m.currentTheme().footer).Render("Enter: submit  Esc: cancel"),
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
	// Both modals: the fit check runs before the verb is chosen in the widest
	// sense, and the wider of the two is what must fit.
	confirmation := m.confirmationModal(pkg)
	if other := m.confirmationModalFor(otherOperation(m.confirmVerb), pkg); lipgloss.Width(other) > lipgloss.Width(confirmation) {
		confirmation = other
	}
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
		// Both verbs, every time. The confirmation is for one of them, but the
		// password and progress dialogs must stay renderable for either, so the
		// narrowest terminal that passes here clears the longest string of both.
		progressStatus(brew.Uninstall, pkg.Name),
		progressStatus(brew.Upgrade, pkg.Name),
		cancelledStatus(brew.Uninstall),
		cancelledStatus(brew.Upgrade),
		tooSmallStatus(brew.Uninstall),
		tooSmallStatus(brew.Upgrade),
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
