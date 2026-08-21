package ui

import (
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazybrew/internal/brew"
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

func (m *model) headerLine() string {
	return "lazybrew [" + m.currentTheme().name + "]  " + activeListLabel(m.kind) + "  Tab: switch"
}

func activeListLabel(kind brew.Kind) string {
	if kind == brew.Cask {
		return "Apps [casks]"
	}
	return "Formulae [formula]"
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
	for row := 0; row < height; row++ {
		index := start + row
		if index >= len(visible) {
			lines[row] = strings.Repeat(" ", width)
			continue
		}
		item, ok := visible[index].(packageItem)
		if !ok {
			lines[row] = strings.Repeat(" ", width)
			continue
		}
		selected := index == m.list.Index()
		lines[row] = m.packageLine(item.packageValue, selected, width)
	}
	return lines
}

func (m *model) packageLine(pkg brew.Package, selected bool, width int) string {
	marker := " "
	if selected {
		marker = ">"
	}
	kind := string(pkg.Kind)
	nameWidth := min(30, max(8, width-4-lipgloss.Width(kind)))
	name := fit(pkg.Name, nameWidth)
	line := " " + marker + " " + name + " " + kind
	if pkg.Version != "" {
		line += " " + pkg.Version
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
	if status != "" {
		return prefix + " | " + status
	}
	return prefix
}

func kindPlural(kind brew.Kind) string {
	if kind == brew.Cask {
		return "casks"
	}
	return "formulas"
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
		"Loading formulas...",
		"Refreshing casks...",
		"Refreshing formulas...",
		"Reloading casks...",
		"Reloading formulas...",
	}
	for _, line := range lines {
		if lipgloss.Width(line) > available {
			return false
		}
	}
	return true
}

var _ help.KeyMap = footerKeys{}
