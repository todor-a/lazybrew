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
type Package struct {
	Name    string
	Version string
	Kind    Kind
}

// Homebrew exposes the read-only operations used by the application.
type Homebrew interface {
	List(context.Context, Kind) ([]Package, error)
	Info(context.Context, Package) (string, error)
}

type client struct{}

var (
	errInvalidKind = errors.New("invalid Homebrew package kind")
	errUnsafeInfo  = errors.New("Unsafe package name; info refused")
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
