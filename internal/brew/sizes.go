package brew

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// duPath is the absolute base-system du rather than a PATH lookup. macOS-only is
// already a non-goal boundary, /usr/bin/du is SIP-protected base system, and
// resolving it through the child environment would only add a hijack surface for
// a process this package spawns.
const duPath = "/usr/bin/du"

var errMissingRoot = errors.New("Homebrew did not report a usable package root")

// safeRootPath applies the section 3 argv rule to a path Homebrew printed.
func safeRootPath(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	for _, r := range path {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Sizes is one measurement of installed size, in KB, for every installed
// formula, plus their total.
//
// Formulae only, and the Caskroom is not measured at all. A cask's Caskroom entry
// is frequently a symlink to the bundle in /Applications, which du reports as
// about 12 KB; where it is not a symlink it may be a leftover installer package,
// which reports the installer rather than the app. Measured on the development
// machine: 29 of 39 casks read under 1 MB, alt-tab read 12 KB against a real
// 12 MB bundle, and the largest cask row was a 1.1 GB leftover Excel installer
// against a 2.4 GB application. Those numbers are not sizes, so none is offered -
// not per row, and not summed into a total either.
//
// The Cellar is different: it holds the real files, and it is 9.2 GB of the
// 11.3 GB this screen exists to account for.
type Sizes struct {
	Formula map[string]int64
	Total   int64
}

// KB reports one formula's measured size. Anything unmeasured - every cask, and
// any formula du could not read - reports false, which renders as a blank size
// column rather than as a zero.
func (s Sizes) KB(kind Kind, name string) (int64, bool) {
	if kind != Formula {
		return 0, false
	}
	size, ok := s.Formula[name]
	return size, ok
}

// Sizes measures every installed formula in ONE du pass over the Cellar that
// Homebrew itself prints.
//
// There is no argv-only alternative: `brew info` carries no installed size (see
// section 5 of SPEC.md), and per-package `brew info` would be 304 calls at
// ~400ms each. One `du -k -d 1` over the Cellar yields every formula size and
// the fleet total, measured at about 2s warm over 9.2 GB.
//
// The layout assumption is exactly one sentence and exactly one directory level:
// each direct child of the Cellar is one formula, named by the formula name. It
// is verified as a zero-diff match against `brew list --formula` (304) on the
// measured machine.
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
	stdout, _, runErr := runTool(ctx, duPath, os.Environ(), []string{"-k", "-d", "1", cellar})
	sizes := parseSizes(string(stdout), cellar)
	// du exits nonzero for one unreadable subdirectory while still measuring
	// everything else, so partial sizes are kept. Nothing parsed means nothing
	// was measured, and that is reported rather than rendered as zeroes.
	if runErr != nil && len(sizes.Formula) == 0 {
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
	// The same rule section 3 applies to package names, applied here because this
	// value also reaches an argv. A path beginning with "-" would be read by du as
	// an option rather than an operand, and a control character has no business in
	// a path Homebrew printed. It comes from brew rather than from the inventory,
	// which is why it is checked rather than assumed.
	if !safeRootPath(path) {
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
func parseSizes(output, cellar string) Sizes {
	sizes := Sizes{Formula: make(map[string]int64)}
	for _, rawLine := range strings.Split(output, "\n") {
		field, path, ok := strings.Cut(strings.TrimRight(rawLine, "\r"), "\t")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
		if err != nil {
			continue
		}
		if path == cellar {
			// du prints the root last, so this is the whole Cellar and not a
			// running sum of the rows above.
			sizes.Total = kb
			continue
		}
		if filepath.Dir(path) == cellar {
			sizes.Formula[filepath.Base(path)] = kb
		}
	}
	return sizes
}
