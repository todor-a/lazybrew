package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsGoRun(t *testing.T) {
	if !isGoRun("/var/folders/x/T/go-build123/b001/exe/lazybrew") {
		t.Fatal("go run path not detected")
	}
	if isGoRun("/opt/homebrew/bin/lazybrew") {
		t.Fatal("installed path detected as go run")
	}
}

func TestSetupWritesJSONToFile(t *testing.T) {
	dir := t.TempDir()
	closeLog := Setup(dir)
	slog.Info("probe")
	closeLog()

	raw, err := os.ReadFile(filepath.Join(dir, "lazybrew.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"msg":"lazybrew started"`) || !strings.Contains(string(raw), `"msg":"probe"`) {
		t.Fatalf("log content %q", raw)
	}
}

func TestSetupWithoutDirDiscards(t *testing.T) {
	closeLog := Setup("")
	slog.Info("probe") // must not panic or write anywhere visible
	closeLog()
}
