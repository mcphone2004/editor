// Package layout implements a recursive split-window layout tree.
//
// A Node is either a leaf (holding one Pane) or a container whose
// children are arranged horizontally (stacked top-to-bottom) or
// vertically (side-by-side).  The tree mirrors vim's window layout
// model: every split produces a new sub-tree, and closing a window
// collapses single-child containers.
//
// Layout is computed by AssignBounds, which walks the tree top-down
// and calls SetBounds on every leaf Pane.  All other operations
// (Split, Close, navigation, resize) manipulate only the tree
// structure and the floating-point ratio stored on each node.
package layout

import "math"

// Dir is the split direction for a container node.
type Dir int

const (
	// Horizontal splits stack children top-to-bottom (:split / :sp).
	Horizontal Dir = iota + 1
	// Vertical splits place children side-by-side (:vsplit / :vs).
	Vertical
)

// Pane is the interface that a leaf node stores.
// AssignBounds calls SetBounds once per frame; Bounds is used for
// directional navigation between panes.
type Pane interface {
	SetBounds(x, y, w, h int)
	Bounds() (x, y, w, h int)
}

// Node is one node in the layout tree.
// Exactly one of (Pane, Children) is non-nil.
//
//	Leaf:      Pane != nil,  Children == nil, Dir == 0
//	Container: Pane == nil,  Children != nil, Dir != 0
//
// Ratio is this node's share of its parent container's relevant
// dimension (height for Horizontal containers, width for Vertical).
// Sibling ratios must sum to 1.0.  The root node's ratio is ignored.
type Node struct {
	Pane     Pane
	Dir      Dir
	Children []*Node
	Ratio    float64
}

// NewLeaf returns a new leaf node wrapping p with ratio 1.0.
func NewLeaf(p Pane) *Node {
	return &Node{Pane: p, Ratio: 1.0}
}

// IsLeaf reports whether n is a leaf node.
func (n *Node) IsLeaf() bool { return n.Pane != nil }

// --- Tree queries ---

// AllLeaves returns all leaf panes in in-order traversal (top-to-bottom,
// left-to-right), which matches vim's C-w w cycle order.
func AllLeaves(root *Node) []Pane {
	if root.IsLeaf() {
		return []Pane{root.Pane}
	}
	var out []Pane
	for _, c := range root.Children {
		out = append(out, AllLeaves(c)...)
	}
	return out
}

// Contains reports whether the subtree rooted at n contains p.
func Contains(n *Node, p Pane) bool {
	if n.IsLeaf() {
		return n.Pane == p
	}
	for _, c := range n.Children {
		if Contains(c, p) {
			return true
		}
	}
	return false
}

// --- Navigation ---

// CycleNext returns the pane after current in traversal order, wrapping around.
func CycleNext(root *Node, current Pane) Pane {
	leaves := AllLeaves(root)
	for i, p := range leaves {
		if p == current {
			return leaves[(i+1)%len(leaves)]
		}
	}
	return current
}

// CyclePrev returns the pane before current in traversal order, wrapping around.
func CyclePrev(root *Node, current Pane) Pane {
	leaves := AllLeaves(root)
	for i, p := range leaves {
		if p == current {
			return leaves[(i-1+len(leaves))%len(leaves)]
		}
	}
	return current
}

// NeighborInDirection returns the nearest pane in the given vim direction
// ('h', 'j', 'k', 'l'). Requires AssignBounds to have been called so
// that each pane knows its bounding box.  Returns current if no neighbor
// is found.
func NeighborInDirection(root *Node, current Pane, dir rune) Pane {
	leaves := AllLeaves(root)

	// Find current pane's bounds.
	var fx, fy, fw, fh int
	found := false
	for _, p := range leaves {
		if p == current {
			fx, fy, fw, fh = p.Bounds()
			found = true
			break
		}
	}
	if !found {
		return current
	}
	fcx := fx + fw/2
	fcy := fy + fh/2

	var best Pane
	bestScore := math.MaxInt64

	for _, p := range leaves {
		if p == current {
			continue
		}
		px, py, pw, ph := p.Bounds()

		var isCandidate bool
		var axialDist, lateralMid int

		switch dir {
		case 'l':
			isCandidate = px >= fx+fw && py < fy+fh && py+ph > fy
			axialDist = px - (fx + fw)
			lateralMid = intAbs(py + ph/2 - fcy)
		case 'h':
			isCandidate = px+pw <= fx && py < fy+fh && py+ph > fy
			axialDist = fx - (px + pw)
			lateralMid = intAbs(py + ph/2 - fcy)
		case 'j':
			isCandidate = py >= fy+fh && px < fx+fw && px+pw > fx
			axialDist = py - (fy + fh)
			lateralMid = intAbs(px + pw/2 - fcx)
		case 'k':
			isCandidate = py+ph <= fy && px < fx+fw && px+pw > fx
			axialDist = fy - (py + ph)
			lateralMid = intAbs(px + pw/2 - fcx)
		}

		if !isCandidate {
			continue
		}
		score := axialDist*1000 + lateralMid
		if score < bestScore {
			bestScore = score
			best = p
		}
	}
	if best == nil {
		return current
	}
	return best
}

// --- Split ---

// Split finds the leaf containing target, replaces it with a container
// holding the existing leaf and a new leaf wrapping newPane, and returns
// the (possibly new) root.  If the containing parent already has direction
// dir, newPane is added as a sibling rather than creating a nested container.
func Split(root *Node, target Pane, dir Dir, newPane Pane) *Node {
	newLeaf := &Node{Pane: newPane, Ratio: 0.5}
	return insertSibling(root, target, dir, newLeaf)
}

func insertSibling(n *Node, target Pane, dir Dir, sibling *Node) *Node {
	if n.IsLeaf() {
		if n.Pane != target {
			return n
		}
		// Wrap this leaf with a new container.
		containerRatio := n.Ratio
		n.Ratio = 0.5
		sibling.Ratio = 0.5
		return &Node{
			Dir:      dir,
			Children: []*Node{n, sibling},
			Ratio:    containerRatio,
		}
	}
	for i, child := range n.Children {
		if !Contains(child, target) {
			continue
		}
		if child.IsLeaf() && n.Dir == dir {
			// Insert as a sibling in this container.
			half := child.Ratio / 2
			child.Ratio = half
			sibling.Ratio = half
			updated := make([]*Node, 0, len(n.Children)+1)
			updated = append(updated, n.Children[:i+1]...)
			updated = append(updated, sibling)
			updated = append(updated, n.Children[i+1:]...)
			n.Children = updated
			return n
		}
		n.Children[i] = insertSibling(child, target, dir, sibling)
		return n
	}
	return n
}

// --- Close ---

// Close removes the leaf containing target from the tree.
// Returns (newRoot, newFocus, last) where:
//   - newRoot is the updated root (nil if the tree is now empty)
//   - newFocus is a suggested replacement pane (adjacent sibling)
//   - last is true when target was the only pane
func Close(root *Node, target Pane) (newRoot *Node, newFocus Pane, last bool) {
	if root.IsLeaf() {
		return nil, nil, true
	}
	updated, focus := prunePane(root, target)
	if updated == nil {
		return nil, nil, true
	}
	return updated, focus, false
}

// applyPruneResult updates n.Children after a child at index i was pruned.
// If updated is nil the child was fully removed and the adjacent sibling becomes
// the suggested focus (unless focus is already set). Returns (newFocus, newChildren).
func applyPruneResult(n *Node, i int, updated *Node, focus Pane) (Pane, []*Node) {
	if updated == nil {
		if focus == nil {
			if i+1 < len(n.Children) {
				focus = AllLeaves(n.Children[i+1])[0]
			} else if i > 0 {
				ll := AllLeaves(n.Children[i-1])
				focus = ll[len(ll)-1]
			}
		}
		return focus, append(n.Children[:i], n.Children[i+1:]...)
	}
	n.Children[i] = updated
	return focus, n.Children
}

// prunePane recursively removes the leaf containing target.
// Returns (updatedNode, suggestedFocus); updatedNode is nil if n itself
// was the removed leaf or the last child of its container.
func prunePane(n *Node, target Pane) (*Node, Pane) {
	if n.IsLeaf() {
		if n.Pane == target {
			return nil, nil
		}
		return n, nil
	}

	var newFocus Pane
	for i, child := range n.Children {
		if !Contains(child, target) {
			continue
		}
		updated, f := prunePane(child, target)
		if f != nil {
			newFocus = f
		}
		newFocus, n.Children = applyPruneResult(n, i, updated, newFocus)
		break
	}

	if len(n.Children) == 0 {
		return nil, newFocus
	}

	normalizeRatios(n)

	// Collapse single-child containers.
	if len(n.Children) == 1 {
		only := n.Children[0]
		only.Ratio = n.Ratio
		return only, newFocus
	}
	return n, newFocus
}

// --- Layout ---

// AssignBounds walks the tree top-down and calls SetBounds on every leaf
// pane.  x, y are the top-left origin; w, h are the available dimensions.
// For Vertical containers, one column per non-last pane is reserved for a
// visual divider; callers are responsible for rendering the divider.
func AssignBounds(n *Node, x, y, w, h int) {
	if n.IsLeaf() {
		n.Pane.SetBounds(x, y, max(w, 1), max(h, 1))
		return
	}
	if len(n.Children) == 0 {
		return
	}
	switch n.Dir {
	case Horizontal:
		heights := distribute(h, childRatios(n))
		cy := y
		for i, child := range n.Children {
			AssignBounds(child, x, cy, w, heights[i])
			cy += heights[i]
		}
	case Vertical:
		widths := distribute(w, childRatios(n))
		cx := x
		for i, child := range n.Children {
			pw := widths[i]
			if i < len(n.Children)-1 {
				pw = max(pw-1, 1) // reserve 1 column for divider
			}
			AssignBounds(child, cx, y, pw, h)
			cx += widths[i]
		}
	}
}

// TreeHeight returns the total height of the subtree (sum of leaf heights
// for Horizontal containers; leaf height for Vertical containers).
func TreeHeight(n *Node) int {
	if n.IsLeaf() {
		_, _, _, h := n.Pane.Bounds()
		return h
	}
	if n.Dir == Horizontal {
		total := 0
		for _, c := range n.Children {
			total += TreeHeight(c)
		}
		return total
	}
	if len(n.Children) > 0 {
		return TreeHeight(n.Children[0])
	}
	return 0
}

// --- Resize ---

// EqualizeRatios sets all siblings in every container to equal ratios.
func EqualizeRatios(n *Node) {
	if n.IsLeaf() {
		return
	}
	eq := 1.0 / float64(len(n.Children))
	for _, c := range n.Children {
		c.Ratio = eq
		EqualizeRatios(c)
	}
}

// AdjustHeight adjusts the height share of the leaf containing target by
// delta (positive = taller) within its nearest Horizontal container ancestor.
func AdjustHeight(root *Node, target Pane, delta float64) {
	adjustInDir(root, target, Horizontal, delta)
}

// AdjustWidth adjusts the width share of the leaf containing target by
// delta (positive = wider) within its nearest Vertical container ancestor.
func AdjustWidth(root *Node, target Pane, delta float64) {
	adjustInDir(root, target, Vertical, delta)
}

func adjustInDir(n *Node, target Pane, dir Dir, delta float64) bool {
	if n.IsLeaf() {
		return false
	}
	for i, child := range n.Children {
		if !Contains(child, target) {
			continue
		}
		if n.Dir == dir {
			adjustChildRatio(n, i, delta)
			return true
		}
		return adjustInDir(child, target, dir, delta)
	}
	return false
}

func adjustChildRatio(n *Node, idx int, delta float64) {
	const minRatio = 0.1
	child := n.Children[idx]
	maxRatio := 1.0 - minRatio*float64(len(n.Children)-1)
	if maxRatio < minRatio {
		maxRatio = minRatio
	}
	newRatio := math.Max(math.Min(child.Ratio+delta, maxRatio), minRatio)
	diff := newRatio - child.Ratio
	child.Ratio = newRatio
	// Absorb the delta from the adjacent sibling.
	adj := idx + 1
	if adj >= len(n.Children) {
		adj = idx - 1
	}
	if adj >= 0 {
		n.Children[adj].Ratio = math.Max(n.Children[adj].Ratio-diff, minRatio)
	}
	normalizeRatios(n)
}

// MoveToEdge removes target from its current position and places it at
// the edge of the root in direction dir.  toEnd=true places it at the
// bottom/right; toEnd=false places it at the top/left.
func MoveToEdge(root *Node, target Pane, dir Dir, toEnd bool) *Node {
	if root.IsLeaf() {
		return root
	}
	newRoot, _, last := Close(root, target)
	if last || newRoot == nil {
		return root
	}
	focusLeaf := &Node{Pane: target, Ratio: 0.5}
	newRoot.Ratio = 0.5
	if toEnd {
		return &Node{Dir: dir, Children: []*Node{newRoot, focusLeaf}, Ratio: 1.0}
	}
	return &Node{Dir: dir, Children: []*Node{focusLeaf, newRoot}, Ratio: 1.0}
}

// --- Internal helpers ---

func normalizeRatios(n *Node) {
	if len(n.Children) == 0 {
		return
	}
	total := 0.0
	for _, c := range n.Children {
		total += c.Ratio
	}
	if total <= 0 {
		eq := 1.0 / float64(len(n.Children))
		for _, c := range n.Children {
			c.Ratio = eq
		}
		return
	}
	for _, c := range n.Children {
		c.Ratio /= total
	}
}

func childRatios(n *Node) []float64 {
	r := make([]float64, len(n.Children))
	for i, c := range n.Children {
		r[i] = c.Ratio
	}
	return r
}

// distribute divides total among buckets by their ratios.
// Each bucket gets at least 1; the last bucket absorbs the rounding remainder.
func distribute(total int, ratios []float64) []int {
	n := len(ratios)
	if n == 0 {
		return nil
	}
	sizes := make([]int, n)
	allocated := 0
	for i, r := range ratios[:n-1] {
		s := max(int(float64(total)*r), 1)
		sizes[i] = s
		allocated += s
	}
	last := max(total-allocated, 1)
	sizes[n-1] = last
	return sizes
}

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
