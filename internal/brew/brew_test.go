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
		want []string
	}{
		{name: "cask", kind: Cask, want: []string{"list", "--cask", "-1"}},
		{name: "formula", kind: Formula, want: []string{"list", "--formula", "--installed-on-request"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "alpha 1.0\n", "", 0, false)
			packages, err := New().List(context.Background(), tt.kind)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if want := []Package{{Name: "alpha", Version: "1.0", Kind: tt.kind}}; !slices.Equal(packages, want) {
				t.Fatalf("List() = %#v, want %#v", packages, want)
			}
			assertRecordedArgs(t, argsFile, tt.want)
		})
	}
}

func TestOutdatedCommandVectors(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		want []string
	}{
		{name: "cask", kind: Cask, want: []string{"outdated", "--cask", "--quiet"}},
		{name: "formula", kind: Formula, want: []string{"outdated", "--formula", "--quiet"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "postman\n\nvault\n", "", 0, false)
			names, err := New().Outdated(context.Background(), tt.kind)
			if err != nil {
				t.Fatalf("Outdated() error = %v", err)
			}
			if want := []string{"postman", "vault"}; !slices.Equal(names, want) {
				t.Fatalf("Outdated() = %#v, want %#v", names, want)
			}
			assertRecordedArgs(t, argsFile, tt.want)
			// --greedy would mark an auto-updating cask that brew will not upgrade.
			// Read back from the recording, not from tt.want: asserting against the
			// expectation table only fires if someone edits the table, and never
			// observes the vector the code actually built.
			assertArgAbsent(t, argsFile, "--greedy")
		})
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
			_, err = PrepareUninstall(os.Environ(), Package{Name: name, Kind: Cask})
			if err == nil || err.Error() != "Unsafe package name; uninstall refused" {
				t.Fatalf("PrepareUninstall() error = %v", err)
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
	if _, err := PrepareUninstall(os.Environ(), Package{Name: "safe", Kind: invalid}); err == nil {
		t.Error("PrepareUninstall() accepted invalid kind")
	}
	if _, err := os.Stat(argsFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid kind started brew; marker error = %v", err)
	}
}
