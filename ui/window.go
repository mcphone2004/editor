package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/anthonybrice/editor/editor"
	"github.com/anthonybrice/editor/highlight"
	goHL "github.com/anthonybrice/editor/highlight/golang"
	"github.com/anthonybrice/editor/layout"
	"github.com/anthonybrice/editor/lsp"
	"github.com/charmbracelet/lipgloss"
)

// --- Syntax highlight styles and registry ---

//nolint:gochecknoglobals // immutable after init
var (
	styleHLKeyword = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	styleHLString  = lipgloss.NewStyle().Foreground(lipgloss.Color("71"))
	styleHLNumber  = lipgloss.NewStyle().Foreground(lipgloss.Color("172"))
	styleHLComment = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
)

//nolint:gochecknoglobals // populated once at init, never mutated
var highlighters = map[string]highlight.Highlighter{
	".go": goHL.New(),
}

// hlStyle maps a highlight.Kind to its lipgloss style.
func hlStyle(k highlight.Kind) lipgloss.Style {
	switch k {
	case highlight.KindNone:
		return lipgloss.NewStyle()
	case highlight.KindKeyword:
		return styleHLKeyword
	case highlight.KindString:
		return styleHLString
	case highlight.KindNumber:
		return styleHLNumber
	case highlight.KindComment:
		return styleHLComment
	}
	return lipgloss.NewStyle() // unreachable: exhaustive linter guards all defined Kind constants
}

// highlighterForPath returns the registered Highlighter for path's extension,
// or nil when no highlighter is available.
func highlighterForPath(path string) highlight.Highlighter {
	if path == "" {
		return nil
	}
	return highlighters[filepath.Ext(path)]
}

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

	// Compute syntax highlights for the visible window (nil when no highlighter
	// is registered for this file type).
	var hlMap map[int]highlight.LineHL
	if h := highlighterForPath(buf.Path()); h != nil {
		hlMap = h.Highlight(buf.String(), p.scroll, p.scroll+visRows-1)
	}

	clipStyle := lipgloss.NewStyle().MaxWidth(p.w)
	for i := 0; i < visRows; i++ {
		row := p.scroll + i

		// Build the line into a temporary buffer then clip to pane width.
		var line strings.Builder

		line.WriteString(styleLineNum.Render(fmt.Sprintf("%d", row+1)))
		line.WriteString(" ")

		if d, ok := diagMap[row]; ok {
			if d.Severity == lsp.SeverityError {
				line.WriteString(styleError.Render("E"))
			} else {
				line.WriteString(styleWarning.Render("W"))
			}
		} else {
			line.WriteString(" ")
		}
		line.WriteString(" ")

		if row < buf.LineCount() {
			content := []rune(buf.Line(row))
			rowHL := hlMap[row] // nil when no highlights for this row
			for col, r := range content {
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
					line.WriteString(styleCursor.Render(ch))
				case inVisual:
					line.WriteString(styleVisualHL.Render(ch))
				default:
					if col < len(rowHL) && rowHL[col] != highlight.KindNone {
						line.WriteString(hlStyle(rowHL[col]).Render(ch))
					} else {
						line.WriteString(ch)
					}
				}
			}
			if isFocused && row == cur.Row && cur.Col >= len(content) {
				line.WriteString(styleCursor.Render(" "))
			}
		}

		sb.WriteString(clipStyle.Render(line.String()))
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

// divider returns a styled single-column vertical bar string h lines tall,
// without a trailing newline, suitable for passing to lipgloss.JoinHorizontal.
func divider(h int) string {
	if h <= 0 {
		return ""
	}
	bar := styleDivider.Render("│")
	return strings.Repeat(bar+"\n", h-1) + bar
}
