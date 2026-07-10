package shirei

import (
	"fmt"
	"testing"
)

// Tests for the identity-tree retention sweep (maybeSweepIdentTree): a
// keyed child not claimed for pruneAfterFrames is removed from its
// parent's child maps, releasing its subtree — the fix for the unbounded
// retention of churning explicit keys (notes/identity-retention-leak.md).
// These pin the sweep's contract: churn stays bounded, short absences
// retain the node, the focused node is exempt, and a detached handle
// queries quietly.

// pruneWindow is the worst-case retention in frames: staleness threshold
// plus the sweep's amortization interval (the sweep runs on the first
// frame where both a sweep is due and the node is stale).
const pruneWindow = pruneAfterFrames + sweepInterval

func TestKeyedChurnStaysBounded(t *testing.T) {
	ResetInputSession()
	scope := new(int)
	var parent, firstItem *identNode
	serial := 0
	view := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			parent = currentIdent
			ContainerWithKey(fmt.Sprintf("item-%d", serial), AttrSet{}, func() {
				if serial == 0 {
					firstItem = currentIdent
				}
			})
			// a stable key alongside the churn must keep its node
			ContainerWithKey("stable", AttrSet{}, func() {})
		})
	}

	identFrame(view)
	stable := parent.keyed["stable"]
	const frames = 40
	for serial = 1; serial <= frames; serial++ {
		identFrame(view)
	}

	// live window: the current key plus up to pruneWindow recent ones
	// (and the stable key)
	if got := len(parent.keyed); got > pruneWindow+2 {
		t.Errorf("churning keys retained %d nodes, want <= %d", got, pruneWindow+2)
	}
	if !firstItem.detached {
		t.Errorf("long-unclaimed keyed node was not detached")
	}
	if parent.keyed["stable"] != stable {
		t.Errorf("stable key lost its node during churn: %p -> %p", stable, parent.keyed["stable"])
	}
}

func TestKeyedNodeRetainedWithinWindow(t *testing.T) {
	// an absence shorter than the window must keep the node — a one-frame
	// conditional flicker or a popup reopening should not remount
	ResetInputSession()
	scope := new(int)
	show := true
	var id ContainerId
	view := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			if show {
				id = ContainerWithKey("row", AttrSet{}, func() {})
			}
		})
	}

	identFrame(view)
	first := id
	show = false
	identFrame(view)
	show = true
	identFrame(view)
	if id != first {
		t.Errorf("one-frame absence must retain the node: %p -> %p", first, id)
	}
}

func TestPrunedKeyRevivesFresh(t *testing.T) {
	ResetInputSession()
	scope := new(int)
	show := true
	var id ContainerId
	view := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			if show {
				id = ContainerWithKey("row", AttrSet{}, func() {})
			}
		})
	}

	identFrame(view)
	first := id
	show = false
	for range pruneWindow * 2 {
		identFrame(view)
	}
	if !resolveIdent(first).detached {
		t.Fatalf("node should be detached after %d absent frames", pruneWindow*2)
	}
	show = true
	identFrame(view)
	if id == first {
		t.Errorf("pruned-then-revived key must get a fresh node")
	}
}

func TestFocusPinsNodeThroughAbsence(t *testing.T) {
	// a focused keyed row scrolled out of a virtual list and back must
	// keep focus: the focused node is exempt from the sweep while its
	// ancestors keep it reachable
	ResetInputSession()
	scope := new(int)
	show := true
	grabFocus := true
	var id ContainerId
	view := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			if show {
				id = ContainerWithKey("row", AttrSet{Focusable: true}, func() {
					if grabFocus {
						Focus()
					}
				})
			}
		})
	}

	identFrame(view)
	grabFocus = false
	identFrame(view) // requested focus takes effect at this frame's start
	first := id
	if !IdHasFocus(first) {
		t.Fatalf("setup: the row should hold focus")
	}

	show = false
	for range pruneWindow * 2 {
		identFrame(view)
	}
	show = true
	identFrame(view)
	if id != first {
		t.Errorf("focused node must survive a long absence: %p -> %p", first, id)
	}
	if !IdHasFocus(id) {
		t.Errorf("focus must survive scroll-away/scroll-back")
	}
	ResetInputSession() // don't leak the focus pin into other tests
}

func TestDetachedHandleDoesNotForceSettle(t *testing.T) {
	// a stale ContainerId held in app state and queried every frame must
	// not put the frame loop in a permanent two-passes-per-frame regime
	ResetInputSession()
	scope := new(int)
	show := true
	var target ContainerId
	var rect Rect
	view := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			if show {
				target = ContainerWithKey("gone", AttrSet{}, func() {})
			} else {
				rect = GetResolvedRectOf(target)
			}
		})
	}

	identFrame(view)
	show = false
	// while the node is merely absent (still attached), the query keeps
	// requesting settle passes — that's the pre-existing forward-reference
	// behavior; each of these calls runs two passes
	for range pruneWindow * 2 {
		identFrame(view)
	}
	if !resolveIdent(target).detached {
		t.Fatalf("node should be detached by now")
	}

	before := FrameNumber
	identFrame(view)
	if got := FrameNumber - before; got != 1 {
		t.Errorf("querying a detached handle ran %d passes, want 1", got)
	}
	if rect != (Rect{}) {
		t.Errorf("detached handle should answer a zero rect, got %v", rect)
	}
}

func TestPositionalPeakPruned(t *testing.T) {
	// pos is bounded by peak (type, ordinal) — the sweep releases the peak
	// once the count shrinks (a once-rendered long non-virtualized list)
	ResetInputSession()
	scope := new(int)
	var parent *identNode
	count := 50
	view := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			parent = currentIdent
			for i := 0; i < count; i++ {
				Container(AttrSet{}, func() {})
			}
		})
	}

	identFrame(view)
	if got := len(parent.pos); got != 50 {
		t.Fatalf("setup: want 50 positional children, got %d", got)
	}
	count = 2
	for range pruneWindow * 2 {
		identFrame(view)
	}
	if got := len(parent.pos); got != 2 {
		t.Errorf("positional peak not released: %d nodes retained, want 2", got)
	}
}
