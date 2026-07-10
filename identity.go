package shirei

import (
	"fmt"
	"runtime"
	"unsafe"
)

// The identity tree: a persistent tree of nodes maintained in lockstep with
// the per-frame container tree, giving every container a stable identity
// (the node's pointer) across frames. It replaced the old scope-id hashing
// (scope.go, deleted), whose identity depended on how an id was boxed into
// `any` — see notes/identity-tree-plan.md for the design and migration
// history.
//
// Reconciliation rule, per parent, per frame:
//
//  1. Explicit id (LayoutWithKey's id != nil): matched under this parent by Go
//     interface equality — pointers by pointer, strings by content. Scoped:
//     the same id under two different parents is two distinct nodes. If the
//     node's component type changed, it's a remount (fresh node). Claiming
//     the same id twice under one parent in one frame is a duplicate
//     (counted; will become a loud error when the tree is load-bearing).
//
//  2. Positional: matched by (component type, per-type ordinal), where the
//     component type is the builder closure's code pointer — every
//     evaluation of one func literal shares it, so loop iterations line up
//     by ordinal, and a conditionally-inserted sibling of a different type
//     does not shift them. This is React's (type, key|index) rule; a
//     same-type insertion still shifts later same-type siblings — that's
//     what explicit ids are for.
//
//     "Component type" means the literal's SOURCE POSITION, not its code
//     address: the compiler duplicates func literals per inline instance
//     of their enclosing function, so one literal can carry several code
//     pointers depending on which call site built it. funcCodePtr
//     canonicalizes the pointer to file:line so those clones stay one
//     type — a view built through different call paths on different
//     frames keeps its identity.
//
// The tree carries everything: render data, UI hooks, and interaction
// state (hover/focus/active) all live on nodes; ids are pure
// reconciliation keys, matched by value — dynamic strings and any other
// comparable values are fully legal ids.

// ContainerId is an opaque handle to a container's identity, returned by
// ContainerWithKey (and Container/Element/CurrentId/GetLastId). Pass it anywhere an id
// is accepted — focus, hover, screen-rect queries, popup anchors. It is a
// value you hold and hand back; there is nothing to inspect. (The backing
// identity node is deliberately unexported.)
type ContainerId *identNode

type identNode struct {
	parent *identNode
	typ    uintptr // builder code pointer at creation (0 = no builder)
	key    any     // explicit id (nil for positional); held, so it pins its pointee

	keyed map[any]*identNode           // explicit-id children
	pos   map[identChildKey]*identNode // positional children by (type, ordinal)

	focusTrapOwner *identNode // the `buildingFocusTrap` that this node say at layout time

	// per-frame reconciliation state
	claimFrame int64       // frame the claim counters below were reset for
	claims     []typeClaim // positional children of each type claimed this frame

	visitFrame int64 // frame this node itself was last claimed

	// stage 2: the container's render data lives on its node. rdFrame
	// stamps which frame wrote it — see prevRenderData for how that
	// reproduces the old map's drop-if-unrendered semantics.
	rd      RenderData
	rdFrame int64

	// bornFrame stamps the frame this node's rd (re)appeared after an
	// absence. Animations use it to tell "has presented-frame history"
	// (animate from the previous data) from "born during the current
	// RunFrameFn call" (snap to the computed layout: the only previous
	// data is a discarded settle pass — see runFirstFrame).
	bornFrame int64

	// stage 3: UI hook state (Use/UseWithInit) lives on its node, keyed by
	// the hook's itemKey. Retention is prune-per-frame, matching the old
	// hooksMap double buffer: a slot untouched for a full frame reads as
	// absent and is re-initialized on next use (the stale value lingers in
	// the map until then — same order of retention as the nodes themselves).
	hooks map[any]hookSlot

	// detached marks a node pruned from the tree (sweepIdentTree): its
	// parent's child-map entry is gone, so reconciliation can never reach
	// it again — it lives on only through outside references (a stale
	// ContainerId in app state, the focus globals). Queries on a detached
	// node return zeros WITHOUT requesting a settle pass; see
	// queriedRenderData.
	detached bool
}

type hookSlot struct {
	value any
	frame int64 // last frame this slot was used
}

// identChildKey addresses a positional child: one flat map rather than
// per-type slices. For realistic distributions (mostly n=1..20 per type;
// big collections use explicit ids and never come through here) the two
// perform the same, and the flat map has no nil-slice/bounds guard classes
// — reads off a nil map are safe, so the lookup path needs no checks at
// all. Note that all nil-builder Elements share type 0, so a conditionally
// inserted Element shifts later Element siblings' ordinals; benign
// (Elements have no builder, hence no hooks or scroll state — worst case a
// one-frame animation blip), but it bounds the different-type-insertion
// guarantee.
type identChildKey struct {
	typ     uintptr
	ordinal int
}

type typeClaim struct {
	typ uintptr
	n   int
}

// identRoot persists across frames; currentIdent is the build cursor,
// mirroring `current *_Container`.
var identRoot = newNode(nil, 0, nil)
var currentIdent *identNode

// newNode: rdFrame must start at -1, NOT the zero value — on the very
// first frame of a process FrameNumber-1 is 0, and a zero rdFrame would
// make every fresh container look like it rendered "last frame"
// (FirstRender false, AutoFocus suppressed) on that frame only.
func newNode(parent *identNode, typ uintptr, key any) *identNode {
	return &identNode{parent: parent, typ: typ, key: key, rdFrame: -1}
}

// identDupCount counts explicit ids claimed more than once under the same
// parent within one frame — a program bug: an id is an identity claim, and
// two claimants under one parent means state, hover, and focus can't tell
// them apart. Reported loudly (capped so a 60fps loop can't flood stderr);
// the same id under DIFFERENT parents is fine.
var identDupCount int64
var identDupLogged int

func reportDuplicateKey(key any) {
	identDupCount++
	const logCap = 8
	if identDupLogged < logCap {
		identDupLogged++
		fmt.Printf("shirei: duplicate container key %v (%T) claimed twice under one parent in a single frame\n", key, key)
		if identDupLogged == logCap {
			fmt.Println("shirei: further duplicate-key reports suppressed")
		}
	}
}

// resolveIdent unwraps a ContainerId handle to its node. Container state
// (focus, hover, geometry) is addressed only by handle — the handle IS the
// node — so there is no key→node lookup: hold the ContainerId that
// ContainerWithKey/Container/CurrentId/GetLastId returned. nil handle → nil node.
func resolveIdent(id ContainerId) *identNode {
	return (*identNode)(id)
}

// funcCodePtr returns a builder func's component type: a value that every
// closure created from the same source-level func literal shares. The raw
// code pointer alone is not that value — the compiler clones a func
// literal per INLINE INSTANCE of its enclosing function, so one literal
// can have several code addresses depending on which call site built it
// (verified empirically; it remounted a virtual list mid-test — see
// notes/architecture.md, sharp edges). So the code pointer is
// canonicalized to the literal's source position: the first pointer seen
// for a given file:line stands for all later clones of it. The FuncForPC
// lookup runs once per distinct code address in the process, then it's a
// single map hit per claim.
//
// Deliberate coarseness: two literals written on one source line share a
// type, and generic instantiations of one literal merge across type
// parameters (same-gcshape instantiations already share code anyway).
// Same source = same component type is the intended meaning.
func funcCodePtr(f func()) uintptr {
	pc := rawFuncCodePtr(f)
	if pc == 0 {
		return 0
	}
	if typ, ok := funcTypeByPC[pc]; ok {
		return typ
	}
	typ := pc
	if fn := runtime.FuncForPC(pc); fn != nil {
		file, line := fn.FileLine(pc)
		src := funcSourceKey{file: file, line: line}
		if canonical, ok := funcTypeBySource[src]; ok {
			typ = canonical
		} else {
			funcTypeBySource[src] = pc
		}
	}
	funcTypeByPC[pc] = typ
	return typ
}

type funcSourceKey struct {
	file string
	line int
}

var funcTypeByPC = map[uintptr]uintptr{}
var funcTypeBySource = map[funcSourceKey]uintptr{}

// rawFuncCodePtr extracts a func value's code pointer: a func value
// points at a closure object whose first word is the code address.
func rawFuncCodePtr(f func()) uintptr {
	if f == nil {
		return 0
	}
	return **(**uintptr)(unsafe.Pointer(&f))
}

// frameInProgress is true while RunFrameFn is building/resolving a frame;
// it decides which frame prevRenderData considers "most recently completed".
var frameInProgress bool

// prevRenderData returns the node's render data from the most recently
// completed frame, mirroring the renderData map's semantics exactly:
// during a frame's build that's the previous frame; between frames (e.g. a
// test querying after RunFrameFn returned) it's the just-finished one. A
// node not rendered in that frame reads as absent — reproducing the map's
// drop-if-unrendered behavior (FirstRender fires again, scroll offset
// resets, animations have no source rect).
func (n *identNode) prevRenderData() (RenderData, bool) {
	want := FrameNumber
	if frameInProgress {
		want = FrameNumber - 1
	}
	if n.rdFrame == want {
		return n.rd, true
	}
	return RenderData{}, false
}

// queriedRenderData is prevRenderData for the public geometry accessors:
// a miss flags the frame as incomplete. Internal callers (FirstRender,
// animation sources, scroll restore) keep using prevRenderData directly —
// for them absence is a normal state, not an unmet dependency.
//
// A detached node is exempt: it can never resolve (reconciliation can't
// reach it), so requesting a settle pass for it would put the frame loop
// in a permanent two-passes-per-frame regime. Its queries just answer
// zeros, like a nil handle.
func queriedRenderData(n *identNode) RenderData {
	rd, ok := n.prevRenderData()
	if !ok && frameInProgress && !n.detached {
		stabilizeRequested = true
	}
	return rd
}

// claimChild resolves (or creates) the identity node for a child opened
// under p this frame, per the reconciliation rule above.
func (p *identNode) claimChild(key any, typ uintptr) *identNode {
	var node *identNode
	if key != nil {
		node = p.keyed[key] // nil-map read is safe
		// duplicate check BEFORE the remount decision: a second claim of
		// the same key this frame is a duplicate even if its type differs
		// (remounting would otherwise silently swallow it)
		if node != nil && node.visitFrame == FrameNumber {
			reportDuplicateKey(key)
		}
		if node != nil && node.typ != typ {
			node = nil // type change on the same key = remount
		}
		if node == nil {
			node = newNode(p, typ, key)
			if p.keyed == nil {
				p.keyed = make(map[any]*identNode)
			}
			p.keyed[key] = node
		}
	} else {
		if p.claimFrame != FrameNumber {
			p.claimFrame = FrameNumber
			p.claims = p.claims[:0]
		}
		ordinal := 0
		claimed := false
		for i := range p.claims {
			if p.claims[i].typ == typ {
				ordinal = p.claims[i].n
				p.claims[i].n++
				claimed = true
				break
			}
		}
		if !claimed {
			p.claims = append(p.claims, typeClaim{typ: typ, n: 1})
		}
		k := identChildKey{typ: typ, ordinal: ordinal}
		node = p.pos[k] // nil-map read is safe
		if node == nil {
			node = newNode(p, typ, nil)
			if p.pos == nil {
				p.pos = make(map[identChildKey]*identNode)
			}
			p.pos[k] = node
		}
	}

	node.visitFrame = FrameNumber
	return node
}

// --- retention sweep ---
//
// Without it, `keyed` is insert-only and a churning key population (virtual
// list rows keyed per item, dynamic tabs) retains a node — with its render
// data, hooks, child maps, and the key value it pins — for every distinct
// key ever shown: notes/identity-retention-leak.md. The sweep runs from
// RunFrameFn AFTER the settle loop's final pass, which is what makes it
// safe: every node claimed in any pass of the frame is stamped by then, so
// a forward-referenced key never looks stale mid-frame.

// pruneAfterFrames is a memory/continuity knob, not a correctness bound: a
// node unclaimed for a full frame already reads as absent (prevRenderData,
// hook slots), so pruning it loses only the allocation and the pointer
// identity. It counts frame PASSES — a settle frame advances FrameNumber
// twice — hence a few frames of slack rather than the minimal 2.
const pruneAfterFrames = 4

// sweepInterval amortizes the sweep: the full-tree walk each frame costed
// +6% time geomean (+15% on the deep-tree bench), and nothing about the
// leak needs frame-exact pruning. Every 8th frame puts the walk under 1%
// and stretches worst-case retention to pruneAfterFrames+sweepInterval
// frames (~200ms at 60fps) — still bounded, which is all that matters.
const sweepInterval = 8

var lastSweepFrame int64

// maybeSweepIdentTree prunes child nodes not claimed within
// pruneAfterFrames, at most once per sweepInterval frames. Deleting the
// map entry is the whole release: the entry holds the only tree reference
// to the subtree, so render data, hooks, and descendants go to the GC with
// it (an outside reference — a held ContainerId, the focus globals — keeps
// its single node alive, detached, and that's fine).
//
// The focused and active nodes are exempt while an ancestor keeps them
// reachable: focus must survive a keyed row scrolling out of a virtual
// list and back (their stale CHILDREN are still swept; the revived row
// rebuilds its subtree fresh, which is what absence already means for
// state). nextFocused is exempt for the same reason: it's a focus about to
// take effect next frame (FocusOn with a held handle).
func maybeSweepIdentTree() {
	if FrameNumber-lastSweepFrame < sweepInterval {
		return
	}
	lastSweepFrame = FrameNumber
	identRoot.sweepChildren(FrameNumber - pruneAfterFrames)
}

func (n *identNode) sweepChildren(staleFrame int64) {
	for key, child := range n.keyed {
		if child.visitFrame <= staleFrame && !isRetentionExempt(child) {
			delete(n.keyed, key)
			child.detached = true
		} else {
			child.sweepChildren(staleFrame)
		}
	}
	for key, child := range n.pos {
		if child.visitFrame <= staleFrame && !isRetentionExempt(child) {
			delete(n.pos, key)
			child.detached = true
		} else {
			child.sweepChildren(staleFrame)
		}
	}
}

func isRetentionExempt(n *identNode) bool {
	return n == focused || n == active || n == nextFocused
}
