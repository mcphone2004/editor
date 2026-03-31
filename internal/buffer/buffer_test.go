package buffer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthonybrice/editor/internal/buffer"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- helpers ---

func newBuf(t *testing.T, content string) *buffer.Buffer {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "buf_test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	buf, err := buffer.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

// --- Open / Save ---

func TestOpen_nonexistent(t *testing.T) {
	_, err := buffer.Open("/nonexistent/path/x.go")
	if err == nil {
		t.Fatal("expected error opening non-existent file")
	}
}

func TestOpen_readsContent(t *testing.T) {
	buf := newBuf(t, "hello\nworld")
	if buf.Line(0) != "hello" {
		t.Errorf("Line(0) = %q", buf.Line(0))
	}
	if buf.Line(1) != "world" {
		t.Errorf("Line(1) = %q", buf.Line(1))
	}
}

func TestSave_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	buf := buffer.New()
	buf.Path = path
	buf.InsertString(0, 0, "package main")
	if err := buf.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main" {
		t.Fatalf("saved %q", string(data))
	}
	if buf.Modified {
		t.Fatal("Modified should be false after save")
	}
}

// --- Line access ---

func TestLineCount(t *testing.T) {
	buf := newBuf(t, "a\nb\nc")
	if buf.LineCount() != 3 {
		t.Fatalf("expected 3, got %d", buf.LineCount())
	}
}

func TestLine(t *testing.T) {
	buf := newBuf(t, "foo\nbar")
	if buf.Line(0) != "foo" {
		t.Errorf("Line(0) = %q", buf.Line(0))
	}
	if buf.Line(1) != "bar" {
		t.Errorf("Line(1) = %q", buf.Line(1))
	}
}

func TestLineRunes(t *testing.T) {
	buf := newBuf(t, "hello")
	if string(buf.LineRunes(0)) != "hello" {
		t.Fatalf("got %q", string(buf.LineRunes(0)))
	}
}

func TestLineLen(t *testing.T) {
	buf := newBuf(t, "hello\nworld")
	if buf.LineLen(0) != 5 {
		t.Errorf("LineLen(0) = %d", buf.LineLen(0))
	}
	if buf.LineLen(1) != 5 {
		t.Errorf("LineLen(1) = %d", buf.LineLen(1))
	}
}

// --- Insert ---

func TestInsert_singleRune(t *testing.T) {
	buf := newBuf(t, "hllo")
	buf.Insert(0, 1, 'e')
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

func TestInsert_setsModified(t *testing.T) {
	buf := newBuf(t, "x")
	buf.Insert(0, 0, 'y')
	if !buf.Modified {
		t.Fatal("expected Modified=true")
	}
}

func TestInsertString(t *testing.T) {
	buf := newBuf(t, "")
	buf.InsertString(0, 0, "hello")
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

// --- Newline ---

func TestNewline_splitsLine(t *testing.T) {
	buf := newBuf(t, "helloworld")
	buf.Newline(0, 5)
	if buf.LineCount() != 2 {
		t.Fatalf("expected 2, got %d", buf.LineCount())
	}
	if buf.Line(0) != "hello" {
		t.Errorf("line 0 = %q", buf.Line(0))
	}
	if buf.Line(1) != "world" {
		t.Errorf("line 1 = %q", buf.Line(1))
	}
}

// --- DeleteBack ---

func TestDeleteBack_inLine(t *testing.T) {
	buf := newBuf(t, "hello")
	row, col := buf.DeleteBack(0, 5)
	if row != 0 || col != 4 {
		t.Errorf("cursor = (%d,%d), want (0,4)", row, col)
	}
	if buf.Line(0) != "hell" {
		t.Errorf("line = %q", buf.Line(0))
	}
}

func TestDeleteBack_mergesLines(t *testing.T) {
	buf := newBuf(t, "foo\nbar")
	row, col := buf.DeleteBack(1, 0)
	if row != 0 || col != 3 {
		t.Errorf("cursor = (%d,%d), want (0,3)", row, col)
	}
	if buf.LineCount() != 1 {
		t.Fatalf("expected 1 line, got %d", buf.LineCount())
	}
	if buf.Line(0) != "foobar" {
		t.Errorf("line = %q", buf.Line(0))
	}
}

func TestDeleteBack_atOrigin(t *testing.T) {
	buf := newBuf(t, "x")
	row, col := buf.DeleteBack(0, 0)
	if row != 0 || col != 0 {
		t.Errorf("cursor = (%d,%d), want (0,0)", row, col)
	}
}

// --- DeleteRune ---

func TestDeleteRune_basic(t *testing.T) {
	buf := newBuf(t, "hello")
	ok := buf.DeleteRune(0, 0)
	if !ok {
		t.Fatal("expected true")
	}
	if buf.Line(0) != "ello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

func TestDeleteRune_outOfBounds(t *testing.T) {
	buf := newBuf(t, "hi")
	if buf.DeleteRune(0, 99) {
		t.Fatal("expected false for out-of-bounds col")
	}
}

// --- DeleteRange ---

func TestDeleteRange_sameLine(t *testing.T) {
	buf := newBuf(t, "hello world")
	buf.DeleteRange(0, 5, 0, 11)
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

func TestDeleteRange_multiLine(t *testing.T) {
	buf := newBuf(t, "abc\ndef\nghi")
	buf.DeleteRange(0, 2, 1, 2)
	// Deletes from c through de, leaving "abf\nghi"
	if buf.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", buf.LineCount())
	}
}

// --- DeleteLines ---

func TestDeleteLines_single(t *testing.T) {
	buf := newBuf(t, "a\nb\nc")
	buf.DeleteLines(1, 1)
	if buf.LineCount() != 2 {
		t.Fatalf("expected 2, got %d", buf.LineCount())
	}
	if buf.Line(1) != "c" {
		t.Errorf("line 1 = %q", buf.Line(1))
	}
}

func TestDeleteLines_all(t *testing.T) {
	buf := newBuf(t, "a\nb\nc")
	buf.DeleteLines(0, 2)
	// Should not panic; at least one empty line remains.
	if buf.LineCount() < 1 {
		t.Fatal("expected at least 1 line")
	}
}

// --- Yank ---

func TestYankRange(t *testing.T) {
	buf := newBuf(t, "hello world")
	got := buf.YankRange(0, 6, 0, 11)
	if got != "world" {
		t.Fatalf("got %q", got)
	}
}

func TestYankLines(t *testing.T) {
	buf := newBuf(t, "foo\nbar\nbaz")
	got := buf.YankLines(0, 1)
	if got != "foo\nbar" {
		t.Fatalf("got %q", got)
	}
}

// --- InsertLine ---

func TestInsertLineBelow(t *testing.T) {
	buf := newBuf(t, "a\nb")
	newRow := buf.InsertLineBelow(0)
	if newRow != 1 {
		t.Fatalf("expected row 1, got %d", newRow)
	}
	if buf.LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", buf.LineCount())
	}
	if buf.Line(1) != "" {
		t.Errorf("new line = %q, want empty", buf.Line(1))
	}
}

func TestInsertLineAbove(t *testing.T) {
	buf := newBuf(t, "a\nb")
	newRow := buf.InsertLineAbove(1)
	if newRow != 1 {
		t.Fatalf("expected row 1, got %d", newRow)
	}
	if buf.LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", buf.LineCount())
	}
}

// --- Paste ---

func TestPasteAfter_charwise(t *testing.T) {
	buf := newBuf(t, "helo")
	buf.PasteAfter(0, 2, "l", false)
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

func TestPasteBefore_charwise(t *testing.T) {
	buf := newBuf(t, "ello")
	buf.PasteBefore(0, 0, "h", false)
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

func TestPasteAfter_linewise(t *testing.T) {
	buf := newBuf(t, "a\nc")
	buf.PasteAfter(0, 0, "b", true)
	if buf.LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", buf.LineCount())
	}
	if buf.Line(1) != "b" {
		t.Errorf("line 1 = %q", buf.Line(1))
	}
}

// --- Gap buffer integration ---

func TestActivateGap_insertsViaGap(t *testing.T) {
	buf := newBuf(t, "hllo")
	buf.ActivateGap(0, 1) // gap at col 1
	buf.Insert(0, 1, 'e') // should go into gap
	buf.FlushGap()
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

func TestFlushGap_idempotent(t *testing.T) {
	buf := newBuf(t, "hello")
	buf.FlushGap() // no-op when no gap active
	buf.FlushGap() // should not panic
	if buf.Line(0) != "hello" {
		t.Fatalf("got %q", buf.Line(0))
	}
}

// --- String ---

func TestString(t *testing.T) {
	buf := newBuf(t, "hello\nworld")
	if buf.String() != "hello\nworld" {
		t.Fatalf("got %q", buf.String())
	}
}
