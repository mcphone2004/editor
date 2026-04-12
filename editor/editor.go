// Package editor implements the core vim-like editing engine.
// It is decoupled from any rendering or terminal concerns.
package editor

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anthonybrice/editor/buffer"
)

// Mode represents the current vim editing mode.
type Mode int

// Mode constants for the editor's editing modes.
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual
	ModeVisualLine
	ModeCommand
)

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	case ModeCommand:
		return "COMMAND"
	}
	return ""
}

// Register holds yanked text and whether it was yanked linewise.
type Register struct {
	Text     string
	Linewise bool
}

// Diagnostic is a single error/warning at a specific location.
type Diagnostic struct {
	Row      int
	Col      int
	Severity int // 1=error, 2=warning, 3=info, 4=hint
	Message  string
	Source   string
}

// Editor is the interface for the complete state of one open file.
type Editor interface {
	Buf() buffer.Buffer
	Mode() Mode
	Cursor() Pos
	VisualAnchor() Pos
	VisualRange() (start, end Pos)
	CmdBuf() string
	CmdMode() rune
	StatusMsg() string
	SetDiagnostics(d []Diagnostic)
	GetDiagnostics() []Diagnostic
	HandleKey(key string)
	// Register returns the current value of the unnamed register.
	// The UI layer owns register state and injects it before each keypress
	// so that all panes share a single global register (vim semantics).
	Register() Register
	// SetRegister replaces the unnamed register before a keypress is dispatched.
	SetRegister(r Register)
}

// editorState is the concrete state of one open file.
type editorState struct {
	buf    buffer.Buffer
	mode   Mode
	cursor Pos

	// Visual mode anchor.
	visualAnchor Pos

	// Pending operator/count state for normal-mode key chaining.
	pendingCount string
	pendingOp    rune   // 'd', 'c', 'y', 'r', 'g', 'z', or 0
	pendingOpStr string // multi-char ops like "gg", "gc"
	lastFindChar rune
	lastFindFwd  bool
	lastFindTill bool

	// Registers (default register is '"').
	register Register

	// Command-line buffer (for : and / mode).
	cmdBuf  string
	cmdMode rune // ':' or '/'

	// Search state.
	lastSearch    string
	searchForward bool

	// Diagnostics (set externally by LSP/vet).
	diagnostics []Diagnostic

	// Status message for the user (cleared on next normal-mode key).
	statusMsg string
}

// New creates an Editor for the given buffer.
func New(buf buffer.Buffer) Editor {
	return &editorState{buf: buf, mode: ModeNormal, searchForward: true}
}

// Buf exposes the underlying buffer (read-only access for the UI).
func (e *editorState) Buf() buffer.Buffer { return e.buf }

// Mode returns the current mode.
func (e *editorState) Mode() Mode { return e.mode }

// Cursor returns the current cursor position.
func (e *editorState) Cursor() Pos { return e.cursor }

// VisualAnchor returns the visual mode anchor.
func (e *editorState) VisualAnchor() Pos { return e.visualAnchor }

// CmdBuf returns the current command-line buffer contents.
func (e *editorState) CmdBuf() string { return e.cmdBuf }

// CmdMode returns ':' or '/' depending on which command mode is active.
func (e *editorState) CmdMode() rune { return e.cmdMode }

// StatusMsg returns the current status message (empty when none).
func (e *editorState) StatusMsg() string { return e.statusMsg }

// Register returns the current value of the unnamed register.
func (e *editorState) Register() Register { return e.register }

// SetRegister replaces the unnamed register.
func (e *editorState) SetRegister(r Register) { e.register = r }

// SetDiagnostics replaces the current diagnostic list.
func (e *editorState) SetDiagnostics(d []Diagnostic) { e.diagnostics = d }

// GetDiagnostics returns the current diagnostics.
func (e *editorState) GetDiagnostics() []Diagnostic { return e.diagnostics }

// VisualRange returns the start/end of the current visual selection
// in canonical (start <= end) order.
func (e *editorState) VisualRange() (start, end Pos) {
	a, b := e.visualAnchor, e.cursor
	if a.Row > b.Row || (a.Row == b.Row && a.Col > b.Col) {
		a, b = b, a
	}
	return a, b
}

// HandleKey processes a single key event and updates state.
// key is a string like "a", "A", "<C-c>", "<Esc>", "<Enter>", etc.
func (e *editorState) HandleKey(key string) {
	switch e.mode {
	case ModeNormal:
		e.handleNormal(key)
	case ModeInsert:
		e.handleInsert(key)
	case ModeCommand:
		e.handleCommand(key)
	case ModeVisual, ModeVisualLine:
		e.handleVisual(key)
	}
}

// --- Normal mode ---

//nolint:maintidx // vim normal-mode dispatch is inherently a large switch; splitting it adds indirection without clarity
func (e *editorState) handleNormal(key string) {
	e.statusMsg = ""

	// Count accumulation.
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' && e.pendingOp == 0 {
		e.pendingCount += key
		return
	}
	if key == "0" && e.pendingCount != "" {
		e.pendingCount += "0"
		return
	}

	count := e.consumeCount()

	// Multi-char op continuations.
	if e.pendingOp != 0 {
		e.handlePendingOp(key, count)
		return
	}

	switch key {
	// Movement.
	case "h", "<Left>":
		e.applyMotionN(motionLeft, count)
	case "l", "<Right>":
		e.applyMotionN(motionRight, count)
	case "j", "<Down>":
		e.applyMotionN(motionDown, count)
	case "k", "<Up>":
		e.applyMotionN(motionUp, count)
	case "w":
		e.applyMotionN(motionWordForward, count)
	case "W":
		e.applyMotionN(motionWordForwardBig, count)
	case "b":
		e.applyMotionN(motionWordBack, count)
	case "B":
		e.applyMotionN(motionWordBackBig, count)
	case "e":
		e.applyMotionN(motionWordEnd, count)
	case "E":
		e.applyMotionN(motionWordEndBig, count)
	case "0", "<Home>":
		e.cursor.Col = 0
	case "^":
		dst, _ := motionFirstNonBlank(e)
		e.cursor = dst
	case "$", "<End>":
		dst, _ := motionLineEnd(e)
		e.cursor = dst
	case "G":
		if count > 0 {
			dst, _ := motionGoToLine(count)(e)
			e.cursor = dst
		} else {
			dst, _ := motionFileEnd(e)
			e.cursor = dst
		}
	case "{":
		e.applyMotionN(motionParaBack, count)
	case "}":
		e.applyMotionN(motionParaForward, count)

	// Find char.
	case "f", "F", "t", "T":
		r, _ := utf8.DecodeRuneInString(key)
		e.pendingOp = r

	// Repeat find.
	case ";":
		if e.lastFindChar != 0 {
			m := motionFindChar(e.lastFindChar, e.lastFindFwd, e.lastFindTill)
			e.applyMotionN(m, max(count, 1))
		}
	case ",":
		if e.lastFindChar != 0 {
			m := motionFindChar(e.lastFindChar, !e.lastFindFwd, e.lastFindTill)
			e.applyMotionN(m, max(count, 1))
		}

	// Operators.
	case "d":
		e.pendingOp = 'd'
	case "c":
		e.pendingOp = 'c'
	case "y":
		e.pendingOp = 'y'
	case "r":
		e.pendingOp = 'r'
	case "g":
		e.pendingOp = 'g'
	case ">":
		e.pendingOp = '>'
	case "<":
		e.pendingOp = '<'

	// Delete char under cursor (x).
	case "x":
		n := max(count, 1)
		for i := 0; i < n; i++ {
			col := e.cursor.Col
			if col >= e.buf.LineLen(e.cursor.Row) {
				break
			}
			ch := string(e.buf.LineRunes(e.cursor.Row)[col])
			e.register = Register{Text: e.register.Text + ch, Linewise: false}
			e.buf.DeleteRune(e.cursor.Row, col)
		}
		e.clampCursor()

	// Delete char before cursor (X).
	case "X":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		n := max(count, 1)
		for i := 0; i < n; i++ {
			e.cursor.Row, e.cursor.Col = e.buf.DeleteBack(e.cursor.Row, e.cursor.Col)
		}

	// Paste.
	case "p":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		n := max(count, 1)
		for i := 0; i < n; i++ {
			e.cursor.Row, e.cursor.Col = e.buf.PasteAfter(
				e.cursor.Row, e.cursor.Col, e.register.Text, e.register.Linewise)
		}
	case "P":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		n := max(count, 1)
		for i := 0; i < n; i++ {
			e.cursor.Row, e.cursor.Col = e.buf.PasteBefore(
				e.cursor.Row, e.cursor.Col, e.register.Text, e.register.Linewise)
		}

	// Join lines.
	case "J":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		n := max(count, 1)
		for i := 0; i < n; i++ {
			row := e.cursor.Row
			if row >= e.buf.LineCount()-1 {
				break
			}
			col := e.buf.LineLen(row)
			next := strings.TrimLeft(e.buf.Line(row+1), " \t")
			e.buf.DeleteLines(row+1, row+1)
			if next != "" {
				e.buf.InsertString(row, col, " "+next)
			}
			e.cursor.Col = col
		}

	// Insert modes.
	case "i":
		e.setMode(ModeInsert)
	case "I":
		dst, _ := motionFirstNonBlank(e)
		e.cursor = dst
		e.setMode(ModeInsert)
	case "a":
		if e.buf.LineLen(e.cursor.Row) > 0 {
			e.cursor.Col++
		}
		e.setMode(ModeInsert)
	case "A":
		e.cursor.Col = e.buf.LineLen(e.cursor.Row)
		e.setMode(ModeInsert)
	case "o":
		newRow := e.buf.InsertLineBelow(e.cursor.Row)
		e.cursor = Pos{newRow, 0}
		// Auto-indent.
		indent := leadingWhitespace(e.buf.Line(e.cursor.Row - 1))
		e.buf.InsertString(newRow, 0, indent)
		e.cursor.Col = len([]rune(indent))
		e.setMode(ModeInsert)
	case "O":
		newRow := e.buf.InsertLineAbove(e.cursor.Row)
		e.cursor = Pos{newRow, 0}
		indent := leadingWhitespace(e.buf.Line(e.cursor.Row + 1))
		e.buf.InsertString(newRow, 0, indent)
		e.cursor.Col = len([]rune(indent))
		e.setMode(ModeInsert)

	// Replace mode (single char).
	case "s":
		e.buf.DeleteRune(e.cursor.Row, e.cursor.Col)
		e.setMode(ModeInsert)
	case "S":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		e.buf.DeleteLines(e.cursor.Row, e.cursor.Row)
		e.buf.InsertLineAbove(e.cursor.Row)
		e.cursor.Col = 0
		e.setMode(ModeInsert)

	// Visual modes.
	case "v":
		e.visualAnchor = e.cursor
		e.setMode(ModeVisual)
	case "V":
		e.visualAnchor = e.cursor
		e.setMode(ModeVisualLine)

	// Undo/redo.
	case "u":
		if row, col, ok := e.buf.Undo(); ok {
			e.cursor = Pos{row, col}
			e.clampCursor()
		} else {
			e.statusMsg = "already at oldest change"
		}
	case "<C-r>":
		if row, col, ok := e.buf.Redo(); ok {
			e.cursor = Pos{row, col}
			e.clampCursor()
		} else {
			e.statusMsg = "already at newest change"
		}

	// Command mode.
	case ":":
		e.cmdBuf = ""
		e.cmdMode = ':'
		e.setMode(ModeCommand)
	case "/":
		e.cmdBuf = ""
		e.cmdMode = '/'
		e.setMode(ModeCommand)
	case "?":
		e.cmdBuf = ""
		e.cmdMode = '?'
		e.setMode(ModeCommand)
	case "n":
		e.searchNext(e.searchForward)
	case "N":
		e.searchNext(!e.searchForward)

	// Scroll shortcuts.
	case "<C-f>", "<PageDown>":
		e.applyMotionN(motionDown, 20)
	case "<C-b>", "<PageUp>":
		e.applyMotionN(motionUp, 20)
	case "<C-d>":
		e.applyMotionN(motionDown, 10)
	case "<C-u>":
		e.applyMotionN(motionUp, 10)

	// Misc.
	case "~":
		e.toggleCase()
	case ".", "<F5>":
		// Repeat last change — placeholder.
		e.statusMsg = ". (repeat) not yet implemented"
	}
}

func (e *editorState) handlePendingOp(key string, count int) {
	op := e.pendingOp
	e.pendingOp = 0

	// f/F/t/T: expect a char.
	if op == 'f' || op == 'F' || op == 't' || op == 'T' {
		if len(key) != 1 {
			return
		}
		ch, _ := utf8.DecodeRuneInString(key)
		fwd := op == 'f' || op == 't'
		till := op == 't' || op == 'T'
		e.lastFindChar = ch
		e.lastFindFwd = fwd
		e.lastFindTill = till
		m := motionFindChar(ch, fwd, till)
		e.applyMotionN(m, max(count, 1))
		return
	}

	// r: replace char.
	if op == 'r' {
		if len(key) == 1 {
			e.buf.DeleteRune(e.cursor.Row, e.cursor.Col)
			rr, _ := utf8.DecodeRuneInString(key)
			e.buf.Insert(e.cursor.Row, e.cursor.Col, rr)
		}
		return
	}

	// g prefix.
	if op == 'g' {
		switch key {
		case "g":
			if count > 0 {
				dst, _ := motionGoToLine(count)(e)
				e.cursor = dst
			} else {
				e.cursor = Pos{0, 0}
			}
		case "d":
			// go-to-definition — signalled via StatusMsg for LSP to pick up.
			e.statusMsg = "lsp:gd"
		case "h":
			e.statusMsg = "lsp:hover"
		}
		return
	}

	// Indent operators.
	if op == '>' || op == '<' { //nolint:nestif // inherently nested dispatch logic
		if key == string(op) {
			// Double: operate on current line(s).
			n := max(count, 1)
			for i := 0; i < n; i++ {
				row := e.cursor.Row + i
				if row >= e.buf.LineCount() {
					break
				}
				if op == '>' {
					e.buf.InsertString(row, 0, "\t")
				} else {
					l := e.buf.LineRunes(row)
					if len(l) > 0 && (l[0] == '\t' || l[0] == ' ') {
						e.buf.DeleteRune(row, 0)
					}
				}
			}
		}
		return
	}

	// d/c/y with doubled key = line-wise.
	if (op == 'd' || op == 'c' || op == 'y') && key == string(op) { //nolint:nestif // inherently nested dispatch logic
		n := max(count, 1)
		r1 := e.cursor.Row
		r2 := r1 + n - 1
		if r2 >= e.buf.LineCount() {
			r2 = e.buf.LineCount() - 1
		}
		e.register = Register{Text: e.buf.YankLines(r1, r2), Linewise: true}
		if op != 'y' {
			e.buf.DeleteLines(r1, r2)
			if e.cursor.Row >= e.buf.LineCount() {
				e.cursor.Row = e.buf.LineCount() - 1
			}
			e.clampCursor()
			if op == 'c' {
				indent := leadingWhitespace(e.buf.Line(e.cursor.Row))
				e.buf.InsertString(e.cursor.Row, 0, indent)
				e.cursor.Col = len([]rune(indent))
				e.setMode(ModeInsert)
			}
		}
		return
	}

	// d/c/y with text object.
	if key == "i" || key == "a" {
		e.pendingOp = op
		e.pendingOpStr = key // wait for next key
		return
	}
	if e.pendingOpStr == "i" || e.pendingOpStr == "a" {
		inner := e.pendingOpStr == "i"
		e.pendingOpStr = ""
		ch, _ := utf8.DecodeRuneInString(key)
		if toFn, ok := textObjects[ch]; ok {
			r1, c1, r2, c2, linewise, ok2 := toFn(e, inner)
			if ok2 {
				e.applyOperatorRange(op, r1, c1, r2, c2, linewise)
			}
		}
		return
	}

	// d/c/y with motion.
	motion := e.keyToMotion(key)
	if motion == nil {
		return
	}
	dst, linewise := motion(e)
	e.applyOperatorToMotion(op, dst, linewise, max(count, 1))
}

func (e *editorState) applyOperatorToMotion(op rune, dst Pos, linewise bool, count int) {
	src := e.cursor
	for i := 1; i < count; i++ {
		tmp := e.cursor
		e.cursor = dst
		dst, linewise = e.keyToMotion("")(e)
		_ = tmp
	}
	if linewise {
		r1, r2 := src.Row, dst.Row
		if r1 > r2 {
			r1, r2 = r2, r1
		}
		e.applyOperatorRange(op, r1, 0, r2+1, 0, true)
	} else {
		r1, c1, r2, c2 := src.Row, src.Col, dst.Row, dst.Col
		if r1 > r2 || (r1 == r2 && c1 > c2) {
			r1, c1, r2, c2 = r2, c2, r1, c1
		}
		// c2 is exclusive: vim word motions (w, b) do not include the
		// destination character. Inclusive motions (e, $, f) require the
		// caller to pass c2+1. TODO: encode inclusive per-motion.
		e.applyOperatorRange(op, r1, c1, r2, c2, false)
	}
}

func (e *editorState) applyOperatorRange(op rune, r1, c1, r2, c2 int, linewise bool) {
	e.buf.SetCursorHint(r1, c1)
	if linewise { //nolint:nestif // inherently nested dispatch logic
		e.register = Register{Text: e.buf.YankLines(r1, r2-1), Linewise: true}
		if op != 'y' {
			e.buf.DeleteLines(r1, r2-1)
			if e.cursor.Row >= e.buf.LineCount() {
				e.cursor.Row = e.buf.LineCount() - 1
			}
			e.clampCursor()
			if op == 'c' {
				e.setMode(ModeInsert)
			}
		}
	} else {
		e.register = Register{Text: e.buf.YankRange(r1, c1, r2, c2), Linewise: false}
		if op != 'y' {
			e.cursor.Row, e.cursor.Col = e.buf.DeleteRange(r1, c1, r2, c2)
			e.clampCursor()
			if op == 'c' {
				e.setMode(ModeInsert)
			}
		}
	}
}

func (e *editorState) keyToMotion(key string) Motion {
	switch key {
	case "h":
		return motionLeft
	case "l":
		return motionRight
	case "j":
		return motionDown
	case "k":
		return motionUp
	case "w":
		return motionWordForward
	case "W":
		return motionWordForwardBig
	case "b":
		return motionWordBack
	case "B":
		return motionWordBackBig
	case "e":
		return motionWordEnd
	case "E":
		return motionWordEndBig
	case "$":
		return motionLineEnd
	case "0":
		return func(e *editorState) (Pos, bool) { return Pos{e.cursor.Row, 0}, false }
	case "^":
		return motionFirstNonBlank
	case "G":
		return motionFileEnd
	case "{":
		return motionParaBack
	case "}":
		return motionParaForward
	}
	return nil
}

// --- Insert mode ---

func (e *editorState) handleInsert(key string) {
	switch key {
	case "<Esc>", "<C-c>":
		// Move cursor back one if not at start.
		if e.cursor.Col > 0 {
			e.cursor.Col--
		}
		e.setMode(ModeNormal)

	case "<Enter>":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		indent := autoIndent(e.buf.Line(e.cursor.Row))
		e.buf.Newline(e.cursor.Row, e.cursor.Col)
		e.cursor.Row++
		e.cursor.Col = 0
		e.buf.InsertString(e.cursor.Row, 0, indent)
		e.cursor.Col = len([]rune(indent))

	case "<Backspace>":
		e.buf.SetCursorHint(e.cursor.Row, e.cursor.Col)
		e.cursor.Row, e.cursor.Col = e.buf.DeleteBack(e.cursor.Row, e.cursor.Col)

	case "<Delete>":
		e.buf.DeleteRune(e.cursor.Row, e.cursor.Col)

	case "<Tab>":
		e.buf.InsertString(e.cursor.Row, e.cursor.Col, "\t")
		e.cursor.Col++

	case "<Left>":
		if e.cursor.Col > 0 {
			e.cursor.Col--
		}
	case "<Right>":
		if e.cursor.Col < e.buf.LineLen(e.cursor.Row) {
			e.cursor.Col++
		}
	case "<Up>":
		dst, _ := motionUp(e)
		e.cursor = dst
	case "<Down>":
		dst, _ := motionDown(e)
		e.cursor = dst
	case "<Home>":
		e.cursor.Col = 0
	case "<End>":
		e.cursor.Col = e.buf.LineLen(e.cursor.Row)

	case "<C-n>", "<C-p>":
		// Trigger LSP completion — signalled via StatusMsg.
		e.statusMsg = "lsp:complete"

	default:
		if len(key) == 1 {
			r, _ := utf8.DecodeRuneInString(key)
			e.buf.Insert(e.cursor.Row, e.cursor.Col, r)
			e.cursor.Col++
			// Auto-close brackets.
			switch r {
			case '(':
				e.buf.Insert(e.cursor.Row, e.cursor.Col, ')')
			case '{':
				e.buf.Insert(e.cursor.Row, e.cursor.Col, '}')
			case '[':
				e.buf.Insert(e.cursor.Row, e.cursor.Col, ']')
			}
		}
	}
}

// --- Visual mode ---

func (e *editorState) handleVisual(key string) {
	// Count prefix accumulation (e.g. "4l" to move 4 right).
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		e.pendingCount += key
		return
	}
	if key == "0" && e.pendingCount != "" {
		e.pendingCount += "0"
		return
	}
	count := e.consumeCount()
	if count <= 0 {
		count = 1
	}

	switch key {
	case "<Esc>", "<C-c>", "v", "V":
		e.setMode(ModeNormal)
		return
	case "d", "x":
		start, end := e.VisualRange()
		if e.mode == ModeVisualLine {
			e.register = Register{Text: e.buf.YankLines(start.Row, end.Row), Linewise: true}
			e.buf.DeleteLines(start.Row, end.Row)
		} else {
			e.register = Register{Text: e.buf.YankRange(start.Row, start.Col, end.Row, end.Col+1), Linewise: false}
			e.cursor.Row, e.cursor.Col = e.buf.DeleteRange(start.Row, start.Col, end.Row, end.Col+1)
		}
		e.clampCursor()
		e.setMode(ModeNormal)
		return
	case "y":
		start, end := e.VisualRange()
		if e.mode == ModeVisualLine {
			e.register = Register{Text: e.buf.YankLines(start.Row, end.Row), Linewise: true}
		} else {
			e.register = Register{Text: e.buf.YankRange(start.Row, start.Col, end.Row, end.Col+1), Linewise: false}
		}
		e.cursor = start
		e.setMode(ModeNormal)
		return
	case ">":
		start, end := e.VisualRange()
		for r := start.Row; r <= end.Row; r++ {
			e.buf.InsertString(r, 0, "\t")
		}
		e.setMode(ModeNormal)
		return
	case "<":
		start, end := e.VisualRange()
		for r := start.Row; r <= end.Row; r++ {
			l := e.buf.LineRunes(r)
			if len(l) > 0 && (l[0] == '\t' || l[0] == ' ') {
				e.buf.DeleteRune(r, 0)
			}
		}
		e.setMode(ModeNormal)
		return
	}

	// Apply motions to extend selection (with count).
	motion := e.keyToMotion(key)
	if motion != nil {
		e.applyMotionN(motion, count)
	}
}

// --- Command mode (:, /, ?) ---

func (e *editorState) handleCommand(key string) {
	switch key {
	case "<Esc>", "<C-c>":
		e.cmdBuf = ""
		e.setMode(ModeNormal)
	case "<Enter>":
		e.execCommand()
		if e.mode == ModeCommand {
			e.setMode(ModeNormal)
		}
		e.cmdBuf = ""
	case "<Backspace>":
		if e.cmdBuf != "" {
			r := []rune(e.cmdBuf)
			e.cmdBuf = string(r[:len(r)-1])
		} else {
			e.setMode(ModeNormal)
		}
	default:
		if len(key) == 1 {
			e.cmdBuf += key
		}
	}
}

func (e *editorState) execCommand() {
	if e.cmdMode == '/' || e.cmdMode == '?' {
		e.lastSearch = e.cmdBuf
		e.searchForward = e.cmdMode == '/'
		e.setMode(ModeNormal)
		e.searchNext(e.searchForward)
		return
	}

	cmd := strings.TrimSpace(e.cmdBuf)
	switch {
	case cmd == "w":
		if err := e.buf.Save(); err != nil {
			e.statusMsg = fmt.Sprintf("error: %v", err)
		} else {
			e.statusMsg = fmt.Sprintf("written: %s", e.buf.Path())
		}
	case cmd == "q":
		if e.buf.Modified() {
			e.statusMsg = "unsaved changes — use :q! to force quit"
		} else {
			e.statusMsg = "quit"
		}
	case cmd == "q!":
		e.statusMsg = "quit!"
	case cmd == "wq" || cmd == "x":
		_ = e.buf.Save()
		e.statusMsg = "quit"
	case strings.HasPrefix(cmd, "e "):
		e.statusMsg = "open:" + strings.TrimSpace(cmd[2:])
	case strings.HasPrefix(cmd, "w "):
		e.buf.SetPath(strings.TrimSpace(cmd[2:]))
		if err := e.buf.Save(); err != nil {
			e.statusMsg = fmt.Sprintf("error: %v", err)
		} else {
			e.statusMsg = fmt.Sprintf("written: %s", e.buf.Path())
		}
	case strings.HasPrefix(cmd, "set "):
		e.statusMsg = "set:" + cmd[4:]
	// Split commands: emit a sentinel that the UI layer handles.
	case cmd == "sp" || cmd == "split":
		e.statusMsg = "split:"
	case cmd == "vs" || cmd == "vsp" || cmd == "vsplit":
		e.statusMsg = "vsplit:"
	case strings.HasPrefix(cmd, "sp ") || strings.HasPrefix(cmd, "split "):
		arg := strings.TrimSpace(cmd[strings.Index(cmd, " ")+1:])
		e.statusMsg = "split:" + arg
	case strings.HasPrefix(cmd, "vs ") || strings.HasPrefix(cmd, "vsp ") || strings.HasPrefix(cmd, "vsplit "):
		arg := strings.TrimSpace(cmd[strings.Index(cmd, " ")+1:])
		e.statusMsg = "vsplit:" + arg
	case cmd == "only":
		e.statusMsg = "only"
	default:
		e.statusMsg = fmt.Sprintf("unknown command: %s", cmd)
	}
}

// --- Search ---

func (e *editorState) searchNext(forward bool) {
	if e.lastSearch == "" {
		return
	}
	start := e.cursor
	rows := e.buf.LineCount()
	for i := 0; i < rows; i++ {
		var row int
		if forward {
			row = (start.Row + 1 + i) % rows
		} else {
			row = ((start.Row - 1 - i) + rows*2) % rows
		}
		line := e.buf.Line(row)
		idx := strings.Index(line, e.lastSearch)
		if idx >= 0 {
			e.cursor = Pos{row, idx}
			e.statusMsg = fmt.Sprintf("/%s", e.lastSearch)
			return
		}
	}
	e.statusMsg = fmt.Sprintf("pattern not found: %s", e.lastSearch)
}

// --- Helpers ---

func (e *editorState) setMode(m Mode) {
	e.mode = m
}

func (e *editorState) consumeCount() int {
	if e.pendingCount == "" {
		return 0
	}
	n := 0
	for _, ch := range e.pendingCount {
		n = n*10 + int(ch-'0')
	}
	e.pendingCount = ""
	return n
}

func (e *editorState) applyMotionN(m Motion, count int) {
	if count <= 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		dst, _ := m(e)
		e.cursor = dst
	}
}

func (e *editorState) clampCursor() {
	if e.cursor.Row >= e.buf.LineCount() {
		e.cursor.Row = e.buf.LineCount() - 1
	}
	if e.cursor.Row < 0 {
		e.cursor.Row = 0
	}
	maxCol := e.buf.LineLen(e.cursor.Row) - 1
	if maxCol < 0 {
		maxCol = 0
	}
	if e.cursor.Col > maxCol {
		e.cursor.Col = maxCol
	}
}

func (e *editorState) toggleCase() {
	line := e.buf.LineRunes(e.cursor.Row)
	col := e.cursor.Col
	if col >= len(line) {
		return
	}
	r := line[col]
	if unicode.IsUpper(r) {
		e.buf.DeleteRune(e.cursor.Row, col)
		e.buf.Insert(e.cursor.Row, col, unicode.ToLower(r))
	} else if unicode.IsLower(r) {
		e.buf.DeleteRune(e.cursor.Row, col)
		e.buf.Insert(e.cursor.Row, col, unicode.ToUpper(r))
	}
	if e.cursor.Col < e.buf.LineLen(e.cursor.Row)-1 {
		e.cursor.Col++
	}
}

func leadingWhitespace(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return s[:i]
		}
	}
	return ""
}

func autoIndent(line string) string {
	indent := leadingWhitespace(line)
	trimmed := strings.TrimRight(line, " \t")
	if trimmed != "" && trimmed[len(trimmed)-1] == '{' {
		indent += "\t"
	}
	return indent
}
