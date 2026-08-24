package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Themes are true-color palettes, so a theme looks the same in every terminal
// instead of inheriting whatever the terminal maps the ANSI slots to. Each
// color names two hexes and the terminal's reported background picks the one
// that reads on it (model.isDark, from tea.BackgroundColorMsg). The page
// background itself is never painted: chrome that assumes its own canvas is
// what produced black bars on light terminals, and leaving the canvas to the
// terminal means only per-cell contrast has to be designed here.

type adaptive struct {
	light string
	dark  string
}

func (a adaptive) resolve(dark bool) (color.Color, bool) {
	hex := a.light
	if dark {
		hex = a.dark
	}
	if hex == "" {
		return nil, false
	}
	return lipgloss.Color(hex), true
}

type colorPair struct {
	foreground adaptive
	background adaptive
}

type theme struct {
	name     string
	border   colorPair
	header   colorPair
	selected colorPair
	status   colorPair
	footer   colorPair
	search   colorPair
}

var themes = []theme{
	{
		// GitHub Primer's grays and accents: unassertive chrome, one green
		// accent, lazygit's white-on-blue selection.
		name:     "Lazygit",
		border:   colorPair{foreground: adaptive{light: "#d0d7de", dark: "#3d444d"}},
		header:   colorPair{foreground: adaptive{light: "#1a7f37", dark: "#57ab5a"}},
		selected: colorPair{foreground: adaptive{light: "#ffffff", dark: "#ffffff"}, background: adaptive{light: "#0969da", dark: "#316dca"}},
		footer:   colorPair{foreground: adaptive{light: "#0969da", dark: "#539bf5"}},
		search:   colorPair{foreground: adaptive{light: "#1b7c83", dark: "#39c5cf"}},
	},
	{
		name:     "Bright",
		border:   colorPair{foreground: adaptive{light: "#087990", dark: "#22d3ee"}},
		header:   colorPair{foreground: adaptive{light: "#ffffff", dark: "#0c1618"}, background: adaptive{light: "#0e7490", dark: "#22d3ee"}},
		selected: colorPair{foreground: adaptive{light: "#ffffff", dark: "#1c2128"}, background: adaptive{light: "#bf8700", dark: "#e3b341"}},
		status:   colorPair{foreground: adaptive{light: "#1a7f37", dark: "#3fb950"}},
		footer:   colorPair{foreground: adaptive{light: "#1f2328", dark: "#e6edf3"}, background: adaptive{light: "#eaeef2", dark: "#30363d"}},
		search:   colorPair{foreground: adaptive{light: "#8250df", dark: "#d2a8ff"}},
	},
	{
		// Nord. The header, selection, and status bar keep one foreground and
		// background per mode because Nord's blues carry enough contrast to sit
		// on either canvas.
		name:     "Ocean",
		border:   colorPair{foreground: adaptive{light: "#8b9bb4", dark: "#4c566a"}},
		header:   colorPair{foreground: adaptive{light: "#eceff4", dark: "#eceff4"}, background: adaptive{light: "#5e81ac", dark: "#5e81ac"}},
		selected: colorPair{foreground: adaptive{light: "#2e3440", dark: "#2e3440"}, background: adaptive{light: "#88c0d0", dark: "#88c0d0"}},
		status:   colorPair{foreground: adaptive{light: "#eceff4", dark: "#eceff4"}, background: adaptive{light: "#5e81ac", dark: "#5e81ac"}},
		footer:   colorPair{foreground: adaptive{light: "#5e81ac", dark: "#88c0d0"}},
		search:   colorPair{foreground: adaptive{light: "#a5573f", dark: "#d08770"}},
	},
	{
		// Dracula in the dark, its official light counterpart Alucard's hues in
		// the light. The dark selection is Dracula's current-line gray rather
		// than a purple pill, which is how the scheme itself highlights.
		name:     "Dracula",
		border:   colorPair{foreground: adaptive{light: "#6e6c7e", dark: "#6272a4"}},
		header:   colorPair{foreground: adaptive{light: "#ffffff", dark: "#282a36"}, background: adaptive{light: "#644ac9", dark: "#bd93f9"}},
		selected: colorPair{foreground: adaptive{light: "#ffffff", dark: "#f8f8f2"}, background: adaptive{light: "#644ac9", dark: "#44475a"}},
		status:   colorPair{foreground: adaptive{light: "#036a96", dark: "#8be9fd"}},
		footer:   colorPair{foreground: adaptive{light: "#644ac9", dark: "#bd93f9"}},
		search:   colorPair{foreground: adaptive{light: "#a3144d", dark: "#ff79c6"}},
	},
}

func roleStyle(pair colorPair, dark bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c, ok := pair.foreground.resolve(dark); ok {
		s = s.Foreground(c)
	}
	if c, ok := pair.background.resolve(dark); ok {
		s = s.Background(c)
	}
	return s
}

func borderStyle(pair colorPair, dark bool) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	if c, ok := pair.foreground.resolve(dark); ok {
		s = s.BorderForeground(c)
	}
	if c, ok := pair.background.resolve(dark); ok {
		s = s.BorderBackground(c)
	}
	return s
}

// The model-bound forms exist so view code does not thread isDark through
// every call site.
func (m *model) roleStyle(pair colorPair) lipgloss.Style   { return roleStyle(pair, m.isDark) }
func (m *model) borderStyle(pair colorPair) lipgloss.Style { return borderStyle(pair, m.isDark) }
