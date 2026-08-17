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
	current           *_Container
	surfaces          []Surface
	surfaceHash       uint64 // hash of last presented surfaces (present skip)
	SurfaceCount      int    // debug: surfaces emitted last pass
	hoverables        []HoverableArtifacts
	focusables        []*identNode
	hoverList         []*identNode
	touchingList      []ContainerTouchInfo
	directHovered     *identNode
	frameFocusTrap    *identNode // stays for the whole frame
	buildingFocusTrap *identNode // only while the trap is laying out content
	popups []func() // deferred end-of-frame builders
	popupZ f32 // drain index (1-based) while PopupsHost runs a callback; else 0

	// Identity tree and interaction focus (per-UI world).
	identRoot       *identNode
	currentIdent    *identNode // build cursor for identity, mirrors current
	identDupCount   int64
	identDupLogged  int
	lastSweepFrame  int64
	frameInProgress bool

	// Mouse/key focus graph (identity-node pointers).
	active      *identNode // engaged with the mouse
	focused     *identNode // receives key events
	prevFocused *identNode
	nextFocused *identNode // requested focus

	// Widget command queue (per-UI).
	pendingCommands map[_CommandKey]pendingCommand

	// Frame clock and pass control (per-UI).
	FrameNumber        int64
	runFirstFrame      int64 // FrameNumber of the current RunFrameFn call's first pass
	frameStart         time.Time
	timeDelta          float32 // fraction of a second since previous pass start
	stabilizeRequested bool
	lastClickTime      time.Time
	lastClickPoint     Vec2
	clickStreak        int
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
