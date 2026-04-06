package ui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/anthonybrice/editor/telemetry"
	"github.com/anthonybrice/editor/ui"
	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// ansiRE strips all CSI escape sequences (colours, bold, cursor moves, etc.)
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// newModel returns a Model initialised with an 80×24 terminal, no file, no LSP.
func newModel(t *testing.T) tea.Model {
	t.Helper()
	m, err := ui.New("", nil, telemetry.Noop())
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m2
}

// press feeds key events to the model and returns the updated model.
func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		m, _ = m.Update(keyMsg(k))
	}
	return m
}

// keyMsg converts an editor key-string notation to a bubbletea KeyMsg.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "<Esc>":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "<Enter>":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "<Backspace>":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "<Tab>":
		return tea.KeyMsg{Type: tea.KeyTab}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "<C-n>":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "<C-p>":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// viewText returns the ANSI-stripped View() output.
func viewText(m tea.Model) string {
	return stripANSI(m.View())
}

// viewLines returns View() split into lines (ANSI stripped).
func viewLines(m tea.Model) []string {
	return strings.Split(viewText(m), "\n")
}

// contentLines returns only the editor-content lines (not the status/cmd line).
// With height 24, visibleRows = 22, so lines [0..21] are content.
func contentLines(m tea.Model) []string {
	all := viewLines(m)
	if len(all) <= 2 {
		return all
	}
	return all[:len(all)-2]
}

// statusLine returns the status bar line (second-to-last line).
func statusLine(m tea.Model) string {
	all := viewLines(m)
	if len(all) < 2 {
		return ""
	}
	return all[len(all)-2]
}

// cmdLine returns the command/message line (last line).
func cmdLine(m tea.Model) string {
	all := viewLines(m)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// contentHas returns true if any content line contains substr.
func contentHas(m tea.Model, substr string) bool {
	for _, l := range contentLines(m) {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}
