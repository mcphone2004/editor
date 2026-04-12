package golang_test

import (
	"testing"

	"github.com/anthonybrice/editor/highlight"
	goHL "github.com/anthonybrice/editor/highlight/golang"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- helpers ---

func kindAt(t *testing.T, hl map[int]highlight.LineHL, row, col int) highlight.Kind {
	t.Helper()
	line, ok := hl[row]
	if !ok {
		t.Fatalf("row %d not present in result", row)
	}
	if col >= len(line) {
		t.Fatalf("col %d out of range for row %d (len %d)", col, row, len(line))
	}
	return line[col]
}

func assertKind(t *testing.T, hl map[int]highlight.LineHL, row, col int, want highlight.Kind) {
	t.Helper()
	got := kindAt(t, hl, row, col)
	if got != want {
		t.Errorf("row %d col %d: got Kind %d, want %d", row, col, got, want)
	}
}

func assertRangeKind(t *testing.T, hl map[int]highlight.LineHL, row, startCol, endCol int, want highlight.Kind) {
	t.Helper()
	for c := startCol; c < endCol; c++ {
		assertKind(t, hl, row, c, want)
	}
}

// --- tests ---

func TestHighlight_Keyword(t *testing.T) {
	h := goHL.New()
	// "package" is cols 0–6, "main" is an identifier (not a keyword).
	result := h.Highlight("package main", 0, 0)

	assertRangeKind(t, result, 0, 0, 7, highlight.KindKeyword) // "package"
	assertKind(t, result, 0, 7, highlight.KindNone)            // space
	assertRangeKind(t, result, 0, 8, 12, highlight.KindNone)   // "main" identifier
}

func TestHighlight_FuncKeyword(t *testing.T) {
	h := goHL.New()
	result := h.Highlight("func main() {}", 0, 0)

	assertRangeKind(t, result, 0, 0, 4, highlight.KindKeyword) // "func"
}

func TestHighlight_StringLiteral(t *testing.T) {
	h := goHL.New()
	// x := "hello"  →  "hello" with quotes starts at col 5, length 7.
	result := h.Highlight(`x := "hello"`, 0, 0)

	assertRangeKind(t, result, 0, 0, 5, highlight.KindNone)    // "x := "
	assertRangeKind(t, result, 0, 5, 12, highlight.KindString) // `"hello"`
}

func TestHighlight_LineComment(t *testing.T) {
	h := goHL.New()
	// "// comment" starts at col 7.
	result := h.Highlight("x := 1 // comment", 0, 0)

	assertRangeKind(t, result, 0, 7, 17, highlight.KindComment)
}

func TestHighlight_IntLiteral(t *testing.T) {
	h := goHL.New()
	// "42" is at cols 5–6.
	result := h.Highlight("x := 42", 0, 0)

	assertRangeKind(t, result, 0, 5, 7, highlight.KindNumber)
}

func TestHighlight_FloatLiteral(t *testing.T) {
	h := goHL.New()
	result := h.Highlight("x := 3.14", 0, 0)

	assertRangeKind(t, result, 0, 5, 9, highlight.KindNumber)
}

func TestHighlight_CharLiteral(t *testing.T) {
	h := goHL.New()
	result := h.Highlight("x := 'a'", 0, 0)

	assertRangeKind(t, result, 0, 5, 8, highlight.KindString)
}

func TestHighlight_BlockComment_SingleLine(t *testing.T) {
	h := goHL.New()
	result := h.Highlight("/* note */", 0, 0)

	assertRangeKind(t, result, 0, 0, 10, highlight.KindComment)
}

func TestHighlight_BlockComment_MultiLine(t *testing.T) {
	h := goHL.New()
	src := "/* line1\nline2 */"
	result := h.Highlight(src, 0, 1)

	assertRangeKind(t, result, 0, 0, 8, highlight.KindComment) // "/* line1"
	assertRangeKind(t, result, 1, 0, 8, highlight.KindComment) // "line2 */"
}

func TestHighlight_RawString_MultiLine(t *testing.T) {
	h := goHL.New()
	// Backtick string spanning two lines: x := `line1\nline2`
	src := "x := `line1\nline2`"
	result := h.Highlight(src, 0, 1)

	// Line 0: "`line1" starts at col 5.
	assertRangeKind(t, result, 0, 5, 11, highlight.KindString) // "`line1"
	// Line 1: "line2`" fully string.
	assertRangeKind(t, result, 1, 0, 6, highlight.KindString)
}

func TestHighlight_VisibleRangeOnly(t *testing.T) {
	h := goHL.New()
	src := "package main\n\nfunc foo() {}"
	// Request only line 2.
	result := h.Highlight(src, 2, 2)

	if _, ok := result[0]; ok {
		t.Error("row 0 should not be present in result")
	}
	if _, ok := result[1]; ok {
		t.Error("row 1 should not be present in result")
	}
	if _, ok := result[2]; !ok {
		t.Fatal("row 2 should be present in result")
	}
	// "func" at cols 0–3.
	assertRangeKind(t, result, 2, 0, 4, highlight.KindKeyword)
}

func TestHighlight_TokenStartingBeforeWindow(t *testing.T) {
	h := goHL.New()
	// Block comment starts on line 0 but we only ask for line 1.
	src := "/* starts here\ncontinues here */"
	result := h.Highlight(src, 1, 1)

	if _, ok := result[0]; ok {
		t.Error("row 0 should not be present in result")
	}
	// The continuation on line 1 should still be highlighted.
	assertRangeKind(t, result, 1, 0, 16, highlight.KindComment) // "continues here */"
}

func TestHighlight_EmptySource(t *testing.T) {
	h := goHL.New()
	result := h.Highlight("", 0, 0)

	line, ok := result[0]
	if !ok {
		t.Fatal("row 0 should be present")
	}
	if len(line) != 0 {
		t.Errorf("empty source: expected empty LineHL, got len %d", len(line))
	}
}

func TestHighlight_PartialSource_NoPanic(t *testing.T) {
	h := goHL.New()
	// Syntactically invalid Go should not panic.
	src := "func ("
	result := h.Highlight(src, 0, 0)

	if _, ok := result[0]; !ok {
		t.Fatal("row 0 should be present")
	}
}

func TestHighlight_MultipleKeywordsOnOneLine(t *testing.T) {
	h := goHL.New()
	src := "if x == nil { return }"
	result := h.Highlight(src, 0, 0)

	assertRangeKind(t, result, 0, 0, 2, highlight.KindKeyword)   // "if"
	assertRangeKind(t, result, 0, 14, 20, highlight.KindKeyword) // "return"
}
