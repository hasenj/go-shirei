package shirei

import "time"

// UI is the per-window (per-frame-world) runtime context. Today the process
// has a single package-level ui pointer; Measure and multi-window will swap
// or allocate additional *UI values.
//
// Field migration onto UI is incremental: some state still lives in package
// vars and will move here over time.
//
// Shared caches (fonts, shape, glyphs, images, …) are process-global on
// package res / SharedResources() — not a field of UI.
type UI struct {
	// Host is backend ↔ app I/O for this UI (window size, input, clipboard
	// requests, IME anchors, next-frame, …). Nested rather than embedded so
	// call sites read ui.Host.* during migration; embedding is optional later.
	Host Host

	// Build cursor and pass buffers (layout / surface emit for one frame world).
	current          *_Container
	nextAccess       AccessAttrs
	access           []AccessNode
	publishedAccess  []AccessNode
	accessByName     map[string][]int
	accessPaintOrder int
	surfaces         []Surface
	glyphRuns        []GlyphRun
	surfaceHash      uint64 // hash of last presented surfaces (present skip)
	SurfaceCount     int    // surfaces emitted last pass
	ContainerCount   int    // live layout tree (last pass)
	ContainerBuilt   int    // ContainerWithKey this pass, including nested Measure
	containerBuilt   int    // running ContainerWithKey count for the current pass
	// treeCount counts containers in the current pass's live tree (root
	// excluded). Nested Measure increments the measure-UI's own counter,
	// which is discarded; unlike containerBuilt it is not transferred back.
	treeCount            int
	anyFocusable         bool // set during build; skip collectFocusables when false
	anyTabAfter          bool // set during build; skip gatherTabAfter when false
	anyAccess            bool // set by AssignAccess or text capture; skip access walk when false
	hoverables           []HoverableArtifacts
	focusables           []*identNode
	tabAfterSpecs        []*_Container
	hoverList            []*identNode
	touchingList         []ContainerTouchInfo
	directHovered        *identNode
	frameFocusTrap       *identNode // stays for the whole frame
	buildingFocusTrap    *identNode // only while the trap is laying out content
	trapMountedThisFrame bool       // a FocusTrap's FirstRender stole focus this pass
	popups               []func()   // deferred end-of-frame builders
	popupZ               f32        // drain index (1-based) while PopupsHost runs a callback; else 0

	// Identity tree and interaction focus (per-UI world).
	identRoot       *identNode
	currentIdent    *identNode // build cursor for identity, mirrors current
	identDupCount   int64
	identDupLogged  int
	lastSweepFrame  int64
	frameInProgress bool

	// Mouse/key focus graph (identity-node pointers).
	active       *identNode // engaged with the mouse
	focused      *identNode // receives key events
	prevFocused  *identNode
	nextFocused  *identNode // requested focus
	focusVisible bool       // explicit visual indication for current/pending keyboard focus

	// Widget command queue (per-UI).
	pendingCommands map[_CommandKey]pendingCommand

	// lastCopy is the clipboard-copy request harvested from the most recently
	// completed RunFrameFn (Host.Copy is cleared at harvest). Read by the
	// input-command access dump (lastFrameCopy), under the frame lock.
	lastCopy string

	// FrameTimings is filled by RunFrameFn (produce) and the backend (paint).
	FrameTimings FrameTimings

	// Frame clock and pass control (per-UI).
	FrameNumber   int64
	runFirstFrame int64 // FrameNumber of the current RunFrameFn call's first pass
	frameStart    time.Time
	timeDelta     float32 // fraction of a second since previous pass start
	// pinnedTimeDelta, when true, keeps timeDelta as set by the caller
	// instead of sampling the wall clock. Layout dump tests pin animation
	// goldens this way.
	pinnedTimeDelta    bool
	stabilizeRequested bool
	lastClickTime      time.Time
	lastClickPoint     Vec2
	clickStreak        int

	// Container pool (see swapContainerSlab / newContainer): two slabs
	// alternate per frame pass, so a container handed out in pass N stays
	// intact through pass N+1 and is recycled at the start of pass N+2.
	containerSlabs [2]containerSlab
	slabIndex      int
}

// containerSlab is one half of the container pool: every container ever
// handed out while this slab was active, in hand-out order. used is the
// count consumed this pass. dirty is the high-water mark of entries that
// still hold data from the last pass that used this slab. Reset happens at
// hand-out (newContainer); the unused tail [used:dirty] is reset at swap
// so stale pointers are not retained when the tree shrinks. The slab never
// shrinks — its high-water mark of ~600 B shells is the pool's
// steady-state footprint.
type containerSlab struct {
	items []*_Container
	used  int
	dirty int
}

// Package-level process handles. mu stays the frame lock; dataHooks (UseData)
// stays a separate package global by design — not UI-scoped.
//
// res is initialized at package-var time (not in init) so other files' init
// functions that register fonts can use the resource pack safely.
var (
	ui  *UI
	res = NewResources() // process-shared caches (fonts, shape, glyphs, images, …)
)

// NewUI constructs a UI world. Shared resources are always the process pack
// (SharedResources / res), not owned by the UI.
func NewUI() *UI {
	root := newNode(nil, 0, nil)
	return &UI{
		Host:            defaultHost(),
		surfaces:        make([]Surface, 0, 1024*16),
		popups:          make([]func(), 0, 128),
		identRoot:       root,
		pendingCommands: make(map[_CommandKey]pendingCommand),
		frameStart:      time.Now(),
	}
}

// swapContainerSlab flips the active container slab at the start of a frame
// pass. Containers are frame-transient but not pass-transient: ui.hoverables
// built in pass N is read by pass N+1's hover detection, so pass-N
// containers must survive one extra pass. The slab being switched to was
// last used two passes ago — nothing can reference its containers anymore.
//
// Hover detection at pass start reads the previous pass's containers, which
// live in the other slab; the incoming slab's entries are unread between
// swap and their individual reuse. Reset happens at hand-out (newContainer)
// so the cache lines are written as they are reused. When the tree shrinks,
// entries beyond this pass's used count still hold data from an earlier
// occupancy of this slab; dirty is that high-water mark, and the unused
// tail is reset here so stale pointers (child links, identity nodes,
// shape-cache glyph stamps) are not retained indefinitely. When the tree
// grows, used exceeds dirty and the tail loop is empty — new entries are
// already zero.
//
// Always called under the frame lock (RunFrameFn / measureLocked pass loops).
func swapContainerSlab() {
	ui.slabIndex ^= 1
	s := &ui.containerSlabs[ui.slabIndex]
	if s.used < s.dirty {
		for _, c := range s.items[s.used:s.dirty] {
			*c = _Container{
				children:  c.children[:0],
				wrapLines: c.wrapLines[:0],
			}
		}
	}
	s.dirty = s.used
	s.used = 0
}

// newContainer hands out a zeroed container from the active slab, growing
// the slab the first time the tree reaches this size. Steady-state frames
// allocate no containers at all. Reused entries are reset here (keeping
// children / wrapLines capacity; glyphData references immutable cached geometry).
func newContainer() *_Container {
	s := &ui.containerSlabs[ui.slabIndex]
	if s.used < len(s.items) {
		c := s.items[s.used]
		s.used++
		*c = _Container{children: c.children[:0], wrapLines: c.wrapLines[:0]}
		return c
	}
	c := new(_Container)
	s.items = append(s.items, c)
	s.used++
	return c
}

// ActiveUI returns the currently building/presenting UI. Nil only before init.
func ActiveUI() *UI {
	return ui
}

// SharedResources returns the process-shared resource pack (fonts, shape
// caches, glyph bitmaps, image registry, IM filesystem caches, …).
func SharedResources() *Resources {
	return res
}

// bindUI sets the active UI. Input is always ui.Host.Input / FrameInput
// (or GetInputState / GetFrameInput); swapping ui is the only context switch.
func bindUI(u *UI) {
	if u == nil {
		panic("shirei: bindUI(nil)")
	}
	ui = u
}

func init() {
	bindUI(NewUI())
}
