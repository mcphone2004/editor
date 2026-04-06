// Package ui implements the bubbletea TUI model for the editor.
//
// It owns the screen layout and bridges bubbletea key events to the editor
// engine (internal/editor).  The LSP session runs in the background; results
// arrive as tea.Msg values via tea.Cmd channels.
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/anthonybrice/editor/buffer"
	"github.com/anthonybrice/editor/editor"
	"github.com/anthonybrice/editor/layout"
	"github.com/anthonybrice/editor/lsp"
	"github.com/anthonybrice/editor/telemetry"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Styles ---

//nolint:gochecknoglobals // lipgloss styles are immutable after init, equivalent to constants
var (
	styleLineNum = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Width(5).
			Align(lipgloss.Right)

	styleStatusNormal = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230")).
				Bold(true).
				Padding(0, 1)

	styleStatusInsert = lipgloss.NewStyle().
				Background(lipgloss.Color("2")).
				Foreground(lipgloss.Color("230")).
				Bold(true).
				Padding(0, 1)

	styleStatusVisual = lipgloss.NewStyle().
				Background(lipgloss.Color("3")).
				Foreground(lipgloss.Color("0")).
				Bold(true).
				Padding(0, 1)

	styleStatusCommand = lipgloss.NewStyle().
				Background(lipgloss.Color("5")).
				Foreground(lipgloss.Color("230")).
				Bold(true).
				Padding(0, 1)

	styleCursor = lipgloss.NewStyle().
			Background(lipgloss.Color("212")).
			Foreground(lipgloss.Color("0"))

	styleVisualHL = lipgloss.NewStyle().
			Background(lipgloss.Color("240")).
			Foreground(lipgloss.Color("255"))

	styleError = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1"))

	styleWarning = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3"))

	styleCompletionItem = lipgloss.NewStyle().
				Background(lipgloss.Color("235")).
				Foreground(lipgloss.Color("252")).
				Padding(0, 1)

	styleCompletionSel = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("255")).
				Padding(0, 1)

	styleStatusLSPOn = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Bold(true)

	styleStatusLSPOff = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

	styleStatusInactive = lipgloss.NewStyle().
				Background(lipgloss.Color("240")).
				Foreground(lipgloss.Color("252")).
				Padding(0, 1)
)

// --- Message types for async operations ---

type msgLSPExited struct{}

type msgDiagnostics struct {
	path  string
	diags []editor.Diagnostic
}

type msgHover struct{ text string }
type msgCompletion struct{ items []lsp.CompletionItem }
type msgVetDiags struct{ diags []editor.Diagnostic }
type msgQuit struct{}

// --- Model ---

// Model is the top-level bubbletea model.
type Model struct {
	root    *layout.Node
	focused *winPane
	lsp     lsp.Session // may be nil
	tel     telemetry.Telemetry
	width   int
	height  int
}

// New creates a Model. Opens the file at path (empty = new buffer).
// lspSession may be nil to disable LSP features.
// tel may be nil; pass telemetry.Noop() or telemetry.New() from the caller.
func New(path string, lspSession lsp.Session, tel telemetry.Telemetry) (*Model, error) {
	var buf buffer.Buffer
	var err error
	if path != "" {
		buf, err = buffer.Open(path)
	} else {
		buf = buffer.New()
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if tel == nil {
		tel = telemetry.Noop()
	}
	p := &winPane{ed: editor.New(buf)}
	root := layout.NewLeaf(p)
	return &Model{root: root, focused: p, lsp: lspSession, tel: tel}, nil
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.lsp != nil {
		cmds = append(cmds, m.listenLSPExit())
		if m.focused.ed.Buf().Path() != "" {
			cmds = append(cmds, m.openDoc(), m.listenNotifications())
		}
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		layout.AssignBounds(m.root, 0, 0, m.width, m.height-1)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case msgLSPExited:
		m.lsp = nil

	case msgDiagnostics:
		m.mergeDiagnostics(msg.path, msg.diags)

	case msgHover:
		m.focused.hoverText = msg.text

	case msgCompletion:
		m.focused.completions = msg.items
		m.focused.compIdx = 0
		m.focused.compVisible = len(msg.items) > 0

	case msgVetDiags:
		m.focused.vetDiags = msg.diags
		m.mergeDiagnostics("", nil) // re-merge

	case msgQuit:
		return m, tea.Quit
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := keyString(msg)
	p := m.focused
	prevMode := p.ed.Mode()

	// Completion navigation while popup is visible.
	if p.compVisible {
		switch key {
		case "<C-n>", "<Tab>":
			p.compIdx = (p.compIdx + 1) % len(p.completions)
			return m, nil
		case "<C-p>":
			p.compIdx = (p.compIdx - 1 + len(p.completions)) % len(p.completions)
			return m, nil
		case "<Enter>":
			m.applyCompletion()
			return m, nil
		case "<Esc>":
			p.compVisible = false
			return m, nil
		}
	}

	// Activate gap buffer when entering insert mode.
	if prevMode == editor.ModeNormal && key == "i" || key == "a" || key == "o" || key == "O" {
		cur := p.ed.Cursor()
		p.ed.Buf().SetCursorHint(cur.Row, cur.Col)
		p.ed.Buf().ActivateGap(cur.Row, cur.Col)
	}

	p.ed.HandleKey(key)

	// Clear hover when cursor moves.
	p.hoverText = ""

	var cmds []tea.Cmd

	// Handle signals encoded in StatusMsg.
	status := p.ed.StatusMsg()
	switch {
	case status == "quit" || status == "quit!":
		if status == "quit!" || !p.ed.Buf().Modified() {
			return m, tea.Quit
		}
	case strings.HasPrefix(status, "open:"):
		path := strings.TrimPrefix(status, "open:")
		cmds = append(cmds, m.openFile(path))
	case status == "lsp:gd":
		cmds = append(cmds, m.gotoDefinition())
	case status == "lsp:hover":
		cmds = append(cmds, m.hover())
	case status == "lsp:complete":
		cmds = append(cmds, m.complete())
	}

	// Flush gap buffer when leaving insert mode.
	if prevMode == editor.ModeInsert && p.ed.Mode() != editor.ModeInsert {
		cur := p.ed.Cursor()
		p.ed.Buf().SetCursorHint(cur.Row, cur.Col)
		p.ed.Buf().FlushGap()
		p.compVisible = false
		// Notify LSP of the change and run go vet.
		if m.lsp != nil && p.ed.Buf().Path() != "" {
			cmds = append(cmds, m.didChange(), m.runVet())
		}
	}

	// Save: run vet + notify LSP.
	if strings.HasPrefix(status, "written:") {
		if m.lsp != nil && p.ed.Buf().Path() != "" {
			cmds = append(cmds, m.didSave(), m.runVet())
		}
	}

	p.scrollToCursor()
	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	p := m.focused
	var sb strings.Builder

	// Render the layout tree (all pane content + per-pane status bars).
	sb.WriteString(renderNode(m.root, p, m.lsp))

	// Shared command line / message line.
	sb.WriteString("\n")
	switch p.ed.Mode() {
	case editor.ModeCommand:
		sb.WriteString(string(p.ed.CmdMode()) + p.ed.CmdBuf())
	case editor.ModeNormal, editor.ModeInsert, editor.ModeVisual, editor.ModeVisualLine:
		msg := p.ed.StatusMsg()
		if msg != "" &&
			msg != "quit" &&
			msg != "quit!" &&
			!strings.HasPrefix(msg, "lsp:") &&
			!strings.HasPrefix(msg, "open:") {
			sb.WriteString(msg)
		}
	}

	// Hover popup.
	if p.hoverText != "" {
		sb.WriteString("\n" + p.hoverText)
	}

	// Completion popup.
	if p.compVisible && len(p.completions) > 0 {
		sb.WriteString(m.renderCompletion())
	}

	return sb.String()
}

func (m *Model) renderCompletion() string {
	p := m.focused
	var sb strings.Builder
	maxItems := 8
	if len(p.completions) < maxItems {
		maxItems = len(p.completions)
	}
	for i := 0; i < maxItems; i++ {
		label := p.completions[i].Label
		if len(label) > 30 {
			label = label[:30]
		}
		if i == p.compIdx {
			sb.WriteString("\n" + styleCompletionSel.Render(label))
		} else {
			sb.WriteString("\n" + styleCompletionItem.Render(label))
		}
	}
	return sb.String()
}

// --- LSP commands ---

func (m *Model) listenLSPExit() tea.Cmd {
	exited := m.lsp.Exited()
	return func() tea.Msg {
		<-exited
		return msgLSPExited{}
	}
}

func (m *Model) openDoc() tea.Cmd {
	buf := m.focused.ed.Buf()
	return func() tea.Msg {
		_ = m.lsp.DidOpen(context.Background(), buf.Path(), buf.String())
		return nil
	}
}

func (m *Model) didChange() tea.Cmd {
	buf := m.focused.ed.Buf()
	text := buf.String()
	return func() tea.Msg {
		_ = m.lsp.DidChange(context.Background(), buf.Path(), text)
		return nil
	}
}

func (m *Model) didSave() tea.Cmd {
	buf := m.focused.ed.Buf()
	return func() tea.Msg {
		_ = m.lsp.DidSave(context.Background(), buf.Path())
		return nil
	}
}

func (m *Model) gotoDefinition() tea.Cmd {
	cur := m.focused.ed.Cursor()
	path := m.focused.ed.Buf().Path()
	return func() tea.Msg {
		locs, err := m.lsp.Definition(context.Background(), path, cur.Row, cur.Col)
		if err != nil || len(locs) == 0 {
			return msgHover{text: fmt.Sprintf("definition: %v", err)}
		}
		// Signal the UI to open the target file at the target position.
		// For now surface as a hover message; a full implementation would
		// open a new buffer.
		loc := locs[0]
		return msgHover{text: fmt.Sprintf("-> %s:%d:%d",
			lsp.URIToPath(loc.URI),
			loc.Range.Start.Line+1,
			loc.Range.Start.Character+1,
		)}
	}
}

func (m *Model) hover() tea.Cmd {
	cur := m.focused.ed.Cursor()
	path := m.focused.ed.Buf().Path()
	return func() tea.Msg {
		text, err := m.lsp.Hover(context.Background(), path, cur.Row, cur.Col)
		if err != nil {
			return msgHover{text: fmt.Sprintf("hover error: %v", err)}
		}
		return msgHover{text: text}
	}
}

func (m *Model) complete() tea.Cmd {
	cur := m.focused.ed.Cursor()
	path := m.focused.ed.Buf().Path()
	return func() tea.Msg {
		items, err := m.lsp.Completion(context.Background(), path, cur.Row, cur.Col)
		if err != nil || len(items) == 0 {
			return msgCompletion{}
		}
		return msgCompletion{items: items}
	}
}

func (m *Model) listenNotifications() tea.Cmd {
	ch := m.lsp.Notifications()
	return func() tea.Msg {
		for n := range ch {
			if n.Method != "textDocument/publishDiagnostics" {
				continue
			}
			p, err := lsp.ParseDiagnostics(n)
			if err != nil {
				continue
			}
			diags := make([]editor.Diagnostic, 0, len(p.Diagnostics))
			for _, d := range p.Diagnostics {
				diags = append(diags, editor.Diagnostic{
					Row:      d.Range.Start.Line,
					Col:      d.Range.Start.Character,
					Severity: d.Severity,
					Message:  d.Message,
					Source:   d.Source,
				})
			}
			return msgDiagnostics{path: lsp.URIToPath(p.URI), diags: diags}
		}
		return nil
	}
}

// --- go vet ---

func (m *Model) runVet() tea.Cmd {
	path := m.focused.ed.Buf().Path()
	if path == "" {
		return nil
	}
	tel := m.tel
	return func() tea.Msg {
		tel.VetStart(".")
		start := time.Now()
		out, err := exec.CommandContext(context.Background(), "go", "vet", "./...").
			CombinedOutput()
		durationMS := time.Since(start).Milliseconds()
		exitCode := 0
		if err != nil {
			exitCode = 1
			tel.VetEnd(".", durationMS, exitCode, string(out))
			diags := parseVetOutput(string(out), path)
			return msgVetDiags{diags: diags}
		}
		tel.VetEnd(".", durationMS, exitCode, "")
		return msgVetDiags{}
	}
}

func parseVetOutput(output, _ string) []editor.Diagnostic {
	var diags []editor.Diagnostic
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: path/file.go:line:col: message
		var file string
		var row, col int
		var msg string
		if n, _ := fmt.Sscanf(line, "%s", &file); n == 0 {
			continue
		}
		rest := strings.SplitN(line, ":", 4)
		if len(rest) < 3 {
			continue
		}
		_, _ = fmt.Sscanf(rest[1], "%d", &row)
		_, _ = fmt.Sscanf(rest[2], "%d", &col)
		if len(rest) == 4 {
			msg = strings.TrimSpace(rest[3])
		}
		diags = append(diags, editor.Diagnostic{
			Row:      row - 1,
			Col:      col - 1,
			Severity: lsp.SeverityWarning,
			Message:  msg,
			Source:   "go vet",
		})
	}
	return diags
}

// --- helpers ---

func (m *Model) openFile(path string) tea.Cmd {
	return func() tea.Msg {
		buf, err := buffer.Open(path)
		if err != nil {
			return msgHover{text: fmt.Sprintf("open error: %v", err)}
		}
		m.focused.ed = editor.New(buf)
		m.focused.scroll = 0
		if m.lsp != nil {
			_ = m.lsp.DidOpen(context.Background(), path, buf.String())
		}
		return nil
	}
}

func (m *Model) applyCompletion() {
	p := m.focused
	if !p.compVisible || p.compIdx >= len(p.completions) {
		return
	}
	item := p.completions[p.compIdx]
	text := item.InsertText
	if text == "" {
		text = item.Label
	}
	cur := p.ed.Cursor()
	p.ed.Buf().InsertString(cur.Row, cur.Col, text)
	p.compVisible = false
}

func (m *Model) mergeDiagnostics(path string, lspDiags []editor.Diagnostic) {
	// Replace LSP diags for the given path and merge vet diags.
	m.focused.ed.SetDiagnostics(append(lspDiags, m.focused.vetDiags...))
	_ = path
}

func (m Model) String() string {
	return m.focused.ed.Buf().String()
}

// LineCount returns the number of lines in the buffer.
func (m *Model) LineCount() int {
	return m.focused.ed.Buf().LineCount()
}

func inVisualRange(row, col int, start, end editor.Pos, linewise bool) bool {
	if linewise {
		return row >= start.Row && row <= end.Row
	}
	if row < start.Row || row > end.Row {
		return false
	}
	if row == start.Row && col < start.Col {
		return false
	}
	if row == end.Row && col > end.Col {
		return false
	}
	return true
}

// keyString converts a bubbletea KeyMsg to the string notation the editor
// engine uses: printable runes pass through as-is; special keys become
// "<Name>" strings.
func keyString(msg tea.KeyMsg) string {
	switch msg.Type { //nolint:exhaustive // only handles the subset of keys the editor uses
	case tea.KeyRunes:
		return string(msg.Runes)
	case tea.KeyEnter:
		return "<Enter>"
	case tea.KeyBackspace:
		return "<Backspace>"
	case tea.KeyDelete:
		return "<Delete>"
	case tea.KeyEscape:
		return "<Esc>"
	case tea.KeyTab:
		return "<Tab>"
	case tea.KeySpace:
		return " "
	case tea.KeyUp:
		return "<Up>"
	case tea.KeyDown:
		return "<Down>"
	case tea.KeyLeft:
		return "<Left>"
	case tea.KeyRight:
		return "<Right>"
	case tea.KeyHome:
		return "<Home>"
	case tea.KeyEnd:
		return "<End>"
	case tea.KeyPgUp:
		return "<PageUp>"
	case tea.KeyPgDown:
		return "<PageDown>"
	case tea.KeyCtrlC:
		return "<C-c>"
	case tea.KeyCtrlD:
		return "<C-d>"
	case tea.KeyCtrlF:
		return "<C-f>"
	case tea.KeyCtrlN:
		return "<C-n>"
	case tea.KeyCtrlP:
		return "<C-p>"
	case tea.KeyCtrlR:
		return "<C-r>"
	case tea.KeyCtrlU:
		return "<C-u>"
	case tea.KeyCtrlB:
		return "<C-b>"
	case tea.KeyF5:
		return "<F5>"
	default:
		if msg.Alt {
			return "<A-" + string(msg.Runes) + ">"
		}
		return string(msg.Runes)
	}
}
