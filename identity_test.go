package shirei

import (
	"fmt"
	"testing"
)

// Stage-1 tests for the identity tree (identity.go): the tree is not yet
// load-bearing, so these assert its own invariants — node-pointer
// stability across frames under the (explicit id | type+ordinal)
// reconciliation rule. They encode, at the identity level, exactly the
// semantics the later stages will hang state on.

func identFrame(fn FrameFn) {
	WindowSize = Vec2{800, 600}
	RunFrameFn(fn)
}

func TestIdentPositionalStability(t *testing.T) {
	scope := new(int)
	var got [][]*identNode
	view := func() {
		var frame []*identNode
		ContainerWithKey(scope, AttrSet{}, func() {
			for i := 0; i < 3; i++ {
				Container(AttrSet{}, func() {
					frame = append(frame, currentIdent)
				})
			}
		})
		got = append(got, frame)
	}
	identFrame(view)
	identFrame(view)

	first := got[0]
	if first[0] == first[1] || first[1] == first[2] || first[0] == first[2] {
		t.Fatalf("loop iterations must get distinct nodes: %p %p %p", first[0], first[1], first[2])
	}
	for i := range first {
		if got[1][i] != first[i] {
			t.Errorf("node %d not stable across frames: %p -> %p", i, first[i], got[1][i])
		}
	}
}

func TestIdentDifferentTypeInsertionDoesNotShift(t *testing.T) {
	// ONE view closure with a captured toggle, matching how real apps
	// conditionally render. (Building fresh closure chains through
	// different call sites is also safe — inline clones of a literal
	// canonicalize to its source position; see funcCodePtr and
	// TestFuncCodePtrMergesInlineClones.)
	scope := new(int)
	divider := func() {} // a different component type than the items below

	withDivider := false
	var items []*identNode
	view := func() {
		items = items[:0]
		ContainerWithKey(scope, AttrSet{}, func() {
			if withDivider {
				Container(AttrSet{}, divider)
			}
			for i := 0; i < 4; i++ {
				Container(AttrSet{}, func() {
					items = append(items, currentIdent)
				})
			}
		})
	}

	identFrame(view)
	before := append([]*identNode(nil), items...)
	withDivider = true
	identFrame(view)
	after := append([]*identNode(nil), items...)

	for i := range before {
		if after[i] != before[i] {
			t.Errorf("item %d shifted identity after different-type insertion: %p -> %p",
				i, before[i], after[i])
		}
	}
}

func TestIdentSameTypeInsertionShifts(t *testing.T) {
	// documented limitation, pinned so a change is deliberate: inserting a
	// sibling of the SAME type shifts later same-type ordinals — explicit
	// ids are the tool for dynamic same-type collections
	scope := new(int)
	var items []*identNode
	item := func() {
		items = append(items, currentIdent)
	}
	extraInFront := false
	view := func() {
		items = items[:0]
		ContainerWithKey(scope, AttrSet{}, func() {
			if extraInFront {
				Container(AttrSet{}, item)
			}
			for i := 0; i < 3; i++ {
				Container(AttrSet{}, item)
			}
		})
	}

	identFrame(view)
	before := append([]*identNode(nil), items...)
	extraInFront = true
	identFrame(view)
	after := append([]*identNode(nil), items...)

	if after[0] != before[0] {
		t.Errorf("inserted item should take the first ordinal's node")
	}
	if after[len(after)-1] == before[len(before)-1] {
		t.Errorf("last same-type item should have shifted to a new node")
	}
}

func TestIdentDynamicStringIdsStable(t *testing.T) {
	// the boxing trap dies at the tree level: ids are matched by value, so
	// a string built fresh every frame still resolves to the same node
	scope := new(int)
	var got [][]*identNode
	view := func() {
		var frame []*identNode
		ContainerWithKey(scope, AttrSet{}, func() {
			for i := 0; i < 3; i++ {
				id := fmt.Sprintf("row-%d", i) // fresh string (and boxing) every frame
				ContainerWithKey(id, AttrSet{}, func() {
					frame = append(frame, currentIdent)
				})
			}
		})
		got = append(got, frame)
	}
	identFrame(view)
	identFrame(view)

	for i := range got[0] {
		if got[1][i] != got[0][i] {
			t.Errorf("dynamic string id row-%d not stable: %p -> %p", i, got[0][i], got[1][i])
		}
	}
}

func TestIdentExplicitIdsScopedToParent(t *testing.T) {
	// the same explicit id under two different parents is two distinct
	// nodes — and legal (no duplicate counted)
	scope := new(int)
	var a, b *identNode
	dupsBefore := identDupCount
	identFrame(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			Container(AttrSet{}, func() {
				ContainerWithKey("shared", AttrSet{}, func() { a = currentIdent })
			})
			Container(AttrSet{}, func() {
				ContainerWithKey("shared", AttrSet{}, func() { b = currentIdent })
			})
		})
	})
	if a == b {
		t.Errorf("same id under different parents must be distinct nodes")
	}
	if identDupCount != dupsBefore {
		t.Errorf("cross-parent id reuse wrongly counted as duplicate")
	}
}

func TestIdentDuplicateDetection(t *testing.T) {
	scope := new(int)
	twin := func() {}
	dupsBefore := identDupCount
	identFrame(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			ContainerWithKey("twin", AttrSet{}, twin)
			ContainerWithKey("twin", AttrSet{}, twin)
		})
	})
	if identDupCount != dupsBefore+1 {
		t.Errorf("same id twice under one parent in one frame: dup count %d, want %d",
			identDupCount-dupsBefore, 1)
	}
}

func TestIdentDuplicateDetectedAcrossTypeChange(t *testing.T) {
	// two different literals under the same key in one frame: still a
	// duplicate (the remount rule must not swallow it)
	scope := new(int)
	dupsBefore := identDupCount
	identFrame(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			ContainerWithKey("twin", AttrSet{}, func() {})
			ContainerWithKey("twin", AttrSet{}, func() {})
		})
	})
	if identDupCount != dupsBefore+1 {
		t.Errorf("duplicate across type change: dup count %d, want 1", identDupCount-dupsBefore)
	}
}

// inlineCloneBuilder returns its nested literal; it is small enough that
// the compiler inlines it at each call site, cloning the literal into a
// distinct compiled function per site — distinct raw code pointers for
// ONE source-level literal. funcCodePtr must canonicalize the clones to
// one component type. (The widgets reveal test pins the same property
// end-to-end: a virtual list must keep its scroll state when its builder
// reaches it through different call sites on different frames.)
func inlineCloneBuilder() func() {
	n := 0
	return func() { n++ }
}

func TestFuncCodePtrMergesInlineClones(t *testing.T) {
	f1 := inlineCloneBuilder() // call site 1
	f2 := inlineCloneBuilder() // call site 2
	if funcCodePtr(f1) != funcCodePtr(f2) {
		t.Fatalf("clones of one literal must share a component type: raw %x vs %x",
			rawFuncCodePtr(f1), rawFuncCodePtr(f2))
	}
	if funcCodePtr(nil) != 0 {
		t.Fatal("nil builder must stay type 0")
	}
	// canonicalization must not over-merge: distinct literals (distinct
	// source lines) keep distinct types
	a := func() { _ = 1 }
	b := func() { _ = 2 }
	if funcCodePtr(a) == funcCodePtr(b) {
		t.Fatal("different literals must keep different component types")
	}
	// if the inliner ever stops cloning, the merge assertion above holds
	// vacuously — surface that instead of silently passing
	if rawFuncCodePtr(f1) == rawFuncCodePtr(f2) {
		t.Skip("inliner did not clone the literal on this toolchain; merge property not exercised")
	}
}

func TestIdentTypeChangeOnKeyRemounts(t *testing.T) {
	scope := new(int)
	var first, second *identNode
	builderA := func() { first = currentIdent }
	builderB := func() { second = currentIdent }
	identFrame(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			ContainerWithKey("slot", AttrSet{}, builderA)
		})
	})
	identFrame(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			ContainerWithKey("slot", AttrSet{}, builderB)
		})
	})
	if first == second {
		t.Errorf("type change on the same key must remount (fresh node)")
	}
}
