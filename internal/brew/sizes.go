package brew

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// duPath is the absolute base-system du rather than a PATH lookup. macOS-only is
// already a non-goal boundary, /usr/bin/du is SIP-protected base system, and
// resolving it through the child environment would only add a hijack surface for
// a process this package spawns.
const duPath = "/usr/bin/du"

var errMissingRoot = errors.New("Homebrew did not report its package root")

// Sizes is one measurement of installed size, in KB, for every installed
// package, keyed by kind and name.
type Sizes struct {
	Formula map[string]int64
	Cask    map[string]int64
	Total   int64
}

// KB reports one package's measured size. A package with no measurement reports
// false, which renders as a blank size column rather than as a zero.
func (s Sizes) KB(kind Kind, name string) (int64, bool) {
	var byName map[string]int64
	switch kind {
	case Cask:
		byName = s.Cask
	case Formula:
		byName = s.Formula
	default:
		return 0, false
	}
	size, ok := byName[name]
	return size, ok
}

// Sizes measures every installed package in ONE du pass over the two roots that
// Homebrew itself prints.
//
// There is no argv-only alternative: `brew info` carries no installed size (see
// section 5 of SPEC.md), and per-package `brew info` would be 304 calls at
// ~400ms each. One `du -k -d 1` over both roots yields every package size and
// both fleet totals, measured at 2.1s warm and 5.6s cold over 11.3 GB.
//
// The layout assumption is exactly one sentence and exactly one directory level:
// each direct child of those two roots is one package, named by the package name.
// It is verified as a zero-diff match against `brew list --formula` (304) and
// `brew list --cask -1` (39) on the measured machine.
//
// ponytail: no persisted cache. Ceiling is the pass cost, which is unbounded in
// principle — a much larger Cellar or a slow volume only makes sizes arrive
// later, because this never blocks Update and is cancellable. Upgrade path if
// that stops being acceptable is an on-disk cache keyed by the two roots' mtimes.
func (c client) Sizes(ctx context.Context) (Sizes, error) {
	cellar, err := c.root(ctx, "--cellar")
	if err != nil {
		return Sizes{}, err
	}
	caskroom, err := c.root(ctx, "--caskroom")
	if err != nil {
		return Sizes{}, err
	}

	stdout, _, runErr := runTool(ctx, duPath, os.Environ(), []string{"-k", "-d", "1", cellar, caskroom})
	sizes := parseSizes(string(stdout), cellar, caskroom)
	// du exits nonzero for one unreadable subdirectory while still measuring
	// everything else, so partial sizes are kept. Nothing parsed means nothing
	// was measured, and that is reported rather than rendered as zeroes.
	if runErr != nil && len(sizes.Formula) == 0 && len(sizes.Cask) == 0 {
		return Sizes{}, runErr
	}
	return sizes, nil
}

func (client) root(ctx context.Context, flag string) (string, error) {
	stdout, _, err := run(ctx, []string{flag})
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(stdout))
	if path == "" {
		return "", errMissingRoot
	}
	return path, nil
}

// parseSizes reads du's `<kb>\t<path>` rows. A direct child of a root is one
// package keyed by its base name; a row whose path is a root itself carries that
// root's total. Malformed rows are skipped, so a Homebrew or du output change
// degrades to a blank size column and an absent total, never to a wrong number.
//
// SECURITY: the names read here are only ever used as map keys, looked up against
// names that came from `brew list`. They never reach an argv, so this path cannot
// introduce an unsafe package name and needs no second validator.
func parseSizes(output, cellar, caskroom string) Sizes {
	sizes := Sizes{Formula: make(map[string]int64), Cask: make(map[string]int64)}
	for _, rawLine := range strings.Split(output, "\n") {
		field, path, ok := strings.Cut(strings.TrimRight(rawLine, "\r"), "\t")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
		if err != nil {
			continue
		}
		if path == cellar || path == caskroom {
			sizes.Total += kb
			continue
		}
		switch filepath.Dir(path) {
		case cellar:
			sizes.Formula[filepath.Base(path)] = kb
		case caskroom:
			sizes.Cask[filepath.Base(path)] = kb
		}
	}
	return sizes
}
