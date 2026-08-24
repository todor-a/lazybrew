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
	want := "Search: / · Switch: tab · Unin"
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
	m.Update(textKey("d"))
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

func TestSelfUninstallConfirmationNamesExit(t *testing.T) {
	m, _ := newTestModel(t)
	m.setPackages([]brew.Package{{Name: "lazybrew", Kind: brew.Cask}}, 0)
	m.Update(textKey("d"))

	confirmation := strings.Join(strippedLines(m), "\n")
	if !strings.Contains(confirmation, "This removes lazybrew itself; the app will exit when done") {
		t.Fatalf("self-uninstall confirmation did not name the exit\n%s", confirmation)
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
	if themes[0].selected.background.dark != "#316dca" || themes[0].selected.foreground.dark != "#ffffff" {
		t.Fatalf("Lazygit selected colors changed: %#v", themes[0].selected)
	}
	if themes[3].selected.background.dark != "#44475a" || themes[3].selected.background.light != "#644ac9" {
		t.Fatalf("Dracula selected colors changed: %#v", themes[3].selected)
	}
	// Every color a theme sets names both variants: a role that resolves on only
	// one background is the light-terminal black-bar bug in a new coat.
	for _, candidate := range themes {
		for role, pair := range map[string]colorPair{
			"border": candidate.border, "header": candidate.header, "selected": candidate.selected,
			"status": candidate.status, "footer": candidate.footer, "search": candidate.search,
		} {
			for _, half := range []adaptive{pair.foreground, pair.background} {
				if (half.light == "") != (half.dark == "") {
					t.Fatalf("%s %s sets only one variant: %#v", candidate.name, role, half)
				}
			}
		}
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
	if !strings.Contains(lines[m.height-2], "Quit: q") || strings.Contains(lines[m.height-2], "Search:") {
		t.Fatalf("refresh footer=%q", lines[m.height-2])
	}

	m.loadPurpose = loadAfterOperation
	m.mode = modeOperation
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
	rows := max(0, m.contentRows-1)
	column := make([]string, 0, rows)
	// The first content row is the table header; the bar spans only the list
	// rows below it.
	for row := 0; row < rows; row++ {
		pane := []rune(lines[4+row])
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
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
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
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})

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

// The offered version renders as "installed \u2192 latest" only on rows Homebrew's
// verdict marked; a fresh row with the same versions on it must stay silent, so
// the arrow can never claim an upgrade the \u2191 cell does not.
func TestOutdatedRowShowsInstalledAndLatestVersions(t *testing.T) {
	m, _ := newTestModel(t)
	plain := func(pkg brew.Package) string {
		// 60 cells: wide enough that the version survives the end-of-line clip,
		// which is the layout where the arrow is expected to appear at all.
		return ansiSequence.ReplaceAllString(m.packageLine(pkg, false, 60), "")
	}
	stale := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask, Outdated: true, LatestVersion: "2.0"})
	if !strings.Contains(stale, "1.0 \u2192 2.0") {
		t.Fatalf("outdated row=%q, want the installed and offered versions", stale)
	}
	fresh := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask, LatestVersion: "2.0"})
	if strings.Contains(fresh, "\u2192") {
		t.Fatalf("unmarked row=%q, want no version arrow", fresh)
	}
	// A revision bump reports the same version string on both sides; an arrow
	// from a version to itself would read as noise, not news.
	same := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask, Outdated: true, LatestVersion: "1.0"})
	if strings.Contains(same, "\u2192") {
		t.Fatalf("same-version row=%q, want no version arrow", same)
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

func TestPinnedFormulaClaimsTheFreshnessCell(t *testing.T) {
	m, _ := newTestModel(t)
	row := ansiSequence.ReplaceAllString(m.packageLine(brew.Package{Name: "Alpha", Kind: brew.Formula, Outdated: true, Pinned: true}, true, 40), "")
	if !strings.HasPrefix(row, " >P Alpha") {
		t.Fatalf("pinned row=%q, want P to win over outdated", row)
	}
}

// The search footer is the ambient discoverability surface for the filter
// vocabulary; one row, per the issue-#31 plan.
func TestSearchFooterTeachesTheFilterVocabulary(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m.Update(textKey("/"))
	lines := strippedLines(m)
	footer := strings.TrimRight(strings.Trim(lines[len(lines)-2], "\u2502"), " ")
	want := "Filters: is:outdated \u00b7 is:untrusted \u00b7 is:dep \u00b7 Keep: enter \u00b7 Clear: esc"
	if footer != want {
		t.Fatalf("search footer=%q, want %q", footer, want)
	}
}

func TestFooterListsEveryNormalKey(t *testing.T) {
	m, _ := newTestModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	lines := strippedLines(m)
	footer := strings.TrimRight(strings.Trim(lines[len(lines)-2], "│"), " ")
	want := "Search: / · Switch: tab · Uninstall: d · Upgrade: u · All: a · Filter: f · Sort: o · Theme: t · Refresh: r · Quit: q"
	if footer != want {
		t.Fatalf("footer=%q, want %q", footer, want)
	}
}

// The size column is reserved before the measurement lands, so the name and
// dep columns sit at the same cells either way and nothing reflows when sizes
// arrive seconds after first paint.
// The column is reserved on the formula list before the measurement arrives, so
// a late size cannot reflow rows the user is already reading. The cask list
// reserves nothing, because it is never measured.
func TestSizeColumnIsReservedBeforeTheMeasurementLands(t *testing.T) {
	m, _ := newTestModel(t)
	switchTo(t, m)

	for _, width := range []int{32, 72, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 16})
		measured := strippedLines(m)[4]

		landed := m.sizes
		m.sizes = nil
		blank := strippedLines(m)[4]
		m.sizes = landed

		if lipgloss.Width(measured) != lipgloss.Width(blank) {
			t.Fatalf("at width %d the row changed width when sizes landed: %q vs %q", width, blank, measured)
		}
		if index := cellIndex(blank, "Alpha"); index < 0 || index != cellIndex(measured, "Alpha") {
			t.Fatalf("at width %d the name column moved when sizes landed: %q vs %q", width, blank, measured)
		}
		if !strings.Contains(measured, "1MB") {
			t.Fatalf("at width %d the measured row carries no size: %q", width, measured)
		}
		if strings.ContainsAny(strings.TrimRight(blank, " │"), "KMG") {
			t.Fatalf("at width %d the unmeasured row invented a size: %q", width, blank)
		}
	}

	// Back on the cask list: no size, measured or not.
	switchTo(t, m)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	if row := strippedLines(m)[4]; strings.ContainsAny(strings.TrimRight(row, " │"), "KMG") {
		t.Fatalf("the cask row carries a size: %q", row)
	}
}

func TestRowShapeAndNameColumnBounds(t *testing.T) {
	m, _ := newTestModel(t)
	tests := []struct {
		name  string
		pkg   brew.Package
		width int
		want  string
	}{
		{
			// No size column and no dep column: the Caskroom is not measured and
			// casks have no dependency relation, so the cask list reserves
			// nothing beyond the name and the whole width goes to it.
			name:  "cask row",
			pkg:   brew.Package{Name: "Alpha", Kind: brew.Cask},
			width: 40,
			want:  "    Alpha                               ",
		},
		{
			name:  "on-request formula keeps the dep slot blank",
			pkg:   brew.Package{Name: "vault", Kind: brew.Formula},
			width: 40,
			want:  "    vault                               ",
		},
		{
			name:  "dependency formula carries dep in its column",
			pkg:   brew.Package{Name: "llvm@22", Kind: brew.Formula, Dependency: true},
			width: 40,
			want:  "    llvm@22                   dep       ",
		},
		{
			// 32-column narrow layout with a scrollbar: the tightest supported row.
			name:  "narrowest supported row keeps the pinned name minimum",
			pkg:   brew.Package{Name: "llvm@22", Kind: brew.Formula, Dependency: true},
			width: 29,
			want:  "    llvm@22        dep       ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.sizes = nil
			// A row is only ever drawn on its own kind's list, and the reserved size
			// column depends on which list that is.
			m.kind = tt.pkg.Kind
			got := ansiSequence.ReplaceAllString(m.packageLine(tt.pkg, false, tt.width), "")
			if got != tt.want {
				t.Fatalf("row=%q, want %q", got, tt.want)
			}
			if lipgloss.Width(got) != tt.width {
				t.Fatalf("row width=%d, want %d", lipgloss.Width(got), tt.width)
			}
		})
	}
}

// The total is the Cellar's, so it renders on the formula list and nowhere else.
func TestHeaderCarriesTheFleetTotalOnTheFormulaListOnly(t *testing.T) {
	m, _ := newTestModel(t)
	switchTo(t, m)
	if m.kind != brew.Formula {
		t.Fatalf("kind = %q, want the formula list", m.kind)
	}

	for _, width := range []int{32, 72, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 16})
		header := strippedLines(m)[1]
		if lipgloss.Width(header) != width {
			t.Fatalf("at width %d header width=%d", width, lipgloss.Width(header))
		}
		if !strings.Contains(header, "Apps    [ Formulae ]") {
			t.Fatalf("at width %d the tab bar was disturbed: %q", width, header)
		}
		// Right aligned against the interior edge, so it reads as the column's sum.
		if !strings.HasSuffix(header, "9.2GB│") {
			t.Fatalf("at width %d the total is not flush right: %q", width, header)
		}
	}

	// The cask list is not measured, so it must claim no total.
	switchTo(t, m)
	if header := strippedLines(m)[1]; strings.Contains(header, "GB") {
		t.Fatalf("the cask header claimed a Cellar total: %q", header)
	}

	m.sizes = nil
	if header := strippedLines(m)[1]; strings.Contains(header, "GB") {
		t.Fatalf("header claimed a total before measuring: %q", header)
	}
}

// The layout guarantees the total fits at the minimum renderable width: the
// interior is 30 cells and the tab bar plus a gap plus a six-cell total is 29.
func TestTotalFitsTheMinimumInterior(t *testing.T) {
	m, _ := newTestModel(t)
	switchTo(t, m)
	m.Update(tea.WindowSizeMsg{Width: 32, Height: 9})
	if got := m.headerLine(); got != "  Apps    [ Formulae ]   9.2GB" {
		t.Fatalf("header at the minimum width=%q", got)
	}
	if got := lipgloss.Width(m.headerLine()); got > 30 {
		t.Fatalf("header interior width=%d, want at most 30", got)
	}
}

func TestHumanKBSpellingAndSixCellCeiling(t *testing.T) {
	tests := []struct {
		kilobytes int64
		want      string
	}{
		{kilobytes: 0, want: "0KB"},
		{kilobytes: 12, want: "12KB"},
		{kilobytes: 1023, want: "1023KB"},
		{kilobytes: 1024, want: "1MB"},
		{kilobytes: 48568, want: "47MB"},
		{kilobytes: 1048576, want: "1.0GB"},
		{kilobytes: 1550732, want: "1.5GB"},
		{kilobytes: 11902796, want: "11.4GB"},
		{kilobytes: 104857600, want: "100GB"},
		{kilobytes: -1, want: ""},
	}
	for _, tt := range tests {
		got := humanKB(tt.kilobytes)
		if got != tt.want {
			t.Errorf("humanKB(%d) = %q, want %q", tt.kilobytes, got, tt.want)
		}
		if lipgloss.Width(got) > 6 {
			t.Errorf("humanKB(%d) = %q, wider than the reserved 6 cells", tt.kilobytes, got)
		}
	}

	// The 6-cell ceiling holds below 10 TB, which is already past where the
	// decimal is dropped. Beyond that the value widens, and the header omits it
	// rather than clipping the tab bar.
	if got := humanKB(10736369664); got != "10239GB" {
		t.Errorf("humanKB(10736369664) = %q, want %q", got, "10239GB")
	}
}

func TestScrollbarNeverChangesTheRenderedWidth(t *testing.T) {
	m, _ := newTestModel(t)
	for _, size := range []tea.WindowSizeMsg{
		{Width: 32, Height: 9}, {Width: 40, Height: 9},
		{Width: 71, Height: 10}, {Width: 72, Height: 10}, {Width: 80, Height: 9},
		{Width: 120, Height: 40},
	} {
		// Both size states, because the size column is reserved before the
		// measurement lands and filled afterwards.
		for _, measured := range []bool{true, false} {
			landed := m.sizes
			if !measured {
				m.sizes = nil
			}
			m.Update(size)
			for row, line := range strippedLines(m) {
				if got := lipgloss.Width(line); got != size.Width {
					t.Fatalf("at %dx%d measured=%v row %d width=%d, want %d: %q",
						size.Width, size.Height, measured, row, got, size.Width, line)
				}
			}
			m.sizes = landed
		}
	}
}

// The name-column arithmetic in packageLine decides where the kind column lands.
// Nothing else pinned it: fit() pads every row to the pane width unconditionally,
// so an off-by-one there only shifts the kind column and eats one name character,
// which no width assertion can see. These indices are written out rather than
// recomputed from the formula, so the test disagrees with the code when the code
// changes.
// The dep marker keeps a fixed cell on the formula list whatever the row
// holds, and no row spells its kind: the active tab already names it.
func TestDepColumnLandsAtAFixedCellAndKindWordsAreGone(t *testing.T) {
	m, _ := newTestModel(t)
	for _, tt := range []struct {
		name      string
		width     int
		wantIndex int
	}{
		{"formula at 32", 32, 22},
		{"formula at 40", 40, 30},
		{"formula at 72", 72, 35},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m.kind = brew.Formula
			pkg := brew.Package{Name: "alpha", Kind: brew.Formula, Dependency: true}
			plain := ansiSequence.ReplaceAllString(m.packageLine(pkg, false, tt.width), "")
			if got := lipgloss.Width(plain); got != tt.width {
				t.Fatalf("row width = %d, want %d: %q", got, tt.width, plain)
			}
			if got := cellIndex(plain, "dep"); got != tt.wantIndex {
				t.Fatalf("dep column at cell %d, want %d: %q", got, tt.wantIndex, plain)
			}
		})
	}
	for _, pkg := range []brew.Package{
		{Name: "alpha", Kind: brew.Formula},
		{Name: "alpha", Kind: brew.Cask},
	} {
		m.kind = pkg.Kind
		plain := ansiSequence.ReplaceAllString(m.packageLine(pkg, false, 72), "")
		if strings.Contains(plain, string(pkg.Kind)) {
			t.Fatalf("row spells its kind: %q", plain)
		}
	}
}

// The table header sits in the first content row, labels exactly the columns
// the active list renders, and carries the sort cue on the ordered column.
func TestListHeaderLabelsColumnsAndCarriesTheSortCue(t *testing.T) {
	m, _ := newTestModel(t)
	// 120 columns: wide enough that the version column survives the tail clip,
	// which is where its heading is expected to appear at all.
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	lines := strippedLines(m)
	header := lines[3]
	if !strings.Contains(header, "Name \u25b2") || !strings.Contains(header, "Version") {
		t.Fatalf("cask header=%q, want Name \u25b2 and Version", header)
	}
	if strings.Contains(header, "Dep") || strings.Contains(header, "Size") {
		t.Fatalf("cask header=%q, want no Dep or Size heading", header)
	}
	// The header labels the row below it rather than replacing it: the first
	// package still renders, one row down.
	if got := lines[4]; !strings.Contains(got, "Alpha") {
		t.Fatalf("first row=%q, want the first package under the header", got)
	}

	switchTo(t, m)
	header = strippedLines(m)[3]
	for _, want := range []string{"Name \u25b2", "Dep", "Version", "Size"} {
		if !strings.Contains(header, want) {
			t.Fatalf("formula header=%q, want %q", header, want)
		}
	}
	if strings.Contains(header, "Size \u25bc") {
		t.Fatalf("formula header=%q, want no size cue before o is pressed", header)
	}

	m.Update(textKey("o"))
	header = strippedLines(m)[3]
	if !strings.Contains(header, "Size \u25bc") || strings.Contains(header, "\u25b2") {
		t.Fatalf("size-sorted header=%q, want the cue on Size only", header)
	}
	m.Update(textKey("o"))
	if header := strippedLines(m)[3]; !strings.Contains(header, "Size \u25b2") {
		t.Fatalf("ascending-size header=%q, want Size \u25b2", header)
	}
	m.Update(textKey("o"))
	if header := strippedLines(m)[3]; !strings.Contains(header, "Name \u25bc") {
		t.Fatalf("descending-name header=%q, want Name \u25bc", header)
	}
	m.Update(textKey("o"))
	if header := strippedLines(m)[3]; !strings.Contains(header, "Name \u25b2") {
		t.Fatalf("restored header=%q, want the cue back on Name", header)
	}
}

// The freshness cell is fixed-width whether or not it is marked, so a marked row
// cannot shift the columns of its neighbours.
func TestOutdatedMarkerDoesNotShiftTheRow(t *testing.T) {
	m, _ := newTestModel(t)
	fresh := brew.Package{Name: "alpha", Version: "1.0", Kind: brew.Cask}
	stale := brew.Package{Name: "alpha", Version: "1.0", Kind: brew.Cask, Outdated: true}

	plainOf := func(p brew.Package) string {
		return ansiSequence.ReplaceAllString(m.packageLine(p, false, 40), "")
	}
	a, b := plainOf(fresh), plainOf(stale)
	if cellIndex(a, "1.0") != cellIndex(b, "1.0") {
		t.Fatalf("marker moved the version column:\n fresh %q\n stale %q", a, b)
	}
	if lipgloss.Width(a) != lipgloss.Width(b) {
		t.Fatalf("marker changed the row width: %d vs %d", lipgloss.Width(a), lipgloss.Width(b))
	}
	if !strings.Contains(b, "↑") || strings.Contains(a, "↑") {
		t.Fatalf("marker not applied exactly to the outdated row:\n fresh %q\n stale %q", a, b)
	}
}

// cellIndex reports where needle starts in display cells, not bytes. The outdated
// marker is multi-byte, so a byte offset reports a column shift on a marked row
// whose columns are in fact identical.
func cellIndex(haystack, needle string) int {
	at := strings.Index(haystack, needle)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(haystack[:at])
}

// switchTo presses tab and drains the resulting load, so the caller is on the
// other list with keys accepted again. A bare Update leaves the model loading,
// where every ordinary key including the next tab is ignored.
func switchTo(t *testing.T, m *model) {
	t.Helper()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	drainList(t, m, cmd)
}

// backgroundActiveAtTrailingBlanks walks the row's SGR sequences and reports
// whether the theme background is still armed at the first trailing blank.
// Searching for the sequence anywhere in the row is not enough: an enclosing
// style emits one too, and the bleed guarded against here is precisely that its
// background stops at the first inner reset while the padding comes after it.
func backgroundActiveAtTrailingBlanks(row, backgroundParam string) (armed, hasPadding bool) {
	plain := ansiSequence.ReplaceAllString(row, "")
	trimmed := strings.TrimRight(plain, " ")
	if trimmed == plain {
		return false, false
	}
	lastVisible := len([]rune(trimmed))

	seen := 0
	for i := 0; i < len(row); {
		if loc := ansiSequence.FindStringIndex(row[i:]); loc != nil && loc[0] == 0 {
			seq := row[i : i+loc[1]]
			switch {
			case seq == "\x1b[m", seq == "\x1b[0m":
				armed = false
			case strings.Contains(seq, backgroundParam):
				armed = true
			}
			i += loc[1]
			continue
		}
		if seen == lastVisible {
			return armed, true
		}
		seen++
		i++
	}
	return armed, true
}

// Two-tone means every footer segment carries the full role rather than relying
// on an enclosing style, and the trailing padding does too. Otherwise a theme
// whose footer has a background ends that background at the first inner reset,
// leaving the rest of the row bare.
func TestFooterKeepsItsBackgroundAcrossTheWholeRow(t *testing.T) {
	m, _ := newTestModel(t)
	// Wide enough that the footer does not fill the row, so there is padding.
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 12})

	bright := -1
	for i, candidate := range themes {
		if candidate.name == "Bright" {
			bright = i
		}
	}
	if bright < 0 || themes[bright].footer.background.dark == "" {
		t.Fatal("no theme with a footer background left to exercise this")
	}
	m.themeIndex = bright

	row := m.footerLine(m.width - 2)
	if got := lipgloss.Width(row); got != m.width-2 {
		t.Fatalf("footer width=%d, want %d", got, m.width-2)
	}
	// Adaptive hexes render as truecolor backgrounds: any 48;2;r;g;b sequence.
	armed, hasPadding := backgroundActiveAtTrailingBlanks(row, "48;2;")
	if !hasPadding {
		t.Fatalf("no padding at width %d, so this asserts nothing", m.width-2)
	}
	if !armed {
		t.Fatalf("footer padding lost the theme background: %q", row)
	}
}

// An untrusted package claims the freshness cell and wins it over outdated:
// brew refuses to load its definition at all, so the refusal explains every
// other failure on the row and is the state to clear first.
func TestUntrustedRowClaimsTheFreshnessCell(t *testing.T) {
	m, _ := newTestModel(t)
	plain := func(pkg brew.Package, selected bool) string {
		return ansiSequence.ReplaceAllString(m.packageLine(pkg, selected, 40), "")
	}
	marked := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask, Untrusted: true}, true)
	if !strings.HasPrefix(marked, " >! Alpha") {
		t.Fatalf("untrusted row=%q, want a marker then the ! cell", marked)
	}
	both := plain(brew.Package{Name: "Alpha", Version: "1.0", Kind: brew.Cask, Outdated: true, Untrusted: true}, false)
	if !strings.HasPrefix(both, "  ! Alpha") {
		t.Fatalf("outdated+untrusted row=%q, want the ! cell to win", both)
	}
	if lipgloss.Width(marked) != 40 {
		t.Fatalf("row width=%d, want 40", lipgloss.Width(marked))
	}
}

// The bump highlight is enhancement only, and only where it can compose: the
// selected row is styled whole-line so an inner reset would cut its
// background, and monochrome carries no styling at all — both get plain text.
func TestBumpHighlightBoldsOnlyUnselectedColoredRows(t *testing.T) {
	m, _ := newTestModel(t)
	m.monochrome = false
	pkg := brew.Package{Version: "1.0.1", LatestVersion: "1.2.0", Kind: brew.Cask}

	highlighted := m.bumpHighlighted(pkg, false)
	if !strings.HasPrefix(highlighted, "1.") || !strings.Contains(highlighted, "2.0") {
		t.Fatalf("highlight lost the version text: %q", highlighted)
	}
	if highlighted == pkg.LatestVersion {
		t.Fatal("unselected colored row carries no highlight")
	}
	if got := m.bumpHighlighted(pkg, true); got != pkg.LatestVersion {
		t.Fatalf("selected row=%q, want plain text so the row style survives", got)
	}
	m.monochrome = true
	if got := m.bumpHighlighted(pkg, false); got != pkg.LatestVersion {
		t.Fatalf("monochrome=%q, want plain text", got)
	}
	m.monochrome = false
	unreadable := brew.Package{Version: "1.3.19-stable", LatestVersion: "1.4.0", Kind: brew.Cask}
	if got := m.bumpHighlighted(unreadable, false); got != unreadable.LatestVersion {
		t.Fatalf("unreadable pair=%q, want plain text (fail open, no highlight)", got)
	}
}
