package brew

import (
	"strconv"
	"strings"
)

// Distance classifies how far apart two Homebrew version strings are, so the
// outdated threshold can decide whether a bump is worth a mark.
type Distance uint8

const (
	DistanceNone Distance = iota
	DistancePatch
	DistanceMinor
	DistanceMajor
	// DistanceUnknown deliberately orders above every real distance so that
	// every threshold comparison passes: a version pair this package could not
	// read must never hide a brew-reported update. Failing open is the whole
	// contract of this type.
	DistanceUnknown
)

// VersionDistance classifies the jump from installed to latest.
//
// Homebrew versions are not semver — "1.3.19-stable", "2026.07.27.00_1", and
// bare dates like "20260426" are all real — so this is a heuristic over
// dotted numeric segments plus Homebrew's `_N` revision suffix. Segments are
// compared numerically with missing trailing segments read as 0 ("1.2" equals
// "1.2.0"); the index of the first difference names the class: 0 is major,
// 1 is minor, deeper — and a revision-only change — is patch. Any segment on
// either side that is not plain digits makes the whole pair Unknown, which
// fails open per the constant above.
func VersionDistance(installed, latest string) Distance {
	a, aRevision, ok := parseVersion(installed)
	if !ok {
		return DistanceUnknown
	}
	b, bRevision, ok := parseVersion(latest)
	if !ok {
		return DistanceUnknown
	}
	for i := range max(len(a), len(b)) {
		if segmentAt(a, i) == segmentAt(b, i) {
			continue
		}
		switch i {
		case 0:
			return DistanceMajor
		case 1:
			return DistanceMinor
		default:
			return DistancePatch
		}
	}
	if aRevision != bRevision {
		return DistancePatch
	}
	return DistanceNone
}

// BumpOffset reports the byte offset in latest where the first differing
// segment starts, so a renderer can embolden the part that actually changed.
// -1 means no highlight: the pair is unreadable, equal, or the difference is
// not addressable inside latest (a shorter latest whose missing segment is
// the change). Offsets are computed by walking the string, not by
// reformatting parsed numbers, so leading zeros ("2026.07.27") keep their
// true positions.
func BumpOffset(installed, latest string) int {
	a, aRevision, ok := parseVersion(installed)
	if !ok {
		return -1
	}
	b, bRevision, ok := parseVersion(latest)
	if !ok {
		return -1
	}
	difference := -1
	for i := range max(len(a), len(b)) {
		if segmentAt(a, i) != segmentAt(b, i) {
			difference = i
			break
		}
	}
	if difference < 0 {
		if aRevision != bRevision {
			if i := strings.IndexByte(latest, '_'); i >= 0 {
				return i
			}
		}
		return -1
	}
	if difference >= len(b) {
		return -1
	}
	base := latest
	if i := strings.IndexByte(latest, '_'); i >= 0 {
		base = latest[:i]
	}
	offset := 0
	for range difference {
		next := strings.IndexByte(base[offset:], '.')
		if next < 0 {
			return -1
		}
		offset += next + 1
	}
	return offset
}

func segmentAt(segments []int64, i int) int64 {
	if i < len(segments) {
		return segments[i]
	}
	return 0
}

// parseVersion splits "1.2.3_4" into numeric segments [1 2 3] and revision 4.
// ok is false when the base is empty or any piece is not plain digits, which
// callers translate into the fail-open Unknown.
func parseVersion(version string) (segments []int64, revision int64, ok bool) {
	base := version
	if i := strings.IndexByte(version, '_'); i >= 0 {
		base = version[:i]
		parsed, err := strconv.ParseInt(version[i+1:], 10, 64)
		if err != nil {
			return nil, 0, false
		}
		revision = parsed
	}
	if base == "" {
		return nil, 0, false
	}
	for _, part := range strings.Split(base, ".") {
		parsed, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, 0, false
		}
		segments = append(segments, parsed)
	}
	return segments, revision, true
}
