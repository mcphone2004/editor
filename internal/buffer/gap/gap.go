// Package gap implements a gap buffer for fast cursor-local editing.
//
// A gap buffer is a contiguous rune array with a "gap" (unused space) kept at
// the cursor position. Insertions and deletions at the cursor are O(1); moving
// the cursor requires shifting the gap, which is O(distance moved).
//
// Layout:  [ text before gap | <gap> | text after gap ]
//           0            gapStart  gapEnd          len(buf)
//
// Logical text = buf[0:gapStart] + buf[gapEnd:]
package gap

const defaultGapSize = 64

// Buffer is a gap buffer operating on runes (Unicode code points).
type Buffer struct {
	buf      []rune
	gapStart int // first rune of the gap (insertion point)
	gapEnd   int // first rune after the gap
}

// New returns a gap buffer pre-loaded with the given text.
func New(text []rune) *Buffer {
	b := &Buffer{}
	b.buf = make([]rune, len(text)+defaultGapSize)
	copy(b.buf, text)
	b.gapStart = len(text)
	b.gapEnd = len(b.buf)
	return b
}

// Len returns the number of logical runes (gap not counted).
func (b *Buffer) Len() int {
	return len(b.buf) - (b.gapEnd - b.gapStart)
}

// GapStart returns the current cursor position (= gapStart in logical coords).
func (b *Buffer) GapStart() int { return b.gapStart }

// Rune returns the rune at logical position i.
func (b *Buffer) Rune(i int) rune {
	if i < b.gapStart {
		return b.buf[i]
	}
	return b.buf[b.gapEnd+(i-b.gapStart)]
}

// Text returns all logical runes as a new slice.
func (b *Buffer) Text() []rune {
	out := make([]rune, b.Len())
	copy(out, b.buf[:b.gapStart])
	copy(out[b.gapStart:], b.buf[b.gapEnd:])
	return out
}

// String returns the logical contents as a string.
func (b *Buffer) String() string {
	return string(b.Text())
}

// MoveTo moves the gap (cursor) to logical position pos.
func (b *Buffer) MoveTo(pos int) {
	if pos == b.gapStart {
		return
	}
	if pos < b.gapStart {
		// Shift text rightward into the gap.
		n := b.gapStart - pos
		copy(b.buf[b.gapEnd-n:], b.buf[pos:b.gapStart])
		b.gapStart -= n
		b.gapEnd -= n
	} else {
		// Shift text leftward into the gap.
		n := pos - b.gapStart
		copy(b.buf[b.gapStart:], b.buf[b.gapEnd:b.gapEnd+n])
		b.gapStart += n
		b.gapEnd += n
	}
}

// Insert inserts rune r at the current cursor position and advances it.
func (b *Buffer) Insert(r rune) {
	b.grow(1)
	b.buf[b.gapStart] = r
	b.gapStart++
}

// InsertAt inserts r at logical position pos.
func (b *Buffer) InsertAt(pos int, r rune) {
	b.MoveTo(pos)
	b.Insert(r)
}

// InsertSlice inserts all runes at the current cursor position.
func (b *Buffer) InsertSlice(runes []rune) {
	b.grow(len(runes))
	copy(b.buf[b.gapStart:], runes)
	b.gapStart += len(runes)
}

// DeleteBefore deletes n runes before the cursor (backspace).
func (b *Buffer) DeleteBefore(n int) {
	if n > b.gapStart {
		n = b.gapStart
	}
	b.gapStart -= n
}

// DeleteAfter deletes n runes after the cursor (delete key).
func (b *Buffer) DeleteAfter(n int) {
	avail := len(b.buf) - b.gapEnd
	if n > avail {
		n = avail
	}
	b.gapEnd += n
}

// DeleteRange deletes logical runes [start, end).
func (b *Buffer) DeleteRange(start, end int) {
	if start >= end {
		return
	}
	b.MoveTo(start)
	b.DeleteAfter(end - start)
}

// Slice returns a copy of logical runes [start, end).
func (b *Buffer) Slice(start, end int) []rune {
	if start >= end {
		return nil
	}
	out := make([]rune, end-start)
	for i := start; i < end; i++ {
		out[i-start] = b.Rune(i)
	}
	return out
}

// grow ensures the gap is at least need runes wide.
func (b *Buffer) grow(need int) {
	avail := b.gapEnd - b.gapStart
	if avail >= need {
		return
	}
	// Allocate a new backing array with a fresh gap.
	newGap := need + defaultGapSize
	newBuf := make([]rune, len(b.buf)+newGap-avail)
	copy(newBuf, b.buf[:b.gapStart])
	copy(newBuf[b.gapStart+newGap:], b.buf[b.gapEnd:])
	b.gapEnd = b.gapStart + newGap
	b.buf = newBuf
}
