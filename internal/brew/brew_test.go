package brew

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

func TestKindValues(t *testing.T) {
	if Cask != "cask" || Formula != "formula" {
		t.Fatalf("unexpected kind values: %q, %q", Cask, Formula)
	}
}

func TestParseList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		kind   Kind
		want   []Package
	}{
		{
			name:   "blank output",
			output: " \n\t\n",
			kind:   Cask,
		},
		{
			name:   "name without version",
			output: "  firefox  \n",
			kind:   Cask,
			want:   []Package{{Name: "firefox", Kind: Cask}},
		},
		{
			name:   "complete version remainder",
			output: "one  1.0   build 2\ntwo\t2.0  extra\n",
			kind:   Formula,
			want: []Package{
				{Name: "one", Version: "1.0   build 2", Kind: Formula},
				{Name: "two", Version: "2.0  extra", Kind: Formula},
			},
		},
		{
			// brew list --versions prints every installed version of one keg on a
			// single row; the remainder is kept verbatim rather than guessed at.
			name:   "multiple installed versions stay on the row",
			output: "jq 1.7.1 1.8.0\n",
			kind:   Formula,
			want:   []Package{{Name: "jq", Version: "1.7.1 1.8.0", Kind: Formula}},
		},
		{
			name:   "unicode whitespace and output order",
			output: "\u2003first\u20021.0\u2003beta\r\nsecond 2.0\n",
			kind:   Cask,
			want: []Package{
				{Name: "first", Version: "1.0\u2003beta", Kind: Cask},
				{Name: "second", Version: "2.0", Kind: Cask},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseList(tt.output, tt.kind)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parseList() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestListCommandVectors(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		want [][]string
	}{
		{
			name: "cask",
			kind: Cask,
			want: [][]string{{"list", "--cask", "-1"}},
		},
		{
			name: "formula enumerates every install and then marks dependencies",
			kind: Formula,
			want: [][]string{
				{"list", "--formula", "--versions"},
				{"list", "--formula", "--no-installed-on-request"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "alpha 1.0\n", "", 0, false)
			fakeBrewStdoutByArg(t, map[string]string{"--no-installed-on-request": ""})
			packages, err := New().List(context.Background(), tt.kind)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if want := []Package{{Name: "alpha", Version: "1.0", Kind: tt.kind}}; !slices.Equal(packages, want) {
				t.Fatalf("List() = %#v, want %#v", packages, want)
			}
			// Any order: the formula case's two reads are concurrent.
			assertRecordedArgsAnyOrder(t, argsFile, tt.want...)
		})
	}
}

func TestOutdatedCommandVectors(t *testing.T) {
	// One report carrying both arrays, as brew always prints it; each kind must
	// read only its own array, because a formula and a cask can share a name.
	const report = `{"formulae":[{"name":"vault","installed_versions":["1.16.0","1.17.0"],"current_version":"1.18.1"}],` +
		`"casks":[{"name":"postman","installed_versions":["12.24.4"],"current_version":"12.24.5"}]}`
	tests := []struct {
		name string
		kind Kind
		args []string
		want []OutdatedPackage
	}{
		{name: "cask", kind: Cask, args: []string{"outdated", "--cask", "--json=v2"},
			want: []OutdatedPackage{{Name: "postman", Installed: "12.24.4", Latest: "12.24.5"}}},
		// The formula row also pins that the newest of several installed
		// versions is the one reported, since that is the one an upgrade replaces.
		{name: "formula", kind: Formula, args: []string{"outdated", "--formula", "--json=v2"},
			want: []OutdatedPackage{{Name: "vault", Installed: "1.17.0", Latest: "1.18.1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, report, "", 0, false)
			packages, err := New().Outdated(context.Background(), tt.kind)
			if err != nil {
				t.Fatalf("Outdated() error = %v", err)
			}
			if !slices.Equal(packages, tt.want) {
				t.Fatalf("Outdated() = %#v, want %#v", packages, tt.want)
			}
			assertRecordedArgs(t, argsFile, tt.args)
			// --greedy would mark an auto-updating cask that brew will not upgrade.
			// Read back from the recording, not from tt.args: asserting against the
			// expectation table only fires if someone edits the table, and never
			// observes the vector the code actually built.
			assertArgAbsent(t, argsFile, "--greedy")
		})
	}
}

// Malformed JSON must surface as an error the list load absorbs into an
// unmarked list, and a nameless row must vanish rather than mark by accident.
func TestParseOutdatedDegradations(t *testing.T) {
	if _, err := parseOutdated([]byte("not json"), Formula); err == nil {
		t.Error("parseOutdated() accepted malformed JSON")
	}
	packages, err := parseOutdated([]byte(`{"formulae":[{"name":"","current_version":"1.1"},{"name":"ok","current_version":"2.0"}]}`), Formula)
	if err != nil {
		t.Fatalf("parseOutdated() error = %v", err)
	}
	if want := []OutdatedPackage{{Name: "ok", Latest: "2.0"}}; !slices.Equal(packages, want) {
		t.Fatalf("parseOutdated() = %#v, want %#v", packages, want)
	}
}

func TestOutdatedRefusesAnInvalidKindWithoutStartingBrew(t *testing.T) {
	argsFile := configureFakeBrew(t, "must not run", "", 0, false)
	if _, err := New().Outdated(context.Background(), Kind("other")); err == nil {
		t.Error("Outdated() accepted invalid kind")
	}
	if _, err := os.Stat(argsFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid kind started brew; marker error = %v", err)
	}
}

// The measured machine reports 116 formulae on request and 180 not on request
// out of 304 installed, so eight explicitly requested third-party-tap formulae
// appear under neither filter. Enumerating the unfiltered list and using
// `--no-installed-on-request` only as a marker is what keeps those eight visible.
func TestFormulaListMarksOnlyTheReportedDependencySet(t *testing.T) {
	// The enumeration uses the default stdout; only the marker call is keyed, so
	// the argument selecting it is unique to that invocation.
	argsFile := configureFakeBrew(t, "onrequest\ndependency\ntapinstall\n", "", 0, false)
	fakeBrewStdoutByArg(t, map[string]string{"--no-installed-on-request": "dependency\n"})

	packages, err := New().List(context.Background(), Formula)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []Package{
		{Name: "onrequest", Kind: Formula},
		{Name: "dependency", Kind: Formula, Dependency: true},
		{Name: "tapinstall", Kind: Formula},
	}
	if !slices.Equal(packages, want) {
		t.Fatalf("List() = %#v, want %#v", packages, want)
	}
	assertRecordedArgsAnyOrder(t, argsFile,
		[]string{"list", "--formula", "--versions"},
		[]string{"list", "--formula", "--no-installed-on-request"},
	)
}

// The marker call is load-bearing for every formula list load: its failure fails
// the whole load rather than degrading to an unmarked list, because a silently
// unmarked list would show every dependency as installed on request. Failing only
// that invocation is the point - failing every invocation fails the enumeration
// first and never reaches the marker call at all.
func TestFormulaListFailsWhenTheDependencyMarkerCallFails(t *testing.T) {
	argsFile := configureFakeBrew(t, "onrequest\n", "", 0, false)
	fakeBrewFailByArg(t, "--no-installed-on-request", "Error: invalid option: --no-installed-on-request")

	packages, err := New().List(context.Background(), Formula)
	if err == nil || err.Error() != "Error: invalid option: --no-installed-on-request" {
		t.Fatalf("List() error = %v, want the mapped marker failure", err)
	}
	if packages != nil {
		t.Fatalf("List() returned %#v with a failed marker call, want no packages", packages)
	}
	// Both invocations ran: the enumeration succeeded and the marker call is the
	// one that failed.
	assertRecordedArgsAnyOrder(t, argsFile,
		[]string{"list", "--formula", "--versions"},
		[]string{"list", "--formula", "--no-installed-on-request"})
}

func TestCaskListNeverReportsADependency(t *testing.T) {
	configureFakeBrew(t, "firefox\n", "", 0, false)
	packages, err := New().List(context.Background(), Cask)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(packages) != 1 || packages[0].Dependency {
		t.Fatalf("List() = %#v, want one cask that is not a dependency", packages)
	}
}

func TestInfoCommandVectors(t *testing.T) {
	tests := []struct {
		name string
		pkg  Package
		want []string
	}{
		{
			name: "cask",
			pkg:  Package{Name: "firefox", Kind: Cask},
			want: []string{"info", "--cask", "firefox"},
		},
		{
			name: "formula",
			pkg:  Package{Name: "go", Kind: Formula},
			want: []string{"info", "--formula", "go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "first line\nsecond line\r\n", "", 0, false)
			got, err := New().Info(context.Background(), tt.pkg)
			if err != nil {
				t.Fatalf("Info() error = %v", err)
			}
			if got != "first line\nsecond line" {
				t.Fatalf("Info() = %q", got)
			}
			assertRecordedArgs(t, argsFile, tt.want)
		})
	}
}

func TestListAndInfoResolveBrewFromTheCurrentEnvironment(t *testing.T) {
	client := New()

	listArgsFile := configureFakeBrew(t, "alpha 1.0\n", "", 0, false)
	if _, err := client.List(context.Background(), Cask); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	assertRecordedArgs(t, listArgsFile, []string{"list", "--cask", "-1"})

	infoArgsFile := configureFakeBrew(t, "details\n", "", 0, false)
	if _, err := client.Info(context.Background(), Package{Name: "alpha", Kind: Cask}); err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	assertRecordedArgs(t, infoArgsFile, []string{"info", "--cask", "alpha"})
}

func TestUnsafePackageNamesAreRefusedBeforeInfoOrUninstall(t *testing.T) {
	unsafeNames := []string{"", "-option", "nul\x00byte", "line\nbreak", "tab\tname", "delete\x7f", "control\u0085"}
	for _, name := range unsafeNames {
		t.Run(name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "must not run", "", 0, false)
			_, err := New().Info(context.Background(), Package{Name: name, Kind: Cask})
			if err == nil || err.Error() != "Unsafe package name; info refused" {
				t.Fatalf("Info() error = %v", err)
			}
			if _, statErr := os.Stat(argsFile); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unsafe Info started brew; marker error = %v", statErr)
			}

			t.Setenv("PATH", t.TempDir())
			_, err = PrepareCommand(os.Environ(), Uninstall, Package{Name: name, Kind: Cask})
			if err == nil || err.Error() != "Unsafe package name; uninstall refused" {
				t.Fatalf("PrepareCommand() error = %v", err)
			}
		})
	}
}

func TestSafePackageNameBoundaries(t *testing.T) {
	tests := []struct {
		name string
		safe bool
	}{
		{name: "package", safe: true},
		{name: "a-b", safe: true},
		{name: "ümlaut", safe: true},
		{name: "—not-an-option", safe: true},
		{name: "embedded space", safe: true},
		{name: "\u200d", safe: true},
		{name: "", safe: false},
		{name: "-option", safe: false},
		{name: "a\x00b", safe: false},
		{name: "a\nb", safe: false},
		{name: "a\u0085b", safe: false},
	}
	for _, tt := range tests {
		if got := safePackageName(tt.name); got != tt.safe {
			t.Errorf("safePackageName(%q) = %v, want %v", tt.name, got, tt.safe)
		}
	}
}

func TestInvalidKindStartsNoCommand(t *testing.T) {
	argsFile := configureFakeBrew(t, "must not run", "", 0, false)
	invalid := Kind("other")

	if _, err := New().List(context.Background(), invalid); err == nil {
		t.Error("List() accepted invalid kind")
	}
	if _, err := New().Info(context.Background(), Package{Name: "safe", Kind: invalid}); err == nil {
		t.Error("Info() accepted invalid kind")
	}
	if _, err := PrepareCommand(os.Environ(), Uninstall, Package{Name: "safe", Kind: invalid}); err == nil {
		t.Error("PrepareCommand() accepted invalid kind")
	}
	if _, err := os.Stat(argsFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid kind started brew; marker error = %v", err)
	}
}
