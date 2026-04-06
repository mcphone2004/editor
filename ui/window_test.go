package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/anthonybrice/editor/buffer/fake"
	"github.com/anthonybrice/editor/editor"
	"github.com/anthonybrice/editor/layout"
	"github.com/stretchr/testify/require"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func newTestPane(content string) *winPane {
	buf := fake.New(content)
	return &winPane{ed: editor.New(buf)}
}

func TestWinPane_SetGetBounds(t *testing.T) {
	p := newTestPane("")
	p.SetBounds(2, 3, 40, 20)
	x, y, w, h := p.Bounds()
	require.Equal(t, 2, x)
	require.Equal(t, 3, y)
	require.Equal(t, 40, w)
	require.Equal(t, 20, h)
}

func TestWinPane_ContentRows(t *testing.T) {
	tests := []struct {
		h    int
		want int
	}{
		{h: 0, want: 1}, // guard: min 1
		{h: 1, want: 1}, // exactly status bar height → still min 1
		{h: 10, want: 9},
		{h: 24, want: 23},
	}
	for _, tc := range tests {
		p := newTestPane("")
		p.h = tc.h
		require.Equal(t, tc.want, p.contentRows(), "h=%d", tc.h)
	}
}

func TestWinPane_ScrollToCursor_ScrollsDown(t *testing.T) {
	p := newTestPane("a\nb\nc\nd\ne\nf\ng\nh")
	p.SetBounds(0, 0, 80, 5)
	// Move cursor to row 6 (0-indexed), which is past the visible window.
	for i := 0; i < 6; i++ {
		p.ed.HandleKey("j")
	}
	p.scrollToCursor()
	cur := p.ed.Cursor()
	require.GreaterOrEqual(t, cur.Row, p.scroll, "cursor row should be >= scroll")
	require.Less(t, cur.Row, p.scroll+p.contentRows(), "cursor row should be in view")
}

func TestWinPane_ScrollToCursor_ScrollsUp(t *testing.T) {
	p := newTestPane("a\nb\nc\nd\ne\nf\ng\nh")
	p.SetBounds(0, 0, 80, 5)
	p.scroll = 5 // simulate being scrolled down

	// Cursor is at row 0 (initial state), which is above the window.
	p.scrollToCursor()
	require.Equal(t, 0, p.scroll, "scroll should reset to show cursor at row 0")
}

func TestWinPane_ImplementsLayoutPane(_ *testing.T) {
	var _ layout.Pane = (*winPane)(nil)
}

func TestRenderNode_SingleLeaf(t *testing.T) {
	p := newTestPane("hello")
	p.SetBounds(0, 0, 80, 5)
	root := layout.NewLeaf(p)
	out := renderNode(root, p, nil)

	require.NotEmpty(t, out)
	// Should contain the text "hello".
	plain := stripANSI(out)
	require.Contains(t, plain, "hello")
	// Should have exactly p.h lines (4 content + 1 status = 5).
	lines := strings.Split(out, "\n")
	require.Equal(t, p.h, len(lines), "renderNode output should have h lines")
}

func TestRenderNode_HorizontalSplit(t *testing.T) {
	top := newTestPane("top line")
	bot := newTestPane("bot line")
	top.SetBounds(0, 0, 80, 12)
	bot.SetBounds(0, 12, 80, 12)

	topLeaf := layout.NewLeaf(top)
	botLeaf := layout.NewLeaf(bot)
	topLeaf.Ratio = 0.5
	botLeaf.Ratio = 0.5
	root := &layout.Node{
		Dir:      layout.Horizontal,
		Children: []*layout.Node{topLeaf, botLeaf},
		Ratio:    1.0,
	}

	out := renderNode(root, top, nil)
	plain := stripANSI(out)

	require.Contains(t, plain, "top line")
	require.Contains(t, plain, "bot line")

	// Total lines should equal top.h + bot.h = 24.
	lines := strings.Split(out, "\n")
	require.Equal(t, top.h+bot.h, len(lines))
}

func TestRenderNode_VerticalSplit(t *testing.T) {
	left := newTestPane("left")
	right := newTestPane("right")
	// In a vertical split, AssignBounds gives left w-1, right w.
	// Simulate: total=80, divider=1, left=39, right=40, h=23.
	left.SetBounds(0, 0, 39, 23)
	right.SetBounds(40, 0, 40, 23)

	leftLeaf := layout.NewLeaf(left)
	rightLeaf := layout.NewLeaf(right)
	leftLeaf.Ratio = 0.5
	rightLeaf.Ratio = 0.5
	root := &layout.Node{
		Dir:      layout.Vertical,
		Children: []*layout.Node{leftLeaf, rightLeaf},
		Ratio:    1.0,
	}

	out := renderNode(root, left, nil)
	plain := stripANSI(out)

	require.Contains(t, plain, "left")
	require.Contains(t, plain, "right")
	// Divider column present.
	require.Contains(t, plain, "│")
}

func TestDivider(t *testing.T) {
	require.Equal(t, "", divider(0))
	require.Equal(t, "│", divider(1))
	d := divider(3)
	lines := strings.Split(d, "\n")
	require.Equal(t, 3, len(lines))
	for _, l := range lines {
		require.Equal(t, "│", l)
	}
}
