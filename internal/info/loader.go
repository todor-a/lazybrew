package info

import (
	"context"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"lazybrew/internal/brew"
)

// LoadingText is the info-pane placeholder shown while a package's details are
// still being fetched. Exported so the view can render the same string for the
// window where a list load has left nothing selected to fetch details for.
const LoadingText = "Loading info..."

// LoadFunc loads package information without blocking the Bubble Tea update loop.
type LoadFunc func(context.Context, brew.Package) (string, error)

// Result is the non-secret completion message returned by an info command.
type Result struct {
	Generation uint64
	Kind       brew.Kind
	Name       string
	Text       string
	Err        error
}

type key struct {
	kind brew.Kind
	name string
}

type identity struct {
	generation uint64
	key        key
}

type request struct {
	identity identity
	cancel   context.CancelFunc
}

type target struct {
	identity identity
	pkg      brew.Package
}

// Loader serializes info commands and retains only the latest pending target.
type Loader struct {
	mu sync.Mutex

	load       LoadFunc
	generation uint64
	cache      map[key]string
	current    *key
	text       string
	active     *request
	pending    *target
	done       <-chan struct{}
	stopped    bool
}

// New creates an idle loader.
func New(load LoadFunc) *Loader {
	done := make(chan struct{})
	close(done)
	return &Loader{
		load:  load,
		cache: make(map[key]string),
		done:  done,
	}
}

// Select targets pkg and returns a command only when no other info command is active.
func (l *Loader) Select(pkg *brew.Package) tea.Cmd {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.selectLocked(pkg)
}

// Refresh starts a fresh cache generation and targets pkg in that generation.
func (l *Loader) Refresh(pkg *brew.Package) tea.Cmd {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.generation++
	l.cache = make(map[key]string)
	l.current = nil
	l.text = ""
	l.pending = nil
	return l.selectLocked(pkg)
}

// Complete handles one command result and starts the retained pending target, if any.
func (l *Loader) Complete(result Result) tea.Cmd {
	l.mu.Lock()
	defer l.mu.Unlock()

	completed := identity{
		generation: result.Generation,
		key:        key{kind: result.Kind, name: result.Name},
	}
	if l.active == nil || l.active.identity != completed {
		return nil
	}

	l.active.cancel()
	l.active = nil
	if !l.stopped && result.Generation == l.generation {
		text := strings.TrimRight(result.Text, "\r\n")
		if result.Err != nil {
			text = result.Err.Error()
		}
		l.cache[completed.key] = text
		if l.current != nil && *l.current == completed.key {
			l.text = text
		}
	}

	if l.stopped || l.pending == nil {
		l.pending = nil
		return nil
	}

	next := *l.pending
	l.pending = nil
	if next.identity.generation != l.generation {
		return nil
	}
	if text, ok := l.cache[next.identity.key]; ok {
		if l.current != nil && *l.current == next.identity.key {
			l.text = text
		}
		return nil
	}
	return l.startLocked(next)
}

// Text returns the information for the current target.
func (l *Loader) Text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text
}

// Cancel prevents new work, clears pending work, and cancels the active command.
func (l *Loader) Cancel() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.stopped = true
	l.pending = nil
	if l.active != nil {
		l.active.cancel()
	}
}

// Done closes when the currently active load function has returned. It is already
// closed while the loader is idle.
func (l *Loader) Done() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.done
}

func (l *Loader) selectLocked(pkg *brew.Package) tea.Cmd {
	if l.stopped {
		return nil
	}
	if pkg == nil {
		l.current = nil
		l.text = ""
		l.pending = nil
		return nil
	}

	pkgCopy := *pkg
	selected := key{kind: pkgCopy.Kind, name: pkgCopy.Name}
	l.current = &selected
	if text, ok := l.cache[selected]; ok {
		l.text = text
		l.pending = nil
		return nil
	}

	wanted := identity{generation: l.generation, key: selected}
	l.text = LoadingText
	if l.active != nil {
		if l.active.identity == wanted {
			l.pending = nil
		} else {
			l.pending = &target{identity: wanted, pkg: pkgCopy}
		}
		return nil
	}
	return l.startLocked(target{identity: wanted, pkg: pkgCopy})
}

func (l *Loader) startLocked(next target) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	l.active = &request{
		identity: next.identity,
		cancel:   cancel,
	}
	l.done = done

	load := l.load
	return func() tea.Msg {
		defer close(done)
		text, err := load(ctx, next.pkg)
		return Result{
			Generation: next.identity.generation,
			Kind:       next.identity.key.kind,
			Name:       next.identity.key.name,
			Text:       text,
			Err:        err,
		}
	}
}
