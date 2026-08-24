package brew

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	helperModeEnv          = "LAZYBREW_TEST_BREW_HELPER"
	helperArgsEnv          = "LAZYBREW_TEST_BREW_ARGS_FILE"
	helperStdoutEnv        = "LAZYBREW_TEST_BREW_STDOUT"
	helperStderrEnv        = "LAZYBREW_TEST_BREW_STDERR"
	helperExitEnv          = "LAZYBREW_TEST_BREW_EXIT"
	helperBlockEnv         = "LAZYBREW_TEST_BREW_BLOCK"
	helperPIDEnv           = "LAZYBREW_TEST_BREW_PID_FILE"
	helperDescendantPIDEnv = "LAZYBREW_TEST_BREW_DESCENDANT_PID_FILE"
	helperDescendantEnv    = "LAZYBREW_TEST_BREW_DESCENDANT"
	helperStdoutByArgEnv   = "LAZYBREW_TEST_BREW_STDOUT_BY_ARG"
	helperFailByArgEnv     = "LAZYBREW_TEST_BREW_FAIL_BY_ARG"
)

const (
	recordSeparator = "\x1e"
	pairSeparator   = "\x1f"
)

// helperStdout lets one test give different output to each brew invocation of a
// single operation, selected by an argument that only that invocation carries.
func helperStdout() string {
	for _, pair := range strings.Split(os.Getenv(helperStdoutByArgEnv), recordSeparator) {
		arg, value, ok := strings.Cut(pair, pairSeparator)
		if ok && slices.Contains(os.Args[1:], arg) {
			return value
		}
	}
	return os.Getenv(helperStdoutEnv)
}

// helperFailure lets one test fail a single brew invocation of a multi-call
// operation, selected by an argument that only that invocation carries. Without
// it, a test can only fail every invocation, which fails the first one and never
// reaches the call it meant to exercise.
func helperFailure() (string, bool) {
	for _, pair := range strings.Split(os.Getenv(helperFailByArgEnv), recordSeparator) {
		arg, stderr, ok := strings.Cut(pair, pairSeparator)
		if ok && slices.Contains(os.Args[1:], arg) {
			return stderr, true
		}
	}
	return "", false
}

func appendRecord(path, record string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(record + recordSeparator); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func TestMain(m *testing.M) {
	if os.Getenv(helperModeEnv) != "1" {
		os.Exit(m.Run())
	}
	if os.Getenv(helperDescendantEnv) == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if path := os.Getenv(helperArgsEnv); path != "" {
		// Appended, one record per invocation, so a single operation that shells out
		// more than once has every one of its argv vectors asserted rather than only
		// its last.
		if err := appendRecord(path, strings.Join(os.Args[1:], "\x00")); err != nil {
			os.Exit(98)
		}
		// Recorded beside the argv so a test can assert what the child actually
		// inherited, not merely what the parent believes it set.
		if err := os.WriteFile(path+".autoupdate", []byte(os.Getenv("HOMEBREW_NO_AUTO_UPDATE")), 0o600); err != nil {
			os.Exit(96)
		}
		// Recorded beside the argv so a test can assert what the child actually
		// inherited, not merely what the parent believes it set.
		if err := os.WriteFile(path+".autoupdate", []byte(os.Getenv("HOMEBREW_NO_AUTO_UPDATE")), 0o600); err != nil {
			os.Exit(96)
		}
	}
	if path := os.Getenv(helperPIDEnv); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(97)
		}
	}
	if path := os.Getenv(helperDescendantPIDEnv); path != "" {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), helperDescendantEnv+"=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(95)
		}
		if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			os.Exit(94)
		}
	}
	if os.Getenv(helperBlockEnv) == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	_, _ = io.WriteString(os.Stdout, helperStdout())
	_, _ = io.WriteString(os.Stderr, os.Getenv(helperStderrEnv))
	if stderr, fail := helperFailure(); fail {
		_, _ = io.WriteString(os.Stderr, stderr)
		os.Exit(1)
	}
	code, err := strconv.Atoi(os.Getenv(helperExitEnv))
	if err != nil {
		os.Exit(96)
	}
	os.Exit(code)
}

func TestPrepareUninstallVectorsAndPATHResolution(t *testing.T) {
	tests := []struct {
		name string
		pkg  Package
		want []string
	}{
		{
			name: "cask",
			pkg:  Package{Name: "firefox", Kind: Cask},
			want: []string{"uninstall", "--cask", "firefox"},
		},
		{
			name: "formula",
			pkg:  Package{Name: "go", Kind: Formula},
			want: []string{"uninstall", "--formula", "go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "", "", 0, false)
			prepared, err := PrepareCommand(os.Environ(), Uninstall, tt.pkg)
			if err != nil {
				t.Fatalf("PrepareCommand() error = %v", err)
			}
			wantPath := strings.TrimSuffix(argsFile, ".args")
			if prepared.Path != wantPath {
				t.Fatalf("Path = %q, want %q", prepared.Path, wantPath)
			}
			if !slices.Equal(prepared.Args, tt.want) {
				t.Fatalf("Args = %#v, want %#v", prepared.Args, tt.want)
			}
			if _, statErr := os.Stat(argsFile); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("PrepareUninstall started brew; marker error = %v", statErr)
			}
		})
	}
}

func TestPrepareUninstallUsesOnlySuppliedPATH(t *testing.T) {
	argsFile := configureFakeBrew(t, "", "", 0, false)
	childPath := strings.TrimSuffix(argsFile, ".args")
	hostPath := t.TempDir()
	t.Setenv("PATH", hostPath)

	resolved, err := PrepareCommand([]string{"PATH=" + filepath.Dir(childPath)}, Uninstall, Package{Name: "safe", Kind: Cask})
	if err != nil {
		t.Fatalf("PrepareCommand() error = %v", err)
	}
	if resolved.Path != childPath {
		t.Fatalf("Path = %q, want child-only path %q", resolved.Path, childPath)
	}
	if got := os.Getenv("PATH"); got != hostPath {
		t.Fatalf("process PATH mutated to %q, want %q", got, hostPath)
	}
}

func TestPrepareUninstallRejectsMissingAndRelativePATH(t *testing.T) {
	_, err := PrepareCommand([]string{"PATH=" + t.TempDir()}, Uninstall, Package{Name: "safe", Kind: Cask})
	if err == nil || err.Error() != missingBrewMessage {
		t.Fatalf("missing PATH error = %v", err)
	}

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(mustExecutable(t), filepath.Join(dir, "brew")); err != nil {
		t.Fatalf("create relative brew: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	_, err = PrepareCommand([]string{"PATH=."}, Uninstall, Package{Name: "safe", Kind: Cask})
	if !errors.Is(err, exec.ErrDot) {
		t.Fatalf("relative PATH error = %v, want exec.ErrDot", err)
	}
}

func TestMissingBrewErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	operations := []struct {
		name string
		run  func() error
	}{
		{
			name: "list",
			run: func() error {
				_, err := New().List(context.Background(), Cask)
				return err
			},
		},
		{
			name: "outdated",
			run: func() error {
				_, err := New().Outdated(context.Background(), Cask)
				return err
			},
		},
		{
			name: "info",
			run: func() error {
				_, err := New().Info(context.Background(), Package{Name: "safe", Kind: Formula})
				return err
			},
		},
		{
			name: "uninstall preparation",
			run: func() error {
				_, err := PrepareCommand(os.Environ(), Uninstall, Package{Name: "safe", Kind: Cask})
				return err
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			err := operation.run()
			if err == nil || err.Error() != missingBrewMessage {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestMapCommandFailure(t *testing.T) {
	exitErr := fakeExitError(t, 7)
	tests := []struct {
		name   string
		err    error
		stdout []byte
		stderr []byte
		want   string
	}{
		{name: "nil", want: ""},
		{name: "missing lookup", err: &exec.Error{Name: "brew", Err: exec.ErrNotFound}, want: missingBrewMessage},
		{name: "missing start", err: &os.PathError{Op: "fork/exec", Path: "/missing/brew", Err: os.ErrNotExist}, want: missingBrewMessage},
		{name: "generic lookup or start", err: errors.New("permission denied"), want: "Could not run brew: permission denied"},
		{name: "stderr wins", err: exitErr, stdout: []byte(" stdout \n"), stderr: []byte(" stderr \n"), want: "stderr"},
		{name: "stdout fallback", err: exitErr, stdout: []byte(" stdout \n"), stderr: []byte(" \n"), want: "stdout"},
		{name: "status fallback", err: exitErr, stdout: []byte(" \n"), stderr: []byte("\t"), want: "brew exited with status 7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapCommandFailure(tt.err, tt.stdout, tt.stderr)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("MapCommandFailure() = %v, want nil", got)
				}
				return
			}
			if got == nil || got.Error() != tt.want {
				t.Fatalf("MapCommandFailure() = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandOutputIsCapturedSeparately(t *testing.T) {
	argsFile := configureFakeBrew(t, "stdout detail\n", "stderr detail\n", 9, false)
	_, err := New().Info(context.Background(), Package{Name: "go", Kind: Formula})
	if err == nil || err.Error() != "stderr detail" {
		t.Fatalf("Info() error = %v", err)
	}
	assertRecordedArgs(t, argsFile, []string{"info", "--formula", "go"})
}

func TestListAndInfoRespectCancellationAndReapTheirChild(t *testing.T) {
	operations := []struct {
		name string
		run  func(context.Context) error
	}{
		{
			name: "list",
			run: func(ctx context.Context) error {
				_, err := New().List(ctx, Cask)
				return err
			},
		},
		{
			name: "info",
			run: func(ctx context.Context) error {
				_, err := New().Info(ctx, Package{Name: "go", Kind: Formula})
				return err
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			configureFakeBrew(t, "", "", 0, true)
			pidFile := os.Getenv(helperPIDEnv)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			result := make(chan error, 1)
			go func() { result <- operation.run(ctx) }()

			pid := waitForHelperPID(t, pidFile)
			cancel()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("canceled command returned nil error")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled command did not return")
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("child %d was not reaped: %v", pid, err)
			}
		})
	}
}

func TestCommandWaitDelayBoundsInheritedDescriptors(t *testing.T) {
	configureFakeBrew(t, "alpha 1.0\n", "", 0, false)
	descendantPIDFile := filepath.Join(t.TempDir(), "descendant.pid")
	t.Setenv(helperDescendantPIDEnv, descendantPIDFile)

	start := time.Now()
	_, err := New().List(context.Background(), Cask)
	elapsed := time.Since(start)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("List() error = %v, want exec.ErrWaitDelay", err)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("List() took %v with inherited descriptors, want at most 4s", elapsed)
	}

	pid := waitForHelperPID(t, descendantPIDFile)
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("kill descriptor-holding descendant: %v", err)
		}
	})
}

func configureFakeBrew(t *testing.T, stdout, stderr string, exitCode int, block bool) string {
	t.Helper()
	dir := t.TempDir()
	brewPath := dir + "/brew"
	executable := mustExecutable(t)
	if err := os.Symlink(executable, brewPath); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	argsFile := brewPath + ".args"
	pidFile := brewPath + ".pid"
	t.Setenv("PATH", dir)
	t.Setenv(helperModeEnv, "1")
	t.Setenv(helperArgsEnv, argsFile)
	t.Setenv(helperPIDEnv, pidFile)
	t.Setenv(helperStdoutEnv, stdout)
	t.Setenv(helperStdoutByArgEnv, "")
	t.Setenv(helperFailByArgEnv, "")
	t.Setenv(helperStderrEnv, stderr)
	t.Setenv(helperExitEnv, strconv.Itoa(exitCode))
	if block {
		t.Setenv(helperBlockEnv, "1")
	} else {
		t.Setenv(helperBlockEnv, "0")
	}
	return argsFile
}

func mustExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	return executable
}

// assertRecordedArgsAnyOrder asserts the complete SET of brew invocations without
// pinning their order. For an operation whose reads run concurrently the order is
// not a guarantee the code makes, so asserting it would pin scheduling rather than
// behaviour and fail at random.
func assertRecordedArgsAnyOrder(t *testing.T, path string, want ...[]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	records := strings.Split(string(data), recordSeparator)
	records = records[:len(records)-1]
	if len(records) != len(want) {
		t.Fatalf("recorded %d brew invocations, want %d: %q", len(records), len(want), records)
	}
	remaining := append([][]string(nil), want...)
	for _, record := range records {
		var got []string
		if record != "" {
			got = strings.Split(record, "\x00")
		}
		at := slices.IndexFunc(remaining, func(candidate []string) bool { return slices.Equal(candidate, got) })
		if at < 0 {
			t.Fatalf("unexpected invocation argv = %#v; still expecting %#v", got, remaining)
		}
		remaining = slices.Delete(remaining, at, at+1)
	}
}

// assertArgAbsent asserts no invocation carried the given argument. Parsed with
// the same record split as assertRecordedArgs, so a separator cannot glue the
// argument onto a neighbouring token and hide it.
func assertArgAbsent(t *testing.T, path, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	for _, record := range strings.Split(string(data), recordSeparator) {
		if slices.Contains(strings.Split(record, "\x00"), unwanted) {
			t.Fatalf("argv contains %q, which it must never carry", unwanted)
		}
	}
}

// assertRecordedArgs asserts the complete sequence of brew invocations: one
// want vector per invocation, in order, and no others.
func assertRecordedArgs(t *testing.T, path string, want ...[]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	records := strings.Split(string(data), recordSeparator)
	records = records[:len(records)-1]
	if len(records) != len(want) {
		t.Fatalf("recorded %d brew invocations, want %d: %q", len(records), len(want), records)
	}
	for i, record := range records {
		var got []string
		if record != "" {
			got = strings.Split(record, "\x00")
		}
		if !slices.Equal(got, want[i]) {
			t.Fatalf("invocation %d argv = %#v, want %#v", i, got, want[i])
		}
	}
}

// fakeBrewFailByArg fails only the invocation carrying the given argument, with
// the given stderr, leaving every other invocation of the same operation to
// succeed.
func fakeBrewFailByArg(t *testing.T, arg, stderr string) {
	t.Helper()
	t.Setenv(helperFailByArgEnv, arg+pairSeparator+stderr)
}

// fakeBrewStdoutByArg maps a distinguishing argument to the stdout the
// invocation carrying it should produce.
func fakeBrewStdoutByArg(t *testing.T, byArg map[string]string) {
	t.Helper()
	pairs := make([]string, 0, len(byArg))
	for arg, value := range byArg {
		pairs = append(pairs, arg+pairSeparator+value)
	}
	t.Setenv(helperStdoutByArgEnv, strings.Join(pairs, recordSeparator))
}

func fakeExitError(t *testing.T, code int) error {
	t.Helper()
	argsFile := configureFakeBrew(t, "", "", code, false)
	path := strings.TrimSuffix(argsFile, ".args")
	cmd := exec.Command(path)
	cmd.Env = os.Environ()
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("helper error = %T %v, want *exec.ExitError", err, err)
	}
	return err
}

func waitForHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(string(data))
			if parseErr != nil {
				t.Fatalf("parse helper PID: %v", parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read helper PID: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("helper did not write PID to %s", path)
	return 0
}

// Homebrew auto-updates on install, outdated, upgrade, bundle, and release unless
// HOMEBREW_NO_AUTO_UPDATE is set, which would run a network fetch that mutates the
// local installation before the requested command, inside the load that gates
// first paint, and would put its stdout report into the buffer this package
// parses. Assert against what the child inherited rather than what run() set.
func TestReadsSuppressHomebrewAutoUpdateInTheChildEnvironment(t *testing.T) {
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"list", func() error { _, err := New().List(context.Background(), Cask); return err }},
		{"info", func() error {
			_, err := New().Info(context.Background(), Package{Name: "go", Kind: Formula})
			return err
		}},
		{"uses", func() error {
			_, err := New().Uses(context.Background(), Package{Name: "go", Kind: Formula})
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := configureFakeBrew(t, "", "", 0, false)
			// Set to the Homebrew default so a pass cannot come from the ambient
			// environment: this machine's shell exports HOMEBREW_NO_AUTO_UPDATE=1,
			// which is exactly how the hazard stayed invisible during development.
			t.Setenv("HOMEBREW_NO_AUTO_UPDATE", "")
			if err := tt.call(); err != nil {
				t.Fatalf("call error = %v", err)
			}
			got, err := os.ReadFile(argsFile + ".autoupdate")
			if err != nil {
				t.Fatalf("child did not record its environment: %v", err)
			}
			if string(got) != "1" {
				t.Fatalf("child HOMEBREW_NO_AUTO_UPDATE = %q, want \"1\"", got)
			}
		})
	}
}

// One argv builder for both privileged verbs. A second builder for either verb
// must never exist, so this pins both vectors and the per-operation refusal.
func TestPrepareCommandBuildsBothVerbsThroughOneSeam(t *testing.T) {
	configureFakeBrew(t, "", "", 0, false)
	for _, tt := range []struct {
		name string
		op   Operation
		pkg  Package
		want []string
	}{
		{"uninstall cask", Uninstall, Package{Name: "alpha", Kind: Cask}, []string{"uninstall", "--cask", "alpha"}},
		{"uninstall formula", Uninstall, Package{Name: "alpha", Kind: Formula}, []string{"uninstall", "--formula", "alpha"}},
		{"upgrade cask", Upgrade, Package{Name: "alpha", Kind: Cask}, []string{"upgrade", "--cask", "alpha"}},
		{"upgrade formula", Upgrade, Package{Name: "alpha", Kind: Formula}, []string{"upgrade", "--formula", "alpha"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prepared, err := PrepareCommand(os.Environ(), tt.op, tt.pkg)
			if err != nil {
				t.Fatalf("PrepareCommand() error = %v", err)
			}
			if !slices.Equal(prepared.Args, tt.want) {
				t.Fatalf("argv = %#v, want %#v", prepared.Args, tt.want)
			}
		})
	}
}

func TestPrepareCommandRefusesPerOperationAndRejectsAnInvalidOne(t *testing.T) {
	configureFakeBrew(t, "", "", 0, false)
	for _, tt := range []struct {
		op   Operation
		want string
	}{
		{Uninstall, "Unsafe package name; uninstall refused"},
		{Upgrade, "Unsafe package name; upgrade refused"},
	} {
		_, err := PrepareCommand(os.Environ(), tt.op, Package{Name: "-rf", Kind: Cask})
		if err == nil || err.Error() != tt.want {
			t.Fatalf("PrepareCommand(%v) error = %v, want %q", tt.op, err, tt.want)
		}
	}
	if _, err := PrepareCommand(os.Environ(), Operation(9), Package{Name: "alpha", Kind: Cask}); err != errInvalidOperation {
		t.Fatalf("PrepareCommand() error = %v, want %v", err, errInvalidOperation)
	}
}
