package brew

import (
	"context"
	"errors"
	"strings"
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
}

// Homebrew exposes the read-only operations used by the application.
type Homebrew interface {
	List(context.Context, Kind) ([]Package, error)
	Outdated(context.Context, Kind) ([]string, error)
	Info(context.Context, Package) (string, error)
	Uses(context.Context, Package) ([]string, error)
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

func (client) List(ctx context.Context, kind Kind) ([]Package, error) {
	args, err := listArgs(kind)
	if err != nil {
		return nil, err
	}

	stdout, _, err := run(ctx, args)
	if err != nil {
		return nil, err
	}
	return parseList(string(stdout), kind), nil
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

func listArgs(kind Kind) ([]string, error) {
	switch kind {
	case Cask:
		return []string{"list", "--cask", "-1"}, nil
	case Formula:
		return []string{"list", "--formula", "--installed-on-request"}, nil
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
