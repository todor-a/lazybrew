package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazybrew/internal/brew"
	"lazybrew/internal/info"
)

var ansiSequence = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

func strippedLines(m *model) []string {
	return strings.Split(ansiSequence.ReplaceAllString(m.View().Content, ""), "\n")
}

func TestMinimumSizeAndExactThirtyCellFooter(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 31, Height: 8})
	if got := m.View().Content; got != "lazybrew: terminal too small (need 32x9)" {
		t.Fatalf("small view=%q", got)
	}

	m.Update(tea.WindowSizeMsg{Width: 32, Height: 9})
	lines := strippedLines(m)
	if len(lines) != 9 {
		t.Fatalf("view has %d rows, want 9\n%s", len(lines), strings.Join(lines, "\n"))
	}
	footerRunes := []rune(lines[7])
	if len(footerRunes) < 32 {
		t.Fatalf("footer row width=%d, want 32", len(footerRunes))
	}
	got := string(footerRunes[1:31])
	want := "[/ or s] search  tab switch  u"
	if got != want {
		t.Fatalf("footer interior=%q, want %q", got, want)
	}
}

func TestResponsiveSplitAppearsAtSeventyTwoColumns(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 71, Height: 12})
	narrow := strippedLines(m)
	if got := strings.Count(narrow[3], "│"); got != 2 {
		t.Fatalf("71-column content has %d vertical rules, want outer borders only", got)
	}

	m.Update(tea.WindowSizeMsg{Width: 72, Height: 12})
	wide := strippedLines(m)
	if got := strings.Count(wide[3], "│"); got != 3 {
		t.Fatalf("72-column content has %d vertical rules, want outer borders and divider", got)
	}
	if !strings.Contains(strings.Join(wide, "\n"), "Info: Alpha") {
		t.Fatal("wide view does not render selected package info heading")
	}
	for row, line := range wide {
		if width := lipgloss.Width(line); width != 72 {
			t.Fatalf("row %d width=%d, want 72: %q", row, width, line)
		}
	}
}

func TestConfirmationAndPasswordAreCenteredLayersOverBase(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(textKey("u"))
	confirmation := strings.Join(strippedLines(m), "\n")
	for _, exact := range []string{"[ Apps ]    Formulae", "Confirm uninstall", "Uninstall Alpha?", "y: confirm  other: cancel"} {
		if !strings.Contains(confirmation, exact) {
			t.Fatalf("confirmation view missing %q\n%s", exact, confirmation)
		}
	}

	m.confirmation = nil
	m.operation = m.selectedPackage()
	m.mode = modePassword
	m.passwordAttempts = 2
	m.password = newPasswordInput(m.width)
	m.password.Focus()
	m.password.SetValue("secret")
	password := strings.Join(strippedLines(m), "\n")
	for _, exact := range []string{"Administrator password", "Wrong password? Try again.", "Password: ", "Enter: submit  Esc: cancel"} {
		if !strings.Contains(password, exact) {
			t.Fatalf("password view missing %q\n%s", exact, password)
		}
	}
	if strings.Contains(password, "secret") {
		t.Fatal("password plaintext appeared in rendered view")
	}
	if !strings.Contains(password, "••••••") {
		t.Fatal("password view did not use EchoPassword masks")
	}
}

func TestThemeCycleAndRoleTable(t *testing.T) {
	m, _ := newTestModel(t)
	want := []string{"Lazygit", "Bright", "Ocean", "Dracula", "Lazygit"}
	for index, name := range want {
		if got := m.currentTheme().name; got != name {
			t.Fatalf("theme %d=%q, want %q", index, got, name)
		}
		if index+1 < len(want) {
			m.Update(textKey("t"))
		}
	}
	if themes[0].border.foreground != "2" || themes[0].selected.background != "4" || themes[0].selected.foreground != "" {
		t.Fatalf("Lazygit role colors changed: %#v", themes[0])
	}
	if themes[1].header.foreground != "0" || themes[1].header.background != "6" {
		t.Fatalf("Bright header colors changed: %#v", themes[1].header)
	}
	if themes[2].selected.foreground != "0" || themes[2].selected.background != "6" {
		t.Fatalf("Ocean selected colors changed: %#v", themes[2].selected)
	}
	if themes[3].footer.foreground != "3" || themes[3].footer.background != "0" {
		t.Fatalf("Dracula footer colors changed: %#v", themes[3].footer)
	}
}

func TestLongRowsAndStatusStayInsideBorders(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 72, Height: 10})
	m.status = strings.Repeat("failure ", 30)
	m.priority = true
	m.query = strings.Repeat("query", 30)
	lines := strippedLines(m)
	for row, line := range lines {
		if width := lipgloss.Width(line); width != 72 {
			t.Fatalf("row %d width=%d, want 72", row, width)
		}
	}
}

func TestModeSpecificStatusAndFooterStrings(t *testing.T) {
	m, _ := newTestModel(t)
	m.loading = true
	m.loadPurpose = loadRefresh
	m.spinnerActive = true
	lines := strippedLines(m)
	if !strings.Contains(lines[m.height-3], "Refreshing casks...") {
		t.Fatalf("refresh status=%q", lines[m.height-3])
	}
	if !strings.Contains(lines[m.height-2], "q quit") || strings.Contains(lines[m.height-2], "search") {
		t.Fatalf("refresh footer=%q", lines[m.height-2])
	}

	m.loadPurpose = loadAfterUninstall
	m.mode = modeUninstall
	lines = strippedLines(m)
	if !strings.Contains(lines[m.height-3], "Reloading casks...") {
		t.Fatalf("reload status=%q", lines[m.height-3])
	}
	if !strings.Contains(lines[m.height-2], "Uninstall in progress; controls disabled") {
		t.Fatalf("reload footer=%q", lines[m.height-2])
	}

	m.loading = false
	m.spinnerActive = false
	m.mode = modeSearch
	m.query = "abc"
	lines = strippedLines(m)
	if !strings.Contains(lines[m.height-3], "Search: abc_") {
		t.Fatalf("search status=%q", lines[m.height-3])
	}
}

func TestColorProfileClassificationAndNoColor(t *testing.T) {
	for _, test := range []struct {
		name       string
		message    tea.ColorProfileMsg
		monochrome bool
	}{
		{name: "unknown", message: tea.ColorProfileMsg{Profile: 0}, monochrome: true},
		{name: "no TTY", message: tea.ColorProfileMsg{Profile: 1}, monochrome: true},
		{name: "ASCII", message: tea.ColorProfileMsg{Profile: 2}, monochrome: true},
		{name: "ANSI", message: tea.ColorProfileMsg{Profile: 3}},
		{name: "ANSI256", message: tea.ColorProfileMsg{Profile: 4}},
		{name: "true color", message: tea.ColorProfileMsg{Profile: 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			m, _ := newTestModel(t)
			m.Update(test.message)
			if m.monochrome != test.monochrome {
				t.Fatalf("monochrome=%v, want %v", m.monochrome, test.monochrome)
			}
		})
	}

	t.Run("monochrome selection", func(t *testing.T) {
		m, _ := newTestModel(t)
		m.Update(tea.ColorProfileMsg{Profile: 2})
		got := m.packageLine(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask}, true, 40)
		plain := ansiSequence.ReplaceAllString(got, "")
		want := lipgloss.NewStyle().Reverse(true).Bold(true).Render(plain)
		// ">  Alpha": marker, the blank freshness cell, then the name column.
		if got != want || !strings.Contains(plain, ">  Alpha") {
			t.Fatalf("ASCII selection=%q, want reverse+bold %q", got, want)
		}
	})

	t.Run("NO_COLOR overrides color profile", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		m, _ := newTestModel(t)
		m.Update(tea.ColorProfileMsg{Profile: 5})
		if !m.monochrome {
			t.Fatal("NO_COLOR did not override true color profile")
		}
	})
}

func TestLoadingListPaneShowsNoEmptyStateAndHeaderMarksActiveList(t *testing.T) {
	m, _ := newTestModel(t)
	if got := strippedLines(m)[1]; !strings.Contains(got, "[ Apps ]    Formulae") {
		t.Fatalf("cask header=%q, want the cask tab bracketed", got)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.loading {
		t.Fatal("tab did not start a list load")
	}
	lines := strippedLines(m)
	if got := lines[1]; !strings.Contains(got, "  Apps    [ Formulae ]") {
		t.Fatalf("formula header=%q, want the formula tab bracketed", got)
	}
	if strings.Contains(lines[3], "No packages found") {
		t.Fatalf("loading list pane contradicts the status row: %q", lines[3])
	}
	if got := lines[m.height-3]; !strings.Contains(got, "Loading formulae...") {
		t.Fatalf("loading status=%q, want formulae spelling", got)
	}
}

func TestTabSwitchesListFromSearchMode(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.KeyPressMsg{Code: '/'})
	if m.mode != modeSearch {
		t.Fatal("/ did not enter search mode")
	}
	m.query = "a"
	m.applyFilter(0)

	before := m.kind
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.kind == before {
		t.Fatal("tab in search mode did not switch kind")
	}
	if m.mode != modeNormal {
		t.Fatalf("mode=%v after tab, want normal", m.mode)
	}
	if m.query != "a" {
		t.Fatalf("query=%q after tab, want it preserved", m.query)
	}
}

func TestInstalledCountTracksTheActiveFilter(t *testing.T) {
	m, _ := newTestModel(t)
	statusRow := func() string { return strippedLines(m)[m.height-3] }

	if got := statusRow(); !strings.Contains(got, "3 casks installed") {
		t.Fatalf("unfiltered status=%q, want total count", got)
	}

	m.query = "al"
	m.applyFilter(0)
	got := statusRow()
	if !strings.Contains(got, "1 of 3 casks match") {
		t.Fatalf("filtered status=%q, want filtered count", got)
	}
	if strings.Contains(got, "3 casks installed") {
		t.Fatalf("filtered status still claims the unfiltered total: %q", got)
	}

	m.query = ""
	m.applyFilter(0)
	if got := statusRow(); !strings.Contains(got, "3 casks installed") {
		t.Fatalf("status after clearing query=%q, want total count", got)
	}

	m.setPackages(nil, 0)
	if got := statusRow(); strings.Contains(got, "installed") || strings.Contains(got, "match") {
		t.Fatalf("empty list still reports a count: %q", got)
	}
}

func TestPriorityStatusOwnsTheRowWithoutACount(t *testing.T) {
	m, _ := newTestModel(t)
	m.status, m.priority = "Uninstalled Alpha", true
	got := strings.TrimSpace(strippedLines(m)[m.height-3])
	if want := "│Uninstalled Alpha"; !strings.HasPrefix(got, want) {
		t.Fatalf("priority status row=%q, want it to own the row", got)
	}
	if strings.Contains(got, "casks installed") || strings.Contains(got, "casks match") {
		t.Fatalf("priority status row carries a count: %q", got)
	}
}

func TestInfoPaneShowsLoadingWhileALoadClearsTheSelection(t *testing.T) {
	m, _ := newTestModel(t)
	if got := strippedLines(m)[3]; !strings.Contains(got, "Info: Alpha") {
		t.Fatalf("settled info pane=%q, want a package heading", got)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.loading || m.selectedPackage() != nil {
		t.Fatalf("expected a load with no selection: loading=%v selected=%v", m.loading, m.selectedPackage())
	}
	if got := strippedLines(m)[3]; !strings.Contains(got, info.LoadingText) {
		t.Fatalf("info pane during load=%q, want %q", got, info.LoadingText)
	}

	m.loading = false
	m.setPackages(nil, 0)
	if got := strippedLines(m)[3]; strings.Contains(got, info.LoadingText) {
		t.Fatalf("settled empty list still claims to be loading: %q", got)
	}
}

// scrollbarRows returns the last cell of each content row of the list pane.
func scrollbarRows(m *model) []string {
	lines := strippedLines(m)
	column := make([]string, 0, m.contentRows)
	for row := 0; row < m.contentRows; row++ {
		pane := []rune(lines[3+row])
		// row starts after the left border and ends before the divider.
		column = append(column, string(pane[m.width/2-1]))
	}
	return column
}

func TestScrollbarAppearsOnlyWhenTheListOverflows(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	if m.contentRows < 3 {
		t.Fatalf("contentRows=%d, too small to exercise the bar", m.contentRows)
	}

	// Three packages in five rows: everything fits, so no column is drawn and the
	// row keeps the full pane width.
	for _, cell := range scrollbarRows(m) {
		if cell == "█" {
			t.Fatalf("drew a thumb for a list that fits:\n%s", strings.Join(strippedLines(m), "\n"))
		}
	}

	// Two rows of viewport against three packages forces two pages.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 9})
	column := scrollbarRows(m)
	thumbs := 0
	for _, cell := range column {
		if cell == "█" {
			thumbs++
		}
	}
	if thumbs == 0 {
		t.Fatalf("no thumb for an overflowing list: %q\n%s", column, strings.Join(strippedLines(m), "\n"))
	}
	if thumbs == len(column) {
		t.Fatalf("thumb fills the whole track, so it shows no position: %q", column)
	}
}

func TestScrollbarThumbSitsFlushAtBothEnds(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 9})

	first := scrollbarRows(m)
	if first[0] != "█" {
		t.Fatalf("first page thumb is not flush with the top: %q", first)
	}

	// Walk to the last row so the paginator is on its final page.
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	last := scrollbarRows(m)
	if last[len(last)-1] != "█" {
		t.Fatalf("last page thumb is not flush with the bottom: %q", last)
	}
	if last[0] == "█" {
		t.Fatalf("thumb never moved off the top: %q", last)
	}
}

func TestOutdatedRowCarriesAFixedFreshnessCell(t *testing.T) {
	m, _ := newTestModel(t)
	plain := func(pkg brew.Package, selected bool) string {
		return ansiSequence.ReplaceAllString(m.packageLine(pkg, selected, 40), "")
	}
	stale := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask, Outdated: true}, true)
	fresh := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask}, true)
	if !strings.HasPrefix(stale, " >↑ Alpha") {
		t.Fatalf("outdated row=%q, want a marker then the ↑ cell", stale)
	}
	if !strings.HasPrefix(fresh, " >  Alpha") {
		t.Fatalf("fresh row=%q, want a marker then a blank cell", fresh)
	}
	if lipgloss.Width(stale) != 40 || lipgloss.Width(fresh) != 40 {
		t.Fatalf("row widths=%d and %d, want 40 each", lipgloss.Width(stale), lipgloss.Width(fresh))
	}

	unselected := plain(brew.Package{Name: "Alpha", Kind: brew.Cask, Outdated: true}, false)
	if !strings.HasPrefix(unselected, "  ↑ Alpha") {
		t.Fatalf("unselected outdated row=%q, want the cell independent of the marker", unselected)
	}
}

// The freshness cell is one terminal cell wide by assumption, and every pinned
// row width depends on it. A lipgloss bump that changed its measured width would
// otherwise shift every row at runtime with no test noticing.
func TestOutdatedGlyphIsOneCell(t *testing.T) {
	if got := lipgloss.Width("↑"); got != 1 {
		t.Fatalf("lipgloss.Width(\"↑\")=%d, want 1", got)
	}
}

func TestScrollbarNeverChangesTheRenderedWidth(t *testing.T) {
	m, _ := newTestModel(t)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 32, Height: 9}, {Width: 40, Height: 9},
		{Width: 71, Height: 10}, {Width: 72, Height: 10}, {Width: 80, Height: 9},
	} {
		m.Update(size)
		for row, line := range strippedLines(m) {
			if got := lipgloss.Width(line); got != size.Width {
				t.Fatalf("at %dx%d row %d width=%d, want %d: %q",
					size.Width, size.Height, row, got, size.Width, line)
			}
		}
	}
}
