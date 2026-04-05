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
	"unicode/utf8"

	"github.com/anthonybrice/editor/internal/buffer"
	"github.com/anthonybrice/editor/internal/editor"
	"github.com/anthonybrice/editor/internal/lsp"
	"github.com/anthonybrice/editor/internal/telemetry"
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
)

// --- Message types for async operations ---

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
	ed     *editor.Editor
	buf    *buffer.Buffer
	lsp    *lsp.Session // may be nil
	tel    telemetry.Telemetry
	width  int
	height int
	scroll int // first visible line

	// Completion popup state.
	completions []lsp.CompletionItem
	compIdx     int
	compVisible bool

	// Hover popup.
	hoverText string

	// Go vet diagnostics merged with LSP diagnostics.
	vetDiags []editor.Diagnostic
}

// New creates a Model. Opens the file at path (empty = new buffer).
// lspSession may be nil to disable LSP features.
// tel may be nil; pass telemetry.Noop() or telemetry.New() from the caller.
func New(path string, lspSession *lsp.Session, tel telemetry.Telemetry) (*Model, error) {
	var buf *buffer.Buffer
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
	ed := editor.New(buf)
	return &Model{ed: ed, buf: buf, lsp: lspSession, tel: tel}, nil
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.lsp != nil && m.buf.Path != "" {
		cmds = append(cmds, m.openDoc(), m.listenNotifications())
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKey(msg)

	case msgDiagnostics:
		m.mergeDiagnostics(msg.path, msg.diags)

	case msgHover:
		m.hoverText = msg.text

	case msgCompletion:
		m.completions = msg.items
		m.compIdx = 0
		m.compVisible = len(msg.items) > 0

	case msgVetDiags:
		m.vetDiags = msg.diags
		m.mergeDiagnostics("", nil) // re-merge

	case msgQuit:
		return m, tea.Quit
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := keyString(msg)
	prevMode := m.ed.Mode()

	// Completion navigation while popup is visible.
	if m.compVisible {
		switch key {
		case "<C-n>", "<Tab>":
			m.compIdx = (m.compIdx + 1) % len(m.completions)
			return m, nil
		case "<C-p>":
			m.compIdx = (m.compIdx - 1 + len(m.completions)) % len(m.completions)
			return m, nil
		case "<Enter>":
			m.applyCompletion()
			return m, nil
		case "<Esc>":
			m.compVisible = false
			return m, nil
		}
	}

	// Activate gap buffer when entering insert mode.
	if prevMode == editor.ModeNormal && key == "i" || key == "a" || key == "o" || key == "O" {
		cur := m.ed.Cursor()
		m.buf.ActivateGap(cur.Row, cur.Col)
	}

	m.ed.HandleKey(key)

	// Clear hover when cursor moves.
	m.hoverText = ""

	var cmds []tea.Cmd

	// Handle signals encoded in StatusMsg.
	status := m.ed.StatusMsg()
	switch {
	case status == "quit" || status == "quit!":
		if status == "quit!" || !m.buf.Modified {
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
	if prevMode == editor.ModeInsert && m.ed.Mode() != editor.ModeInsert {
		m.buf.FlushGap()
		m.compVisible = false
		// Notify LSP of the change and run go vet.
		if m.lsp != nil && m.buf.Path != "" {
			cmds = append(cmds, m.didChange(), m.runVet())
		}
	}

	// Save: run vet + notify LSP.
	if strings.HasPrefix(status, "written:") {
		if m.lsp != nil && m.buf.Path != "" {
			cmds = append(cmds, m.didSave(), m.runVet())
		}
	}

	m.scrollToCursor()
	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	visRows := m.visibleRows()
	var sb strings.Builder

	diagMap := m.diagByRow()
	cur := m.ed.Cursor()
	visStart, visEnd := m.ed.VisualRange()
	isVisual := m.ed.Mode() == editor.ModeVisual || m.ed.Mode() == editor.ModeVisualLine

	for i := 0; i < visRows; i++ {
		row := m.scroll + i

		// Line number gutter.
		sb.WriteString(styleLineNum.Render(fmt.Sprintf("%d", row+1)))
		sb.WriteString(" ")

		// Diagnostic gutter icon.
		if d, ok := diagMap[row]; ok {
			if d.Severity == lsp.SeverityError {
				sb.WriteString(styleError.Render("E"))
			} else {
				sb.WriteString(styleWarning.Render("W"))
			}
		} else {
			sb.WriteString(" ")
		}
		sb.WriteString(" ")

		if row < m.buf.LineCount() {
			line := []rune(m.buf.Line(row))
			for col, r := range line {
				// Expand tabs to 4 spaces so visual width is stable regardless
				// of cursor position (a terminal-rendered \t under a styled
				// cursor collapses to 1 column instead of expanding to the tab
				// stop, causing the rest of the line to shift).
				var ch string
				switch {
				case r == '\t':
					ch = "    "
				case utf8.RuneLen(r) == 0:
					ch = " "
				default:
					ch = string(r)
				}

				inVisual := isVisual && inVisualRange(row, col, visStart, visEnd, m.ed.Mode() == editor.ModeVisualLine)
				isCursor := row == cur.Row && col == cur.Col

				switch {
				case isCursor:
					sb.WriteString(styleCursor.Render(ch))
				case inVisual:
					sb.WriteString(styleVisualHL.Render(ch))
				default:
					sb.WriteString(ch)
				}
			}
			// Cursor past end of line.
			if row == cur.Row && cur.Col >= len(line) {
				sb.WriteString(styleCursor.Render(" "))
			}
		}
		sb.WriteString("\n")
	}

	// Status bar.
	sb.WriteString(m.renderStatus())

	// Command line / message line.
	sb.WriteString("\n")
	switch m.ed.Mode() {
	case editor.ModeCommand:
		sb.WriteString(string(m.ed.CmdMode()) + m.ed.CmdBuf())
	case editor.ModeNormal, editor.ModeInsert, editor.ModeVisual, editor.ModeVisualLine:
		if m.ed.StatusMsg() != "" &&
			m.ed.StatusMsg() != "quit" &&
			m.ed.StatusMsg() != "quit!" &&
			!strings.HasPrefix(m.ed.StatusMsg(), "lsp:") &&
			!strings.HasPrefix(m.ed.StatusMsg(), "open:") {
			sb.WriteString(m.ed.StatusMsg())
		}
	}

	// Hover popup (shown above status bar if non-empty).
	if m.hoverText != "" {
		sb.WriteString("\n" + m.hoverText)
	}

	// Completion popup — render inline at cursor position.
	if m.compVisible && len(m.completions) > 0 {
		sb.WriteString(m.renderCompletion())
	}

	return sb.String()
}

func (m *Model) renderStatus() string {
	cur := m.ed.Cursor()
	modeStr := m.ed.Mode().String()

	var modeStyle lipgloss.Style
	switch m.ed.Mode() {
	case editor.ModeNormal:
		modeStyle = styleStatusNormal
	case editor.ModeInsert:
		modeStyle = styleStatusInsert
	case editor.ModeVisual, editor.ModeVisualLine:
		modeStyle = styleStatusVisual
	case editor.ModeCommand:
		modeStyle = styleStatusCommand
	}

	left := modeStyle.Render(modeStr) + " " + m.buf.Path
	if m.buf.Path == "" {
		left += "[New File]"
	}
	if m.buf.Modified {
		left += " [+]"
	}
	var lspIndicator string
	if m.lsp != nil {
		lspIndicator = styleStatusLSPOn.Render("LSP")
	} else {
		lspIndicator = styleStatusLSPOff.Render("no LSP")
	}
	right := lspIndicator + "  " + fmt.Sprintf("%d:%d", cur.Row+1, cur.Col+1)
	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if pad < 0 {
		pad = 0
	}
	return left + strings.Repeat(" ", pad) + right
}

func (m *Model) renderCompletion() string {
	var sb strings.Builder
	maxItems := 8
	if len(m.completions) < maxItems {
		maxItems = len(m.completions)
	}
	for i := 0; i < maxItems; i++ {
		label := m.completions[i].Label
		if len(label) > 30 {
			label = label[:30]
		}
		if i == m.compIdx {
			sb.WriteString("\n" + styleCompletionSel.Render(label))
		} else {
			sb.WriteString("\n" + styleCompletionItem.Render(label))
		}
	}
	return sb.String()
}

// --- LSP commands ---

func (m *Model) openDoc() tea.Cmd {
	return func() tea.Msg {
		_ = m.lsp.DidOpen(context.Background(), m.buf.Path, m.buf.String())
		return nil
	}
}

func (m *Model) didChange() tea.Cmd {
	text := m.buf.String()
	return func() tea.Msg {
		_ = m.lsp.DidChange(context.Background(), m.buf.Path, text)
		return nil
	}
}

func (m *Model) didSave() tea.Cmd {
	return func() tea.Msg {
		_ = m.lsp.DidSave(context.Background(), m.buf.Path)
		return nil
	}
}

func (m *Model) gotoDefinition() tea.Cmd {
	cur := m.ed.Cursor()
	path := m.buf.Path
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
	cur := m.ed.Cursor()
	path := m.buf.Path
	return func() tea.Msg {
		text, err := m.lsp.Hover(context.Background(), path, cur.Row, cur.Col)
		if err != nil {
			return msgHover{text: fmt.Sprintf("hover error: %v", err)}
		}
		return msgHover{text: text}
	}
}

func (m *Model) complete() tea.Cmd {
	cur := m.ed.Cursor()
	path := m.buf.Path
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
	path := m.buf.Path
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
		m.buf = buf
		m.ed = editor.New(buf)
		m.scroll = 0
		if m.lsp != nil {
			_ = m.lsp.DidOpen(context.Background(), path, buf.String())
		}
		return nil
	}
}

func (m *Model) applyCompletion() {
	if !m.compVisible || m.compIdx >= len(m.completions) {
		return
	}
	item := m.completions[m.compIdx]
	text := item.InsertText
	if text == "" {
		text = item.Label
	}
	cur := m.ed.Cursor()
	m.buf.InsertString(cur.Row, cur.Col, text)
	m.compVisible = false
}

func (m *Model) mergeDiagnostics(path string, lspDiags []editor.Diagnostic) {
	// Replace LSP diags for the given path and merge vet diags.
	m.ed.SetDiagnostics(append(lspDiags, m.vetDiags...))
	_ = path
}

func (m *Model) diagByRow() map[int]editor.Diagnostic {
	out := make(map[int]editor.Diagnostic)
	for _, d := range m.ed.GetDiagnostics() {
		if existing, ok := out[d.Row]; !ok || d.Severity < existing.Severity {
			out[d.Row] = d
		}
	}
	return out
}

func (m *Model) scrollToCursor() {
	cur := m.ed.Cursor()
	rows := m.visibleRows()
	if cur.Row < m.scroll {
		m.scroll = cur.Row
	} else if cur.Row >= m.scroll+rows {
		m.scroll = cur.Row - rows + 1
	}
}

func (m *Model) visibleRows() int {
	rows := m.height - 2 // status bar + message line
	if rows < 1 {
		return 1
	}
	return rows
}

func (m Model) String() string {
	return m.buf.String()
}

// LineCount returns the number of lines in the buffer.
func (m *Model) LineCount() int {
	return m.buf.LineCount()
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
