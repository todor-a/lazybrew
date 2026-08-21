package ui

import "charm.land/bubbles/v2/key"

type footerKeys []key.Binding

func (k footerKeys) ShortHelp() []key.Binding  { return k }
func (k footerKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k} }

var (
	normalHelp = footerKeys{
		key.NewBinding(key.WithKeys("/", "s", "S"), key.WithHelp("[/ or s]", "search")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")),
		key.NewBinding(key.WithKeys("u", "U"), key.WithHelp("u", "uninstall")),
		key.NewBinding(key.WithKeys("t", "T"), key.WithHelp("t", "theme")),
		key.NewBinding(key.WithKeys("r", "R"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("q", "quit")),
	}
	confirmHelp = footerKeys{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
		key.NewBinding(key.WithKeys("__cancel_help__"), key.WithHelp("any other key", "cancels")),
	}
	loadingHelp = footerKeys{
		key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("q", "quit")),
	}
	progressHelp = footerKeys{
		key.NewBinding(key.WithKeys("__progress_help__"), key.WithHelp("Uninstall in progress;", "controls disabled")),
	}
	cleanupHelp = footerKeys{
		key.NewBinding(key.WithKeys("__cleanup_help__"), key.WithHelp("Cleanup in progress;", "controls disabled")),
	}
)
