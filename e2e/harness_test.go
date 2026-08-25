//go:build e2e

package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func buildApp(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "lazybrew")
	command := exec.Command("go", "build", "-o", binary, "./cmd/lazybrew")
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build lazybrew: %v\n%s", err, output)
	}
	return binary
}

func runWithoutTerminal(t *testing.T, binary string) (string, int) {
	t.Helper()
	command := exec.Command(binary)
	command.Env = testEnvironment(t.TempDir())
	command.Stdin = strings.NewReader("")
	command.Stdout = io.Discard
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("non-terminal run error = %v, want an exit error", err)
	}
	return stderr.String(), exitErr.ExitCode()
}

type terminalApp struct {
	command  *exec.Cmd
	pid      int
	input    io.WriteCloser
	output   terminalOutput
	done     chan struct{}
	waitErr  error
	waitLock sync.Mutex
}

func startApp(t *testing.T, binary, home string) *terminalApp {
	t.Helper()
	// Each scenario is a fresh process contract. Keep Homebrew's isolated trust
	// config under home, but never let a previous scenario's snapshot answer a
	// formula switch before the real brew read under test.
	if err := os.RemoveAll(filepath.Join(home, "lazybrew")); err != nil {
		t.Fatal(err)
	}
	app := &terminalApp{
		done:   make(chan struct{}),
		output: terminalOutput{changed: make(chan struct{}, 1)},
	}
	pidFile := filepath.Join(t.TempDir(), "lazybrew.pid")
	app.command = exec.Command(
		"/usr/bin/script", "-q", "-e", "/dev/null",
		"/bin/sh", "-c", `stty rows 24 cols 120; echo "$$" > "$2"; exec "$1"`, "sh", binary, pidFile,
	)
	app.command.Env = testEnvironment(home)
	app.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, err := app.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	app.input = input
	app.command.Stdout = &app.output
	app.command.Stderr = &app.output
	if err := app.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		err := app.command.Wait()
		app.waitLock.Lock()
		app.waitErr = err
		app.waitLock.Unlock()
		close(app.done)
	}()
	t.Cleanup(func() {
		select {
		case <-app.done:
		default:
			_ = syscall.Kill(-app.command.Process.Pid, syscall.SIGKILL)
			<-app.done
		}
	})
	app.pid = waitProcessID(t, pidFile)
	return app
}

func waitProcessID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		value, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(value)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("lazybrew pid was not recorded")
	return 0
}

func (a *terminalApp) send(t *testing.T, value string) {
	t.Helper()
	if _, err := io.WriteString(a.input, value); err != nil {
		t.Fatalf("send %q: %v", value, err)
	}
}

func (a *terminalApp) signal(t *testing.T, signal syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(a.pid, signal); err != nil {
		t.Fatalf("signal %v: %v", signal, err)
	}
}

func (a *terminalApp) waitFor(t *testing.T, value string, timeout time.Duration) {
	a.waitForAfter(t, 0, value, timeout)
}

func (a *terminalApp) mark() int { return a.output.len() }

func (a *terminalApp) waitForAfter(t *testing.T, offset int, value string, timeout time.Duration) {
	t.Helper()
	a.waitForAnyAfter(t, offset, timeout, value)
}

func (a *terminalApp) waitForAnyAfter(t *testing.T, offset int, timeout time.Duration, values ...string) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		output := a.output.bytesFrom(offset)
		for _, value := range values {
			if bytes.Contains(output, []byte(value)) {
				return
			}
		}
		select {
		case <-a.output.changed:
		case <-a.done:
			t.Fatalf("lazybrew exited before rendering any of %q\n%s", values, a.output.bytes())
		case <-timer.C:
			t.Fatalf("timed out waiting for any of %q\n%s", values, a.output.bytes())
		}
	}
}

func (a *terminalApp) wait(t *testing.T) {
	a.waitCode(t, 0)
}

func (a *terminalApp) waitCode(t *testing.T, want int) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(20 * time.Second):
		t.Fatalf("lazybrew did not exit\n%s", a.output.bytes())
	}
	a.waitLock.Lock()
	defer a.waitLock.Unlock()
	got := 0
	if a.waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(a.waitErr, &exitErr) {
			t.Fatalf("lazybrew exit: %v\n%s", a.waitErr, a.output.bytes())
		}
		got = exitErr.ExitCode()
	}
	if got != want {
		t.Fatalf("lazybrew exit=%d, want %d\n%s", got, want, a.output.bytes())
	}
}

type terminalOutput struct {
	sync.Mutex
	buffer  bytes.Buffer
	changed chan struct{}
}

func (o *terminalOutput) Write(value []byte) (int, error) {
	o.Lock()
	defer o.Unlock()
	written, err := o.buffer.Write(value)
	select {
	case o.changed <- struct{}{}:
	default:
	}
	return written, err
}

func (o *terminalOutput) bytes() []byte {
	o.Lock()
	defer o.Unlock()
	return bytes.Clone(o.buffer.Bytes())
}

func (o *terminalOutput) bytesFrom(offset int) []byte {
	o.Lock()
	defer o.Unlock()
	if offset > o.buffer.Len() {
		offset = o.buffer.Len()
	}
	return bytes.Clone(o.buffer.Bytes()[offset:])
}

func (o *terminalOutput) len() int {
	o.Lock()
	defer o.Unlock()
	return o.buffer.Len()
}

type brewFixture struct {
	brew    string
	home    string
	tap     string
	tapPath string
	root    string
	dep     string
	cask    string
	url     string
	sha256  string
	caskURL string
	caskSHA string
}

func installBrewFixture(t *testing.T) *brewFixture {
	t.Helper()
	brewPath, err := exec.LookPath("brew")
	if err != nil {
		t.Fatal("real Homebrew is required")
	}
	fixture := &brewFixture{
		brew: brewPath,
		home: t.TempDir(),
		tap:  "lazybrew/e2e",
		root: "lazybrew-e2e-root",
		dep:  "lazybrew-e2e-dep",
		cask: "lazybrew-e2e-cask",
	}
	for _, name := range []string{fixture.root, fixture.dep} {
		if fixture.installed(name) {
			t.Fatalf("refusing to replace pre-existing formula %s", name)
		}
	}
	if fixture.caskInstalled() {
		t.Fatalf("refusing to replace pre-existing cask %s", fixture.cask)
	}
	if output, _ := fixture.run("tap"); slicesContainLine(output, fixture.tap) {
		t.Fatalf("refusing to replace pre-existing tap %s", fixture.tap)
	}

	fixture.mustRun(t, "tap-new", "--no-git", fixture.tap)
	t.Cleanup(func() { fixture.cleanup(t) })
	fixture.tapPath = strings.TrimSpace(fixture.mustRun(t, "--repository", fixture.tap))
	payload := filepath.Join(t.TempDir(), "payload.txt")
	content := []byte("lazybrew e2e fixture\n")
	if err := os.WriteFile(payload, content, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.url = "file://" + payload
	fixture.sha256 = fmt.Sprintf("%x", sha256.Sum256(content))
	caskPayload := filepath.Join(t.TempDir(), fixture.cask)
	caskContent := []byte("#!/bin/sh\necho lazybrew-e2e\n")
	if err := os.WriteFile(caskPayload, caskContent, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.caskURL = "file://" + caskPayload
	fixture.caskSHA = fmt.Sprintf("%x", sha256.Sum256(caskContent))
	fixture.writeFormula(t, fixture.dep, "LazybrewE2eDep", "1.0", "", false)
	fixture.writeFormula(t, fixture.root, "LazybrewE2eRoot", "1.0", fixture.tap+"/"+fixture.dep, false)
	fixture.writeCask(t, "1.0")
	fixture.trustInstalledTaps(t)
	fixture.mustRun(t, "install", "--cask", fixture.tap+"/"+fixture.cask)
	fixture.mustRun(t, "install", fixture.tap+"/"+fixture.root)
	fixture.requireCaskInstalled(t, "1.0")
	fixture.requireInstalled(t, fixture.root, "1.0")
	fixture.requireInstalled(t, fixture.dep, "1.0")
	return fixture
}

func (f *brewFixture) trustInstalledTaps(t *testing.T) {
	t.Helper()
	args := []string{"trust", "--tap"}
	for tap := range strings.Lines(f.mustRun(t, "tap")) {
		args = append(args, strings.TrimSpace(tap))
	}
	f.mustRun(t, args...)
}

func (f *brewFixture) writeCask(t *testing.T, version string) {
	t.Helper()
	cask := fmt.Sprintf(`cask %q do
  version %q
  sha256 %q
  url %q, using: :nounzip
  name "lazybrew black-box cask"
  desc "lazybrew black-box fixture"
  homepage "https://github.com/todor-a/lazybrew"

  binary %q
end
`, f.cask, version, f.caskSHA, f.caskURL, f.cask)
	dir := filepath.Join(f.tapPath, "Casks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, f.cask+".rb"), []byte(cask), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *brewFixture) setCaskVersion(t *testing.T, version string) {
	t.Helper()
	f.writeCask(t, version)
}

func (f *brewFixture) writeFormula(t *testing.T, name, class, version, dependency string, broken bool) {
	t.Helper()
	dependsOn := ""
	if dependency != "" {
		dependsOn = fmt.Sprintf("  depends_on %q\n", dependency)
	}
	install := fmt.Sprintf("    (bin/%q).write \"#!/bin/sh\\\\necho lazybrew-e2e\\\\n\"\n    chmod 0755, bin/%q", name, name)
	if broken {
		install = `    odie "lazybrew e2e intentional failure"`
	}
	formula := fmt.Sprintf(`class %s < Formula
  desc "lazybrew black-box fixture"
  homepage "https://github.com/todor-a/lazybrew"
  url %q, using: :nounzip
  version %q
  sha256 %q
%s
  def install
%s
  end
end
`, class, f.url, version, f.sha256, dependsOn, install)
	path := filepath.Join(f.tapPath, "Formula", name+".rb")
	if err := os.WriteFile(path, []byte(formula), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *brewFixture) setRootVersion(t *testing.T, version string) {
	t.Helper()
	f.writeFormula(t, f.root, "LazybrewE2eRoot", version, f.tap+"/"+f.dep, false)
}

func (f *brewFixture) setBrokenRootVersion(t *testing.T, version string) {
	t.Helper()
	f.writeFormula(t, f.root, "LazybrewE2eRoot", version, f.tap+"/"+f.dep, true)
}

func (f *brewFixture) pinRoot(t *testing.T) {
	t.Helper()
	f.mustRun(t, "pin", f.root)
	output := f.mustRun(t, "list", "--pinned")
	if !slicesContainLine(output, f.root) && !slicesContainLine(output, f.tap+"/"+f.root) {
		t.Fatalf("fixture was not pinned:\n%s", output)
	}
}

func (f *brewFixture) unpinRoot(t *testing.T) {
	t.Helper()
	f.mustRun(t, "unpin", f.root)
}

func (f *brewFixture) untrustFormulae(t *testing.T) {
	t.Helper()
	f.mustRun(t, "untrust", "--tap", f.tap)
	// Keep the unrelated cask scenario unchanged while both formulae exercise
	// the package-level trust path.
	f.mustRun(t, "trust", "--cask", f.tap+"/"+f.cask)
}

func (f *brewFixture) trustDependency(t *testing.T) {
	t.Helper()
	f.mustRun(t, "trust", "--formula", f.tap+"/"+f.dep)
}

func (f *brewFixture) requireFormulaTrust(t *testing.T, name string, want bool) {
	t.Helper()
	var report struct {
		Formulae []string `json:"formulae"`
	}
	if err := json.Unmarshal([]byte(f.mustRun(t, "trust", "--json=v1")), &report); err != nil {
		t.Fatal(err)
	}
	got := false
	for _, formula := range report.Formulae {
		got = got || formula == f.tap+"/"+name
	}
	if got != want {
		t.Fatalf("formula %s trusted=%v, want %v; trust store=%v", name, got, want, report.Formulae)
	}
}

func (f *brewFixture) cleanupOldRoot(t *testing.T) {
	t.Helper()
	f.mustRun(t, "cleanup", "--prune=all", f.root)
	output := strings.TrimSpace(f.mustRun(t, "list", "--formula", "--versions", f.root))
	if output != f.root+" 2.0" {
		t.Fatalf("fixture cleanup left %q, want only 2.0", output)
	}
}

func (f *brewFixture) requireInstalled(t *testing.T, name, version string) {
	t.Helper()
	output := f.mustRun(t, "list", "--formula", "--versions", name)
	fields := strings.Fields(output)
	found := false
	for _, installed := range fields[1:] {
		found = found || installed == version
	}
	if len(fields) == 0 || fields[0] != name || !found {
		t.Fatalf("installed formula = %q, want version %s", strings.TrimSpace(output), version)
	}
}

func (f *brewFixture) requireOnlyInstalled(t *testing.T, name, version string) {
	t.Helper()
	output := strings.TrimSpace(f.mustRun(t, "list", "--formula", "--versions", name))
	if output != name+" "+version {
		t.Fatalf("installed formula = %q, want only version %s", output, version)
	}
}

func (f *brewFixture) requireNotInstalled(t *testing.T, name string) {
	t.Helper()
	if output, err := f.run("list", "--formula", "--versions", name); err == nil {
		t.Fatalf("formula remains installed: %s", strings.TrimSpace(output))
	}
}

func (f *brewFixture) requireCaskInstalled(t *testing.T, version string) {
	t.Helper()
	output := strings.TrimSpace(f.mustRun(t, "list", "--cask", "--versions", f.cask))
	if output != f.cask+" "+version {
		t.Fatalf("installed cask = %q, want %s %s", output, f.cask, version)
	}
}

func (f *brewFixture) requireCaskOutdated(t *testing.T) {
	t.Helper()
	output := f.mustRun(t, "outdated", "--cask", "--json=v2")
	bare := strings.Contains(output, `"name": "`+f.cask+`"`)
	qualified := strings.Contains(output, `"name": "`+f.tap+`/`+f.cask+`"`)
	if !bare && !qualified {
		info, _ := f.run("info", "--cask", "--json=v2", f.cask)
		t.Fatalf("fixture cask is not outdated\noutdated: %s\ninfo: %s", output, info)
	}
}

func (f *brewFixture) requireCaskNotInstalled(t *testing.T) {
	t.Helper()
	if output, err := f.run("list", "--cask", "--versions", f.cask); err == nil {
		t.Fatalf("cask remains installed: %s", strings.TrimSpace(output))
	}
}

func (f *brewFixture) requireOutdated(t *testing.T) {
	t.Helper()
	output := f.mustRun(t, "outdated", "--formula", "--json=v2")
	if !strings.Contains(output, `"name": "`+f.tap+`/`+f.root+`"`) {
		info, _ := f.run("info", "--json=v2", f.root)
		t.Fatalf("fixture is not outdated\noutdated: %s\ninfo: %s", output, info)
	}
}

func (f *brewFixture) requireDependency(t *testing.T) {
	t.Helper()
	output := f.mustRun(t, "list", "--formula", "--no-installed-on-request")
	if !slicesContainLine(output, f.dep) && !slicesContainLine(output, f.tap+"/"+f.dep) {
		info, _ := f.run("info", "--json=v2", f.dep)
		t.Fatalf("fixture is not classified as a dependency\nlist: %s\ninfo: %s", output, info)
	}
}

func (f *brewFixture) installed(name string) bool {
	_, err := f.run("list", "--formula", "--versions", name)
	return err == nil
}

func (f *brewFixture) caskInstalled() bool {
	_, err := f.run("list", "--cask", "--versions", f.cask)
	return err == nil
}

func (f *brewFixture) cleanup(t *testing.T) {
	t.Helper()
	if f.installed(f.root) {
		_, _ = f.run("unpin", f.root)
	}
	if f.caskInstalled() {
		if output, err := f.run("uninstall", "--force", "--cask", f.cask); err != nil {
			t.Errorf("cleanup %s: %v\n%s", f.cask, err, output)
		}
	}
	for _, name := range []string{f.root, f.dep} {
		if f.installed(name) {
			if output, err := f.run("uninstall", "--force", "--formula", name); err != nil {
				t.Errorf("cleanup %s: %v\n%s", name, err, output)
			}
		}
	}
	if output, err := f.run("untap", f.tap); err != nil {
		t.Errorf("cleanup tap: %v\n%s", err, output)
	}
}

func (f *brewFixture) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	output, err := f.run(args...)
	if err != nil {
		t.Fatalf("brew %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func (f *brewFixture) run(args ...string) (string, error) {
	command := exec.Command(f.brew, args...)
	command.Env = testEnvironment(f.home)
	output, err := command.CombinedOutput()
	return string(output), err
}

func testEnvironment(home string) []string {
	return append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"NO_COLOR=1",
		"TERM=xterm-256color",
		"NONINTERACTIVE=1",
		"HOMEBREW_NO_ANALYTICS=1",
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ENV_HINTS=1",
		"HOMEBREW_NO_INSTALL_CLEANUP=1",
	)
}

func slicesContainLine(value, line string) bool {
	for candidate := range strings.Lines(value) {
		if strings.TrimSpace(candidate) == line {
			return true
		}
	}
	return false
}
