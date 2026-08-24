// Package logging routes the process-wide slog default to a file. A TUI owns
// stdout and stderr, so logs must never reach either stream: when the file
// cannot be opened the logger degrades to io.Discard, never to a terminal
// write that would corrupt the frame.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// maxLogSize is the append ceiling: a file already larger than this at startup
// is truncated instead of appended to. ponytail: startup-only size check, no
// rotation — a boot logs a few KB, so the file needs months to get here.
// Upgrade path is real rotation if the debug volume ever grows.
const maxLogSize = 1 << 20

// Setup opens dir/lazybrew.log as JSON lines and installs it as slog's
// default. Level is debug for a `go run` build and info otherwise. The
// returned function closes the file; it is never nil.
func Setup(dir string) func() {
	level := slog.LevelInfo
	if exe, err := os.Executable(); err == nil && isGoRun(exe) {
		level = slog.LevelDebug
	}
	writer, closeLog := openLog(dir)
	slog.SetDefault(slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})))
	slog.Info("lazybrew started", "level", level.String())
	return closeLog
}

func openLog(dir string) (io.Writer, func()) {
	if dir == "" {
		return io.Discard, func() {}
	}
	if os.MkdirAll(dir, 0o755) != nil {
		return io.Discard, func() {}
	}
	path := filepath.Join(dir, "lazybrew.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return io.Discard, func() {}
	}
	return file, func() { _ = file.Close() }
}

// isGoRun reports whether the executable is a `go run` temp build, which lives
// under a go-build cache directory (…/T/go-build1234/b001/exe/lazybrew).
// ponytail: substring match, so an install path containing "/go-build" also
// reads as dev; the cost is only verbose logs. Tighten to a path-element match
// if that ever happens.
func isGoRun(exePath string) bool {
	return strings.Contains(exePath, string(filepath.Separator)+"go-build")
}
