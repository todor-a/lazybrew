package brew

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const missingBrewMessage = "Homebrew is not installed or brew is not on PATH"

// noAutoUpdate suppresses Homebrew's own auto-update for every read this package
// makes. install, outdated, upgrade, bundle, and release are Homebrew's
// AUTO_UPDATE_COMMANDS, so with HOMEBREW_NO_AUTO_UPDATE unset - the default - the
// first such call in each HOMEBREW_AUTO_UPDATE_SECS window runs
// `brew update --auto-update` first: a network fetch that mutates the local
// Homebrew installation, and only then execs the command that was asked for.
//
// Two things follow, and both are unacceptable here. These reads run inside the
// load that gates first paint, so a launch a day after the last fetch would sit
// on "Loading casks..." for as long as the network takes. And the auto-update
// report goes to stdout in the same process, so it lands in the buffer this
// package parses, where a deleted-formula line - which by construction names an
// installed package - would be read back as inventory.
//
// This application inspects, and uninstalls only on explicit confirmation.
// Updating the user's Homebrew as a side effect of drawing a list is outside what
// it promises, so this applies to every read rather than only to outdated.
const noAutoUpdate = "HOMEBREW_NO_AUTO_UPDATE=1"

var errUnsafeUninstall = errors.New("Unsafe package name; uninstall refused")

// ResolvedCommand is the resolved, exact command boundary used by uninstall.
type ResolvedCommand struct {
	Path string
	Args []string
}

// PrepareUninstall validates a confirmed package and resolves its exact command.
func PrepareUninstall(env []string, pkg Package) (ResolvedCommand, error) {
	if !safePackageName(pkg.Name) {
		return ResolvedCommand{}, errUnsafeUninstall
	}
	flag, err := kindFlag(pkg.Kind)
	if err != nil {
		return ResolvedCommand{}, err
	}
	path, err := resolveBrew(env)
	if err != nil {
		return ResolvedCommand{}, MapCommandFailure(err, nil, nil)
	}
	return ResolvedCommand{
		Path: path,
		Args: []string{"uninstall", flag, pkg.Name},
	}, nil
}

func run(ctx context.Context, args []string) ([]byte, []byte, error) {
	env := os.Environ()
	path, err := resolveBrew(env)
	if err != nil {
		return nil, nil, MapCommandFailure(err, nil, nil)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(env, noAutoUpdate)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), MapCommandFailure(err, stdout.Bytes(), stderr.Bytes())
}

func resolveBrew(env []string) (string, error) {
	var pathEnv string
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && key == "PATH" {
			pathEnv = value
		}
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		path := filepath.Join(dir, "brew")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if !filepath.IsAbs(path) {
			return "", &exec.Error{Name: "brew", Err: exec.ErrDot}
		}
		return path, nil
	}
	return "", &exec.Error{Name: "brew", Err: exec.ErrNotFound}
}

// MapCommandFailure converts command lookup, start, and exit errors to user text.
func MapCommandFailure(runErr error, stdout, stderr []byte) error {
	if runErr == nil {
		return nil
	}
	if errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, fs.ErrNotExist) {
		return errors.New(missingBrewMessage)
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if text := strings.TrimSpace(string(stderr)); text != "" {
			return errors.New(text)
		}
		if text := strings.TrimSpace(string(stdout)); text != "" {
			return errors.New(text)
		}
		return fmt.Errorf("brew exited with status %d", exitErr.ExitCode())
	}
	return fmt.Errorf("Could not run brew: %w", runErr)
}
