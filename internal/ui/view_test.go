package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazybrew/internal/brew"
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
	want := "[/ or s] search  u uninstall  "
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
	for _, exact := range []string{"lazybrew [Lazygit]", "Confirm uninstall", "Uninstall Alpha?", "y: confirm  other: cancel"} {
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
		if got != want || !strings.Contains(plain, "> Alpha") {
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
