// Package piece implements a piece table — a data structure for text editing
// that stores the document as an ordered sequence of "pieces", each pointing
// into one of two immutable buffers:
//
//   - original: the file contents at open time (never modified)
//   - add:      an append-only buffer for new text
//
// Edits create, split, or remove pieces without ever copying the underlying
// text. This makes insertions and deletions O(pieces) rather than O(bytes),
// and makes undo free — just restore a previous piece sequence.
//
// Line-based access is provided via a cached line table (slice of document
// offsets for each line start). The cache is rebuilt lazily when dirty.
package piece

import (
	"strings"
)

// which buffer a piece references.
const (
	bufOriginal = 0
	bufAdd      = 1
)

// Piece is a contiguous span of text in one of the two backing buffers.
type Piece struct {
	Which  int // bufOriginal or bufAdd
	Start  int // rune offset in the backing buffer
	Length int // number of runes
}

// Snapshot captures a complete piece table state for undo/redo.
type Snapshot struct {
	Pieces    []Piece
	AddLen    int // how many runes of the add buffer are in use
	CursorRow int
	CursorCol int
}

// Table is the interface implemented by the piece table document.
type Table interface {
	Len() int
	LineCount() int
	LineStart(row int) int
	LineEnd(row int) int
	LineLen(row int) int
	Line(row int) string
	LineRunes(row int) []rune
	PosToOffset(row, col int) int
	OffsetToPos(offset int) (row, col int)
	Slice(start, end int) []rune
	String() string
	Insert(pos int, text []rune)
	Delete(start, end int)
	InsertLine(row int, text string)
	Lines() []string
	Snapshot() Snapshot
	Restore(s Snapshot)
}

// table is the concrete piece table document.
type table struct {
	original []rune
	add      []rune

	pieces []Piece

	// Line cache: lineStarts[i] = document rune offset of line i.
	// Rebuilt lazily when lineDirty == true.
	lineStarts []int
	lineDirty  bool
}

// New returns an empty Table.
func New() Table {
	t := &table{pieces: []Piece{}}
	t.lineDirty = true
	return t
}

// Load initialises a Table with existing content (e.g. a file).
func Load(content []rune) Table {
	t := &table{
		original: content,
		add:      make([]rune, 0, 4096),
	}
	if len(content) > 0 {
		t.pieces = []Piece{{Which: bufOriginal, Start: 0, Length: len(content)}}
	}
	t.lineDirty = true
	return t
}

// Snapshot returns the current piece sequence and add-buffer length.
func (t *table) Snapshot() Snapshot {
	pieces := make([]Piece, len(t.pieces))
	copy(pieces, t.pieces)
	return Snapshot{Pieces: pieces, AddLen: len(t.add)}
}

// Restore replaces the piece sequence with a previously saved snapshot.
func (t *table) Restore(s Snapshot) {
	t.pieces = make([]Piece, len(s.Pieces))
	copy(t.pieces, s.Pieces)
	t.add = t.add[:s.AddLen]
	t.lineDirty = true
}

// Len returns the total number of runes in the document.
func (t *table) Len() int {
	n := 0
	for _, p := range t.pieces {
		n += p.Length
	}
	return n
}

// LineCount returns the number of lines (always >= 1).
func (t *table) LineCount() int {
	t.rebuildLines()
	return len(t.lineStarts)
}

// LineStart returns the document rune offset for the beginning of line row.
func (t *table) LineStart(row int) int {
	t.rebuildLines()
	if row < 0 {
		return 0
	}
	if row >= len(t.lineStarts) {
		return t.Len()
	}
	return t.lineStarts[row]
}

// LineEnd returns the document rune offset of the last rune on line row
// (the position of the '\n', or end-of-document if the last line has none).
func (t *table) LineEnd(row int) int {
	t.rebuildLines()
	if row+1 < len(t.lineStarts) {
		return t.lineStarts[row+1] - 1 // position of '\n'
	}
	return t.Len()
}

// LineLen returns the number of runes on line row, excluding the newline.
func (t *table) LineLen(row int) int {
	return t.LineEnd(row) - t.LineStart(row)
}

// Line returns the text of line row as a string (no trailing newline).
func (t *table) Line(row int) string {
	start := t.LineStart(row)
	end := t.LineEnd(row)
	if start >= end {
		return ""
	}
	return string(t.Slice(start, end))
}

// LineRunes returns the runes of line row (no trailing newline).
func (t *table) LineRunes(row int) []rune {
	start := t.LineStart(row)
	end := t.LineEnd(row)
	if start >= end {
		return []rune{}
	}
	return t.Slice(start, end)
}

// PosToOffset converts (row, col) to a document rune offset.
func (t *table) PosToOffset(row, col int) int {
	return t.LineStart(row) + col
}

// OffsetToPos converts a document offset to (row, col).
func (t *table) OffsetToPos(offset int) (row, col int) {
	t.rebuildLines()
	// Binary search for the line.
	lo, hi := 0, len(t.lineStarts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if t.lineStarts[mid] <= offset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, offset - t.lineStarts[lo]
}

// Slice returns a copy of document runes [start, end).
func (t *table) Slice(start, end int) []rune {
	if start >= end {
		return nil
	}
	out := make([]rune, 0, end-start)
	pos := 0
	for _, p := range t.pieces {
		pEnd := pos + p.Length
		if pEnd <= start {
			pos = pEnd
			continue
		}
		if pos >= end {
			break
		}
		// This piece overlaps [start, end).
		pStart := start - pos
		if pStart < 0 {
			pStart = 0
		}
		pStop := end - pos
		if pStop > p.Length {
			pStop = p.Length
		}
		out = append(out, t.buf(p)[p.Start+pStart:p.Start+pStop]...)
		pos = pEnd
	}
	return out
}

// String returns the full document as a string.
func (t *table) String() string {
	return string(t.Slice(0, t.Len()))
}

// Insert inserts text at document rune offset pos.
func (t *table) Insert(pos int, text []rune) {
	if len(text) == 0 {
		return
	}
	// Append to the add buffer.
	addStart := len(t.add)
	t.add = append(t.add, text...)
	newPiece := Piece{Which: bufAdd, Start: addStart, Length: len(text)}

	idx, offset := t.findPiece(pos)
	switch {
	case idx == len(t.pieces):
		// Append at end.
		t.pieces = append(t.pieces, newPiece)
	case offset == 0:
		// Insert before piece idx.
		t.pieces = insertPiece(t.pieces, idx, newPiece)
	default:
		// Split piece idx at offset.
		left, right := t.splitPiece(idx, offset)
		t.pieces = spliceIn(t.pieces, idx, left, newPiece, right)
	}
	t.lineDirty = true
}

// Delete removes document runes [start, end).
func (t *table) Delete(start, end int) {
	if start >= end {
		return
	}
	startIdx, startOff := t.findPiece(start)
	endIdx, endOff := t.findPiece(end)

	var newPieces []Piece

	// Keep prefix of the start piece (if any).
	if startOff > 0 {
		p := t.pieces[startIdx]
		newPieces = append(newPieces, Piece{p.Which, p.Start, startOff})
	}

	// Keep suffix of the end piece (if any).
	if endIdx < len(t.pieces) && endOff < t.pieces[endIdx].Length {
		p := t.pieces[endIdx]
		newPieces = append(newPieces, Piece{p.Which, p.Start + endOff, p.Length - endOff})
	}

	// Rebuild: pieces before startIdx + newPieces + pieces after endIdx.
	result := make([]Piece, 0, len(t.pieces))
	result = append(result, t.pieces[:startIdx]...)
	result = append(result, newPieces...)
	if endIdx+1 <= len(t.pieces) {
		result = append(result, t.pieces[endIdx+1:]...)
	}
	t.pieces = result
	t.lineDirty = true
}

// InsertLine inserts text as a new line after row (before newline of row).
// The text should NOT include a trailing newline; one is added automatically.
func (t *table) InsertLine(row int, text string) {
	var pos int
	switch {
	case row < 0:
		pos = 0
	case row >= t.LineCount():
		pos = t.Len()
		// Ensure the document ends with a newline before appending.
		if pos > 0 {
			last := t.Slice(pos-1, pos)
			if len(last) == 0 || last[0] != '\n' {
				t.Insert(pos, []rune{'\n'})
				pos++
			}
		}
	default:
		// Insert before the newline at end of row (i.e. at LineEnd(row)).
		pos = t.LineEnd(row)
		t.Insert(pos, []rune("\n"+text))
		t.lineDirty = true
		return
	}
	t.Insert(pos, []rune(text+"\n"))
	t.lineDirty = true
}

// Lines returns all lines as a string slice (no trailing newlines).
func (t *table) Lines() []string {
	full := t.String()
	parts := strings.Split(full, "\n")
	return parts
}

// --- internal helpers ---

// buf returns the backing rune slice for piece p.
func (t *table) buf(p Piece) []rune {
	if p.Which == bufOriginal {
		return t.original
	}
	return t.add
}

// findPiece returns the piece index and offset within that piece that
// corresponds to document rune position pos.
// Returns (len(pieces), 0) if pos is at or past the end.
func (t *table) findPiece(pos int) (idx, offset int) {
	cur := 0
	for i, p := range t.pieces {
		if cur+p.Length > pos {
			return i, pos - cur
		}
		cur += p.Length
	}
	return len(t.pieces), 0
}

// splitPiece splits pieces[idx] at offset, returning the two halves.
func (t *table) splitPiece(idx, offset int) (left, right Piece) {
	p := t.pieces[idx]
	left = Piece{p.Which, p.Start, offset}
	right = Piece{p.Which, p.Start + offset, p.Length - offset}
	return left, right
}

// rebuildLines scans the pieces and rebuilds lineStarts.
func (t *table) rebuildLines() {
	if !t.lineDirty {
		return
	}
	t.lineStarts = t.lineStarts[:0]
	t.lineStarts = append(t.lineStarts, 0)
	pos := 0
	for _, p := range t.pieces {
		src := t.buf(p)[p.Start : p.Start+p.Length]
		for _, r := range src {
			pos++
			if r == '\n' {
				t.lineStarts = append(t.lineStarts, pos)
			}
		}
	}
	// If the document ends with '\n', the last entry in lineStarts is a
	// phantom empty line — keep it (vim behaviour: trailing newline = last
	// line is empty).
	t.lineDirty = false
}

// --- slice helpers ---

func insertPiece(s []Piece, i int, p Piece) []Piece {
	s = append(s, Piece{})
	copy(s[i+1:], s[i:])
	s[i] = p
	return s
}

func spliceIn(s []Piece, i int, pieces ...Piece) []Piece {
	tail := make([]Piece, len(s)-i-1)
	copy(tail, s[i+1:])
	s = append(s[:i], pieces...)
	s = append(s, tail...)
	return s
}
