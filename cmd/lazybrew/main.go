package main

/*
#include <unistd.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"lazybrew/internal/brew"
	"lazybrew/internal/info"
	"lazybrew/internal/logging"
	"lazybrew/internal/privileged"
	"lazybrew/internal/ui"
)

func main() { os.Exit(run()) }

func run() int {
	if handled, exitCode := privileged.RunHelperFromEnv(); handled {
		return exitCode
	}

	// New remembers this failure and refuses authenticated requests while browsing remains available.
	_ = privileged.DisableCoreDumps()
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		fmt.Fprintln(os.Stderr, "lazybrew requires an interactive terminal")
		return 1
	}

	homebrew := brew.New()
	loader := info.New(info.Details(homebrew.Info, homebrew.Uses))
	runner := privileged.New()
	// A failed lookup disables settings persistence rather than the app.
	settingsDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		settingsDir = filepath.Join(home, "lazybrew")
	}
	closeLog := logging.Setup(settingsDir)
	defer closeLog()
	root, supervisor := ui.New(homebrew, loader, runner, settingsDir)
	program := tea.NewProgram(root, tea.WithoutSignalHandler())

	signals := make(chan os.Signal, 1)
	stopped := make(chan struct{})
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	defer close(stopped)
	go func() {
		select {
		case sig := <-signals:
			code := 130
			if sig == syscall.SIGTERM {
				code = 143
			}
			program.Send(ui.SignalMsg{ExitCode: code})
		case <-stopped:
		}
	}()

	_, runErr := program.Run()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cleanupErr := supervisor.Cleanup(ctx)
	cancel()

	exitCode := supervisor.ExitCode()
	if errors.Is(runErr, tea.ErrInterrupted) && exitCode == 0 {
		exitCode = 130
	}
	if cleanupErr != nil {
		joined := errors.Join(runErr, cleanupErr)
		fmt.Fprintln(os.Stderr, "lazybrew could not start: "+singleLine(joined.Error()))
		return 1
	}
	if exitCode == 130 || exitCode == 143 {
		return exitCode
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "lazybrew could not start: "+singleLine(runErr.Error()))
		return 1
	}
	if exitCode != 0 {
		return exitCode
	}
	return 0
}

func isTerminal(file *os.File) bool { return C.isatty(C.int(file.Fd())) == 1 }

func singleLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	parts := strings.Split(value, "\n")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
