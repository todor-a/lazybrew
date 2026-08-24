package brew

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode"
)

// Kind identifies one of Homebrew's two supported package inventories.
type Kind string

const (
	Cask    Kind = "cask"
	Formula Kind = "formula"
)

// Package is one row returned by Homebrew's installed-package inventory.
//
// Outdated carries Homebrew's own `brew outdated` verdict. Like Version it is
// display data, not identity: it is not part of the info cache key and no argv
// reads it.
type Package struct {
	Name     string
	Version  string
	Kind     Kind
	Outdated bool
	// OutdatedKnown records whether a `brew outdated` verdict was actually
	// obtained. Without it a failed read is indistinguishable from a confirmed
	// fresh package, and the detail panel would answer "up to date" from no
	// evidence at all. Display state only: it is outside the cache key, the
	// filter value, and every argv, exactly as Outdated is.
	OutdatedKnown bool
	// Dependency is true when Homebrew reports the formula as not installed on
	// request. Always false for a cask. Display data, not identity, exactly like
	// Version: the (Kind, Name) info-cache key is unaffected.
	Dependency bool
}

// Homebrew exposes the read-only operations used by the application.
type Homebrew interface {
	List(context.Context, Kind) ([]Package, error)
	Outdated(context.Context, Kind) ([]string, error)
	Info(context.Context, Package) (string, error)
	Uses(context.Context, Package) ([]string, error)
	Sizes(context.Context) (Sizes, error)
}

type client struct{}

var (
	errInvalidKind = errors.New("invalid Homebrew package kind")
	errUnsafeInfo  = errors.New("Unsafe package name; info refused")
	errUnsafeUses  = errors.New("Unsafe package name; dependents refused")
	errUsesKind    = errors.New("dependents are only defined for formulae")
)

// New returns the real Homebrew command adapter.
func New() Homebrew {
	return client{}
}

// List enumerates one kind's installed packages.
//
// The formula inventory is the unfiltered `brew list --formula`, and a second
// call marks which of those rows Homebrew installed as a dependency rather than
// on request. Filtering the inventory with `--installed-on-request` instead —
// which is what parity did — hid two different things: every dependency, and
// also formulae the user did explicitly request whose receipt Homebrew does not
// report under either filter (third-party taps on the measured machine: 116 on
// request plus 180 not-on-request is 296 of 304). Enumerating everything and
// using the filter only as a marker surfaces both, and keeps the row count
// reconcilable with the measured size total.
//
// A failed marker call fails the whole load rather than degrading: 304 rows all
// labelled on-request would be a lie about removal safety on a screen that feeds
// a destructive action.
func (client) List(ctx context.Context, kind Kind) ([]Package, error) {
	args, err := listArgs(kind)
	if err != nil {
		return nil, err
	}
	if kind != Formula {
		stdout, _, err := run(ctx, args)
		if err != nil {
			return nil, err
		}
		return parseList(string(stdout), kind), nil
	}

	// The enumeration and the dependency marker are independent reads of the same
	// inventory, so they run concurrently. Sequentially they were about 0.64s each
	// on the development machine, which doubled the formula load the list cache
	// exists to avoid; concurrently the load is the slower of the two.
	var (
		wg                       sync.WaitGroup
		stdout, dependencyStdout []byte
		listErr, dependencyErr   error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdout, _, listErr = run(ctx, args)
	}()
	go func() {
		defer wg.Done()
		dependencyStdout, _, dependencyErr = run(ctx, []string{"list", "--formula", "--no-installed-on-request"})
	}()
	wg.Wait()

	// The enumeration first: a failure there means there is no inventory at all,
	// which is the more useful thing to report.
	if listErr != nil {
		return nil, listErr
	}
	// The marker read stays load-bearing. Degrading to an unmarked list would show
	// every dependency as installed on request, and the toggle that reveals them
	// would silently show nothing.
	if dependencyErr != nil {
		return nil, dependencyErr
	}
	packages := parseList(string(stdout), kind)
	dependencies := make(map[string]struct{})
	for _, name := range parseNames(string(dependencyStdout)) {
		dependencies[name] = struct{}{}
	}
	for i := range packages {
		if _, ok := dependencies[packages[i].Name]; ok {
			packages[i].Dependency = true
		}
	}
	return packages, nil
}

// Outdated reports the names of kind that `brew upgrade` would act on.
//
// Never `--greedy`. Homebrew's default set already excludes the auto-updating
// casks it will not touch, and adding the flag would mark a cask that legitimately
// sits behind Homebrew's version as out of date — the exact false claim the info
// pane refuses to make. Measured here: the default cask set is {postman}; with
// `--greedy` it is 14 names, including firefox, which brew will not upgrade.
//
// Per kind rather than one combined call: a formula and a cask can share a name,
// and the marker set is consulted per kind.
func (client) Outdated(ctx context.Context, kind Kind) ([]string, error) {
	flag, err := kindFlag(kind)
	if err != nil {
		return nil, err
	}

	stdout, _, err := run(ctx, []string{"outdated", flag, "--quiet"})
	if err != nil {
		return nil, err
	}
	return parseNames(string(stdout)), nil
}

func (client) Info(ctx context.Context, pkg Package) (string, error) {
	if !safePackageName(pkg.Name) {
		return "", errUnsafeInfo
	}
	flag, err := kindFlag(pkg.Kind)
	if err != nil {
		return "", err
	}

	stdout, _, err := run(ctx, []string{"info", flag, pkg.Name})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(stdout), "\r\n"), nil
}

// Uses reports the installed formulae that depend on pkg, so the info pane can
// say whether removing it would break something else.
//
// Formulae only. `brew uses` resolves its argument as a formula, so a cask token
// makes it warn about an unknown formula and report nothing; a cask must not be
// allowed to read as "nothing depends on this".
func (client) Uses(ctx context.Context, pkg Package) ([]string, error) {
	if pkg.Kind != Formula {
		return nil, errUsesKind
	}
	if !safePackageName(pkg.Name) {
		return nil, errUnsafeUses
	}

	stdout, _, err := run(ctx, []string{"uses", "--installed", pkg.Name})
	if err != nil {
		return nil, err
	}
	return parseNames(string(stdout)), nil
}

func parseNames(output string) []string {
	var names []string
	for _, rawLine := range strings.Split(output, "\n") {
		if line := strings.TrimSpace(rawLine); line != "" {
			names = append(names, line)
		}
	}
	return names
}

// listArgs is deliberately asymmetric about --versions. For formulae the flag
// reads keg directory names out of the Cellar, so it fills the version column
// at the same cost as the bare listing (measured ~0.4s either way), and even
// an untrusted-tap formula still lists. For casks the same flag has to
// evaluate every cask to learn its version, and Homebrew's tap-trust gate
// then refuses the whole listing over one untrusted tap — measured on the
// development machine: a single untrusted cask tap exits the command with
// zero rows. One tapped cask must not cost the entire pane, so the cask
// inventory stays bare names.
//
// ponytail: a formula with several installed versions prints them all on one
// row and parseList keeps the remainder verbatim ("1.0 1.2"), which is the
// truth about what is installed. Picking one would impose an order this
// listing does not promise; the upgrade path is brew outdated's
// installed_versions, which does.
func listArgs(kind Kind) ([]string, error) {
	switch kind {
	case Cask:
		return []string{"list", "--cask", "-1"}, nil
	case Formula:
		return []string{"list", "--formula", "--versions"}, nil
	default:
		return nil, errInvalidKind
	}
}

func kindFlag(kind Kind) (string, error) {
	switch kind {
	case Cask:
		return "--cask", nil
	case Formula:
		return "--formula", nil
	default:
		return "", errInvalidKind
	}
}

func safePackageName(name string) bool {
	if name == "" || name[0] == '-' {
		return false
	}
	for _, r := range name {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseList(output string, kind Kind) []Package {
	var packages []Package
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		name, version := line, ""
		if separator := strings.IndexFunc(line, unicode.IsSpace); separator >= 0 {
			name = line[:separator]
			version = strings.TrimLeftFunc(line[separator:], unicode.IsSpace)
		}
		packages = append(packages, Package{Name: name, Version: version, Kind: kind})
	}
	return packages
}
