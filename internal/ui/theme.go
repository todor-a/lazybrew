package ui

import "charm.land/lipgloss/v2"

type colorPair struct {
	foreground string
	background string
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
		name:     "Lazygit",
		border:   colorPair{foreground: "2"},
		header:   colorPair{foreground: "2"},
		selected: colorPair{background: "4"},
		footer:   colorPair{foreground: "4"},
		search:   colorPair{foreground: "6"},
	},
	{
		name:     "Bright",
		border:   colorPair{foreground: "6", background: "0"},
		header:   colorPair{foreground: "0", background: "6"},
		selected: colorPair{foreground: "0", background: "3"},
		status:   colorPair{foreground: "2", background: "0"},
		footer:   colorPair{foreground: "0", background: "7"},
		search:   colorPair{foreground: "6", background: "0"},
	},
	{
		name:     "Ocean",
		border:   colorPair{foreground: "6", background: "0"},
		header:   colorPair{foreground: "7", background: "4"},
		selected: colorPair{foreground: "0", background: "6"},
		status:   colorPair{foreground: "7", background: "4"},
		footer:   colorPair{foreground: "6", background: "0"},
		search:   colorPair{foreground: "6", background: "0"},
	},
	{
		name:     "Dracula",
		border:   colorPair{foreground: "5", background: "0"},
		header:   colorPair{foreground: "7", background: "5"},
		selected: colorPair{foreground: "0", background: "5"},
		status:   colorPair{foreground: "6", background: "0"},
		footer:   colorPair{foreground: "3", background: "0"},
		search:   colorPair{foreground: "6", background: "0"},
	},
}

func roleStyle(pair colorPair) lipgloss.Style {
	s := lipgloss.NewStyle()
	if pair.foreground != "" {
		s = s.Foreground(lipgloss.Color(pair.foreground))
	}
	if pair.background != "" {
		s = s.Background(lipgloss.Color(pair.background))
	}
	return s
}

func borderStyle(pair colorPair) lipgloss.Style {
	s := lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	if pair.foreground != "" {
		s = s.BorderForeground(lipgloss.Color(pair.foreground))
	}
	if pair.background != "" {
		s = s.BorderBackground(lipgloss.Color(pair.background))
	}
	return s
}
