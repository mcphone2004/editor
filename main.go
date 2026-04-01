package main

import (
	"fmt"
	"os"

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

	// Start gopls if it's available. Non-fatal if not found.
	var lspSession *lsp.Session
	rootDir := "."
	if path != "" {
		rootDir = path
	}
	sess, err := lsp.StartGopls(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: gopls unavailable (%v) — LSP features disabled\n", err)
	} else {
		lspSession = sess
	}

	tel := telemetry.New()

	code := run(path, lspSession, tel)
	tel.Close()
	if lspSession != nil {
		lspSession.Shutdown()
	}
	if code != 0 {
		os.Exit(code)
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
