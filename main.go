package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anthonybrice/editor/internal/lsp"
	"github.com/anthonybrice/editor/internal/telemetry"
	"github.com/anthonybrice/editor/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var path string
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	// Create telemetry first so startup/gopls events are captured even if we crash.
	tel := telemetry.New()

	// Start gopls only if a go.mod can be found from the file's directory.
	var lspSession *lsp.Session
	if rootDir, ok := findModuleRoot(path); ok {
		tel.CommandRun("gopls:start")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		sess, err := lsp.StartGopls(ctx, rootDir)
		cancel()
		if err != nil {
			tel.Error("gopls start", err)
			fmt.Fprintf(os.Stderr, "note: gopls unavailable (%v) — LSP features disabled\n", err)
		} else {
			tel.CommandRun("gopls:ready")
			lspSession = sess
		}
	}

	code := run(path, lspSession, tel)
	tel.Close()
	if lspSession != nil {
		lspSession.Shutdown(context.Background())
	}
	if code != 0 {
		os.Exit(code)
	}
}

// findModuleRoot walks up from path's directory looking for a go.mod file.
// Returns the directory containing go.mod and true if found, otherwise ("", false).
func findModuleRoot(path string) (string, bool) {
	dir := path
	if path == "" {
		dir = "."
	}
	info, _ := os.Stat(path) //nolint:gosec // user-supplied CLI path
	if info != nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil { //nolint:gosec // walking up from user-supplied path by design
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func run(path string, lspSession *lsp.Session, tel telemetry.Telemetry) int {
	info, _ := os.Stat(path) //nolint:gosec // path comes from CLI argument, user-controlled by design
	sizeBytes := int64(0)
	if info != nil {
		sizeBytes = info.Size()
	}

	m, err := ui.New(path, lspSession, tel)
	if err != nil {
		tel.Error("open file", err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	tel.Startup(path, sizeBytes, m.LineCount())

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
