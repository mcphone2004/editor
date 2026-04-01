// Package buffer provides the Buffer type used by the editor.
//
// Internally it combines two data structures:
//
//  1. Piece table (internal/buffer/piece) — the canonical document store.
//     The document is a sequence of "pieces" pointing into the original file
//     content and an append-only add-buffer.  Edits are cheap (no text is
//     copied) and undo is natural (just restore a previous piece sequence).
//
//  2. Gap buffer (internal/buffer/gap) — the "hot zone" during insert mode.
//     When the editor enters insert mode the piece spanning the cursor is
//     loaded into a gap buffer.  Keystrokes hit the gap buffer at O(1).
//     When insert mode ends (or the cursor moves far away) the gap buffer is
//     flushed back into the piece table and an undo snapshot is pushed to
//     Postgres.
//
// The external API is identical to the simple [][]rune buffer it replaces so
// the rest of the editor does not need to change.
package buffer

import (
	"fmt"
	"os"
	"strings"

	"github.com/anthonybrice/editor/internal/buffer/gap"
	"github.com/anthonybrice/editor/internal/buffer/piece"
)

// Buffer is the editor's view of an open file.
type Buffer struct {
	Path     string
	Modified bool

	table *piece.Table

	// Gap buffer — active only while in insert mode.
	// hotRow/hotCol track where in the document the gap was "opened".
	gap    *gap.Buffer
	hotRow int
	hotActive bool

	// Optional Postgres-backed undo store.  nil when postgres is unavailable.
	store *piece.PgStore
}

// New returns an empty buffer.
func New() *Buffer {
	return &Buffer{table: piece.New()}
}

// Open reads a file into a new Buffer.
func Open(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := piece.Load([]rune(string(data)))
	return &Buffer{Path: path, table: t}, nil
}

// OpenWithUndo is like Open but also connects a Postgres undo store.
// dsn is a libpq connection string, e.g.
// "host=localhost user=postgres dbname=editor sslmode=disable"
func OpenWithUndo(path, dsn string) (*Buffer, error) {
	b, err := Open(path)
	if err != nil {
		return nil, err
	}
	store, err := piece.OpenPgStore(dsn, path, b.table.Snapshot())
	if err != nil {
		// Log but don't fail — undo still works in-memory via the piece table.
		fmt.Fprintf(os.Stderr, "warning: undo store unavailable: %v\n", err)
		return b, nil
	}
	b.store = store
	return b, nil
}

// Save writes the buffer contents to disk.
func (b *Buffer) Save() error {
	b.FlushGap()
	if err := os.WriteFile(b.Path, []byte(b.table.String()), 0644); err != nil {
		return err
	}
	b.Modified = false
	return nil
}

// Close releases any resources (e.g. the Postgres connection).
func (b *Buffer) Close() {
	if b.store != nil {
		_ = b.store.Close()
	}
}

// --- Gap buffer management ---

// ActivateGap loads the content of line row into the gap buffer, positioning
// the gap at col.  Call this when entering insert mode.
func (b *Buffer) ActivateGap(row, col int) {
	b.FlushGap() // flush any previous hot zone
	line := []rune(b.table.Line(row))
	b.gap = gap.New(line)
	b.gap.MoveTo(col)
	b.hotRow = row
	b.hotActive = true
}

// FlushGap writes the gap buffer back into the piece table and pushes an undo
// snapshot.  Call this when leaving insert mode or before any piece-table
// operation that would invalidate the hot zone.
func (b *Buffer) FlushGap() {
	if !b.hotActive {
		return
	}
	// Replace the hot line in the piece table.
	text := b.gap.String()
	start := b.table.LineStart(b.hotRow)
	end := b.table.LineEnd(b.hotRow)
	b.table.Delete(start, end)
	b.table.Insert(start, []rune(text))

	b.gap = nil
	b.hotActive = false

	// Push undo snapshot.
	b.pushSnapshot()
}

func (b *Buffer) pushSnapshot() {
	if b.store != nil {
		snap := b.table.Snapshot()
		_ = b.store.Push(snap) // best-effort
	}
}

// --- Undo / Redo ---

// Undo restores the previous state. Returns false if nothing to undo.
func (b *Buffer) Undo() bool {
	b.FlushGap()
	if b.store == nil {
		return false
	}
	snap, ok := b.store.Undo()
	if !ok {
		return false
	}
	b.table.Restore(snap)
	b.Modified = true
	return true
}

// Redo reapplies a previously undone change. Returns false if nothing to redo.
func (b *Buffer) Redo() bool {
	b.FlushGap()
	if b.store == nil {
		return false
	}
	snap, ok := b.store.Redo()
	if !ok {
		return false
	}
	b.table.Restore(snap)
	b.Modified = true
	return true
}

// --- Read operations ---

// String returns the full document text.
func (b *Buffer) String() string {
	b.FlushGap()
	return b.table.String()
}

// LineCount returns the number of lines.
func (b *Buffer) LineCount() int {
	if b.hotActive {
		// Gap buffer doesn't change line count of this one line.
		return b.table.LineCount()
	}
	return b.table.LineCount()
}

// Line returns line row as a string (no trailing newline).
func (b *Buffer) Line(row int) string {
	if b.hotActive && row == b.hotRow {
		return b.gap.String()
	}
	return b.table.Line(row)
}

// LineRunes returns line row as a rune slice.
func (b *Buffer) LineRunes(row int) []rune {
	if b.hotActive && row == b.hotRow {
		return b.gap.Text()
	}
	return b.table.LineRunes(row)
}

// LineLen returns the number of runes on line row.
func (b *Buffer) LineLen(row int) int {
	if b.hotActive && row == b.hotRow {
		return b.gap.Len()
	}
	return b.table.LineLen(row)
}

// --- Write operations (used outside insert mode) ---
// All write operations flush the gap buffer first so the piece table is
// authoritative before any structural edit.

// Insert inserts rune r at (row, col).
// If the gap buffer is active for this row, it goes directly into the gap.
func (b *Buffer) Insert(row, col int, r rune) {
	if b.hotActive && row == b.hotRow {
		b.gap.MoveTo(col)
		b.gap.Insert(r)
		b.Modified = true
		return
	}
	b.FlushGap()
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune{r})
	b.Modified = true
}

// InsertString inserts s at (row, col).
func (b *Buffer) InsertString(row, col int, s string) {
	if b.hotActive && row == b.hotRow {
		b.gap.MoveTo(col)
		b.gap.InsertSlice([]rune(s))
		b.Modified = true
		return
	}
	b.FlushGap()
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune(s))
	b.Modified = true
}

// Newline splits the line at (row, col).
func (b *Buffer) Newline(row, col int) {
	b.FlushGap()
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune{'\n'})
	b.Modified = true
	b.pushSnapshot()
}

// DeleteBack deletes the rune before (row, col). Returns new position.
func (b *Buffer) DeleteBack(row, col int) (newRow, newCol int) {
	if b.hotActive && row == b.hotRow {
		if col > 0 {
			b.gap.MoveTo(col)
			b.gap.DeleteBefore(1)
			b.Modified = true
			return row, col - 1
		}
		// Merge with previous line — must flush.
		b.FlushGap()
	} else {
		b.FlushGap()
	}

	if col > 0 {
		pos := b.table.PosToOffset(row, col)
		b.table.Delete(pos-1, pos)
		b.Modified = true
		return row, col - 1
	}
	if row == 0 {
		return 0, 0
	}
	// Merge with previous line.
	prevLen := b.table.LineLen(row - 1)
	lineStart := b.table.LineStart(row)
	// lineStart-1 is the '\n' at the end of the previous line.
	b.table.Delete(lineStart-1, lineStart)
	b.Modified = true
	b.pushSnapshot()
	return row - 1, prevLen
}

// DeleteRune deletes the rune at (row, col). Returns true if deleted.
func (b *Buffer) DeleteRune(row, col int) bool {
	if b.hotActive && row == b.hotRow {
		if col >= b.gap.Len() {
			return false
		}
		b.gap.MoveTo(col)
		b.gap.DeleteAfter(1)
		b.Modified = true
		return true
	}
	b.FlushGap()
	if col >= b.table.LineLen(row) {
		return false
	}
	pos := b.table.PosToOffset(row, col)
	b.table.Delete(pos, pos+1)
	b.Modified = true
	return true
}

// DeleteRange deletes from (r1,c1) inclusive to (r2,c2) exclusive.
func (b *Buffer) DeleteRange(r1, c1, r2, c2 int) (row, col int) {
	b.FlushGap()
	start := b.table.PosToOffset(r1, c1)
	end := b.table.PosToOffset(r2, c2)
	if start > end {
		start, end = end, start
	}
	b.table.Delete(start, end)
	b.Modified = true
	b.pushSnapshot()
	return b.table.OffsetToPos(start)
}

// DeleteLines deletes lines [r1, r2] inclusive.
func (b *Buffer) DeleteLines(r1, r2 int) {
	b.FlushGap()
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	start := b.table.LineStart(r1)
	// Include the trailing newline of r2 if it exists.
	var end int
	if r2+1 < b.table.LineCount() {
		end = b.table.LineStart(r2 + 1)
	} else {
		end = b.table.Len()
		// Also remove the preceding newline so we don't leave a blank line.
		if start > 0 {
			start--
		}
	}
	b.table.Delete(start, end)
	if b.table.LineCount() == 0 {
		b.table.Insert(0, []rune{'\n'})
	}
	b.Modified = true
	b.pushSnapshot()
}

// YankRange returns a copy of the text from (r1,c1) to (r2,c2) exclusive.
func (b *Buffer) YankRange(r1, c1, r2, c2 int) string {
	b.FlushGap()
	start := b.table.PosToOffset(r1, c1)
	end := b.table.PosToOffset(r2, c2)
	if start > end {
		start, end = end, start
	}
	return string(b.table.Slice(start, end))
}

// YankLines returns lines [r1,r2] inclusive joined by newlines.
func (b *Buffer) YankLines(r1, r2 int) string {
	b.FlushGap()
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	parts := make([]string, 0, r2-r1+1)
	for i := r1; i <= r2 && i < b.table.LineCount(); i++ {
		parts = append(parts, b.table.Line(i))
	}
	return strings.Join(parts, "\n")
}

// InsertLineBelow inserts a blank line after row and returns its index.
func (b *Buffer) InsertLineBelow(row int) int {
	b.FlushGap()
	end := b.table.LineEnd(row)
	b.table.Insert(end, []rune{'\n'})
	b.Modified = true
	return row + 1
}

// InsertLineAbove inserts a blank line before row and returns its index.
func (b *Buffer) InsertLineAbove(row int) int {
	b.FlushGap()
	start := b.table.LineStart(row)
	b.table.Insert(start, []rune{'\n'})
	b.Modified = true
	return row
}

// PasteAfter pastes text after col (or below row if linewise).
func (b *Buffer) PasteAfter(row, col int, text string, linewise bool) (newRow, newCol int) {
	b.FlushGap()
	if linewise {
		newRow := b.InsertLineBelow(row)
		lines := strings.Split(text, "\n")
		start := b.table.LineStart(newRow)
		for i, l := range lines {
			if i > 0 {
				b.table.Insert(b.table.LineEnd(newRow+i-1), []rune{'\n'})
			}
			lineStart := b.table.LineStart(newRow + i)
			b.table.Insert(lineStart, []rune(l))
			_ = start
		}
		b.Modified = true
		b.pushSnapshot()
		return newRow, 0
	}
	pos := b.table.PosToOffset(row, col) + 1
	b.table.Insert(pos, []rune(text))
	b.Modified = true
	b.pushSnapshot()
	newRow, newCol = b.table.OffsetToPos(pos + len([]rune(text)) - 1)
	return newRow, newCol
}

// PasteBefore pastes text at col (or above row if linewise).
func (b *Buffer) PasteBefore(row, col int, text string, linewise bool) (newRow, newCol int) {
	b.FlushGap()
	if linewise {
		newRow := b.InsertLineAbove(row)
		lines := strings.Split(text, "\n")
		for i, l := range lines {
			if i > 0 {
				b.table.Insert(b.table.LineEnd(newRow+i-1), []rune{'\n'})
			}
			lineStart := b.table.LineStart(newRow + i)
			b.table.Insert(lineStart, []rune(l))
		}
		b.Modified = true
		b.pushSnapshot()
		return newRow, 0
	}
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune(text))
	b.Modified = true
	b.pushSnapshot()
	return row, col + len([]rune(text)) - 1
}
