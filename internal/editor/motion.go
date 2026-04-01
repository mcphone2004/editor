package editor

import (
	"strings"
	"unicode"
)

// Pos is a (row, col) cursor position.
type Pos struct{ Row, Col int }

// Motion resolves a movement from the current cursor position.
// It returns the destination position and whether the range is linewise.
type Motion func(e *Editor) (dst Pos, linewise bool)

// --- Basic directional motions ---

func motionLeft(e *Editor) (Pos, bool) {
	p := e.cursor
	if p.Col > 0 {
		p.Col--
	}
	return p, false
}

func motionRight(e *Editor) (Pos, bool) {
	p := e.cursor
	maxCol := e.buf.LineLen(p.Row) - 1
	if e.mode == ModeInsert {
		maxCol = e.buf.LineLen(p.Row)
	}
	if maxCol < 0 {
		maxCol = 0
	}
	if p.Col < maxCol {
		p.Col++
	}
	return p, false
}

func motionUp(e *Editor) (Pos, bool) {
	p := e.cursor
	if p.Row > 0 {
		p.Row--
	}
	p.Col = clampCol(e, p.Row, p.Col)
	return p, false
}

func motionDown(e *Editor) (Pos, bool) {
	p := e.cursor
	if p.Row < e.buf.LineCount()-1 {
		p.Row++
	}
	p.Col = clampCol(e, p.Row, p.Col)
	return p, false
}

// --- Line motions ---

func motionLineEnd(e *Editor) (Pos, bool) {
	row := e.cursor.Row
	end := e.buf.LineLen(row) - 1
	if end < 0 {
		end = 0
	}
	return Pos{row, end}, false
}

func motionFirstNonBlank(e *Editor) (Pos, bool) {
	row := e.cursor.Row
	line := []rune(e.buf.Line(row))
	for i, r := range line {
		if !unicode.IsSpace(r) {
			return Pos{row, i}, false
		}
	}
	return Pos{row, 0}, false
}

// --- Word motions ---

func motionWordForward(e *Editor) (Pos, bool) {
	return wordForward(e, false)
}

func motionWordForwardBig(e *Editor) (Pos, bool) {
	return wordForward(e, true)
}

func wordForward(e *Editor, big bool) (Pos, bool) {
	row, col := e.cursor.Row, e.cursor.Col
	line := []rune(e.buf.Line(row))

	if col >= len(line) {
		if row+1 < e.buf.LineCount() {
			return Pos{row + 1, 0}, false
		}
		return e.cursor, false
	}

	isW := wordClass(big)
	cur := isW(line[col])

	// Skip current word.
	for col < len(line) && isW(line[col]) == cur {
		col++
	}
	// Skip whitespace.
	for col < len(line) && unicode.IsSpace(line[col]) {
		col++
	}
	if col >= len(line) {
		if row+1 < e.buf.LineCount() {
			return Pos{row + 1, 0}, false
		}
		col = max(0, len(line)-1)
	}
	return Pos{row, col}, false
}

func motionWordBack(e *Editor) (Pos, bool) {
	return wordBack(e, false)
}

func motionWordBackBig(e *Editor) (Pos, bool) {
	return wordBack(e, true)
}

func wordBack(e *Editor, big bool) (Pos, bool) {
	row, col := e.cursor.Row, e.cursor.Col
	line := []rune(e.buf.Line(row))
	isW := wordClass(big)

	if col == 0 {
		if row == 0 {
			return e.cursor, false
		}
		row--
		line = []rune(e.buf.Line(row))
		col = len(line)
	}

	col--
	// Skip whitespace.
	for col > 0 && unicode.IsSpace(line[col]) {
		col--
	}
	cur := isW(line[col])
	// Skip back over word.
	for col > 0 && isW(line[col-1]) == cur {
		col--
	}
	return Pos{row, col}, false
}

func motionWordEnd(e *Editor) (Pos, bool) {
	return wordEnd(e, false)
}

func motionWordEndBig(e *Editor) (Pos, bool) {
	return wordEnd(e, true)
}

func wordEnd(e *Editor, big bool) (Pos, bool) {
	row, col := e.cursor.Row, e.cursor.Col
	line := []rune(e.buf.Line(row))
	isW := wordClass(big)

	if col+1 >= len(line) {
		if row+1 < e.buf.LineCount() {
			row++
			line = []rune(e.buf.Line(row))
			col = 0
		}
	} else {
		col++
	}
	// Skip whitespace.
	for col < len(line)-1 && unicode.IsSpace(line[col]) {
		col++
	}
	cur := isW(line[col])
	for col < len(line)-1 && isW(line[col+1]) == cur {
		col++
	}
	return Pos{row, col}, false
}

// --- File motions ---

func motionFileEnd(e *Editor) (Pos, bool) {
	row := e.buf.LineCount() - 1
	return Pos{row, 0}, true
}

func motionGoToLine(line int) Motion {
	return func(e *Editor) (Pos, bool) {
		row := line - 1
		if row < 0 {
			row = 0
		}
		if row >= e.buf.LineCount() {
			row = e.buf.LineCount() - 1
		}
		return Pos{row, 0}, true
	}
}

// --- Find-char motions ---

func motionFindChar(ch rune, forward, till bool) Motion {
	return func(e *Editor) (Pos, bool) {
		row, col := e.cursor.Row, e.cursor.Col
		line := []rune(e.buf.Line(row))
		if forward { //nolint:nestif // inherently nested directional search
			for i := col + 1; i < len(line); i++ {
				if line[i] == ch {
					if till {
						return Pos{row, i - 1}, false
					}
					return Pos{row, i}, false
				}
			}
		} else {
			for i := col - 1; i >= 0; i-- {
				if line[i] == ch {
					if till {
						return Pos{row, i + 1}, false
					}
					return Pos{row, i}, false
				}
			}
		}
		return e.cursor, false
	}
}

// --- Paragraph motions ---

func motionParaForward(e *Editor) (Pos, bool) {
	row := e.cursor.Row
	for row < e.buf.LineCount()-1 {
		row++
		if strings.TrimSpace(e.buf.Line(row)) == "" {
			return Pos{row, 0}, false
		}
	}
	return Pos{e.buf.LineCount() - 1, 0}, false
}

func motionParaBack(e *Editor) (Pos, bool) {
	row := e.cursor.Row
	for row > 0 {
		row--
		if strings.TrimSpace(e.buf.Line(row)) == "" {
			return Pos{row, 0}, false
		}
	}
	return Pos{0, 0}, false
}

// --- Helpers ---

func wordClass(big bool) func(rune) int {
	if big {
		return func(r rune) int {
			if unicode.IsSpace(r) {
				return 0
			}
			return 1
		}
	}
	return func(r rune) int {
		if unicode.IsSpace(r) {
			return 0
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return 1
		}
		return 2
	}
}

func clampCol(e *Editor, row, col int) int {
	maxCol := e.buf.LineLen(row) - 1
	if e.mode == ModeInsert {
		maxCol = e.buf.LineLen(row)
	}
	if maxCol < 0 {
		maxCol = 0
	}
	if col > maxCol {
		return maxCol
	}
	return col
}
