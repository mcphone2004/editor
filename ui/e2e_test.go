//go:build e2e

package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anthonybrice/editor/telemetry"
	"github.com/anthonybrice/editor/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// bufContent returns the full document text from the model.
// It type-asserts tea.Model → *ui.Model to reach the String() method.
func bufContent(t *testing.T, m tea.Model) string {
	t.Helper()
	um, ok := m.(*ui.Model)
	if !ok {
		t.Fatal("model is not *ui.Model")
	}
	return um.String()
}

func TestE2E_InitialView(t *testing.T) {
	m := newModel(t)
	v := viewText(m)
	status := statusLine(m)

	// Line number 1 must be visible.
	if !contentHas(m, "1") {
		t.Errorf("line number 1 not found in content\n%s", v)
	}
	// Mode indicator.
	if !strings.Contains(status, "NORMAL") {
		t.Errorf("expected NORMAL in status bar, got: %q", status)
	}
	// File label.
	if !strings.Contains(status, "[New File]") {
		t.Errorf("expected [New File] in status bar, got: %q", status)
	}
	// Cursor position: row 1, col 1.
	if !strings.Contains(status, "1:1") {
		t.Errorf("expected 1:1 in status bar, got: %q", status)
	}
}

func TestE2E_InsertModeActivation(t *testing.T) {
	m := newModel(t)
	m = press(m, "i")

	status := statusLine(m)
	if !strings.Contains(status, "INSERT") {
		t.Errorf("expected INSERT in status bar, got: %q", status)
	}
}

func TestE2E_TypeTextAndEscape(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>")

	if !contentHas(m, "hello") {
		t.Errorf("expected 'hello' in content\n%s", viewText(m))
	}
	status := statusLine(m)
	if !strings.Contains(status, "NORMAL") {
		t.Errorf("expected NORMAL after Esc, got: %q", status)
	}
	// Esc moves cursor back one: col 4 → 1-indexed col 5.
	if !strings.Contains(status, "1:5") {
		t.Errorf("expected 1:5 in status after typing 'hello'<Esc>, got: %q", status)
	}
}

func TestE2E_MultilineTyping(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", "<Enter>", "b", "a", "r", "<Esc>")

	if !contentHas(m, "foo") {
		t.Errorf("expected 'foo' in content\n%s", viewText(m))
	}
	if !contentHas(m, "bar") {
		t.Errorf("expected 'bar' in content\n%s", viewText(m))
	}
	// Both line numbers 1 and 2 should appear in the gutter.
	cl := contentLines(m)
	has1, has2 := false, false
	for _, l := range cl {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "1 ") || trimmed == "1" {
			has1 = true
		}
		if strings.HasPrefix(trimmed, "2 ") || trimmed == "2" {
			has2 = true
		}
	}
	if !has1 || !has2 {
		t.Errorf("expected line numbers 1 and 2 in gutter\n%s", viewText(m))
	}
}

func TestE2E_CursorPosition_StatusBar(t *testing.T) {
	m := newModel(t)
	// Type two lines, end up on row 2.
	m = press(m, "i", "a", "b", "<Enter>", "c", "d", "<Esc>")
	status := statusLine(m)
	// After "cd"<Esc> on row 1 (0-indexed), cursor is col 1 → 1-indexed: 2:2.
	if !strings.Contains(status, "2:") {
		t.Errorf("expected row 2 in status bar, got: %q", status)
	}
}

func TestE2E_CursorMovement_k_movesUp(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "a", "b", "<Enter>", "c", "d", "<Esc>")
	m = press(m, "k")

	status := statusLine(m)
	if !strings.Contains(status, "1:") {
		t.Errorf("after k, expected row 1 in status bar, got: %q", status)
	}
}

func TestE2E_CursorMovement_l_movesRight(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "a", "b", "c", "<Esc>") // cursor at col 1 (1-indexed 2) after Esc
	m = press(m, "0")                         // go to col 0 → 1:1
	m = press(m, "l")                         // move right → 1:2

	status := statusLine(m)
	if !strings.Contains(status, "1:2") {
		t.Errorf("after 0 then l, expected 1:2 in status bar, got: %q", status)
	}
}

func TestE2E_CursorMovement_dollar_goesToLineEnd(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>")
	m = press(m, "0", "$") // go to start then end → last char = col 4, 1-indexed 5

	status := statusLine(m)
	if !strings.Contains(status, "1:5") {
		t.Errorf("after $, expected 1:5, got: %q", status)
	}
}

func TestE2E_VisualMode(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>", "0")
	m = press(m, "v")

	status := statusLine(m)
	if !strings.Contains(status, "VISUAL") {
		t.Errorf("expected VISUAL in status bar, got: %q", status)
	}
}

func TestE2E_VisualLineMode(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>")
	m = press(m, "V")

	status := statusLine(m)
	if !strings.Contains(status, "V-LINE") {
		t.Errorf("expected V-LINE in status bar, got: %q", status)
	}
}

func TestE2E_VisualEscapeReturnsNormal(t *testing.T) {
	m := newModel(t)
	m = press(m, "v", "<Esc>")

	status := statusLine(m)
	if !strings.Contains(status, "NORMAL") {
		t.Errorf("expected NORMAL after Esc from visual, got: %q", status)
	}
}

func TestE2E_DeleteLine_dd(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", "<Enter>", "b", "a", "r", "<Esc>")
	m = press(m, "g", "g", "d", "d") // go to top, delete line

	if contentHas(m, "foo") {
		t.Errorf("'foo' should have been deleted\n%s", viewText(m))
	}
	if !contentHas(m, "bar") {
		t.Errorf("'bar' should remain after deleting 'foo'\n%s", viewText(m))
	}
}

func TestE2E_DeleteWord_dw(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d", "<Esc>")
	m = press(m, "0", "d", "w") // go to col 0, delete word "hello "

	if contentHas(m, "hello") {
		t.Errorf("'hello' should have been deleted\n%s", viewText(m))
	}
	if !contentHas(m, "world") {
		t.Errorf("'world' should remain\n%s", viewText(m))
	}
}

func TestE2E_DeleteChar_x(t *testing.T) {
	m := newModel(t)
	// Type "hxello" in insert mode, then Esc (cursor lands on col 4 = 'l').
	m = press(m, "i", "h", "x", "e", "l", "l", "o", "<Esc>")
	// Go to col 0 ('h') then delete it with x → "xello".
	m = press(m, "0", "x")

	if !contentHas(m, "xello") {
		t.Errorf("expected 'xello' after deleting first char, got:\n%s", viewText(m))
	}
}

func TestE2E_ReplaceChar_r(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>")
	m = press(m, "0", "r", "H") // replace 'h' at col 0 with 'H'

	if !contentHas(m, "Hello") {
		t.Errorf("expected 'Hello' after r replacement, got:\n%s", viewText(m))
	}
}

func TestE2E_BackspaceInInsert(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "x", "<Backspace>", "l", "o", "<Esc>")

	if !contentHas(m, "hello") {
		t.Errorf("expected 'hello' after backspace correction, got:\n%s", viewText(m))
	}
}

func TestE2E_YankAndPaste_CrossPane(t *testing.T) {
	// yy in one pane, switch to the other pane, p — register must be shared.
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", "<Esc>") // type "foo"
	m = press(m, ":", "v", "s", "<Enter>")    // :vs — vertical split (same buffer)
	m = press(m, "<C-w>", "w")                // move focus to original pane
	m = press(m, "y", "y")                    // yank the line
	m = press(m, "<C-w>", "w")                // switch to the other pane
	m = press(m, "p")                         // paste — should see "foo" on a new line

	count := 0
	for _, l := range contentLines(m) {
		if strings.Contains(l, "foo") {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected 'foo' on 2 lines after cross-pane yy+p, found %d\n%s", count, viewText(m))
	}
}

func TestE2E_YankAndPaste_yy_p(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", "<Esc>")
	m = press(m, "y", "y", "p") // yank line, paste below

	count := 0
	for _, l := range contentLines(m) {
		if strings.Contains(l, "foo") {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected 'foo' on 2 lines after yy+p, found %d\n%s", count, viewText(m))
	}
}

func TestE2E_CommandMode_Colon(t *testing.T) {
	m := newModel(t)
	m = press(m, ":")

	status := statusLine(m)
	if !strings.Contains(status, "COMMAND") {
		t.Errorf("expected COMMAND in status bar, got: %q", status)
	}
	// The ":" prompt appears on the command line.
	cmd := cmdLine(m)
	if !strings.HasPrefix(cmd, ":") {
		t.Errorf("expected ':' on command line, got: %q", cmd)
	}
}

func TestE2E_CommandMode_EscCancels(t *testing.T) {
	m := newModel(t)
	m = press(m, ":", "<Esc>")

	status := statusLine(m)
	if !strings.Contains(status, "NORMAL") {
		t.Errorf("expected NORMAL after Esc from command, got: %q", status)
	}
}

func TestE2E_CommandMode_TypedText(t *testing.T) {
	m := newModel(t)
	m = press(m, ":", "w", "q")

	cmd := cmdLine(m)
	if !strings.Contains(cmd, "wq") {
		t.Errorf("expected 'wq' on command line, got: %q", cmd)
	}
}

func TestE2E_SearchMode_Slash(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", " ", "b", "a", "r", "<Esc>")
	m = press(m, "/", "b", "a", "r", "<Enter>")

	// "bar" starts at col 4 (0-indexed) → 1-indexed: col 5.
	status := statusLine(m)
	if !strings.Contains(status, "1:5") {
		t.Errorf("after /bar<Enter>, expected cursor at 1:5, got: %q", status)
	}
}

func TestE2E_SearchNotFound(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>")
	m = press(m, "/", "z", "z", "z", "<Enter>")

	cmd := cmdLine(m)
	if !strings.Contains(cmd, "not found") {
		t.Errorf("expected 'not found' message, got: %q", cmd)
	}
}

func TestE2E_OpenLineBelow_o(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", "<Esc>")
	m = press(m, "o", "b", "a", "r", "<Esc>")

	if !contentHas(m, "foo") {
		t.Errorf("expected 'foo' in content\n%s", viewText(m))
	}
	if !contentHas(m, "bar") {
		t.Errorf("expected 'bar' in content after 'o'\n%s", viewText(m))
	}
	// bar should be on line 2 (gutter shows "2").
	cl := contentLines(m)
	barOnLine2 := false
	for _, l := range cl {
		if strings.Contains(l, "bar") && strings.Contains(l, "2") {
			barOnLine2 = true
		}
	}
	if !barOnLine2 {
		t.Errorf("expected 'bar' on line 2\n%s", viewText(m))
	}
}

func TestE2E_OpenLineAbove_O(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "b", "a", "z", "<Esc>")
	m = press(m, "O", "b", "a", "r", "<Esc>") // insert line above

	if !contentHas(m, "bar") {
		t.Errorf("expected 'bar' in content\n%s", viewText(m))
	}
	// bar should be on line 1.
	cl := contentLines(m)
	barOnLine1 := false
	for _, l := range cl {
		if strings.Contains(l, "bar") && strings.Contains(l, "1") {
			barOnLine1 = true
		}
	}
	if !barOnLine1 {
		t.Errorf("expected 'bar' on line 1\n%s", viewText(m))
	}
}

func TestE2E_AppendAfterCursor_a(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "l", "l", "<Esc>") // "hll", cursor at col 1 after Esc
	m = press(m, "0", "a", "e", "<Esc>")      // 'a' appends after col 0 ('h'), type 'e' → "hell"

	if !contentHas(m, "hell") {
		t.Errorf("expected 'hell' after 'a'+'e', got:\n%s", viewText(m))
	}
}

func TestE2E_AppendAtEOL_A(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "h", "e", "l", "l", "<Esc>")
	m = press(m, "A", "o", "<Esc>")

	if !contentHas(m, "hello") {
		t.Errorf("expected 'hello' after A+o, got:\n%s", viewText(m))
	}
}

func TestE2E_Scrolling(t *testing.T) {
	m := newModel(t)
	// Type 30 lines — more than the 22 visible rows.
	m = press(m, "i")
	for i := 1; i <= 30; i++ {
		for _, ch := range fmt.Sprintf("line%d", i) {
			m = press(m, string(ch))
		}
		m = press(m, "<Enter>")
	}
	m = press(m, "<Esc>")

	// After typing 30 lines and pressing Esc, the cursor is near the bottom.
	// The view should have scrolled so line 30 is visible.
	if !contentHas(m, "line30") {
		t.Errorf("expected 'line30' visible after scrolling\n%s", viewText(m))
	}
	// And line 1 should no longer be visible (scrolled off).
	for _, l := range contentLines(m) {
		// The content area shouldn't show "line1" text (gutter "1" is fine).
		// Check that the text "line1 " doesn't appear (to avoid false match on "line10", etc.)
		if strings.Contains(l, "line1 ") || strings.HasSuffix(strings.TrimSpace(l), "line1") {
			t.Errorf("line1 should have scrolled off, but found in: %q", l)
			break
		}
	}
}

func TestE2E_ModifiedFlag(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "x", "<Esc>")

	status := statusLine(m)
	if !strings.Contains(status, "[+]") {
		t.Errorf("expected [+] modified flag after typing, got: %q", status)
	}
}

// TestE2E_NormalNavigation_DoesNotMutateBuffer verifies that every normal-mode
// motion leaves the document text completely unchanged.
func TestE2E_NormalNavigation_DoesNotMutateBuffer(t *testing.T) {
	m := newModel(t)
	// Seed a three-line document.
	m = press(m, "i",
		"h", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d", "<Enter>",
		"f", "o", "o", " ", "b", "a", "r", " ", "b", "a", "z", "<Enter>",
		"o", "n", "e", " ", "t", "w", "o", " ", "t", "h", "r", "e", "e",
		"<Esc>",
	)

	before := bufContent(t, m)

	// Exercise every normal-mode motion in the editor.
	m = press(m, "g", "g")      // go to top
	m = press(m, "0")           // start of line
	m = press(m, "l", "l", "l") // right ×3
	m = press(m, "h", "h")      // left ×2
	m = press(m, "w")           // next word
	m = press(m, "W")           // next WORD
	m = press(m, "b")           // back word
	m = press(m, "B")           // back WORD
	m = press(m, "e")           // word end
	m = press(m, "$")           // end of line
	m = press(m, "j")           // down
	m = press(m, "^")           // first non-blank
	m = press(m, "k")           // up
	m = press(m, "G")           // last line
	m = press(m, "2", "k")      // 2 up (count prefix)
	m = press(m, "}")           // forward paragraph
	m = press(m, "{")           // backward paragraph
	m = press(m, "f", "o")      // find char 'o'
	m = press(m, ";")           // repeat find
	m = press(m, ",")           // reverse find

	after := bufContent(t, m)

	if before != after {
		t.Errorf("buffer mutated by cursor movement\nbefore: %q\nafter:  %q", before, after)
	}
}

// TestE2E_EnterExitInsert_DoesNotMutateBuffer verifies that pressing i then
// immediately Esc (no text typed) leaves the document unchanged, even though
// FlushGap rewrites the piece table internally.
func TestE2E_EnterExitInsert_DoesNotMutateBuffer(t *testing.T) {
	m := newModel(t)
	m = press(m, "i",
		"f", "o", "o", "<Enter>",
		"b", "a", "r", "<Enter>",
		"b", "a", "z",
		"<Esc>",
	)

	before := bufContent(t, m)

	// Enter and immediately exit insert mode on each line — no text typed.
	m = press(m, "g", "g")
	m = press(m, "i", "<Esc>") // line 1 mid
	m = press(m, "$")
	m = press(m, "a", "<Esc>") // append on line 1
	m = press(m, "j")
	m = press(m, "I", "<Esc>") // insert at first non-blank on line 2
	m = press(m, "A", "<Esc>") // append at EOL on line 2
	m = press(m, "j")
	m = press(m, "i", "<Esc>") // line 3

	after := bufContent(t, m)

	if before != after {
		t.Errorf("buffer mutated by enter/exit insert mode without typing\nbefore: %q\nafter:  %q", before, after)
	}
}

// =============================================================================
// Bug regression tests
// =============================================================================

// --- Bug: tab characters rendered as literal \t ---
//
// Root cause: View() emitted the raw '\t' rune.  When the cursor was positioned
// on a tab, lipgloss wrapped it as styleCursor.Render("\t"), which many
// terminals render as a 1-column coloured cell instead of expanding to the tab
// stop.  This made the text after the tab appear at a different visual column
// depending on whether the cursor was on that tab — so lines seemed to shift
// left/right as you moved the cursor up/down.
//
// Fix: expand '\t' → 4 spaces at render time only (buffer stores '\t' unchanged).

// TestBug_Tab_NoLiteralTabInView verifies that the renderer never emits a
// literal tab character — every \t is expanded to spaces before output.
func TestBug_Tab_NoLiteralTabInView(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "<Tab>", "f", "o", "o", "<Esc>")

	v := viewText(m)
	if strings.Contains(v, "\t") {
		t.Errorf("rendered view must not contain literal tab characters\n%s", v)
	}
}

// TestBug_Tab_RendersAsFourSpaces verifies that a leading tab is rendered as
// exactly 4 spaces so indentation is consistent.
func TestBug_Tab_RendersAsFourSpaces(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "<Tab>", "f", "o", "o", "<Esc>")

	// With cursor on the tab, stripped view must show 4 spaces before "foo".
	if !contentHas(m, "    foo") {
		t.Errorf("expected tab to render as 4 spaces; got:\n%s", viewText(m))
	}
}

// TestBug_Tab_IndentationConsistentOnCursorMove verifies that moving the
// cursor on and off a tab-indented line does not change how that line looks.
func TestBug_Tab_IndentationConsistentOnCursorMove(t *testing.T) {
	m := newModel(t)
	// Two lines: first has a leading tab, second does not.
	m = press(m, "i", "<Tab>", "f", "o", "o", "<Enter>", "b", "a", "r", "<Esc>")

	// Capture the first content line with cursor ON the tab (gg → col 0).
	m = press(m, "g", "g", "0")
	line1CursorOn := contentLines(m)[0]

	// Move cursor to line 2 so cursor is NOT on the tab line.
	m = press(m, "j")
	line1CursorOff := contentLines(m)[0]

	if line1CursorOn != line1CursorOff {
		t.Errorf("line 1 renders differently depending on cursor position\ncursor on tab:  %q\ncursor off tab: %q",
			line1CursorOn, line1CursorOff)
	}
}

// TestBug_Tab_BufferPreservesTabs verifies that tab expansion is display-only:
// the underlying buffer still stores '\t', so saving would write the original
// file content unchanged.
func TestBug_Tab_BufferPreservesTabs(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "<Tab>", "f", "o", "o", "<Esc>")

	content := bufContent(t, m)
	if !strings.Contains(content, "\t") {
		t.Errorf("buffer should still contain literal tab (display-only expansion)\ncontent: %q", content)
	}
	if strings.Contains(content, "    ") {
		t.Errorf("buffer must not contain expanded spaces — only the renderer expands tabs\ncontent: %q", content)
	}
}

// --- Bug: ActivateGap operator-precedence mistake ---
//
// Root cause: the condition in handleKey was
//
//	if prevMode == ModeNormal && key == "i" || key == "a" || key == "o" || key == "O"
//
// Due to Go's operator precedence (&& binds tighter than ||) this parsed as
//
//	if (prevMode == ModeNormal && key == "i") || key == "a" || key == "o" || key == "O"
//
// Consequence: every time the user types "a", "o", or "O" while already in
// insert mode, ActivateGap is called unconditionally.  ActivateGap calls
// FlushGap first, which writes the current gap state back to the piece table
// and pushes an undo snapshot — even though nothing has changed.  With a
// Postgres undo store this pollutes the undo history; in any case it rewrites
// the piece table on every such keystroke.
//
// The tests below confirm that typing those characters in insert mode still
// produces the correct content (no silent corruption) and that the buffer is
// not mutated by the extra flush/reload cycle.

// TestBug_ActivateGap_aInInsertInsertsChar verifies that 'a' typed in insert
// mode inserts the letter, not triggering any mode change or gap mishandling.
func TestBug_ActivateGap_aInInsertInsertsChar(t *testing.T) {
	m := newModel(t)
	// Enter insert mode, type a sequence including 'a'.
	m = press(m, "i", "f", "o", "a", "b", "a", "r", "<Esc>")

	if !contentHas(m, "foabar") {
		t.Errorf("expected 'foabar'; got:\n%s", viewText(m))
	}
	// Mode must return to NORMAL after Esc.
	if !strings.Contains(statusLine(m), "NORMAL") {
		t.Errorf("expected NORMAL mode after Esc, got: %q", statusLine(m))
	}
}

// TestBug_ActivateGap_oInInsertInsertsChar verifies that 'o' typed in insert
// mode inserts the letter rather than opening a new line below.
func TestBug_ActivateGap_oInInsertInsertsChar(t *testing.T) {
	m := newModel(t)
	m = press(m, "i", "f", "o", "o", "<Esc>")

	if !contentHas(m, "foo") {
		t.Errorf("expected 'foo'; got:\n%s", viewText(m))
	}
	// Should still be a single line — 'o' in insert mode must not open a new line.
	// "foo" should appear on exactly one content line.
	count := 0
	for _, l := range contentLines(m) {
		if strings.Contains(l, "foo") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("'foo' should appear on exactly 1 line, found on %d\n%s", count, viewText(m))
	}
}

// TestBug_ActivateGap_aoO_DoNotMutateExistingContent verifies that typing
// 'a', 'o', 'O' in insert mode (which triggers the spurious ActivateGap) does
// not corrupt content that was already in the buffer before entering insert mode.
func TestBug_ActivateGap_aoO_DoNotMutateExistingContent(t *testing.T) {
	m := newModel(t)
	// Seed a buffer with known content.
	m = press(m, "i", "h", "e", "l", "l", "o", "<Esc>")

	// Re-enter insert mode and type characters that trigger the bug.
	m = press(m, "A") // append at EOL
	m = press(m, "a", "o", "O", "<Esc>")

	content := bufContent(t, m)
	// Original "hello" must still be there.
	if !strings.Contains(content, "hello") {
		t.Errorf("original 'hello' was corrupted; buffer: %q", content)
	}
	// The typed characters must also be present.
	if !strings.Contains(content, "aoO") {
		t.Errorf("typed 'aoO' missing from buffer: %q", content)
	}
}

// --- LSP exit detection tests ---

// TestLSPExited_StatusBarUpdates verifies that when msgLSPExited is delivered
// to the model, the status bar switches from "LSP" to "no LSP".
func TestLSPExited_StatusBarUpdates(t *testing.T) {
	m, err := ui.New("", nil, telemetry.Noop())
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Without LSP the status bar should already show "no LSP".
	if !strings.Contains(statusLine(m2), "no LSP") {
		t.Errorf("expected 'no LSP' in status bar without LSP session; got: %q", statusLine(m2))
	}

	// Simulate an LSP exit arriving mid-session.
	m3, _ := m2.Update(ui.MsgLSPExited)
	if !strings.Contains(statusLine(m3), "no LSP") {
		t.Errorf("expected 'no LSP' after msgLSPExited; got: %q", statusLine(m3))
	}
}
