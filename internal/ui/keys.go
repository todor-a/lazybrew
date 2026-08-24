package ui

import "charm.land/bubbles/v2/key"

type footerKeys []key.Binding

func (k footerKeys) ShortHelp() []key.Binding  { return k }
func (k footerKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k} }

var (
	// Action first, then the key that performs it, joined by " | ". A footer is
	// read by someone looking for a verb, not by someone scanning single letters,
	// so the verb leads and the key answers it.
	//
	// The label sits in the binding's help-key slot and the keystroke in its
	// description slot, which is the reverse of the field names. Bubbles renders
	// key-then-description with no way to swap them, and these bindings are
	// already display-only here: nothing dispatches through them, and the
	// placeholder entries below carry no real key at all. The footer styles
	// compensate, so the keystroke is still the emphasised half.
	normalHelp = footerKeys{
		key.NewBinding(key.WithKeys("/", "s", "S"), key.WithHelp("Search:", "/")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("Switch:", "tab")),
		key.NewBinding(key.WithKeys("u", "U"), key.WithHelp("Uninstall:", "u")),
		key.NewBinding(key.WithKeys("d", "D"), key.WithHelp("Deps:", "d")),
		key.NewBinding(key.WithKeys("o", "O"), key.WithHelp("Sort:", "o")),
		key.NewBinding(key.WithKeys("t", "T"), key.WithHelp("Theme:", "t")),
		key.NewBinding(key.WithKeys("r", "R"), key.WithHelp("Refresh:", "r")),
		key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("Quit:", "q")),
	}
	confirmHelp = footerKeys{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("Confirm:", "y")),
		key.NewBinding(key.WithKeys("__cancel_help__"), key.WithHelp("Cancel:", "any other key")),
	}
	loadingHelp = footerKeys{
		key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("Quit:", "q")),
	}
	progressHelp = footerKeys{
		key.NewBinding(key.WithKeys("__progress_help__"), key.WithHelp("Uninstall in progress;", "controls disabled")),
	}
	cleanupHelp = footerKeys{
		key.NewBinding(key.WithKeys("__cleanup_help__"), key.WithHelp("Cleanup in progress;", "controls disabled")),
	}
)
