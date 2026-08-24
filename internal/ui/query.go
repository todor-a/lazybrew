package ui

import (
	"strings"

	"lazybrew/internal/brew"
)

// qualifiers maps an is:<name> token to the rows it keeps. Adding a filter is
// one entry here plus a mention in searchHelp (keys.go); the parser, the
// setPackages seam, and the count line pick it up unchanged. is:dep is
// deliberately not an entry: it lifts the default dependency hide instead of
// narrowing — see parseQuery.
var qualifiers = map[string]func(brew.Package) bool{
	"outdated":  func(pkg brew.Package) bool { return pkg.Outdated },
	"untrusted": func(pkg brew.Package) bool { return pkg.Untrusted },
}

// queryFilter is one parsed search query: the narrowing predicates, the
// dependency-visibility override, and the residual substring text.
type queryFilter struct {
	predicates []func(brew.Package) bool
	showDeps   bool
	text       string
}

// parseQuery splits the query into is: qualifiers and residual text. An
// unknown is: token stays in the text: a typo like is:outdatd must narrow by
// substring like any other word, never render a confident empty list from a
// filter that does not exist. Qualifiers match case-insensitively; residual
// tokens keep their spelling (the substring match lowers both sides anyway).
//
// is:dep is the query spelling of the `a` key: it lifts the default
// dependency hide rather than narrowing to dependencies alone, so the key and
// a typed query can never disagree about what the list shows.
// ponytail: "only dependencies" is not expressible; add a narrowing spelling
// when someone asks for one.
func parseQuery(query string) queryFilter {
	var parsed queryFilter
	var residual []string
	for _, token := range strings.Fields(query) {
		name, ok := strings.CutPrefix(strings.ToLower(token), "is:")
		if !ok {
			residual = append(residual, token)
			continue
		}
		if name == "dep" {
			parsed.showDeps = true
			continue
		}
		if predicate, known := qualifiers[name]; known {
			parsed.predicates = append(parsed.predicates, predicate)
			continue
		}
		residual = append(residual, token)
	}
	parsed.text = strings.Join(residual, " ")
	return parsed
}

// narrowing reports whether the query can shrink the list: predicates or
// text. is:dep alone widens, so it does not count.
func (f queryFilter) narrowing() bool {
	return len(f.predicates) > 0 || f.text != ""
}

// match reports whether pkg passes every narrowing qualifier.
func (f queryFilter) match(pkg brew.Package) bool {
	for _, predicate := range f.predicates {
		if !predicate(pkg) {
			return false
		}
	}
	return true
}

// toggleQualifier adds token to the front of the query or removes every
// occurrence of it, reporting whether the token is present afterwards. The
// front, so a key press surfaces its token where the eye lands first; removal
// folds case so a hand-typed spelling toggles off the same as a written one.
// The query is rejoined on single spaces — the callers are key handlers, so
// this normalizes machine-written text, not a spelling anyone typed.
func toggleQualifier(query, token string) (string, bool) {
	fields := strings.Fields(query)
	kept := fields[:0]
	removed := false
	for _, field := range fields {
		if strings.EqualFold(field, token) {
			removed = true
			continue
		}
		kept = append(kept, field)
	}
	if removed {
		return strings.Join(kept, " "), false
	}
	return strings.Join(append([]string{token}, kept...), " "), true
}
