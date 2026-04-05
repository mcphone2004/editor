package editor_test

import (
	"os"
	"testing"

	"github.com/anthonybrice/editor/buffer"
	"github.com/anthonybrice/editor/editor"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- helpers ---

func newEditor(t *testing.T, content string) editor.Editor {
	t.Helper()
	buf := buffer.New()
	if content != "" {
		buf.InsertString(0, 0, content)
	}
	return editor.New(buf)
}

func typeKeys(e editor.Editor, keys ...string) {
	for _, k := range keys {
		e.HandleKey(k)
	}
}

func line(e editor.Editor, row int) string {
	return e.Buf().Line(row)
}

// --- Mode transitions ---

func TestMode_default_isNormal(t *testing.T) {
	e := newEditor(t, "")
	if e.Mode() != editor.ModeNormal {
		t.Fatalf("expected NORMAL, got %s", e.Mode())
	}
}

func TestMode_i_entersInsert(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "i")
	if e.Mode() != editor.ModeInsert {
		t.Fatalf("expected INSERT, got %s", e.Mode())
	}
}

func TestMode_esc_returnsNormal(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "i", "<Esc>")
	if e.Mode() != editor.ModeNormal {
		t.Fatalf("expected NORMAL, got %s", e.Mode())
	}
}

func TestMode_v_entersVisual(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "v")
	if e.Mode() != editor.ModeVisual {
		t.Fatalf("expected VISUAL, got %s", e.Mode())
	}
}

func TestMode_V_entersVisualLine(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "V")
	if e.Mode() != editor.ModeVisualLine {
		t.Fatalf("expected V-LINE, got %s", e.Mode())
	}
}

func TestMode_colon_entersCommand(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, ":")
	if e.Mode() != editor.ModeCommand {
		t.Fatalf("expected COMMAND, got %s", e.Mode())
	}
}

// --- Basic cursor movement ---

func TestMotion_hjkl(t *testing.T) {
	e := newEditor(t, "ab\ncd")
	// Move right.
	typeKeys(e, "l")
	if e.Cursor().Col != 1 {
		t.Errorf("after l: col=%d, want 1", e.Cursor().Col)
	}
	// Move down.
	typeKeys(e, "j")
	if e.Cursor().Row != 1 {
		t.Errorf("after j: row=%d, want 1", e.Cursor().Row)
	}
	// Move left.
	typeKeys(e, "h")
	if e.Cursor().Col != 0 {
		t.Errorf("after h: col=%d, want 0", e.Cursor().Col)
	}
	// Move up.
	typeKeys(e, "k")
	if e.Cursor().Row != 0 {
		t.Errorf("after k: row=%d, want 0", e.Cursor().Row)
	}
}

func TestMotion_h_clamps_at_start(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "h")
	if e.Cursor().Col != 0 {
		t.Errorf("expected col 0, got %d", e.Cursor().Col)
	}
}

func TestMotion_l_clamps_at_end(t *testing.T) {
	e := newEditor(t, "hi")
	typeKeys(e, "l", "l", "l") // can't go past end in normal mode
	if e.Cursor().Col > 1 {
		t.Errorf("col should not exceed line length-1, got %d", e.Cursor().Col)
	}
}

func TestMotion_dollar_goesToEnd(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "$")
	if e.Cursor().Col != 4 {
		t.Errorf("expected col 4, got %d", e.Cursor().Col)
	}
}

func TestMotion_zero_goesToStart(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "$", "0")
	if e.Cursor().Col != 0 {
		t.Errorf("expected col 0, got %d", e.Cursor().Col)
	}
}

func TestMotion_gg_goesToTop(t *testing.T) {
	e := newEditor(t, "a\nb\nc")
	typeKeys(e, "j", "j", "g", "g")
	if e.Cursor().Row != 0 {
		t.Errorf("expected row 0, got %d", e.Cursor().Row)
	}
}

func TestMotion_G_goesToBottom(t *testing.T) {
	e := newEditor(t, "a\nb\nc")
	typeKeys(e, "G")
	if e.Cursor().Row != 2 {
		t.Errorf("expected row 2, got %d", e.Cursor().Row)
	}
}

func TestMotion_w_movesForwardWord(t *testing.T) {
	e := newEditor(t, "hello world")
	typeKeys(e, "w")
	if e.Cursor().Col != 6 {
		t.Errorf("expected col 6, got %d", e.Cursor().Col)
	}
}

func TestMotion_b_movesBackWord(t *testing.T) {
	e := newEditor(t, "hello world")
	typeKeys(e, "w", "b")
	if e.Cursor().Col != 0 {
		t.Errorf("expected col 0, got %d", e.Cursor().Col)
	}
}

func TestMotion_count_prefix(t *testing.T) {
	e := newEditor(t, "abcde")
	typeKeys(e, "3", "l")
	if e.Cursor().Col != 3 {
		t.Errorf("3l: expected col 3, got %d", e.Cursor().Col)
	}
}

// --- Insert mode typing ---

func TestInsert_typesText(t *testing.T) {
	e := newEditor(t, "")
	typeKeys(e, "i", "h", "e", "l", "l", "o")
	e.Buf().FlushGap()
	if line(e, 0) != "hello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

func TestInsert_enter_splitsLine(t *testing.T) {
	e := newEditor(t, "helloworld")
	// Move to col 5 and press enter.
	typeKeys(e, "5", "l", "i", "<Enter>")
	e.Buf().FlushGap()
	if e.Buf().LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", e.Buf().LineCount())
	}
}

func TestInsert_backspace(t *testing.T) {
	e := newEditor(t, "helo")
	typeKeys(e, "A", "<Backspace>", "<Backspace>", "l", "l", "o")
	e.Buf().FlushGap()
	if line(e, 0) != "hello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

func TestInsert_a_appendsAfter(t *testing.T) {
	e := newEditor(t, "hell")
	typeKeys(e, "$", "a", "o")
	e.Buf().FlushGap()
	if line(e, 0) != "hello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

func TestInsert_A_appendsAtEOL(t *testing.T) {
	e := newEditor(t, "hell")
	typeKeys(e, "A", "o")
	e.Buf().FlushGap()
	if line(e, 0) != "hello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

func TestInsert_o_opensLineBelow(t *testing.T) {
	e := newEditor(t, "foo\nbaz")
	typeKeys(e, "o", "b", "a", "r")
	e.Buf().FlushGap()
	if e.Buf().LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", e.Buf().LineCount())
	}
	if line(e, 1) != "bar" {
		t.Errorf("line 1 = %q", line(e, 1))
	}
}

func TestInsert_O_opensLineAbove(t *testing.T) {
	e := newEditor(t, "foo\nbaz")
	typeKeys(e, "j", "O", "b", "a", "r")
	e.Buf().FlushGap()
	if e.Buf().LineCount() != 3 {
		t.Fatalf("expected 3 lines, got %d", e.Buf().LineCount())
	}
	if line(e, 1) != "bar" {
		t.Errorf("line 1 = %q", line(e, 1))
	}
}

// --- Delete operator ---

func TestOperator_x_deletesChar(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "x")
	if line(e, 0) != "ello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

func TestOperator_dd_deletesLine(t *testing.T) {
	e := newEditor(t, "foo\nbar\nbaz")
	typeKeys(e, "j", "d", "d")
	if e.Buf().LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", e.Buf().LineCount())
	}
	if line(e, 1) != "baz" {
		t.Errorf("line 1 = %q", line(e, 1))
	}
}

func TestOperator_dw_deletesWord(t *testing.T) {
	e := newEditor(t, "hello world")
	typeKeys(e, "d", "w")
	// "world" should remain (cursor was at start of "hello ")
	if line(e, 0) != "world" {
		t.Fatalf("got %q", line(e, 0))
	}
}

func TestOperator_yy_yanksLine(t *testing.T) {
	e := newEditor(t, "foo\nbar")
	typeKeys(e, "y", "y")
	// Yank should not modify buffer.
	if line(e, 0) != "foo" {
		t.Fatalf("buffer modified: %q", line(e, 0))
	}
	// Paste below.
	typeKeys(e, "p")
	if e.Buf().LineCount() != 3 {
		t.Fatalf("expected 3 lines after paste, got %d", e.Buf().LineCount())
	}
}

func TestOperator_p_pastesAfter(t *testing.T) {
	e := newEditor(t, "ac")
	typeKeys(e, "y", "y") // yank line
	// type "i" then insert "b" then esc
	typeKeys(e, "0", "l", "i", "b")
	e.Buf().FlushGap()
	if line(e, 0) != "abc" {
		t.Fatalf("got %q", line(e, 0))
	}
}

// --- r replace ---

func TestReplace_r(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "r", "H")
	if line(e, 0) != "Hello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

// --- ~ toggle case ---

func TestToggleCase(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "~")
	if line(e, 0) != "Hello" {
		t.Fatalf("got %q", line(e, 0))
	}
}

// --- Visual mode delete ---

func TestVisual_d_deletesSelection(t *testing.T) {
	e := newEditor(t, "hello world")
	typeKeys(e, "v", "4", "l", "d")
	// v anchors at 0, 4l moves to col 4. VisualRange = [0,4] inclusive.
	// DeleteRange(0,0,0,5) deletes "hello", leaving " world".
	if line(e, 0) != " world" {
		t.Fatalf("got %q", line(e, 0))
	}
}

// --- Command mode ---

func TestCommand_w_setsWrittenStatus(t *testing.T) {
	buf := buffer.New()
	buf.SetPath("/tmp/editor_test_cmd.txt")
	e := editor.New(buf)
	typeKeys(e, ":", "w", "<Enter>")
	if e.StatusMsg() == "" {
		t.Fatal("expected a status message after :w")
	}
}

func TestCommand_unknown_setsError(t *testing.T) {
	e := newEditor(t, "")
	typeKeys(e, ":", "z", "z", "z", "<Enter>")
	if e.StatusMsg() == "" {
		t.Fatal("expected error status message")
	}
}

func TestCommand_esc_cancels(t *testing.T) {
	e := newEditor(t, "")
	typeKeys(e, ":", "<Esc>")
	if e.Mode() != editor.ModeNormal {
		t.Fatalf("expected NORMAL after esc, got %s", e.Mode())
	}
}

// --- Search ---

func TestSearch_forward(t *testing.T) {
	e := newEditor(t, "foo bar foo")
	typeKeys(e, "/", "b", "a", "r", "<Enter>")
	if e.Cursor().Col != 4 {
		t.Errorf("expected col 4, got %d", e.Cursor().Col)
	}
}

func TestSearch_notFound(t *testing.T) {
	e := newEditor(t, "hello")
	typeKeys(e, "/", "z", "z", "z", "<Enter>")
	if e.StatusMsg() == "" {
		t.Fatal("expected 'pattern not found' status")
	}
}

// --- Diagnostics ---

func TestDiagnostics_setAndGet(t *testing.T) {
	e := newEditor(t, "")
	diags := []editor.Diagnostic{
		{Row: 0, Col: 0, Severity: 1, Message: "syntax error", Source: "gopls"},
	}
	e.SetDiagnostics(diags)
	got := e.GetDiagnostics()
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(got))
	}
	if got[0].Message != "syntax error" {
		t.Errorf("got %q", got[0].Message)
	}
}

func TestDiagnostics_empty(t *testing.T) {
	e := newEditor(t, "")
	if len(e.GetDiagnostics()) != 0 {
		t.Fatal("expected no diagnostics initially")
	}
}

// --- Undo / Redo (requires Postgres) ---

const editorTestDSN = "host=localhost user=postgres dbname=editor sslmode=disable"

func newEditorWithUndo(t *testing.T, content string) editor.Editor {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "editor_undo_test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	buf, err := buffer.OpenWithUndo(f.Name(), editorTestDSN)
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping undo test", err)
	}
	t.Cleanup(buf.Close)
	if !buf.HasUndoStore() {
		t.Skip("postgres unavailable — skipping undo test")
	}
	return editor.New(buf)
}

func TestUndo_dd_restoresCursorAndText(t *testing.T) {
	e := newEditorWithUndo(t, "foo\nbar\nbaz")

	// dd deletes the first line; cursor should be at (0,0) after.
	typeKeys(e, "d", "d")
	if line(e, 0) != "bar" {
		t.Fatalf("after dd: line 0 = %q, want %q", line(e, 0), "bar")
	}

	// u should restore the deleted line and cursor to (0,0).
	typeKeys(e, "u")
	if e.Buf().LineCount() != 3 {
		t.Fatalf("after undo: %d lines, want 3", e.Buf().LineCount())
	}
	if line(e, 0) != "foo" {
		t.Fatalf("after undo: line 0 = %q, want %q", line(e, 0), "foo")
	}
	if e.Cursor().Row != 0 || e.Cursor().Col != 0 {
		t.Errorf("cursor after undo = (%d,%d), want (0,0)",
			e.Cursor().Row, e.Cursor().Col)
	}
}

func TestRedo_restoredByCtrlR(t *testing.T) {
	e := newEditorWithUndo(t, "hello\nworld")

	// Delete line 0, then undo, then redo.
	typeKeys(e, "d", "d")
	typeKeys(e, "u")
	typeKeys(e, "<C-r>")

	if e.Buf().LineCount() != 1 {
		t.Fatalf("after redo: %d lines, want 1", e.Buf().LineCount())
	}
	if line(e, 0) != "world" {
		t.Fatalf("after redo: line 0 = %q, want %q", line(e, 0), "world")
	}
}

func TestUndo_atOldest_showsMessage(t *testing.T) {
	e := newEditorWithUndo(t, "hi")

	// Exhaust undo history.
	for i := 0; i < 5; i++ {
		typeKeys(e, "u")
	}
	if e.StatusMsg() != "already at oldest change" {
		t.Errorf("status = %q, want \"already at oldest change\"", e.StatusMsg())
	}
}
