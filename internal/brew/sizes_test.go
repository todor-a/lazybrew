package brew

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Real `/usr/bin/du -k -d 1 /opt/homebrew/Cellar /opt/homebrew/Caskroom` rows,
// captured on the measured machine. The full pass emits 345 lines: 304 Cellar
// children, 39 Caskroom children, and one total per root.
const realDuOutput = `276628	/opt/homebrew/Cellar/go
712372	/opt/homebrew/Cellar/qemu
1550732	/opt/homebrew/Cellar/llvm@22
519248	/opt/homebrew/Cellar/vault
9687960	/opt/homebrew/Cellar
16	/opt/homebrew/Caskroom/firefox
48568	/opt/homebrew/Caskroom/karabiner-elements
12	/opt/homebrew/Caskroom/zed
2214836	/opt/homebrew/Caskroom
`

func TestParseSizesReadsRealDuOutput(t *testing.T) {
	sizes := parseSizes(realDuOutput, "/opt/homebrew/Cellar", "/opt/homebrew/Caskroom")

	for name, want := range map[string]int64{
		"llvm@22": 1550732,
		"qemu":    712372,
		"vault":   519248,
		"go":      276628,
	} {
		if got, ok := sizes.KB(Formula, name); !ok || got != want {
			t.Errorf("KB(formula, %q) = %d, %v, want %d", name, got, ok, want)
		}
	}
	for name, want := range map[string]int64{
		"karabiner-elements": 48568,
		"firefox":            16,
		"zed":                12,
	} {
		if got, ok := sizes.KB(Cask, name); !ok || got != want {
			t.Errorf("KB(cask, %q) = %d, %v, want %d", name, got, ok, want)
		}
	}

	// The two root rows are the fleet totals, not packages.
	if want := int64(9687960 + 2214836); sizes.Total != want {
		t.Errorf("Total = %d, want %d", sizes.Total, want)
	}
	if _, ok := sizes.KB(Formula, "Cellar"); ok {
		t.Error("the Cellar root row was read as a package")
	}
	if _, ok := sizes.KB(Cask, "Caskroom"); ok {
		t.Error("the Caskroom root row was read as a package")
	}
	if len(sizes.Formula) != 4 || len(sizes.Cask) != 3 {
		t.Errorf("parsed %d formulae and %d casks, want 4 and 3", len(sizes.Formula), len(sizes.Cask))
	}
}

func TestParseSizesSkipsRowsItCannotRead(t *testing.T) {
	output := "" +
		"100\t/roots/Cellar/kept\n" +
		"no-tab-separator\n" +
		"\t/roots/Cellar/blank-size\n" +
		"notanumber\t/roots/Cellar/unparsable\n" +
		"200\t/roots/Cellar/nested/deeper\n" + // below the measured level
		"300\t/roots/Elsewhere/foreign\n" + // neither root
		"\n" +
		"400\t/roots/Caskroom/app\r\n" +
		"500\t/roots/Cellar\n"

	sizes := parseSizes(output, "/roots/Cellar", "/roots/Caskroom")
	if got := slices.Sorted(maps.Keys(sizes.Formula)); !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("formula keys = %q, want only the readable direct child", got)
	}
	if got, ok := sizes.KB(Cask, "app"); !ok || got != 400 {
		t.Fatalf("KB(cask, app) = %d, %v, want 400 after a CRLF row", got, ok)
	}
	if sizes.Total != 500 {
		t.Fatalf("Total = %d, want only the one root row present", sizes.Total)
	}
}

// writeRoots builds a Homebrew-shaped pair of roots and points the fake brew's
// `--cellar` and `--caskroom` output at them.
func writeRoots(t *testing.T, children map[string]int) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	cellar := filepath.Join(base, "Cellar")
	caskroom := filepath.Join(base, "Caskroom")
	for path, kilobytes := range children {
		full := filepath.Join(base, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(full, make([]byte, kilobytes*1024), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	argsFile := configureFakeBrew(t, "", "", 0, false)
	fakeBrewStdoutByArg(t, map[string]string{
		"--cellar":   cellar + "\n",
		"--caskroom": caskroom + "\n",
	})
	return cellar, caskroom, argsFile
}

// This runs the real /usr/bin/du, so it pins the actual argv behaviour rather
// than a recorded string: `-k` fixes the unit at kilobytes and `-d 1` reports
// each package once, with its whole subtree summed and no grandchild rows.
func TestSizesMeasuresBothRootsInOneRealDuPass(t *testing.T) {
	_, _, argsFile := writeRoots(t, map[string]int{
		"Cellar/big/1.2.3/payload": 512,
		"Cellar/small/0.1/payload": 4,
		"Caskroom/app/9.9/payload": 64,
	})

	sizes, err := New().Sizes(context.Background())
	if err != nil {
		t.Fatalf("Sizes() error = %v", err)
	}

	if got := slices.Sorted(maps.Keys(sizes.Formula)); !slices.Equal(got, []string{"big", "small"}) {
		t.Fatalf("formula keys = %q, want the two Cellar children only", got)
	}
	if got := slices.Sorted(maps.Keys(sizes.Cask)); !slices.Equal(got, []string{"app"}) {
		t.Fatalf("cask keys = %q, want the one Caskroom child", got)
	}

	big, _ := sizes.KB(Formula, "big")
	small, _ := sizes.KB(Formula, "small")
	app, _ := sizes.KB(Cask, "app")
	// A 512 KB payload one level below the package directory: reported in KB
	// (not 512-byte blocks, which would be ~1024, nor MB, which would be 1) and
	// summed through the nested version directory.
	if big < 512 || big > 700 {
		t.Fatalf("big = %d KB, want the nested 512 KB payload measured in kilobytes", big)
	}
	if big <= small || small < 4 {
		t.Fatalf("big=%d small=%d, want the larger package to measure larger", big, small)
	}
	if app < 64 {
		t.Fatalf("app = %d KB, want at least its 64 KB payload", app)
	}
	if sizes.Total < big+small+app {
		t.Fatalf("Total = %d, want at least the sum of both roots' children", sizes.Total)
	}

	assertRecordedArgs(t, argsFile, []string{"--cellar"}, []string{"--caskroom"})
}

func TestSizesKeepsPartialMeasurementWhenOneRootIsMissing(t *testing.T) {
	cellar, _, _ := writeRoots(t, map[string]int{"Cellar/kept/1.0/payload": 8})
	if err := os.RemoveAll(filepath.Join(filepath.Dir(cellar), "Caskroom")); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}

	// du exits nonzero for the missing root while still measuring the other one.
	sizes, err := New().Sizes(context.Background())
	if err != nil {
		t.Fatalf("Sizes() error = %v, want a partial measurement", err)
	}
	if _, ok := sizes.KB(Formula, "kept"); !ok {
		t.Fatalf("Formula = %#v, want the readable root measured", sizes.Formula)
	}
	if len(sizes.Cask) != 0 {
		t.Fatalf("Cask = %#v, want nothing measured for the missing root", sizes.Cask)
	}
}

func TestSizesReportsFailureWhenNothingCouldBeMeasured(t *testing.T) {
	base := t.TempDir()
	argsFile := configureFakeBrew(t, "", "", 0, false)
	fakeBrewStdoutByArg(t, map[string]string{
		"--cellar":   filepath.Join(base, "gone-Cellar") + "\n",
		"--caskroom": filepath.Join(base, "gone-Caskroom") + "\n",
	})

	sizes, err := New().Sizes(context.Background())
	if err == nil {
		t.Fatalf("Sizes() = %#v, want an error when both roots are unreadable", sizes)
	}
	if sizes.Total != 0 || len(sizes.Formula) != 0 || len(sizes.Cask) != 0 {
		t.Fatalf("Sizes() = %#v, want the zero value beside its error", sizes)
	}
	assertRecordedArgs(t, argsFile, []string{"--cellar"}, []string{"--caskroom"})
}

func TestSizesRejectsAnEmptyRoot(t *testing.T) {
	configureFakeBrew(t, "\n", "", 0, false)
	if _, err := New().Sizes(context.Background()); err != errMissingRoot {
		t.Fatalf("Sizes() error = %v, want %v", err, errMissingRoot)
	}
}

func TestSizesReportsAMissingBrew(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := New().Sizes(context.Background()); err == nil || err.Error() != missingBrewMessage {
		t.Fatalf("Sizes() error = %v, want %q", err, missingBrewMessage)
	}
}

func TestSizesKBIgnoresAnInvalidKind(t *testing.T) {
	sizes := Sizes{Formula: map[string]int64{"go": 1}, Cask: map[string]int64{"firefox": 2}}
	if _, ok := sizes.KB(Kind("other"), "go"); ok {
		t.Error("KB() reported a size for an invalid kind")
	}
	if _, ok := sizes.KB(Formula, "absent"); ok {
		t.Error("KB() reported a size for an unmeasured package")
	}
}
