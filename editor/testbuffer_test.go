package editor_test

import (
	"fmt"
	"os"
	"strings"
)

// fakeBuffer is an in-memory implementation of buffer.Buffer for unit tests.
// It uses a []string backing store so editor tests never touch the real buffer
// package, piece tables, gap buffers, or Postgres.
type fakeBuffer struct {
	lines    []string
	path     string
	modified bool

	// Cursor hint recorded before a mutating operation (mirrors real buffer).
	hintRow, hintCol int

	// Undo/redo history: same semantics as piece/memstore.
	// entries[stackTop] is the current state; Undo moves stackTop back.
	entries  []fbSnapshot
	stackTop int
}

type fbSnapshot struct {
	lines    []string
	row, col int
}

func copyLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

// newFakeBuffer creates a fakeBuffer pre-loaded with content.
func newFakeBuffer(content string) *fakeBuffer {
	var lines []string
	if content == "" {
		lines = []string{""}
	} else {
		lines = strings.Split(content, "\n")
	}
	b := &fakeBuffer{lines: lines}
	// Seed undo history with the initial state so Undo() at oldest returns false.
	b.entries = []fbSnapshot{{lines: copyLines(lines), row: 0, col: 0}}
	b.stackTop = 0
	return b
}

func (b *fakeBuffer) pushSnapshot() {
	snap := fbSnapshot{
		lines: copyLines(b.lines),
		row:   b.hintRow,
		col:   b.hintCol,
	}
	// Discard any redo entries above the current position.
	b.entries = append(b.entries[:b.stackTop+1], snap)
	b.stackTop = len(b.entries) - 1
}

// posToOffset converts (row, col) to a flat rune offset in the joined document.
func (b *fakeBuffer) posToOffset(row, col int) int {
	off := 0
	for i := 0; i < row && i < len(b.lines); i++ {
		off += len([]rune(b.lines[i])) + 1 // +1 for '\n'
	}
	return off + col
}

// offsetToPos converts a flat rune offset back to (row, col).
func (b *fakeBuffer) offsetToPos(off int) (row, col int) {
	pos := 0
	for i, line := range b.lines {
		lineLen := len([]rune(line))
		if pos+lineLen >= off {
			return i, off - pos
		}
		pos += lineLen + 1
	}
	last := len(b.lines) - 1
	return last, len([]rune(b.lines[last]))
}

func (b *fakeBuffer) toFlat() string    { return strings.Join(b.lines, "\n") }
func (b *fakeBuffer) fromFlat(s string) { b.lines = strings.Split(s, "\n") }

// --- buffer.Buffer interface ---

func (b *fakeBuffer) Path() string         { return b.path }
func (b *fakeBuffer) SetPath(path string)  { b.path = path }
func (b *fakeBuffer) Modified() bool       { return b.modified }
func (b *fakeBuffer) HasUndoStore() bool   { return true }
func (b *fakeBuffer) ActivateGap(_, _ int) {}
func (b *fakeBuffer) FlushGap()            {}
func (b *fakeBuffer) SetCursorHint(row, col int) {
	b.hintRow = row
	b.hintCol = col
}
func (b *fakeBuffer) Close() {}

func (b *fakeBuffer) Save() error {
	if b.path == "" {
		return fmt.Errorf("no file name")
	}
	if err := os.WriteFile(b.path, []byte(b.toFlat()), 0o600); err != nil {
		return err
	}
	b.modified = false
	return nil
}

func (b *fakeBuffer) Undo() (row, col int, ok bool) {
	if b.stackTop <= 0 {
		return 0, 0, false
	}
	b.stackTop--
	snap := b.entries[b.stackTop]
	b.lines = copyLines(snap.lines)
	b.modified = true
	return snap.row, snap.col, true
}

func (b *fakeBuffer) Redo() (row, col int, ok bool) {
	if b.stackTop+1 >= len(b.entries) {
		return 0, 0, false
	}
	b.stackTop++
	snap := b.entries[b.stackTop]
	b.lines = copyLines(snap.lines)
	b.modified = true
	return snap.row, snap.col, true
}

func (b *fakeBuffer) String() string { return b.toFlat() }

func (b *fakeBuffer) LineCount() int { return len(b.lines) }

func (b *fakeBuffer) Line(row int) string {
	if row < 0 || row >= len(b.lines) {
		return ""
	}
	return b.lines[row]
}

func (b *fakeBuffer) LineRunes(row int) []rune {
	return []rune(b.Line(row))
}

func (b *fakeBuffer) LineLen(row int) int {
	return len([]rune(b.Line(row)))
}

func (b *fakeBuffer) Insert(row, col int, r rune) {
	runes := []rune(b.lines[row])
	newRunes := make([]rune, 0, len(runes)+1)
	newRunes = append(newRunes, runes[:col]...)
	newRunes = append(newRunes, r)
	newRunes = append(newRunes, runes[col:]...)
	b.lines[row] = string(newRunes)
	b.modified = true
}

func (b *fakeBuffer) InsertString(row, col int, s string) {
	if !strings.Contains(s, "\n") {
		runes := []rune(b.lines[row])
		ins := []rune(s)
		newRunes := make([]rune, 0, len(runes)+len(ins))
		newRunes = append(newRunes, runes[:col]...)
		newRunes = append(newRunes, ins...)
		newRunes = append(newRunes, runes[col:]...)
		b.lines[row] = string(newRunes)
	} else {
		flat := []rune(b.toFlat())
		off := b.posToOffset(row, col)
		ins := []rune(s)
		result := make([]rune, 0, len(flat)+len(ins))
		result = append(result, flat[:off]...)
		result = append(result, ins...)
		result = append(result, flat[off:]...)
		b.fromFlat(string(result))
	}
	b.modified = true
}

func (b *fakeBuffer) Newline(row, col int) {
	runes := []rune(b.lines[row])
	before := string(runes[:col])
	after := string(runes[col:])
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:row]...)
	newLines = append(newLines, before, after)
	newLines = append(newLines, b.lines[row+1:]...)
	b.lines = newLines
	b.modified = true
	b.pushSnapshot()
}

func (b *fakeBuffer) DeleteBack(row, col int) (newRow, newCol int) {
	if col > 0 {
		runes := []rune(b.lines[row])
		newRunes := make([]rune, 0, len(runes)-1)
		newRunes = append(newRunes, runes[:col-1]...)
		newRunes = append(newRunes, runes[col:]...)
		b.lines[row] = string(newRunes)
		b.modified = true
		return row, col - 1
	}
	if row == 0 {
		return 0, 0
	}
	// Merge with previous line.
	prevLen := len([]rune(b.lines[row-1]))
	b.lines[row-1] += b.lines[row]
	b.lines = append(b.lines[:row], b.lines[row+1:]...)
	b.modified = true
	b.pushSnapshot()
	return row - 1, prevLen
}

func (b *fakeBuffer) DeleteRune(row, col int) bool {
	runes := []rune(b.lines[row])
	if col >= len(runes) {
		return false
	}
	newRunes := make([]rune, 0, len(runes)-1)
	newRunes = append(newRunes, runes[:col]...)
	newRunes = append(newRunes, runes[col+1:]...)
	b.lines[row] = string(newRunes)
	b.modified = true
	return true
}

func (b *fakeBuffer) DeleteRange(r1, c1, r2, c2 int) (row, col int) {
	start := b.posToOffset(r1, c1)
	end := b.posToOffset(r2, c2)
	if start > end {
		start, end = end, start
	}
	flat := []rune(b.toFlat())
	result := make([]rune, 0, len(flat)-(end-start))
	result = append(result, flat[:start]...)
	result = append(result, flat[end:]...)
	b.fromFlat(string(result))
	if len(b.lines) == 0 {
		b.lines = []string{""}
	}
	b.modified = true
	b.pushSnapshot()
	return b.offsetToPos(start)
}

func (b *fakeBuffer) DeleteLines(r1, r2 int) {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	b.lines = append(b.lines[:r1], b.lines[r2+1:]...)
	if len(b.lines) == 0 {
		b.lines = []string{""}
	}
	b.modified = true
	b.pushSnapshot()
}

func (b *fakeBuffer) YankRange(r1, c1, r2, c2 int) string {
	start := b.posToOffset(r1, c1)
	end := b.posToOffset(r2, c2)
	if start > end {
		start, end = end, start
	}
	flat := []rune(b.toFlat())
	if end > len(flat) {
		end = len(flat)
	}
	return string(flat[start:end])
}

func (b *fakeBuffer) YankLines(r1, r2 int) string {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	parts := make([]string, 0, r2-r1+1)
	for i := r1; i <= r2 && i < len(b.lines); i++ {
		parts = append(parts, b.lines[i])
	}
	return strings.Join(parts, "\n")
}

func (b *fakeBuffer) InsertLineBelow(row int) int {
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:row+1]...)
	newLines = append(newLines, "")
	newLines = append(newLines, b.lines[row+1:]...)
	b.lines = newLines
	b.modified = true
	return row + 1
}

func (b *fakeBuffer) InsertLineAbove(row int) int {
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:row]...)
	newLines = append(newLines, "")
	newLines = append(newLines, b.lines[row:]...)
	b.lines = newLines
	b.modified = true
	return row
}

func (b *fakeBuffer) PasteAfter(row, col int, text string, linewise bool) (newRow, newCol int) {
	if linewise {
		nr := b.InsertLineBelow(row)
		lines := strings.Split(text, "\n")
		b.lines[nr] = lines[0]
		for i := 1; i < len(lines); i++ {
			extra := make([]string, 0, len(b.lines)+1)
			extra = append(extra, b.lines[:nr+i]...)
			extra = append(extra, lines[i])
			extra = append(extra, b.lines[nr+i:]...)
			b.lines = extra
		}
		b.modified = true
		b.pushSnapshot()
		return nr, 0
	}
	pos := b.posToOffset(row, col) + 1
	flat := []rune(b.toFlat())
	ins := []rune(text)
	result := make([]rune, 0, len(flat)+len(ins))
	result = append(result, flat[:pos]...)
	result = append(result, ins...)
	result = append(result, flat[pos:]...)
	b.fromFlat(string(result))
	b.modified = true
	b.pushSnapshot()
	endOff := pos + len(ins) - 1
	if endOff < 0 {
		endOff = 0
	}
	newRow, newCol = b.offsetToPos(endOff)
	return newRow, newCol
}

func (b *fakeBuffer) PasteBefore(row, col int, text string, linewise bool) (newRow, newCol int) {
	if linewise {
		nr := b.InsertLineAbove(row)
		lines := strings.Split(text, "\n")
		b.lines[nr] = lines[0]
		for i := 1; i < len(lines); i++ {
			extra := make([]string, 0, len(b.lines)+1)
			extra = append(extra, b.lines[:nr+i]...)
			extra = append(extra, lines[i])
			extra = append(extra, b.lines[nr+i:]...)
			b.lines = extra
		}
		b.modified = true
		b.pushSnapshot()
		return nr, 0
	}
	pos := b.posToOffset(row, col)
	flat := []rune(b.toFlat())
	ins := []rune(text)
	result := make([]rune, 0, len(flat)+len(ins))
	result = append(result, flat[:pos]...)
	result = append(result, ins...)
	result = append(result, flat[pos:]...)
	b.fromFlat(string(result))
	b.modified = true
	b.pushSnapshot()
	endOff := pos + len(ins) - 1
	if endOff < 0 {
		endOff = 0
	}
	newRow, newCol = b.offsetToPos(endOff)
	return newRow, newCol
}
