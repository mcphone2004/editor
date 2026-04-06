// Package fake provides an in-memory implementation of buffer.Buffer for tests.
//
// Buffer is backed by a []string slice and an in-memory undo/redo snapshot
// stack.  It is intentionally simple: no gap buffer, no piece table, no
// Postgres.  Use it in unit tests that need to exercise code depending on
// buffer.Buffer without touching the real storage layer.
package fake

import (
	"fmt"
	"os"
	"strings"
)

// Buffer is an in-memory test double for buffer.Buffer.
type Buffer struct {
	lines    []string
	path     string
	modified bool

	// Cursor hint stored before each mutating operation (mirrors real buffer).
	hintRow, hintCol int

	// Undo/redo history — same semantics as buffer/piece/memstore.
	// entries[stackTop] is the current state; Undo moves stackTop back.
	entries  []snap
	stackTop int
}

type snap struct {
	lines    []string
	row, col int
}

func cloneLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

// New returns a Buffer pre-loaded with content.
// content may contain newlines; an empty string produces a single blank line.
func New(content string) *Buffer {
	var lines []string
	if content == "" {
		lines = []string{""}
	} else {
		lines = strings.Split(content, "\n")
	}
	b := &Buffer{lines: lines}
	b.entries = []snap{{lines: cloneLines(lines)}}
	b.stackTop = 0
	return b
}

func (b *Buffer) pushSnap() {
	s := snap{lines: cloneLines(b.lines), row: b.hintRow, col: b.hintCol}
	b.entries = append(b.entries[:b.stackTop+1], s)
	b.stackTop = len(b.entries) - 1
}

func (b *Buffer) posToOffset(row, col int) int {
	off := 0
	for i := 0; i < row && i < len(b.lines); i++ {
		off += len([]rune(b.lines[i])) + 1
	}
	return off + col
}

func (b *Buffer) offsetToPos(off int) (row, col int) {
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

func (b *Buffer) toFlat() string    { return strings.Join(b.lines, "\n") }
func (b *Buffer) fromFlat(s string) { b.lines = strings.Split(s, "\n") }

// --- buffer.Buffer interface ---

// Path returns the file path.
func (b *Buffer) Path() string { return b.path }

// SetPath sets the file path.
func (b *Buffer) SetPath(path string) { b.path = path }

// Modified reports whether the buffer has unsaved changes.
func (b *Buffer) Modified() bool { return b.modified }

// HasUndoStore always returns true; the fake has a built-in undo stack.
func (b *Buffer) HasUndoStore() bool { return true }

// ActivateGap is a no-op; the fake writes directly to the line slice.
func (b *Buffer) ActivateGap(_, _ int) {}

// FlushGap is a no-op; the fake has no gap buffer to flush.
func (b *Buffer) FlushGap() {}

// SetCursorHint records the cursor position for the next undo snapshot.
func (b *Buffer) SetCursorHint(row, col int) {
	b.hintRow = row
	b.hintCol = col
}

// Close is a no-op; the fake holds no external resources.
func (b *Buffer) Close() {}

// Save writes the buffer contents to b.Path().
func (b *Buffer) Save() error {
	if b.path == "" {
		return fmt.Errorf("no file name")
	}
	if err := os.WriteFile(b.path, []byte(b.toFlat()), 0o600); err != nil {
		return err
	}
	b.modified = false
	return nil
}

// Undo restores the previous snapshot. Returns (0,0,false) at oldest.
func (b *Buffer) Undo() (row, col int, ok bool) {
	if b.stackTop <= 0 {
		return 0, 0, false
	}
	b.stackTop--
	s := b.entries[b.stackTop]
	b.lines = cloneLines(s.lines)
	b.modified = true
	return s.row, s.col, true
}

// Redo reapplies the next snapshot. Returns (0,0,false) at newest.
func (b *Buffer) Redo() (row, col int, ok bool) {
	if b.stackTop+1 >= len(b.entries) {
		return 0, 0, false
	}
	b.stackTop++
	s := b.entries[b.stackTop]
	b.lines = cloneLines(s.lines)
	b.modified = true
	return s.row, s.col, true
}

// String returns the full document text.
func (b *Buffer) String() string { return b.toFlat() }

// LineCount returns the number of lines.
func (b *Buffer) LineCount() int { return len(b.lines) }

// Line returns line row as a string (no trailing newline).
func (b *Buffer) Line(row int) string {
	if row < 0 || row >= len(b.lines) {
		return ""
	}
	return b.lines[row]
}

// LineRunes returns line row as a rune slice.
func (b *Buffer) LineRunes(row int) []rune { return []rune(b.Line(row)) }

// LineLen returns the rune length of line row.
func (b *Buffer) LineLen(row int) int { return len([]rune(b.Line(row))) }

// Insert inserts rune r at (row, col).
func (b *Buffer) Insert(row, col int, r rune) {
	runes := []rune(b.lines[row])
	newRunes := make([]rune, 0, len(runes)+1)
	newRunes = append(newRunes, runes[:col]...)
	newRunes = append(newRunes, r)
	newRunes = append(newRunes, runes[col:]...)
	b.lines[row] = string(newRunes)
	b.modified = true
}

// InsertString inserts s at (row, col). s may contain newlines.
func (b *Buffer) InsertString(row, col int, s string) {
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

// Newline splits the line at (row, col).
func (b *Buffer) Newline(row, col int) {
	runes := []rune(b.lines[row])
	before := string(runes[:col])
	after := string(runes[col:])
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:row]...)
	newLines = append(newLines, before, after)
	newLines = append(newLines, b.lines[row+1:]...)
	b.lines = newLines
	b.modified = true
	b.pushSnap()
}

// DeleteBack deletes the rune before (row, col). Returns new position.
func (b *Buffer) DeleteBack(row, col int) (newRow, newCol int) {
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
	prevLen := len([]rune(b.lines[row-1]))
	b.lines[row-1] += b.lines[row]
	b.lines = append(b.lines[:row], b.lines[row+1:]...)
	b.modified = true
	b.pushSnap()
	return row - 1, prevLen
}

// DeleteRune deletes the rune at (row, col). Returns true if deleted.
func (b *Buffer) DeleteRune(row, col int) bool {
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

// DeleteRange deletes from (r1,c1) inclusive to (r2,c2) exclusive.
func (b *Buffer) DeleteRange(r1, c1, r2, c2 int) (row, col int) {
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
	b.pushSnap()
	return b.offsetToPos(start)
}

// DeleteLines deletes lines [r1, r2] inclusive.
func (b *Buffer) DeleteLines(r1, r2 int) {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	b.lines = append(b.lines[:r1], b.lines[r2+1:]...)
	if len(b.lines) == 0 {
		b.lines = []string{""}
	}
	b.modified = true
	b.pushSnap()
}

// YankRange returns a copy of the text from (r1,c1) to (r2,c2) exclusive.
func (b *Buffer) YankRange(r1, c1, r2, c2 int) string {
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

// YankLines returns lines [r1,r2] inclusive joined by newlines.
func (b *Buffer) YankLines(r1, r2 int) string {
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	parts := make([]string, 0, r2-r1+1)
	for i := r1; i <= r2 && i < len(b.lines); i++ {
		parts = append(parts, b.lines[i])
	}
	return strings.Join(parts, "\n")
}

// InsertLineBelow inserts a blank line after row and returns its index.
func (b *Buffer) InsertLineBelow(row int) int {
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:row+1]...)
	newLines = append(newLines, "")
	newLines = append(newLines, b.lines[row+1:]...)
	b.lines = newLines
	b.modified = true
	return row + 1
}

// InsertLineAbove inserts a blank line before row and returns its index.
func (b *Buffer) InsertLineAbove(row int) int {
	newLines := make([]string, 0, len(b.lines)+1)
	newLines = append(newLines, b.lines[:row]...)
	newLines = append(newLines, "")
	newLines = append(newLines, b.lines[row:]...)
	b.lines = newLines
	b.modified = true
	return row
}

// PasteAfter pastes text after col (or below row if linewise).
func (b *Buffer) PasteAfter(row, col int, text string, linewise bool) (newRow, newCol int) {
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
		b.pushSnap()
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
	b.pushSnap()
	endOff := max(pos+len(ins)-1, 0)
	newRow, newCol = b.offsetToPos(endOff)
	return newRow, newCol
}

// PasteBefore pastes text at col (or above row if linewise).
func (b *Buffer) PasteBefore(row, col int, text string, linewise bool) (newRow, newCol int) {
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
		b.pushSnap()
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
	b.pushSnap()
	endOff := max(pos+len(ins)-1, 0)
	newRow, newCol = b.offsetToPos(endOff)
	return newRow, newCol
}
