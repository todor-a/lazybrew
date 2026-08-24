package ui

import (
	"charm.land/bubbles/v2/key"

	"lazybrew/internal/brew"
)

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
		key.NewBinding(key.WithKeys("d", "D"), key.WithHelp("Uninstall:", "d")),
		key.NewBinding(key.WithKeys("u", "U"), key.WithHelp("Upgrade:", "u")),
		key.NewBinding(key.WithKeys("a", "A"), key.WithHelp("All:", "a")),
		key.NewBinding(key.WithKeys("f", "F"), key.WithHelp("Filter:", "f")),
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

	// searchHelp trades the browse verbs for the search vocabulary: the mode
	// owns every printable key, so the only teachable things are the
	// qualifiers and the two ways out. This row is the ambient discoverability
	// surface behind `f` writing its token into the query; when the qualifier
	// list outgrows one row, that is the cue for a real help overlay — not a
	// longer row.
	searchHelp = footerKeys{
		key.NewBinding(key.WithKeys("__filters_help__"), key.WithHelp("Filters:", "is:outdated · is:untrusted · is:dep")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("Keep:", "enter")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("Clear:", "esc")),
	}

	cleanupHelp = footerKeys{
		key.NewBinding(key.WithKeys("__cleanup_help__"), key.WithHelp("Cleanup in progress;", "controls disabled")),
	}
)

// operationHelp is the job-window footer: the running verb plus the browsing
// keys that stay live while it runs. Navigation is not listed - normalHelp
// never lists it either - so the row teaches only what the window changes.
func operationHelp(op brew.Operation) footerKeys {
	return footerKeys{
		key.NewBinding(
			key.WithKeys("__operation_help__"),
			key.WithHelp(words(op).title+" in progress;", "browse only"),
		),
		key.NewBinding(key.WithKeys("a", "A"), key.WithHelp("All:", "a")),
		key.NewBinding(key.WithKeys("o", "O"), key.WithHelp("Sort:", "o")),
		key.NewBinding(key.WithKeys("t", "T"), key.WithHelp("Theme:", "t")),
	}
}

// progressHelp names the verb actually running. A frozen-controls footer that
// says the wrong verb is worse than one that says none, and the operation is
// already carried immutably beside the confirmation snapshot.
func progressHelp(op brew.Operation) footerKeys {
	return footerKeys{
		key.NewBinding(
			key.WithKeys("__progress_help__"),
			key.WithHelp(words(op).title+" in progress;", "controls disabled"),
		),
	}
}
