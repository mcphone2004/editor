package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anthonybrice/editor/editor"
	"github.com/anthonybrice/editor/layout"
	"github.com/anthonybrice/editor/lsp"
	"github.com/charmbracelet/lipgloss"
)

// winPane is a single editor window that satisfies layout.Pane.
// It bundles an editor engine with its buffer-local UI state (scroll, completion
// popup, hover text, diagnostics) and its rendered bounding box.
type winPane struct {
	ed          editor.Editor
	scroll      int
	completions []lsp.CompletionItem
	compIdx     int
	compVisible bool
	hoverText   string
	vetDiags    []editor.Diagnostic
	x, y, w, h  int
}

// SetBounds implements layout.Pane.
func (p *winPane) SetBounds(x, y, w, h int) {
	p.x, p.y, p.w, p.h = x, y, w, h
}

// Bounds implements layout.Pane.
func (p *winPane) Bounds() (x, y, w, h int) {
	return p.x, p.y, p.w, p.h
}

// contentRows is the number of rows available for editor content in this pane.
// One row is reserved for the pane's status bar.
func (p *winPane) contentRows() int {
	rows := p.h - 1
	if rows < 1 {
		return 1
	}
	return rows
}

// scrollToCursor adjusts p.scroll so the cursor remains visible.
func (p *winPane) scrollToCursor() {
	cur := p.ed.Cursor()
	rows := p.contentRows()
	if cur.Row < p.scroll {
		p.scroll = cur.Row
	} else if cur.Row >= p.scroll+rows {
		p.scroll = cur.Row - rows + 1
	}
}

// diagByRow returns the most-severe diagnostic per row.
func (p *winPane) diagByRow() map[int]editor.Diagnostic {
	out := make(map[int]editor.Diagnostic)
	for _, d := range p.ed.GetDiagnostics() {
		if existing, ok := out[d.Row]; !ok || d.Severity < existing.Severity {
			out[d.Row] = d
		}
	}
	return out
}

// render returns the rendered pane content (contentRows lines) followed by
// the pane status bar.  The result ends with the status bar line; no trailing
// newline is appended so callers can compose panes with lipgloss.Join*.
// isFocused controls whether the cursor is drawn and which status bar style
// is used.
func (p *winPane) render(isFocused bool, lspSession lsp.Session) string {
	buf := p.ed.Buf()
	visRows := p.contentRows()
	var sb strings.Builder

	diagMap := p.diagByRow()
	cur := p.ed.Cursor()
	visStart, visEnd := p.ed.VisualRange()
	isVisual := p.ed.Mode() == editor.ModeVisual || p.ed.Mode() == editor.ModeVisualLine

	for i := 0; i < visRows; i++ {
		row := p.scroll + i

		sb.WriteString(styleLineNum.Render(fmt.Sprintf("%d", row+1)))
		sb.WriteString(" ")

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

		if row < buf.LineCount() {
			line := []rune(buf.Line(row))
			for col, r := range line {
				var ch string
				switch {
				case r == '\t':
					ch = "    "
				case utf8.RuneLen(r) == 0:
					ch = " "
				default:
					ch = string(r)
				}

				inVisual := isVisual && inVisualRange(row, col, visStart, visEnd, p.ed.Mode() == editor.ModeVisualLine)
				isCursor := isFocused && row == cur.Row && col == cur.Col

				switch {
				case isCursor:
					sb.WriteString(styleCursor.Render(ch))
				case inVisual:
					sb.WriteString(styleVisualHL.Render(ch))
				default:
					sb.WriteString(ch)
				}
			}
			if isFocused && row == cur.Row && cur.Col >= len(line) {
				sb.WriteString(styleCursor.Render(" "))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(p.renderStatus(isFocused, lspSession))
	return sb.String()
}

// renderStatus returns the one-line status bar for this pane.
func (p *winPane) renderStatus(isFocused bool, lspSession lsp.Session) string {
	buf := p.ed.Buf()
	cur := p.ed.Cursor()
	modeStr := p.ed.Mode().String()

	var modeStyle lipgloss.Style
	if isFocused {
		switch p.ed.Mode() {
		case editor.ModeNormal:
			modeStyle = styleStatusNormal
		case editor.ModeInsert:
			modeStyle = styleStatusInsert
		case editor.ModeVisual, editor.ModeVisualLine:
			modeStyle = styleStatusVisual
		case editor.ModeCommand:
			modeStyle = styleStatusCommand
		default:
			modeStyle = styleStatusNormal
		}
	} else {
		modeStyle = styleStatusInactive
	}

	left := modeStyle.Render(modeStr) + " " + buf.Path()
	if buf.Path() == "" {
		left += "[New File]"
	}
	if buf.Modified() {
		left += " [+]"
	}

	var lspIndicator string
	if lspSession != nil {
		lspIndicator = styleStatusLSPOn.Render("LSP")
	} else {
		lspIndicator = styleStatusLSPOff.Render("no LSP")
	}
	right := lspIndicator + "  " + fmt.Sprintf("%d:%d", cur.Row+1, cur.Col+1)
	pad := p.w - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}
	return left + strings.Repeat(" ", pad) + right
}

// renderNode recursively renders the layout tree rooted at n.
// For Horizontal containers children are stacked top-to-bottom; for Vertical
// containers they are placed side-by-side with a single-column "│" divider
// between non-last children (matching the 1-column reservation made by
// AssignBounds).
func renderNode(n *layout.Node, focused *winPane, lspSession lsp.Session) string {
	if n.IsLeaf() {
		p := n.Pane.(*winPane) //nolint:forcetypeassert // all leaves created by this package
		return p.render(p == focused, lspSession)
	}
	switch n.Dir {
	case layout.Horizontal:
		parts := make([]string, len(n.Children))
		for i, child := range n.Children {
			parts[i] = renderNode(child, focused, lspSession)
		}
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	case layout.Vertical:
		parts := make([]string, 0, len(n.Children)*2-1)
		for i, child := range n.Children {
			parts = append(parts, renderNode(child, focused, lspSession))
			if i < len(n.Children)-1 {
				parts = append(parts, divider(layout.TreeHeight(child)))
			}
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	return ""
}

// divider returns a single-column vertical bar string h lines tall, without a
// trailing newline, suitable for passing to lipgloss.JoinHorizontal.
func divider(h int) string {
	if h <= 0 {
		return ""
	}
	return strings.Repeat("│\n", h-1) + "│"
}
