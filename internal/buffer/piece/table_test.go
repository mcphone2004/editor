package piece_test

import (
	"strings"
	"testing"

	"github.com/anthonybrice/editor/internal/buffer/piece"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- helpers ---

func load(t *testing.T, content string) *piece.Table {
	t.Helper()
	return piece.Load([]rune(content))
}

// --- Table.Load / Table.String ---

func TestLoad_empty(t *testing.T) {
	tab := piece.New()
	if tab.Len() != 0 {
		t.Fatalf("expected len 0, got %d", tab.Len())
	}
	if tab.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", tab.LineCount())
	}
}

func TestLoad_singleLine(t *testing.T) {
	tab := load(t, "hello")
	if tab.String() != "hello" {
		t.Fatalf("got %q", tab.String())
	}
	if tab.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", tab.LineCount())
	}
}

func TestLoad_multiLine(t *testing.T) {
	tab := load(t, "foo\nbar\nbaz")
	if tab.LineCount() != 3 {
		t.Fatalf("expected 3, got %d", tab.LineCount())
	}
	for i, want := range []string{"foo", "bar", "baz"} {
		if got := tab.Line(i); got != want {
			t.Errorf("Line(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestLoad_trailingNewline(t *testing.T) {
	// A file ending in \n has an empty final line.
	tab := load(t, "foo\nbar\n")
	if tab.LineCount() != 3 {
		t.Fatalf("expected 3 lines (including empty final), got %d", tab.LineCount())
	}
	if tab.Line(2) != "" {
		t.Fatalf("expected empty last line, got %q", tab.Line(2))
	}
}

// --- Insert ---

func TestInsert_atStart(t *testing.T) {
	tab := load(t, "ello")
	tab.Insert(0, []rune("h"))
	if tab.String() != "hello" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestInsert_atEnd(t *testing.T) {
	tab := load(t, "hell")
	tab.Insert(4, []rune("o"))
	if tab.String() != "hello" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestInsert_inMiddle(t *testing.T) {
	tab := load(t, "hllo")
	tab.Insert(1, []rune("e"))
	if tab.String() != "hello" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestInsert_newline(t *testing.T) {
	tab := load(t, "helloworld")
	tab.Insert(5, []rune("\n"))
	if tab.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", tab.LineCount())
	}
	if tab.Line(0) != "hello" {
		t.Fatalf("line 0 = %q", tab.Line(0))
	}
	if tab.Line(1) != "world" {
		t.Fatalf("line 1 = %q", tab.Line(1))
	}
}

func TestInsert_empty(t *testing.T) {
	tab := load(t, "hello")
	tab.Insert(2, []rune{}) // no-op
	if tab.String() != "hello" {
		t.Fatalf("got %q", tab.String())
	}
}

// --- Delete ---

func TestDelete_singleChar(t *testing.T) {
	tab := load(t, "hello")
	tab.Delete(1, 2) // remove 'e'
	if tab.String() != "hllo" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestDelete_wholeContent(t *testing.T) {
	tab := load(t, "hello")
	tab.Delete(0, 5)
	if tab.String() != "" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestDelete_newline(t *testing.T) {
	tab := load(t, "foo\nbar")
	// Delete the newline at position 3 to merge lines.
	tab.Delete(3, 4)
	if tab.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", tab.LineCount())
	}
	if tab.String() != "foobar" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestDelete_range_multiLine(t *testing.T) {
	tab := load(t, "abc\ndef\nghi")
	// Delete from offset 2 (c) to offset 8 (f\n) → "abghi"
	tab.Delete(2, 8)
	if tab.String() != "abghi" {
		t.Fatalf("got %q", tab.String())
	}
}

func TestDelete_noop(t *testing.T) {
	tab := load(t, "hello")
	tab.Delete(2, 2) // empty range
	if tab.String() != "hello" {
		t.Fatalf("got %q", tab.String())
	}
}

// --- Slice ---

func TestSlice(t *testing.T) {
	tab := load(t, "hello world")
	got := string(tab.Slice(6, 11))
	if got != "world" {
		t.Fatalf("got %q", got)
	}
}

func TestSlice_empty(t *testing.T) {
	tab := load(t, "hello")
	got := tab.Slice(2, 2)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %q", string(got))
	}
}

// --- Line helpers ---

func TestLineLen(t *testing.T) {
	tab := load(t, "foo\nba\n")
	if tab.LineLen(0) != 3 {
		t.Errorf("line 0 len = %d, want 3", tab.LineLen(0))
	}
	if tab.LineLen(1) != 2 {
		t.Errorf("line 1 len = %d, want 2", tab.LineLen(1))
	}
	if tab.LineLen(2) != 0 {
		t.Errorf("line 2 len = %d, want 0", tab.LineLen(2))
	}
}

func TestLineRunes(t *testing.T) {
	tab := load(t, "hello\nworld")
	r := tab.LineRunes(1)
	if string(r) != "world" {
		t.Fatalf("got %q", string(r))
	}
}

func TestPosToOffset_and_back(t *testing.T) {
	tab := load(t, "foo\nbar\nbaz")
	// (1, 2) → offset 6, back → (1, 2)
	off := tab.PosToOffset(1, 2)
	if off != 6 {
		t.Fatalf("expected offset 6, got %d", off)
	}
	row, col := tab.OffsetToPos(6)
	if row != 1 || col != 2 {
		t.Fatalf("expected (1,2), got (%d,%d)", row, col)
	}
}

// --- Lines ---

func TestLines(t *testing.T) {
	tab := load(t, "a\nb\nc")
	lines := tab.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3, got %d", len(lines))
	}
	for i, want := range []string{"a", "b", "c"} {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

// --- Snapshot / Restore ---

func TestSnapshot_restore(t *testing.T) {
	tab := load(t, "hello")
	snap := tab.Snapshot()
	tab.Insert(5, []rune(" world"))
	if tab.String() != "hello world" {
		t.Fatalf("after insert: %q", tab.String())
	}
	tab.Restore(snap)
	if tab.String() != "hello" {
		t.Fatalf("after restore: %q", tab.String())
	}
}

// --- InsertLine ---

func TestInsertLine(t *testing.T) {
	tab := load(t, "line0\nline2")
	tab.InsertLine(0, "line1")
	lines := tab.Lines()
	if lines[1] != "line1" {
		t.Fatalf("expected line1, got %q", lines[1])
	}
	if lines[2] != "line2" {
		t.Fatalf("expected line2, got %q", lines[2])
	}
}

// --- stress: many alternating inserts and deletes ---

func TestStress_insertDelete(t *testing.T) {
	tab := piece.New()
	var want strings.Builder
	for i := range 50 {
		ch := rune('a' + i%26)
		tab.Insert(tab.Len(), []rune{ch})
		want.WriteRune(ch)
	}
	if tab.String() != want.String() {
		t.Fatalf("after inserts: got %q, want %q", tab.String(), want.String())
	}
	// Delete every other character.
	i := 0
	for tab.Len() > 0 {
		tab.Delete(i, i+1)
		i++
		if i >= tab.Len() {
			break
		}
	}
	// Just assert no panic and length makes sense.
	if tab.Len() < 0 {
		t.Fatal("negative length")
	}
}
