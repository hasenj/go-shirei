package shirei

import (
	"cmp"
	"hash/maphash"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.hasen.dev/generic"
	g "go.hasen.dev/generic"
)

var mutex sync.Mutex

// WithFrameLock runs fn while holding the frame lock, serializing it against the
// render loop. Background goroutines use it to mutate shared state (caches,
// stores) safely — they block until the current frame finishes if one is
// in progress.
//
// Do not call WithFrameLock from code that already runs inside RunFrameFn
// (button handlers, widget bodies, layout): the frame lock is already held
// by that goroutine, and a nested Lock deadlocks the whole app. Mutate
// UI-thread state directly on that path; reserve WithFrameLock for
// background work only.
func WithFrameLock(fn func()) {
	mutex.Lock()
	defer mutex.Unlock()

	fn()
}

type FrameFn func()

// for the backend
var requested atomic.Bool

// RequestNextFrame asks the backend to render another frame after this one, even
// if no input arrives — used by animations and by state that settles over
// several frames.
func RequestNextFrame() {
	requested.Store(true)
}

// FrameRequested reports whether another frame has been requested; backends
// check it to decide whether to keep rendering or go idle.
func FrameRequested() bool {
	return requested.Load()
}

var stabilizeRequested bool

// TODO: decide whether this should be made public or get deleted
func _RequestStabilize() {
	stabilizeRequested = true
}

type MouseButton uint8

// mirrors the values in gioui
const (
	MousePrimary MouseButton = iota
	MouseSecondary
	MouseTertiary
)

type MouseAction uint8

const (
	MouseClick MouseAction = 1 + iota
	MouseRelease
)

type Modifiers uint32

// mirrors the values in gioui
const (
	ModCtrl Modifiers = 1 << iota
	ModCmd
	ModShift
	ModAlt
	ModSuper
)

const ModNone Modifiers = 0

// persistent input state
var InputState struct {
	MousePoint  Vec2
	MouseButton MouseButton

	DownKeys []KeyCode

	// control keys state
	Modifiers Modifiers

	Composition    string // text being input via IME
	CompositionSel [2]int // selected clause/caret as rune offsets into Composition
}

// transient (frame level) input state
var FrameInput struct {
	Mouse  MouseAction
	Motion Vec2 // mouse movement
	Scroll Vec2

	// ClickCount is the click-streak position of this frame's MouseClick:
	// 1 for a single click, 2 for the second click of a double-click, and
	// so on (macOS style). Computed by core at frame start from click
	// timing and position — backends only deliver the clicks. Valid only
	// when Mouse == MouseClick.
	ClickCount int

	Key KeyCode

	Text string // text inputted this frame (could come from IME completion)
}

// Double-click detection tunables: a click within this interval of the
// previous click, and within this distance of it, continues the streak.
var (
	DoubleClickInterval         = 400 * time.Millisecond
	DoubleClickSlop     float32 = 6
)

// click-streak state for ClickCount (see RunFrameFn)
var (
	lastClickTime  time.Time
	lastClickPoint Vec2
	clickStreak    int
)

// Text inputs publish caret/composition geometry so native IME UI can anchor
// near the active text. Positions are bottom-left screen points in logical
// coordinates; backends convert them to platform coordinates.
var CaretPos Vec2
var CaretHeight float32
var CompositionPos Vec2

// to be set by backend
var WindowSize Vec2

// backing scale (device pixels per logical point); set by the backend. Defaults
// to 1 so backends that don't care (e.g. giobackend) are unaffected. Used to key
// the glyph bitmap cache by device-pixel size. See cocoabackend/GLYPH_CACHE_PLAN.md.
var WindowScale float32 = 1

// WindowFocused is whether the app window currently has OS focus (is the key
// window). Set by the backend; defaults true (backends that don't track it leave it
// on). Widgets use it to drop focus-only affordances when the app is in the
// background — chiefly the text caret, which most apps stop drawing when unfocused.
var WindowFocused = true

var hoverList []*identNode

var frameStart time.Time = time.Now()
var timeDelta float32 // fraction of a second

var FrameNumber int64

// runFirstFrame is the FrameNumber of the current RunFrameFn call's first
// pass. A node whose bornFrame is at or past it was born inside this call
// and has no presented-frame history (see the animation gate in
// resolveOrigins).
var runFirstFrame int64

// to be filled by the backend
var TotalFrameTime time.Duration

// to be filled here
var LayoutTime time.Duration

var copyRequested string
var pasteRequested bool

// DecorationFn, when set by a backend, draws window chrome (e.g. Wayland
// client-side decorations) transparently above the app's content. RunFrameFn
// reserves DecorationHeight points at the top for it and runs the app's frame in
// the area below; the app keeps seeing WindowSize as its own (content) size and
// needs no awareness of the decoration. Backends that get decorations from the OS
// /window manager (cocoa, win32, X11) leave this nil.
var DecorationFn func()
var DecorationHeight float32

// RequestTextCopy places text on the system clipboard at the end of the frame.
func RequestTextCopy(text string) {
	copyRequested = text
}

// RequestPaste requests the system clipboard's text, delivered as input on a
// subsequent frame.
func RequestPaste() {
	pasteRequested = true
}

type FrameOutputData struct {
	Surfaces []Surface

	Copy  string // things we want to put into the clipboard
	Paste bool   // to request a clipboard read!

	NextFrameRequested bool
	FrameHasChanges    bool

	// SurfacesHash is the content hash of this frame's surface list (what
	// FrameHasChanges is derived from). A backend can compare it against the hash
	// of the frame currently on screen to decide there is nothing to present —
	// robust to produce/present not being 1:1 (tear-defer, collapsed produces),
	// where FrameHasChanges (produced-vs-produced) would be misleading.
	SurfacesHash uint64

	// Glyph bitmap cache deltas for this frame (only populated when
	// GlyphCacheBudgetBytes > 0). The backend keeps a plain map of platform
	// handles that these two lists keep mirrored with core's cache: free the
	// evicted, upload the added (via GlyphBitmap). See glyphcache.go.
	GlyphsAdded   []GlyphKey
	GlyphsEvicted []GlyphKey
}

// RunFrame is meant to be called by the app & rendering backend
func RunFrameFn(frameFn FrameFn) FrameOutputData {
	// absolutely necessary or this mutex would be useless!
	mutex.Lock()
	defer mutex.Unlock()

	runStart := time.Now()
	frameInProgress = true
	runFirstFrame = FrameNumber + 1

	// Build the frame; if the build queried geometry that had no answer yet
	// (see geometryQueryMissed), the layout is known-incomplete — run one
	// more pass so the backend never presents it. Each pass is a complete
	// frame (FrameNumber advances; input is consumed by the first pass
	// only), so the second pass reads the first's resolved geometry. One
	// extra pass settles the direct-dependency case; longer chains keep
	// converging across presented frames via FrameHasChanges below. Output
	// (surfaces, glyph deltas, hashes, clipboard) is harvested once, from
	// the final pass — glyph deltas or a copy request harvested from a
	// discarded pass would be lost to the backend.
	var anyRequested bool
	for pass := 0; ; pass++ {
		// ======== begin frame pass ========
		FrameNumber++
		stabilizeRequested = false
		flushStaleCommands()

		// reset frame variables
		frameFocusTrap = nil
		buildingFocusTrap = nil

		prevFrameStart := frameStart
		frameStart = time.Now()
		timeDelta = float32(frameStart.Sub(prevFrameStart).Milliseconds()) / 1e3

		// click-streak detection (double clicks and beyond): a click close in
		// time and space to the previous one continues the streak
		if FrameInput.Mouse == MouseClick {
			d := Vec2Sub(InputState.MousePoint, lastClickPoint)
			near := d[0]*d[0]+d[1]*d[1] <= DoubleClickSlop*DoubleClickSlop
			if near && frameStart.Sub(lastClickTime) <= DoubleClickInterval {
				clickStreak++
			} else {
				clickStreak = 1
			}
			FrameInput.ClickCount = clickStreak
			lastClickTime = frameStart
			lastClickPoint = InputState.MousePoint
		}

		// focus cycling state
		prevFocused = focused
		focused = nextFocused
		_cycleFocusOnTab(nil) // this should work if nothing is focused!

		// detect hovers based on last frame artifacts
		directHovered = nil
		g.ResetSlice(&hoverList)
		for _, hoverable := range slices.Backward(hoverables) {
			if RectContainsPoint(hoverable.Rect, InputState.MousePoint) {
				c := hoverable.Container
				directHovered = c.node
				for c != nil {
					if !c.ClickThrough {
						g.Append(&hoverList, c.node)
					}
					c = c.parent
				}
				break
			}
		}

		requested.Store(false)

		// root container
		root := new(_Container)
		current = root
		root.node = identRoot
		currentIdent = identRoot
		rootPrevRD, _ := identRoot.prevRenderData()
		rootSize := WindowSize
		if DecorationFn != nil {
			rootSize[1] += DecorationHeight // reserve top space for backend chrome (CSD)
		}
		current.resolvedSize = rootSize
		current.MinSize = rootSize
		current.MaxSize = rootSize
		current.Clip = true
		current.ScrollOffset = rootPrevRD.ScrollOffset

		if DecorationFn != nil {
			// Draw window chrome (e.g. Wayland client-side decorations) above the app,
			// transparently: the app's frame runs in a content area sized to WindowSize,
			// below the chrome. The app needs no awareness of the decoration.
			DecorationFn()
			Container(AttrSet{Grow: 1, ExpandAcross: true, Clip: true}, func() {
				frameFn()
				// Drain popups in the app's content scope so they layer over the
				// app but under the chrome — matching where a manual PopupsHost
				// used to run (deferred at the end of the frame function).
				PopupsHost()
			})
		} else {
			frameFn()
			PopupsHost()
		}

		resolveSizeFromInside(root)

		// ======== begin layout ========
		// note: "current" is the root container when we arrive here
		resolveSizesFromOutside(current)
		resolveOrigins(current)
		applyClipping(current, Rect{Size: WindowSize})

		// ======== begin rendering surfaces ========
		g.ResetSlice(&surfaces)
		g.ResetSlice(&hoverables)
		g.ResetSlice(&focusables)

		_renderToSurfaces(current)
		SurfaceCount = len(surfaces)

		// DEBUG
		// count push and pop items
		/*
			var pushes, pops int
			for _, s := range surfaces {
				if s.PushClip {
					pushes++
				}
				pops += s.PopCount
			}
			fmt.Println("Pushes:", pushes, "Pops:", pops)
		*/
		// ======== end rendering surfaces ========
		// ======== end layout ========

		generic.Reset(&FrameInput)
		// ======== end frame pass ========

		anyRequested = anyRequested || requested.Load()
		if !stabilizeRequested || pass >= 1 {
			break
		}
	}

	// Prune identity nodes whose key wasn't claimed for a few frames —
	// AFTER the final pass, so a forward-referenced key that was built
	// late in the frame is already stamped and never looks stale
	// (identity.go, retention sweep).
	maybeSweepIdentTree()

	var output FrameOutputData

	output.Surfaces = surfaces

	if GlyphCacheBudgetBytes > 0 {
		output.GlyphsAdded, output.GlyphsEvicted = updateGlyphCache(surfaces)
	}

	var newSurfacesHash = computeSurfacesHash(surfaces)
	output.SurfacesHash = newSurfacesHash
	if surfaceHash != newSurfacesHash {
		output.FrameHasChanges = true
	}
	output.NextFrameRequested = anyRequested || output.FrameHasChanges || pendingCommandNeedsNextFrame()
	surfaceHash = newSurfacesHash

	frameInProgress = false

	output.Copy = copyRequested
	output.Paste = pasteRequested
	copyRequested = ""
	pasteRequested = false

	LayoutTime = time.Since(runStart)

	return output
}

// -----------------------------------------------------------------------------
//      Surfaces
// -----------------------------------------------------------------------------
// Surfaces are the basic building blocks. A surface represents a rectangle with
// rounded corners, background color, potentially some text or even an arbitrary
// shape. All UI is built by composing surfaces in different ways.

type f32 = float32

type Vec2 = [2]f32
type Vec4 = [4]f32

// N4 returns a Vec4 with all four components set to v — handy for uniform
// padding, corner radii, or grayscale colors.
func N4(v f32) Vec4 {
	return [4]f32{v, v, v, v}
}

type Rect struct {
	Origin Vec2
	Size   Vec2
}

// RectContainsPoint reports whether p lies inside r, with the left and top edges
// inclusive and the right and bottom edges exclusive.
func RectContainsPoint(r Rect, p Vec2) bool {
	tl := r.Origin                  // top left
	br := Vec2Add(r.Origin, r.Size) // bottom right
	// TODO: a version that can also do it for rounded corners! where corners are Vec4
	return p[0] >= tl[0] && p[0] < br[0] && p[1] >= tl[1] && p[1] < br[1]
}

// RectIntersect returns the overlapping region of two rectangles.
func RectIntersect(r1 Rect, r2 Rect) Rect {
	// min points
	min1 := r1.Origin
	min2 := r2.Origin

	// min result
	var min3 Vec2

	min3[0] = max(min1[0], min2[0])
	min3[1] = max(min1[1], min2[1])

	// max points
	max1 := Vec2Add(r1.Origin, r1.Size)
	max2 := Vec2Add(r2.Origin, r2.Size)

	var max3 Vec2
	max3[0] = min(max1[0], max2[0])
	max3[1] = min(max1[1], max2[1])

	var r3 Rect
	r3.Origin = min3
	r3.Size = Vec2Sub(max3, min3)
	if r3.Size[0] < 0 || r3.Size[1] < 0 {
		return Rect{}
	} else {
		return r3
	}
}

type ClipStackOp int

const (
	_ ClipStackOp = iota
	ClipPush
	ClipPop
)

type Surface struct {
	Rect    Rect
	Color1  Vec4
	Color2  Vec4
	Corners Vec4 // corner radius

	Stroke     float32 // for borders!
	ImageId    ImageId
	ImageScale bool // if set, scales image down to fit surface!

	FontId      FontId
	GlyphId     GlyphId
	GlyphOffset Vec2

	Clip ClipStackOp

	Transparency    float32
	PopTransparency bool

	// applies to both image and glyph
	// ContentScale float32

	// TODO: image, glyph, shape (vector)
}

// Vec2Add returns the component-wise sum v1 + v2.
func Vec2Add(v1 Vec2, v2 Vec2) Vec2 {
	return Vec2{
		v1[0] + v2[0],
		v1[1] + v2[1],
	}
}

// Vec2Sub returns the component-wise difference v1 - v2.
func Vec2Sub(v1 Vec2, v2 Vec2) Vec2 {
	return Vec2{
		v1[0] - v2[0],
		v1[1] - v2[1],
	}
}

// Vec2Mul returns v1 scaled by the scalar f.
func Vec2Mul(v1 Vec2, f float32) Vec2 {
	return Vec2{
		v1[0] * f,
		v1[1] * f,
	}
}

// Vec4Add returns the component-wise sum v1 + v2.
func Vec4Add(v1 Vec4, v2 Vec4) Vec4 {
	return Vec4{
		v1[0] + v2[0],
		v1[1] + v2[1],
		v1[2] + v2[2],
		v1[3] + v2[3],
	}
}

// Vec4Sub returns the component-wise difference v1 - v2.
func Vec4Sub(v1 Vec4, v2 Vec4) Vec4 {
	return Vec4{
		v1[0] - v2[0],
		v1[1] - v2[1],
		v1[2] - v2[2],
		v1[3] - v2[3],
	}
}

func ClampColorVec(v *Vec4) {
	g.Clamp(0, &v[0], 360)
	g.Clamp(0, &v[1], 100)
	g.Clamp(0, &v[2], 100)
	g.Clamp(0, &v[3], 1)
}

var surfaces = make([]Surface, 0, 1024*16)

func pushSurface(s Surface) {
	g.Append(&surfaces, s)
}

var surfaceHash uint64
var surfaceHashSeed = maphash.MakeSeed()

func computeSurfacesHash(ss []Surface) uint64 {
	var h maphash.Hash
	h.SetSeed(surfaceHashSeed)

	for _, s := range ss {
		// this relies on Surface being a flat plain object with no pointers
		h.Write(generic.UnsafeRawBytes(&s))
		// An image's pixels can change behind a stable ImageId without any surface
		// byte changing (async decode, UseImage). Fold the generation in so this
		// whole-frame check sees the change; otherwise the frame is treated as static
		// and skipped, and the decoded image never gets drawn (see images.go).
		if gen := surfaceImageGeneration(&s); gen != 0 {
			var buf [8]byte
			putUint64(&buf, gen)
			h.Write(buf[:])
		}
	}
	return h.Sum64()
}

// -----------------------------------------------------------------------------
//      Containers
// -----------------------------------------------------------------------------
// Containers are the basic units of layout. They let you layout surfaces with
// flex-box like way. Although not exactly the same, they are similar in spirit.

// Note: when Vec4 is used as color, the convention is HLSA with
// H: 0-360
// S: 0-100
// L: 0-100
// A: 0-1

const (
	HUE        = 0
	SATURATION = 1
	LIGHT      = 2
	ALPHA      = 3
)

type Border struct {
	BorderColor Vec4
	BorderWidth f32
}

type Alignment int

const (
	AlignUnset Alignment = iota

	AlignStart
	AlignMiddle
	AlignEnd
)

type AttrSet struct {

	// padding order is: top right bottom left
	Padding Vec4

	Gap float32

	// 0 means opaque, 1 means transperant (opacity = 1)
	// using this instead of opacity because the zero value is the good default
	Transparency float32

	MainAlign  Alignment
	CrossAlign Alignment

	// properties for self with respect to parent!
	Grow      float32
	SelfAlign Alignment // override the parent's cross-align setting

	MinSize Vec2
	MaxSize Vec2

	Float Vec2

	Background Vec4
	Gradient   Vec4 // diff applied to background

	Border

	Shadow

	// css order: top-left, top-right, bottom-right, bottom-left
	Corners Vec4

	// flags
	// Layout things ..
	Row          bool
	Wrap         bool
	ExpandAcross bool
	Floats       bool
	// size is not determined by content but by size constraints, flex growth, and cross axis expansion
	ExtrinsicSize bool

	// z-index
	Z f32

	// Event things
	ClickThrough bool
	Focusable    bool // items that can receive focus via clicking or tab-cycling
	FocusTrap    bool // this container wants to be a focus trap (only for modals)

	// clip content drawn outside container boundaries
	// defaults to no clipping, because clip by default can have some undesirable side effects
	Clip bool

	// When certain interactions feel off if animated
	NoAnimate bool
}

type Shadow struct {
	Offset Vec2
	Blur   f32
	Alpha  f32
}

const PAD_TOP = 0
const PAD_RIGHT = 1
const PAD_BOTTOM = 2
const PAD_LEFT = 3

// PaddingVH builds a padding Vec4 from a vertical (top and bottom) and a
// horizontal (left and right) amount.
func PaddingVH(v float32, h float32) Vec4 {
	return Vec4{v, h, v, h}
}

// PadSize returns the space a padding Vec4 consumes: combined left+right padding
// in x, combined top+bottom padding in y.
func PadSize(padding Vec4) Vec2 {
	var size Vec2
	size[0] = padding[PAD_LEFT] + padding[PAD_RIGHT]
	size[1] = padding[PAD_TOP] + padding[PAD_BOTTOM]
	return size
}

type Handle int32

type _Container struct {
	AttrSet

	// image!
	imageId ImageId

	// text!
	fontId      FontId
	glyphId     GlyphId
	glyphOffset Vec2

	resolvedSize   Vec2
	relativeOrigin Vec2
	resolvedOrigin Vec2

	ScreenRect Rect // resolved size / origin clipped by parent clipping region

	ScrollOffset Vec2

	// wrapping info!
	wrapLines   []_WrapLine
	ContentSize Vec2 // used for scrolling

	parent   *_Container
	children []*_Container

	// node is this container's stable cross-frame identity in the identity
	// tree (identity.go), holding its render data, hooks, and interaction
	// state.
	node *identNode
}

type _WrapLine struct {
	size Vec2
	// slice into the parent container's children
	start, end int
}

type RenderData struct {
	AttrSet
	ResolvedSize   Vec2
	RelativeOrigin Vec2
	ResolvedOrigin Vec2
	ContentSize    Vec2
	ScrollOffset   Vec2
	screenRect     Rect
}

// builder stuff
var current *_Container

// Container opens a container with the given attributes, runs builder to
// populate its children, closes it, and returns a handle to it. This is the
// primary building block; the returned ContainerId can be passed to the query
// functions (focus, hover, screen-rect, popup anchors). Use ContainerWithKey
// when the container needs an explicit reconciliation key.
func Container(attrs AttrSet, builder func()) ContainerId {
	return ContainerWithKey(nil, attrs, builder)
}

// ContainerWithKey opens a container, runs builder inside it, and closes it,
// returning the container's identity node — a stable handle usable
// anywhere an id is accepted (focus, hover, screen-rect queries, popup
// anchors).
//
// The id contract (see identity.go for the full reconciliation rule):
//
//   - nil id: the container is matched positionally by (component type,
//     per-type ordinal), where the component type is the builder's func
//     literal. This is right for fixed structure and for loops whose
//     membership doesn't change.
//   - explicit id: matched by Go value equality (pointers by pointer,
//     strings by content — dynamic strings are fine), SCOPED to the
//     parent: the same id under two parents is two distinct containers.
//     Ids must be unique among siblings within a frame; duplicates are
//     reported (see claimChild). Use explicit ids for dynamic collections
//     (rows keyed by row data) and wherever cross-frame continuity must
//     survive structural change.
func ContainerWithKey(key any, attrs AttrSet, builder func()) ContainerId {
	parentIdent := currentIdent
	node := parentIdent.claimChild(key, funcCodePtr(builder))

	// cascade some special attributes
	// caller can override this by using `ModAttrs` inside the builder function
	// note: current is still the parent at this point in the function
	if current.NoAnimate {
		attrs.NoAnimate = current.NoAnimate
	}
	if current.ClickThrough {
		attrs.ClickThrough = current.ClickThrough
	}

	var c = new(_Container)
	generic.Append(&current.children, c)
	c.node = node
	c.AttrSet = attrs
	c.parent = current
	current = c
	currentIdent = node
	prevRD, _ := node.prevRenderData()
	c.ScrollOffset = prevRD.ScrollOffset

	if attrs.FocusTrap {
		buildingFocusTrap = c.node
		frameFocusTrap = c.node
		defer func() {
			buildingFocusTrap = nil
		}()

		// NOTE: timing sensitive: FirstRender assumes `current` is set properly
		// in this case, it is set, so this should work, but something to be
		// aware of
		stealFocusOnMount()
	}
	c.node.focusTrapOwner = buildingFocusTrap // the focus trap is its own focus trap owner

	if builder != nil {
		builder()
	}

	resolveSizeFromInside(c)

	current = c.parent
	currentIdent = parentIdent
	return ContainerId(node)
}

// small helper to make code look cleaner
func Element(attrs AttrSet) ContainerId {
	return ContainerWithKey(nil, attrs, nil)
}

// ElementWithKey adds a childless (leaf) container with an explicit
// reconciliation key — the keyed form of Element.
func ElementWithKey(key any, attrs AttrSet) ContainerId {
	return ContainerWithKey(key, attrs, nil)
}

// Nil adds an empty container that draws nothing.
func Nil() {
	ContainerWithKey(nil, AttrSet{}, nil)
}

// Void adds an empty floating container pinned far behind everything: it draws
// nothing and takes no space in the normal layout flow.
func Void() {
	ContainerWithKey(nil, AttrSet{Floats: true, Z: -10000000}, nil)
}

// ModAttrs applies setters to the current container's attributes. It must be
// called before any child is added; modifying attributes once children exist
// panics.
func ModAttrs(fns ...func(*AttrSet)) {
	if len(current.children) > 0 {
		panic("ATTRS SHOULD BE CHANGED **BEFORE** ADD CHILD ELEMENTS!")
	}
	for _, fn := range fns {
		fn(&current.AttrSet)
	}
}

// GetAttrs returns the current container's attribute set.
func GetAttrs() AttrSet {
	return current.AttrSet
}

// CapBelow lowers *v to c when it exceeds c, so *v ends up no greater than c.
func CapBelow[T cmp.Ordered](v *T, c T) {
	*v = min(*v, c)
}

// CapAbove raises *v to f when it is below f, so *v ends up no less than f.
func CapAbove[T cmp.Ordered](v *T, f T) {
	*v = max(*v, f)
}

// ScrollOnInput scrolls the current container by this frame's wheel input when
// it is hovered, clamped to the container's scrollable range.
func ScrollOnInput() {
	if IsHovered() {
		// Wheel input scrolls what's on screen, so clamping against the
		// previous frame eagerly is right here — unlike SetScrollOffset,
		// which records a target for this frame's layout to reconcile.
		desired := Vec2Add(current.ScrollOffset, FrameInput.Scroll)

		var paddingSize Vec2
		paddingSize[0] = current.Padding[PAD_LEFT] + current.Padding[PAD_RIGHT]
		paddingSize[1] = current.Padding[PAD_TOP] + current.Padding[PAD_BOTTOM]

		prevRD, _ := current.node.prevRenderData()
		scrollableSize := Vec2Sub(prevRD.ContentSize, Vec2Sub(prevRD.ResolvedSize, paddingSize))
		CapAbove(&scrollableSize[0], 0)
		CapAbove(&scrollableSize[1], 0)

		g.Clamp(0, &desired[0], scrollableSize[0])
		g.Clamp(0, &desired[1], scrollableSize[1])
		current.ScrollOffset = desired
	}
}

// GetScrollOffset returns the current container's scroll offset.
func GetScrollOffset() Vec2 {
	return current.ScrollOffset
}

// SetScrollOffset records the desired scroll offset as-is; layout clamps it
// against THIS frame's content and available size once both are known (see
// resolveOrigins), and the clamped value is what the frame renders with and
// commits. Clamping here would have to use previous-frame data — which
// silently wiped offsets restored onto containers whose previous frame had
// no content yet (a list rebuilt on tab switch).
func SetScrollOffset(offset Vec2) {
	current.ScrollOffset = offset
}

// PressAction reports a completed click gesture on the current container: it
// becomes active on mouse-down while hovered and returns true when the button is
// released while still hovered — the standard button behavior.
func PressAction() bool {
	var action bool
	if IsHovered() {
		if FrameInput.Mouse == MouseClick {
			// action = true
			setActive()
		}
	}
	if IsActive() {
		if FrameInput.Mouse == MouseRelease {
			unsetActive()
			action = IsHovered() // if released while over the target!
		}
	}
	if action {
		RequestNextFrame()
	}
	return action
}

// returns true if focus was received now
func FocusOnClick() {
	if FrameInput.Mouse == MouseClick {
		if focused != current.node && IsHovered() {
			focusImmediate()
		} else if focused == current.node && !IsHovered() {
			// blur.
			//
			// this should not conflict with any other element trying to grab
			// focus on input (e.g. by running this very function)
			Blur()
		}
	}
}

// ReceivedFocusNow reports whether the current container gained focus on this
// frame — it is focused now but was not on the previous frame.
func ReceivedFocusNow() bool {
	return focused == current.node && prevFocused != current.node
}

// IdReceivedFocusNow reports whether the container with the given handle gained
// focus on this frame.
func IdReceivedFocusNow(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && focused == n && prevFocused != n
}

// MainCrossAxes returns the Vec component indices of the main and cross axes:
// (0, 1) for a row layout, (1, 0) for a column.
func MainCrossAxes(row bool) (int, int) {
	if row {
		return 0, 1
	} else {
		return 1, 0
	}
}

// Absf32 returns the absolute value of x.
func Absf32(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) &^ (1 << 31))
}

// Roundf32 rounds x to the nearest integer, returned as a float32.
func Roundf32(x f32) f32 {
	return f32(math.Round(float64(x)))
}

func animate(value float32, target float32, rate float32, cutoff float32) float32 {
	diff := Absf32(target - value)
	if diff < cutoff {
		return target
	} else {
		return value + (target-value)*rate
	}
}

// returns true if there was a change! (meaning we still need to animate so should request a frame)
func animateFrom(value *float32, prev float32, rate float32, cutoff float32) {
	*value = animate(prev, *value, rate, cutoff)
}

func animateVec2From(value *Vec2, prev Vec2, rate float32, cutoff float32) {
	animateFrom(&value[0], prev[0], rate, cutoff)
	animateFrom(&value[1], prev[1], rate, cutoff)
}

func animateVec4From(value *Vec4, prev Vec4, rate float32, cutoff float32) {
	animateFrom(&value[0], prev[0], rate, cutoff)
	animateFrom(&value[1], prev[1], rate, cutoff)
	animateFrom(&value[2], prev[2], rate, cutoff)
	animateFrom(&value[3], prev[3], rate, cutoff)
}

func resolveOrigins(container *_Container) {
	// sizes are already resolved here!
	mainAxis, crossAxis := MainCrossAxes(container.Row)

	var paddingSize Vec2
	paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
	paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]

	availableSize := Vec2Sub(container.resolvedSize, paddingSize)

	// clamp the scroll offset against this frame's actual content — the
	// authoritative clamp; SetScrollOffset records the desire unclamped
	// (see its doc). Runs before the offset is used to position children
	// and before it's committed to rd, so the frame renders and remembers
	// the clamped value.
	scrollableSize := Vec2Sub(container.ContentSize, availableSize)
	CapAbove(&scrollableSize[0], 0)
	CapAbove(&scrollableSize[1], 0)
	g.Clamp(0, &container.ScrollOffset[0], scrollableSize[0])
	g.Clamp(0, &container.ScrollOffset[1], scrollableSize[1])

	var nextLineOrigin Vec2
	nextLineOrigin[0] += container.Padding[PAD_LEFT]
	nextLineOrigin[1] += container.Padding[PAD_TOP]

	nextLineOrigin = Vec2Sub(nextLineOrigin, container.ScrollOffset)

	// cross alignment works on two levels: first we apply it to the wrap lines, then we apply it inside each wrap line!
	switch container.CrossAlign {
	case AlignMiddle:
		nextLineOrigin[crossAxis] += (availableSize[crossAxis] - container.ContentSize[crossAxis]) / 2
	case AlignEnd:
		nextLineOrigin[crossAxis] += (availableSize[crossAxis] - container.ContentSize[crossAxis])
	}

	for i := range container.wrapLines {
		nextItemOrigin := nextLineOrigin
		wrapLine := &container.wrapLines[i]
		crossSize := wrapLine.size[crossAxis]

		// apply main axis alignment
		switch container.MainAlign {
		case AlignMiddle:
			nextItemOrigin[mainAxis] += (availableSize[mainAxis] - wrapLine.size[mainAxis]) / 2
		case AlignEnd:
			nextItemOrigin[mainAxis] += (availableSize[mainAxis] - wrapLine.size[mainAxis])
		}

		// FIXME: floating items affect the number of gaps! revisit all places where we compute gaps!

		for j := wrapLine.start; j < wrapLine.end; j++ {
			child := container.children[j]
			// floating items are positioned by their designated floating position!
			if child.Floats {
				child.relativeOrigin = child.Float
			} else {
				child.relativeOrigin = nextItemOrigin
				// cross align!
				var childCrossSize = child.resolvedSize[crossAxis]
				if crossSize > childCrossSize {
					var crossAlign = container.CrossAlign
					if child.SelfAlign != AlignUnset {
						crossAlign = child.SelfAlign
					}
					switch crossAlign {
					case AlignMiddle:
						child.relativeOrigin[crossAxis] += (crossSize - childCrossSize) / 2
					case AlignEnd:
						child.relativeOrigin[crossAxis] += (crossSize - childCrossSize)
					}
				}
				nextItemOrigin[mainAxis] += child.resolvedSize[mainAxis] + container.Gap
			}

			// :animate: :apply-animations:
			// bornFrame gate: a node born during this RunFrameFn call has
			// never been presented — its only previous data is a discarded
			// settle pass, laid out from unanswered geometry queries.
			// Animating from that (at the settle pass's ~zero timeDelta)
			// would freeze the node at the wrong rect; snap it instead.
			// Nodes that predate the call animate normally: pass 1 already
			// advanced them by the real timeDelta, and the settle pass's
			// ~zero rate simply holds that value.
			prev, ok := child.node.prevRenderData()
			if ok && !child.NoAnimate && child.node.bornFrame < runFirstFrame {
				var rate = min(1, timeDelta*20)
				var distCutoff float32 = 1
				var clrCutoff float32 = 0.01
				animateVec2From(&child.resolvedSize, prev.ResolvedSize, rate, distCutoff)
				animateVec2From(&child.relativeOrigin, prev.RelativeOrigin, rate, distCutoff)
				animateVec2From(&child.resolvedOrigin, prev.ResolvedOrigin, rate, distCutoff)
				animateVec4From(&child.Padding, prev.Padding, rate, distCutoff)
				animateVec4From(&child.Corners, prev.Corners, rate, distCutoff)
				// animateVec4From(&child.Background, prev.Background, rate, clrCutoff)
				// animateVec4From(&child.Gradient, prev.Gradient, rate, clrCutoff)
				// animateVec4From(&child.BorderColor, prev.BorderColor, rate, clrCutoff)
				animateFrom(&child.BorderWidth, prev.BorderWidth, rate, distCutoff)
				animateFrom(&child.Transparency, prev.Transparency, rate, clrCutoff)
			}

			// Apply the relative origin **after** animations, before
			// recursing (the recursion positions the subtree against it).
			// This used to be a loop recomputing EVERY sibling's origin per
			// child — O(n²) in children, the dominant cost of wide frames —
			// and since each child's own iteration assigns its final value
			// before its subtree recursion, assigning only the current
			// child is behavior-identical. (It also makes explicit that the
			// resolvedOrigin animation above is dead: overwritten here.)
			child.resolvedOrigin = Vec2Add(container.resolvedOrigin, child.relativeOrigin)

			resolveOrigins(child)
		}

		// wrap lines are traversed on the cross axis
		nextLineOrigin[crossAxis] += wrapLine.size[crossAxis] + container.Gap
	}

	rd := RenderData{
		AttrSet:        container.AttrSet,
		ResolvedSize:   container.resolvedSize,
		RelativeOrigin: container.relativeOrigin,
		ResolvedOrigin: container.resolvedOrigin,
		ContentSize:    container.ContentSize,
		ScrollOffset:   container.ScrollOffset,
	}
	if container.node.rdFrame != FrameNumber-1 {
		container.node.bornFrame = FrameNumber
	}
	container.node.rd = rd
	container.node.rdFrame = FrameNumber
}

// this is called after resolving origins for everything
// it doesn't actually "clip" the view; it determines what
// the screen rect is when clipping is taken into account
func applyClipping(container *_Container, clipRect Rect) {
	resolvedRect := Rect{
		Origin: container.resolvedOrigin,
		Size:   container.resolvedSize,
	}
	container.ScreenRect = RectIntersect(clipRect, resolvedRect)
	// the node's rd was just written by resolveOrigins; only the screen
	// rect is known this late (it needs the resolved clip chain)
	container.node.rd.screenRect = container.ScreenRect

	nextClipRect := container.ScreenRect
	if !container.Clip {
		nextClipRect = clipRect
	}
	for _, child := range container.children {
		applyClipping(child, nextClipRect)
	}

}

// called during the build up of the layout
func resolveSizeFromInside(container *_Container) {
	attrs := container.AttrSet

	// assumes children sizes are already resolved!
	// we will now resolve _our_ size based on the content size
	var size Vec2

	var padStart Vec2
	padStart[0] += container.Padding[PAD_LEFT]
	padStart[1] += container.Padding[PAD_TOP]

	// for horizontal layout
	mainAxis, crossAxis := MainCrossAxes(container.Row)

	maxMain := container.MaxSize[mainAxis] // TODO: should this propagate down?

	// apply wrapping if we have a max value for the main axis (e.g. max width for a vertical layout)
	{
		var lineStart int
		var lineSize Vec2
		for i, child := range container.children {
			// skip floating items
			if child.Floats {
				continue
			}
			var gap = container.Gap
			if i == lineStart {
				gap = 0
			}
			if i > lineStart && maxMain > 0 && container.Wrap && padStart[mainAxis]+lineSize[mainAxis]+gap+child.resolvedSize[mainAxis] > maxMain {
				// apply wrapping!
				generic.Append(&container.wrapLines, _WrapLine{
					size:  lineSize,
					start: lineStart,
					end:   i,
				})
				lineStart = i
				lineSize = Vec2{}
				gap = 0
			}

			lineSize[mainAxis] += gap + child.resolvedSize[mainAxis]
			lineSize[crossAxis] = max(child.resolvedSize[crossAxis], lineSize[crossAxis])
		}
		// last line
		// this should work too if there is no wrapping!
		generic.Append(&container.wrapLines, _WrapLine{
			size:  lineSize,
			start: lineStart,
			end:   len(container.children),
		})
	}

	var contentSize Vec2

	// the wrap lines are sorted along the across dimension!! so build the content size by summing the cross axis (with gaps) and maxing the main axis
	for i, wrapLine := range container.wrapLines {
		var gap float32
		if i > 0 {
			gap = container.Gap
		}
		contentSize[mainAxis] = max(contentSize[mainAxis], wrapLine.size[mainAxis])
		contentSize[crossAxis] += gap + wrapLine.size[crossAxis]
	}
	container.ContentSize = contentSize

	if !container.ExtrinsicSize {
		size = contentSize
	}

	// apply padding and gaps
	// note: We do it _after_ combining all child sizes because of the way 'max' works
	size[0] += attrs.Padding[PAD_LEFT] + attrs.Padding[PAD_RIGHT]
	size[1] += attrs.Padding[PAD_TOP] + attrs.Padding[PAD_BOTTOM]

	// apply min size constraints!
	size[mainAxis] = max(size[mainAxis], attrs.MinSize[mainAxis])
	size[crossAxis] = max(size[crossAxis], attrs.MinSize[crossAxis])

	// apply max size constraints
	// max size set to zero does not count!
	if attrs.MaxSize[mainAxis] > 0 {
		size[mainAxis] = min(size[mainAxis], attrs.MaxSize[mainAxis])
	}
	if attrs.MaxSize[crossAxis] > 0 {
		size[crossAxis] = min(size[crossAxis], attrs.MaxSize[crossAxis])
	}

	container.resolvedSize = size
}

// called after the entire layout tree is constructed and basic sizes are
// expand on the cross axis and main axis (flex-grow) then recurseve to
// expand children the same way
func resolveSizesFromOutside(container *_Container) {
	mainAxis, crossAxis := MainCrossAxes(container.Row)

	var paddingSize Vec2
	paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
	paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]

	// sizing hasn't been resolved yet, so we have to use data from previous frame!
	resolvedSize := container.resolvedSize

	availableSize := Vec2Sub(resolvedSize, paddingSize)

	// items expand across to the cross size of their own wrap line; when
	// there is a single line, the line occupies the full available cross
	// size, so expansion reaches the container's content edge
	singleLine := len(container.wrapLines) == 1

	for i := range container.wrapLines {
		wrapLine := &container.wrapLines[i]
		var growthRequest float32
		acrossSize := wrapLine.size[crossAxis]
		if singleLine {
			acrossSize = availableSize[crossAxis]
		}
		roomForGrowth := availableSize[mainAxis] - wrapLine.size[mainAxis]

		for j := wrapLine.start; j < wrapLine.end; j++ {
			child := container.children[j]
			// skip floating items
			if child.Floats {
				continue
			}

			growthRequest += child.Grow
			if child.ExpandAcross {
				child.resolvedSize[crossAxis] = acrossSize
				// apply the expansion to the wrap line too! otherwise the
				// cross alignment computations get out of sync (just like
				// growth does for the main axis below)
				wrapLine.size[crossAxis] = max(wrapLine.size[crossAxis], acrossSize)
			}
		}

		// ues; flex growth is applied inside a wrapped line!!
		if roomForGrowth > 0 && growthRequest > 0 {
			growthFactor := roomForGrowth / growthRequest
			for j := wrapLine.start; j < wrapLine.end; j++ {
				child := container.children[j]
				// skip floating items
				if child.Floats {
					continue
				}

				// works fine for the zero case too, so no need for an if
				growthAmount := child.AttrSet.Grow * growthFactor
				child.resolvedSize[mainAxis] += growthAmount
				wrapLine.size[mainAxis] += growthAmount // don't forget to apply the growth to the wrap line! otherwise alignment computations will get out of sync!
			}
		}
	}

	// rebuild the content size from the updated wrap lines, so that the
	// alignment computations in resolveOrigins work with the post-expansion
	// post-growth sizes
	{
		var contentSize Vec2
		for i := range container.wrapLines {
			var gap float32
			if i > 0 {
				gap = container.Gap
			}
			wrapLine := &container.wrapLines[i]
			contentSize[mainAxis] = max(contentSize[mainAxis], wrapLine.size[mainAxis])
			contentSize[crossAxis] += gap + wrapLine.size[crossAxis]
		}
		container.ContentSize = contentSize
	}

	// recurse!
	for _, child := range container.children {
		resolveSizesFromOutside(child)
	}
}

type HoverableArtifacts struct {
	Rect      Rect
	Container *_Container
}

var hoverables []HoverableArtifacts
var focusables []*identNode
var frameFocusTrap *identNode    // this will stay til the end of the frame
var buildingFocusTrap *identNode // this is only on while the trap is laying out its content

// Interaction state is held as identity-node pointers (stage 4): pointer
// comparison, no boxed-id equality anywhere.
var active *identNode        // active means it's being engaged with the mouse
var focused *identNode       // focused means it receives key events
var directHovered *identNode // (currently only written; kept for debugging)
var prevFocused *identNode   // to know when focus changes!
var nextFocused *identNode   // requested focus!

var SurfaceCount int

// _renderToSurfaces walks the resolved container tree into the frame's
// surfaces / hoverables / focusables lists (see "begin rendering surfaces"
// in RunFrameFn).
func _renderToSurfaces(container *_Container) {
	shouldClip := container.Clip
	var clip1, clip2 ClipStackOp
	if shouldClip {
		clip1 = ClipPush
		clip2 = ClipPop
	}

	resolvedRect := Rect{
		Origin: container.resolvedOrigin,
		Size:   container.resolvedSize,
	}

	if container.Shadow.Alpha > 0 {
		shRect := resolvedRect

		shRect.Origin = Vec2Add(shRect.Origin, container.Shadow.Offset)

		// due to the way the shadow image is generated .. padding is added to make
		// room for hte blur!
		shRect.Origin = Vec2Add(shRect.Origin, Vec2{-container.Shadow.Blur * 2, -container.Shadow.Blur * 2})

		pushSurface(Surface{
			Rect:       shRect,
			ImageId:    _IMBlurShadow(shRect.Size, container.Corners, container.Shadow.Blur, container.Shadow.Alpha),
			ImageScale: false,
		})
	}

	// a bit of tolerance forwhen values in Gradient cause values in color2 to overshoot or undershoot
	color2 := Vec4Add(container.Background, container.Gradient)
	ClampColorVec(&color2)

	pushSurface(Surface{
		Rect:    resolvedRect,
		Color1:  container.Background,
		Color2:  color2,
		Corners: container.Corners,

		ImageId:      container.imageId,
		ImageScale:   true,
		FontId:       container.fontId,
		GlyphId:      container.glyphId,
		GlyphOffset:  container.glyphOffset,
		Clip:         clip1,
		Transparency: container.Transparency,
	})

	if !container.ClickThrough {
		g.Append(&hoverables, HoverableArtifacts{
			// Rect:      resolvedRect,
			Rect:      container.ScreenRect,
			Container: container,
		})
	}

	if container.Focusable {
		if frameFocusTrap == nil || // enforce focus trapping
			container.node.focusTrapOwner == frameFocusTrap {
			g.Append(&focusables, container.node)
		}
	}

	// sort by Z
	// FIXME this is wasteful? most of the time Z will not be set, so we should
	// be able to get by without this cloning
	var children = slices.Clone(container.children)
	slices.SortStableFunc(children, func(a, b *_Container) int {
		if a.Z > b.Z {
			return 1
		} else if a.Z == b.Z {
			return 0
		} else {
			return -1
		}
	})

	for _, child := range children {
		_renderToSurfaces(child)
	}

	// border and clipping
	if container.BorderWidth > 0 || shouldClip || container.Transparency > 0 {
		pushSurface(Surface{
			Rect:    resolvedRect,
			Color1:  container.BorderColor,
			Color2:  container.BorderColor,
			Corners: container.Corners,
			Stroke:  container.BorderWidth,
			Clip:    clip2,

			PopTransparency: container.Transparency > 0,
		})
	}
}

// Focus requests keyboard focus for the current container; the change takes
// effect as the frame is committed.
func Focus() {
	nextFocused = current.node
}

func focusImmediate() {
	focused = current.node
	nextFocused = current.node
}

// FocusImmediateOn moves keyboard focus to the container with the given handle
// immediately (this frame), if the handle is valid.
func FocusImmediateOn(id ContainerId) {
	n := resolveIdent(id)
	if n == nil {
		return
	}
	focused = n
	nextFocused = n
}

// Blur gives up the current container's pending focus, unless another container
// has already requested focus this frame.
func Blur() {
	// do not blur if something else already requested focus!
	if nextFocused == current.node {
		nextFocused = nil
	}
}

// ClearFocus drops keyboard focus immediately (this frame). Use when a parent
// wants to dismiss child focus (e.g. Escape blurring a field).
func ClearFocus() {
	focused = nil
	nextFocused = nil
}

// grab focus if this is our first render and nothing else is focused
func AutoFocus() {
	if FirstRender() && nextFocused == nil {
		Focus()
	}
}

func stealFocusOnMount() {
	if FirstRender() {
		focused = nil
		nextFocused = nil
	}
}

// dir should be 1 or -1, but an arbitrary number should work too ..
func cycleFocus(dir int) {
	idx := slices.Index(focusables, focused)
	if idx == -1 {
		// special case
		if dir < 0 {
			idx = len(focusables)
		}
	}
	nextIdx := (idx + dir) % len(focusables)
	if nextIdx < 0 {
		nextIdx += len(focusables)
	}
	nextFocused = focusables[nextIdx]
}

// CycleFocusOnTab moves focus to the next focusable container (or the previous
// one, with Shift) when the current container has focus and Tab is pressed. Call
// it so you don't have to wire up tab navigation yourself.
func CycleFocusOnTab() {
	_cycleFocusOnTab(current.node)
}

func _cycleFocusOnTab(currentNode *identNode) {
	// if has focus && tab key is pressed: cycle focus

	if focused != currentNode {
		return
	}

	if FrameInput.Key == KeyTab {
		var dir = 1
		if InputState.Modifiers&ModShift != 0 {
			dir = -1
		}
		cycleFocus(dir)
	}
}

// FirstRender reports whether the current container is being built for the first
// time — it has no previous-frame data yet, making this the place for one-time
// setup.
func FirstRender() bool {
	_, found := current.node.prevRenderData()
	return !found
}

// HasFocus reports whether the current container holds keyboard focus.
func HasFocus() bool {
	return focused == current.node
}

// IdHasFocus reports whether the container with the given handle holds keyboard
// focus.
func IdHasFocus(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && focused == n
}

// isChildNode reports whether target is current or a descendant of current,
// walking the identity tree's parent chain.
func isChildNode(target *identNode) bool {
	for n := target; n != nil; n = n.parent {
		if n == current.node {
			return true
		}
	}
	return false
}

// HasFocusWithin reports whether the current container, or any of its
// descendants, holds keyboard focus.
func HasFocusWithin() bool {
	return isChildNode(focused)
}

// IdIsHovered reports whether the pointer is over the container with the given
// handle (anywhere in its hover stack, not necessarily on top).
func IdIsHovered(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && slices.Contains(hoverList, n)
}

// IsIdHoveredDirectly reports whether the container with the given handle is the
// topmost hovered container — nothing else is drawn over it at the pointer.
func IsIdHoveredDirectly(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && len(hoverList) > 0 && hoverList[0] == n
}

// IsHovered reports whether the pointer is over the current container.
func IsHovered() bool {
	return slices.Contains(hoverList, current.node)
}

// IsHoveredDirectly reports whether the current container is the topmost hovered
// container — nothing else is drawn over it at the pointer.
func IsHoveredDirectly() bool {
	return len(hoverList) > 0 && hoverList[0] == current.node
}

// IsClicked reports whether the current container was clicked this frame — it is
// hovered and the mouse went down.
func IsClicked() bool {
	return IsHovered() && FrameInput.Mouse == MouseClick
}

// IsDoubleClicked reports whether this frame's click is the second (or
// later) click of a streak on the current container. Note the first click
// of the pair fires IsClicked on its own frame — the standard select-then-
// escalate pattern (click selects, double-click acts) needs no special
// handling for that.
func IsDoubleClicked() bool {
	return IsHovered() && FrameInput.Mouse == MouseClick && FrameInput.ClickCount >= 2
}

// IdIsClicked reports whether the container with the given handle was clicked
// this frame.
func IdIsClicked(id ContainerId) bool {
	return IdIsHovered(id) && FrameInput.Mouse == MouseClick
}

func setActive() {
	active = current.node
}

func unsetActive() {
	active = nil
}

// IsActive reports whether the current container is the active one — the target
// that captured the pointer on mouse-down and is holding it until release.
func IsActive() bool {
	return active != nil && active == current.node
}

// CurrentId returns the current container's identity handle: an opaque, stable,
// comparable token accepted anywhere a ContainerId is (focus, hover, screen-rect
// queries, popup anchors).
func CurrentId() ContainerId {
	return ContainerId(current.node)
}

// GetLastId returns the identity handle of the current container's last
// child (like CurrentId's, for the child just built).
func GetLastId() ContainerId {
	if len(current.children) == 0 {
		return nil
	}
	return ContainerId(generic.Last(current.children).node)
}

// should be considered a low level function
// it returns the resolved *intrinsic* size of the last child of the current container
func GetLastSize() Vec2 {
	if len(current.children) == 0 {
		return Vec2{}
	}
	return generic.Last(current.children).resolvedSize
}

// The current-container accessors read from the identity node; the ...Of
// variants take a ContainerId handle and read the same data for that container.
// An unknown or not-yet-built handle yields zero values.

func idRenderData(id ContainerId) RenderData {
	n := resolveIdent(id)
	if n == nil {
		// an id can be legitimately unregistered here: a forward reference
		// to a container built later this frame. The settle pass resolves it.
		if frameInProgress {
			stabilizeRequested = true
		}
		return RenderData{}
	}
	return queriedRenderData(n)
}

// GetRenderData returns the current container's render data — resolved geometry,
// padding, and scroll offset.
func GetRenderData() RenderData {
	return queriedRenderData(current.node)
}

// GetRenderDataOf returns the render data of the container with the given handle.
func GetRenderDataOf(id ContainerId) RenderData {
	return idRenderData(id)
}

// Get the screen rect of the current element from the previous frame data
func GetScreenRect() Rect {
	return queriedRenderData(current.node).screenRect
}

// GetScreenRectOf returns the on-screen rectangle (after clipping) of the
// container with the given handle.
func GetScreenRectOf(target ContainerId) Rect {
	return idRenderData(target).screenRect
}

// GetResolvedRectOf returns the laid-out rectangle (resolved origin and size,
// before clipping) of the container with the given handle.
func GetResolvedRectOf(target ContainerId) Rect {
	rd := idRenderData(target)
	return Rect{
		Origin: rd.ResolvedOrigin,
		Size:   rd.ResolvedSize,
	}
}

// GetResolvedSize returns the current container's resolved (laid-out) size.
func GetResolvedSize() Vec2 {
	return queriedRenderData(current.node).ResolvedSize
}

// GetAvailableSize returns the size of the current container's content area —
// its resolved size minus padding.
func GetAvailableSize() Vec2 {
	return GetContentRect().Size
}

// GetContentRect returns the current container's content rectangle: its resolved
// rectangle inset by padding.
func GetContentRect() Rect {
	return contentRectOf(queriedRenderData(current.node))
}

// GetContentRectOf returns the content rectangle (resolved rect inset by padding)
// of the container with the given handle.
func GetContentRectOf(id ContainerId) Rect {
	return contentRectOf(idRenderData(id))
}

func contentRectOf(rd RenderData) Rect {
	var paddingSize Vec2
	paddingSize[0] = rd.Padding[PAD_LEFT] + rd.Padding[PAD_RIGHT]
	paddingSize[1] = rd.Padding[PAD_TOP] + rd.Padding[PAD_BOTTOM]

	var paddingOffset Vec2
	paddingOffset[0] = rd.Padding[PAD_LEFT]
	paddingOffset[1] = rd.Padding[PAD_TOP]

	size := Vec2Sub(rd.ResolvedSize, paddingSize)
	origin := Vec2Add(rd.ResolvedOrigin, paddingOffset)
	return Rect{
		Origin: origin,
		Size:   size,
	}
}
