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

// Buffer is the interface for the editor's view of an open file.
type Buffer interface {
	Path() string
	SetPath(path string)
	Modified() bool
	Save() error
	Close()
	HasUndoStore() bool
	ActivateGap(row, col int)
	FlushGap()
	SetCursorHint(row, col int)
	Undo() (row, col int, ok bool)
	Redo() (row, col int, ok bool)
	String() string
	LineCount() int
	Line(row int) string
	LineRunes(row int) []rune
	LineLen(row int) int
	Insert(row, col int, r rune)
	InsertString(row, col int, s string)
	Newline(row, col int)
	DeleteBack(row, col int) (newRow, newCol int)
	DeleteRune(row, col int) bool
	DeleteRange(r1, c1, r2, c2 int) (row, col int)
	DeleteLines(r1, r2 int)
	YankRange(r1, c1, r2, c2 int) string
	YankLines(r1, r2 int) string
	InsertLineBelow(row int) int
	InsertLineAbove(row int) int
	PasteAfter(row, col int, text string, linewise bool) (newRow, newCol int)
	PasteBefore(row, col int, text string, linewise bool) (newRow, newCol int)
}

// fileBuffer is the editor's view of an open file.
type fileBuffer struct {
	path     string
	modified bool

	table piece.Table

	// Gap buffer — active only while in insert mode.
	// hotRow/hotCol track where in the document the gap was "opened".
	gap       gap.Buffer
	hotRow    int
	hotActive bool

	// Optional Postgres-backed undo store.  nil when postgres is unavailable.
	store piece.UndoStore

	// Cursor hint recorded before a mutating operation so the snapshot
	// captures where the user was before the change.
	hintRow, hintCol int
}

// Path returns the file path.
func (b *fileBuffer) Path() string { return b.path }

// SetPath sets the file path.
func (b *fileBuffer) SetPath(path string) { b.path = path }

// Modified reports whether the buffer has unsaved changes.
func (b *fileBuffer) Modified() bool { return b.modified }

// New returns an empty buffer.
func New() Buffer {
	return &fileBuffer{table: piece.New()}
}

// Open reads a file into a new Buffer.
func Open(path string) (Buffer, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided file argument
	if err != nil {
		return nil, err
	}
	t := piece.Load([]rune(string(data)))
	return &fileBuffer{path: path, table: t}, nil
}

// OpenWithUndo is like Open but also connects a Postgres undo store.
// dsn is a libpq connection string, e.g.
// "host=localhost user=postgres dbname=editor sslmode=disable".
func OpenWithUndo(path, dsn string) (Buffer, error) {
	b, err := Open(path)
	if err != nil {
		return nil, err
	}
	fb := b.(*fileBuffer)
	store, err := piece.OpenPgStore(dsn, path, fb.table.Snapshot())
	if err != nil {
		// Log but don't fail — undo still works in-memory via the piece table.
		fmt.Fprintf(os.Stderr, "warning: undo store unavailable: %v\n", err)
		return b, nil
	}
	fb.store = store
	return fb, nil
}

// Save writes the buffer contents to disk.
func (b *fileBuffer) Save() error {
	b.FlushGap()
	if err := os.WriteFile(b.path, []byte(b.table.String()), 0o600); err != nil {
		return err
	}
	b.modified = false
	return nil
}

// HasUndoStore reports whether a Postgres undo store is connected.
func (b *fileBuffer) HasUndoStore() bool { return b.store != nil }

// Close releases any resources (e.g. the Postgres connection).
func (b *fileBuffer) Close() {
	if b.store != nil {
		_ = b.store.Close()
	}
}

// --- Gap buffer management ---

// ActivateGap loads the content of line row into the gap buffer, positioning
// the gap at col.  Call this when entering insert mode.
func (b *fileBuffer) ActivateGap(row, col int) {
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
func (b *fileBuffer) FlushGap() {
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

// SetCursorHint records the cursor position to store in the next undo snapshot.
// Call this immediately before any buffer operation that will push a snapshot.
func (b *fileBuffer) SetCursorHint(row, col int) {
	b.hintRow = row
	b.hintCol = col
}

func (b *fileBuffer) pushSnapshot() {
	if b.store != nil {
		snap := b.table.Snapshot()
		snap.CursorRow = b.hintRow
		snap.CursorCol = b.hintCol
		_ = b.store.Push(snap) // best-effort
	}
}

// --- Undo / Redo ---

// Undo restores the previous state.
// Returns the cursor position recorded at that snapshot and ok=true on success.
// Returns (0, 0, false) if nothing to undo.
func (b *fileBuffer) Undo() (row, col int, ok bool) {
	b.FlushGap()
	if b.store == nil {
		return 0, 0, false
	}
	snap, ok2 := b.store.Undo()
	if !ok2 {
		return 0, 0, false
	}
	b.table.Restore(snap)
	b.modified = true
	return snap.CursorRow, snap.CursorCol, true
}

// Redo reapplies a previously undone change.
// Returns the cursor position recorded at that snapshot and ok=true on success.
// Returns (0, 0, false) if nothing to redo.
func (b *fileBuffer) Redo() (row, col int, ok bool) {
	b.FlushGap()
	if b.store == nil {
		return 0, 0, false
	}
	snap, ok2 := b.store.Redo()
	if !ok2 {
		return 0, 0, false
	}
	b.table.Restore(snap)
	b.modified = true
	return snap.CursorRow, snap.CursorCol, true
}

// --- Read operations ---

// String returns the full document text.
func (b *fileBuffer) String() string {
	b.FlushGap()
	return b.table.String()
}

// LineCount returns the number of lines.
func (b *fileBuffer) LineCount() int {
	if b.hotActive {
		// Gap buffer doesn't change line count of this one line.
		return b.table.LineCount()
	}
	return b.table.LineCount()
}

// Line returns line row as a string (no trailing newline).
func (b *fileBuffer) Line(row int) string {
	if b.hotActive && row == b.hotRow {
		return b.gap.String()
	}
	return b.table.Line(row)
}

// LineRunes returns line row as a rune slice.
func (b *fileBuffer) LineRunes(row int) []rune {
	if b.hotActive && row == b.hotRow {
		return b.gap.Text()
	}
	return b.table.LineRunes(row)
}

// LineLen returns the number of runes on line row.
func (b *fileBuffer) LineLen(row int) int {
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
func (b *fileBuffer) Insert(row, col int, r rune) {
	if b.hotActive && row == b.hotRow {
		b.gap.MoveTo(col)
		b.gap.Insert(r)
		b.modified = true
		return
	}
	b.FlushGap()
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune{r})
	b.modified = true
}

// InsertString inserts s at (row, col).
func (b *fileBuffer) InsertString(row, col int, s string) {
	if b.hotActive && row == b.hotRow {
		b.gap.MoveTo(col)
		b.gap.InsertSlice([]rune(s))
		b.modified = true
		return
	}
	b.FlushGap()
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune(s))
	b.modified = true
}

// Newline splits the line at (row, col).
func (b *fileBuffer) Newline(row, col int) {
	b.FlushGap()
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune{'\n'})
	b.modified = true
	b.pushSnapshot()
}

// DeleteBack deletes the rune before (row, col). Returns new position.
func (b *fileBuffer) DeleteBack(row, col int) (newRow, newCol int) {
	if b.hotActive && row == b.hotRow {
		if col > 0 {
			b.gap.MoveTo(col)
			b.gap.DeleteBefore(1)
			b.modified = true
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
		b.modified = true
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
	b.modified = true
	b.pushSnapshot()
	return row - 1, prevLen
}

// DeleteRune deletes the rune at (row, col). Returns true if deleted.
func (b *fileBuffer) DeleteRune(row, col int) bool {
	if b.hotActive && row == b.hotRow {
		if col >= b.gap.Len() {
			return false
		}
		b.gap.MoveTo(col)
		b.gap.DeleteAfter(1)
		b.modified = true
		return true
	}
	b.FlushGap()
	if col >= b.table.LineLen(row) {
		return false
	}
	pos := b.table.PosToOffset(row, col)
	b.table.Delete(pos, pos+1)
	b.modified = true
	return true
}

// DeleteRange deletes from (r1,c1) inclusive to (r2,c2) exclusive.
func (b *fileBuffer) DeleteRange(r1, c1, r2, c2 int) (row, col int) {
	b.FlushGap()
	start := b.table.PosToOffset(r1, c1)
	end := b.table.PosToOffset(r2, c2)
	if start > end {
		start, end = end, start
	}
	b.table.Delete(start, end)
	b.modified = true
	b.pushSnapshot()
	return b.table.OffsetToPos(start)
}

// DeleteLines deletes lines [r1, r2] inclusive.
func (b *fileBuffer) DeleteLines(r1, r2 int) {
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
	b.modified = true
	b.pushSnapshot()
}

// YankRange returns a copy of the text from (r1,c1) to (r2,c2) exclusive.
func (b *fileBuffer) YankRange(r1, c1, r2, c2 int) string {
	b.FlushGap()
	start := b.table.PosToOffset(r1, c1)
	end := b.table.PosToOffset(r2, c2)
	if start > end {
		start, end = end, start
	}
	return string(b.table.Slice(start, end))
}

// YankLines returns lines [r1,r2] inclusive joined by newlines.
func (b *fileBuffer) YankLines(r1, r2 int) string {
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
func (b *fileBuffer) InsertLineBelow(row int) int {
	b.FlushGap()
	end := b.table.LineEnd(row)
	b.table.Insert(end, []rune{'\n'})
	b.modified = true
	return row + 1
}

// InsertLineAbove inserts a blank line before row and returns its index.
func (b *fileBuffer) InsertLineAbove(row int) int {
	b.FlushGap()
	start := b.table.LineStart(row)
	b.table.Insert(start, []rune{'\n'})
	b.modified = true
	return row
}

// PasteAfter pastes text after col (or below row if linewise).
func (b *fileBuffer) PasteAfter(row, col int, text string, linewise bool) (newRow, newCol int) {
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
		b.modified = true
		b.pushSnapshot()
		return newRow, 0
	}
	pos := b.table.PosToOffset(row, col) + 1
	b.table.Insert(pos, []rune(text))
	b.modified = true
	b.pushSnapshot()
	newRow, newCol = b.table.OffsetToPos(pos + len([]rune(text)) - 1)
	return newRow, newCol
}

// PasteBefore pastes text at col (or above row if linewise).
func (b *fileBuffer) PasteBefore(row, col int, text string, linewise bool) (newRow, newCol int) {
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
		b.modified = true
		b.pushSnapshot()
		return newRow, 0
	}
	pos := b.table.PosToOffset(row, col)
	b.table.Insert(pos, []rune(text))
	b.modified = true
	b.pushSnapshot()
	return row, col + len([]rune(text)) - 1
}
