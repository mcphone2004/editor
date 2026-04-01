package main

import (
	"fmt"
	"os"

	"github.com/anthonybrice/editor/internal/lsp"
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

	if code := run(path, lspSession); code != 0 {
		if lspSession != nil {
			lspSession.Shutdown()
		}
		os.Exit(code)
	}
	if lspSession != nil {
		lspSession.Shutdown()
	}
}

func run(path string, lspSession *lsp.Session) int {
	m, err := ui.New(path, lspSession)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
