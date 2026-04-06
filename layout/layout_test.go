package layout_test

import (
	"testing"

	"github.com/anthonybrice/editor/layout"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// testPane is a minimal layout.Pane implementation for tests.
type testPane struct{ x, y, w, h int }

func (p *testPane) SetBounds(x, y, w, h int) { p.x, p.y, p.w, p.h = x, y, w, h }
func (p *testPane) Bounds() (x, y, w, h int) {
	return p.x, p.y, p.w, p.h
}

func panes(n int) []*testPane {
	ps := make([]*testPane, n)
	for i := range ps {
		ps[i] = &testPane{}
	}
	return ps
}

// --- NewLeaf / IsLeaf ---

func TestNewLeaf_isLeaf(t *testing.T) {
	p := &testPane{}
	n := layout.NewLeaf(p)
	require.True(t, n.IsLeaf())
	require.Equal(t, p, n.Pane)
}

func TestContainer_isNotLeaf(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	require.False(t, root.IsLeaf())
}

// --- AllLeaves ---

func TestAllLeaves_single(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(p)
	require.Equal(t, []layout.Pane{p}, layout.AllLeaves(root))
}

func TestAllLeaves_horizontal(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])
	leaves := layout.AllLeaves(root)
	require.Len(t, leaves, 3)
	require.Equal(t, layout.Pane(ps[0]), leaves[0])
	require.Equal(t, layout.Pane(ps[1]), leaves[1])
	require.Equal(t, layout.Pane(ps[2]), leaves[2])
}

func TestAllLeaves_vertical(t *testing.T) {
	ps := panes(2)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Vertical, ps[1])
	leaves := layout.AllLeaves(root)
	require.Len(t, leaves, 2)
}

// --- Contains ---

func TestContains_present(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	require.True(t, layout.Contains(root, ps[0]))
	require.True(t, layout.Contains(root, ps[1]))
}

func TestContains_absent(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(&testPane{})
	require.False(t, layout.Contains(root, p))
}

// --- CycleNext / CyclePrev ---

func TestCycleNext_wraps(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	require.Equal(t, layout.Pane(ps[1]), layout.CycleNext(root, ps[0]))
	require.Equal(t, layout.Pane(ps[2]), layout.CycleNext(root, ps[1]))
	require.Equal(t, layout.Pane(ps[0]), layout.CycleNext(root, ps[2])) // wraps
}

func TestCyclePrev_wraps(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	require.Equal(t, layout.Pane(ps[2]), layout.CyclePrev(root, ps[0])) // wraps
	require.Equal(t, layout.Pane(ps[0]), layout.CyclePrev(root, ps[1]))
	require.Equal(t, layout.Pane(ps[1]), layout.CyclePrev(root, ps[2]))
}

func TestCycleNext_singlePane_returnsSelf(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(p)
	require.Equal(t, layout.Pane(p), layout.CycleNext(root, p))
}

// --- Split ---

func TestSplit_horizontal_createsContainer(t *testing.T) {
	p0, p1 := &testPane{}, &testPane{}
	root := layout.NewLeaf(p0)
	root = layout.Split(root, p0, layout.Horizontal, p1)

	require.False(t, root.IsLeaf())
	require.Equal(t, layout.Horizontal, root.Dir)
	require.Len(t, root.Children, 2)
}

func TestSplit_horizontal_ratiosEqualHalf(t *testing.T) {
	p0, p1 := &testPane{}, &testPane{}
	root := layout.Split(layout.NewLeaf(p0), p0, layout.Horizontal, p1)

	require.InDelta(t, 0.5, root.Children[0].Ratio, 1e-9)
	require.InDelta(t, 0.5, root.Children[1].Ratio, 1e-9)
}

func TestSplit_sameDirAddsSibling(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	// Second horizontal split should add ps[2] as sibling, not nest.
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	require.Equal(t, layout.Horizontal, root.Dir)
	require.Len(t, root.Children, 3, "second same-dir split should add sibling")
}

func TestSplit_differentDirWraps(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	// Vertical split of ps[0] inside a horizontal container wraps ps[0].
	root = layout.Split(root, ps[0], layout.Vertical, ps[2])

	require.Equal(t, layout.Horizontal, root.Dir, "outer container stays horizontal")
	// First child should now be a vertical sub-container.
	require.False(t, root.Children[0].IsLeaf())
	require.Equal(t, layout.Vertical, root.Children[0].Dir)
}

// --- Close ---

func TestClose_lastPane_returnsLast(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(p)
	_, _, last := layout.Close(root, p)
	require.True(t, last)
}

func TestClose_twoPane_collapsesToSingleLeaf(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])

	newRoot, newFocus, last := layout.Close(root, ps[0])
	require.False(t, last)
	require.True(t, newRoot.IsLeaf(), "single remaining pane should collapse to leaf")
	require.Equal(t, layout.Pane(ps[1]), newFocus)
}

func TestClose_threePane_removesCorrect(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	newRoot, _, last := layout.Close(root, ps[1])
	require.False(t, last)
	leaves := layout.AllLeaves(newRoot)
	require.Len(t, leaves, 2)
	require.Equal(t, layout.Pane(ps[0]), leaves[0])
	require.Equal(t, layout.Pane(ps[2]), leaves[1])
}

func TestClose_ratiosNormalized(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	newRoot, _, _ := layout.Close(root, ps[2])
	total := 0.0
	for _, c := range newRoot.Children {
		total += c.Ratio
	}
	require.InDelta(t, 1.0, total, 1e-9, "ratios must sum to 1.0 after close")
}

func TestClose_suggestsFocusAdjacentRight(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	_, newFocus, _ := layout.Close(root, ps[1])
	require.Equal(t, layout.Pane(ps[2]), newFocus, "focus should move to right sibling")
}

func TestClose_suggestsFocusAdjacentLeft_whenLast(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])

	_, newFocus, _ := layout.Close(root, ps[1])
	require.Equal(t, layout.Pane(ps[0]), newFocus)
}

// --- AssignBounds ---

func TestAssignBounds_singleLeaf(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(p)
	layout.AssignBounds(root, 0, 0, 100, 50)

	x, y, w, h := p.Bounds()
	require.Equal(t, 0, x)
	require.Equal(t, 0, y)
	require.Equal(t, 100, w)
	require.Equal(t, 50, h)
}

func TestAssignBounds_horizontal_dividesHeight(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	_, _, _, h0 := ps[0].Bounds()
	_, _, _, h1 := ps[1].Bounds()
	require.Equal(t, 24, h0+h1, "heights must sum to total")
	require.Greater(t, h0, 0)
	require.Greater(t, h1, 0)
}

func TestAssignBounds_horizontal_fullWidth(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	_, _, w0, _ := ps[0].Bounds()
	_, _, w1, _ := ps[1].Bounds()
	require.Equal(t, 80, w0, "horizontal split: full width for both panes")
	require.Equal(t, 80, w1)
}

func TestAssignBounds_vertical_dividesWidth(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Vertical, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	_, _, w0, _ := ps[0].Bounds()
	_, _, w1, _ := ps[1].Bounds()
	// Total visible columns = w0 + 1 (divider) + w1 = 80
	require.Equal(t, 80, w0+w1+1, "vertical split: widths + divider = total")
	require.Greater(t, w0, 0)
	require.Greater(t, w1, 0)
}

func TestAssignBounds_vertical_fullHeight(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Vertical, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	_, _, _, h0 := ps[0].Bounds()
	_, _, _, h1 := ps[1].Bounds()
	require.Equal(t, 24, h0, "vertical split: full height for both panes")
	require.Equal(t, 24, h1)
}

func TestAssignBounds_originOffset(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	layout.AssignBounds(root, 5, 10, 80, 24)

	x0, y0, _, _ := ps[0].Bounds()
	x1, y1, _, h0 := ps[0].Bounds()
	_, y1b, _, _ := ps[1].Bounds()

	require.Equal(t, 5, x0)
	require.Equal(t, 10, y0)
	require.Equal(t, 5, x1)
	require.Equal(t, 10+h0, y1b, "second pane starts below first")
	_ = y1
}

func TestAssignBounds_three_horizontal(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])
	layout.AssignBounds(root, 0, 0, 80, 30)

	_, _, _, h0 := ps[0].Bounds()
	_, _, _, h1 := ps[1].Bounds()
	_, _, _, h2 := ps[2].Bounds()
	require.Equal(t, 30, h0+h1+h2)
}

// --- EqualizeRatios ---

func TestEqualizeRatios_twoPane(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	// Skew one ratio first.
	root.Children[0].Ratio = 0.3
	root.Children[1].Ratio = 0.7

	layout.EqualizeRatios(root)

	require.InDelta(t, 0.5, root.Children[0].Ratio, 1e-9)
	require.InDelta(t, 0.5, root.Children[1].Ratio, 1e-9)
}

func TestEqualizeRatios_threePane(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	layout.EqualizeRatios(root)

	for _, c := range root.Children {
		require.InDelta(t, 1.0/3.0, c.Ratio, 1e-9)
	}
}

// --- AdjustHeight / AdjustWidth ---

func TestAdjustHeight_increasesRatio(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	before := root.Children[0].Ratio

	layout.AdjustHeight(root, ps[0], 0.1)

	require.Greater(t, root.Children[0].Ratio, before)
}

func TestAdjustHeight_ratiosSumToOne(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])

	layout.AdjustHeight(root, ps[0], 0.2)

	total := root.Children[0].Ratio + root.Children[1].Ratio
	require.InDelta(t, 1.0, total, 1e-9)
}

func TestAdjustHeight_clampsToMinimum(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])

	layout.AdjustHeight(root, ps[0], -999.0)

	require.GreaterOrEqual(t, root.Children[0].Ratio, 0.1)
}

func TestAdjustWidth_onVerticalSplit(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Vertical, ps[1])
	before := root.Children[0].Ratio

	layout.AdjustWidth(root, ps[0], 0.1)

	require.Greater(t, root.Children[0].Ratio, before)
}

func TestAdjustHeight_noopOnVerticalSplit(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Vertical, ps[1])
	before := root.Children[0].Ratio

	// AdjustHeight looks for a Horizontal container — vertical split has none.
	layout.AdjustHeight(root, ps[0], 0.2)

	require.Equal(t, before, root.Children[0].Ratio, "AdjustHeight should not affect vertical splits")
}

// --- MoveToEdge ---

func TestMoveToEdge_toEnd(t *testing.T) {
	ps := panes(3)
	root := layout.NewLeaf(ps[0])
	root = layout.Split(root, ps[0], layout.Horizontal, ps[1])
	root = layout.Split(root, ps[1], layout.Horizontal, ps[2])

	root = layout.MoveToEdge(root, ps[0], layout.Vertical, true)

	leaves := layout.AllLeaves(root)
	require.Len(t, leaves, 3)
	// ps[0] should now be the last leaf.
	require.Equal(t, layout.Pane(ps[0]), leaves[len(leaves)-1])
}

func TestMoveToEdge_toStart(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])

	root = layout.MoveToEdge(root, ps[1], layout.Vertical, false)

	leaves := layout.AllLeaves(root)
	require.Equal(t, layout.Pane(ps[1]), leaves[0], "ps[1] should be at the start")
}

func TestMoveToEdge_singlePane_noOp(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(p)
	newRoot := layout.MoveToEdge(root, p, layout.Vertical, true)
	require.True(t, newRoot.IsLeaf())
}

// --- NeighborInDirection ---

func TestNeighborInDirection_horizontalSplit_jk(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	// ps[0] is on top, ps[1] is below.
	require.Equal(t, layout.Pane(ps[1]), layout.NeighborInDirection(root, ps[0], 'j'))
	require.Equal(t, layout.Pane(ps[0]), layout.NeighborInDirection(root, ps[1], 'k'))
}

func TestNeighborInDirection_verticalSplit_hl(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Vertical, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	// ps[0] is on the left, ps[1] is on the right.
	require.Equal(t, layout.Pane(ps[1]), layout.NeighborInDirection(root, ps[0], 'l'))
	require.Equal(t, layout.Pane(ps[0]), layout.NeighborInDirection(root, ps[1], 'h'))
}

func TestNeighborInDirection_noNeighbor_returnsCurrent(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)

	// ps[0] is on top — no neighbor above.
	require.Equal(t, layout.Pane(ps[0]), layout.NeighborInDirection(root, ps[0], 'k'))
	// ps[1] is on the bottom — no neighbor below.
	require.Equal(t, layout.Pane(ps[1]), layout.NeighborInDirection(root, ps[1], 'j'))
}

// --- TreeHeight ---

func TestTreeHeight_singleLeaf(t *testing.T) {
	p := &testPane{}
	root := layout.NewLeaf(p)
	layout.AssignBounds(root, 0, 0, 80, 24)
	require.Equal(t, 24, layout.TreeHeight(root))
}

func TestTreeHeight_horizontalSum(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Horizontal, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)
	require.Equal(t, 24, layout.TreeHeight(root))
}

func TestTreeHeight_verticalUniform(t *testing.T) {
	ps := panes(2)
	root := layout.Split(layout.NewLeaf(ps[0]), ps[0], layout.Vertical, ps[1])
	layout.AssignBounds(root, 0, 0, 80, 24)
	require.Equal(t, 24, layout.TreeHeight(root))
}
