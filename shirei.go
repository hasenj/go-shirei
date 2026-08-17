package shirei

import (
	"cmp"
	"hash/maphash"
	"math"
	"runtime"
	"slices"
	"sync"
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

// RequestNextFrame asks the backend to render another frame after this one, even
// if no input arrives — used by animations and by state that settles over
// several frames.
func RequestNextFrame() {
	ui.Host.NextFrame.Store(true)
}

// FrameRequested reports whether another frame has been requested; backends
// check it to decide whether to keep rendering or go idle.
func FrameRequested() bool {
	return ui.Host.NextFrame.Load()
}

func RequestStabilize() {
	ui.stabilizeRequested = true
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

// MaxTouches is the fixed capacity of ui.Host.Input.Touches and ui.Host.FrameInput
// began/ended id lists. Enough for phone and typical iPad multi-touch;
// backends drop contacts beyond this.
const MaxTouches = 10

// TouchInfo is one contact in ui.Host.Input.Touches. Backends fill Active/Id/Pos;
// force/radius/rotation are omitted until needed.
type TouchInfo struct {
	Active bool
	Id     uint32 // stable for the life of this contact; new value next contact
	Pos    Vec2   // logical points, same space as MousePoint
}

// ContainerTouchInfo is one entry in the per-frame touchingList: a touch id
// over a container (direct hit or ancestor), rebuilt with hoverList.
type ContainerTouchInfo struct {
	TouchId uint32
	Target  *identNode
	Direct  bool
}

// Double-click detection tunables (package-level process knobs, not per-UI).
var (
	DoubleClickInterval         = 400 * time.Millisecond
	DoubleClickSlop     float32 = 6
)

// Host I/O lives entirely on ui.Host (see host.go). Accessors: GetHost,
// GetInputState, GetFrameInput, ActiveUI. WantKeyboard / RequestTextCopy /
// RequestPaste / RequestOpenURL are convenience writers for core→backend Host fields.

// WantKeyboard marks that this frame wants platform text entry active.
func WantKeyboard() {
	ui.Host.WantsKeyboard = true
}

// RequestTextCopy places text on the system clipboard at the end of the frame.
func RequestTextCopy(text string) {
	ui.Host.Copy = text
}

// RequestPaste requests the system clipboard's text, delivered as input on a
// subsequent frame.
func RequestPaste() {
	ui.Host.Paste = true
}

// RequestOpenURL asks the backend to open url in the system browser (or the
// scheme's handler) after the frame. Empty url is ignored; last write wins.
// Errors are ignored for now (backends may later report via Host if needed).
func RequestOpenURL(url string) {
	if url != "" {
		ui.Host.OpenURL = url
	}
}

// Frame clock (FrameNumber, timeDelta, …) lives on *UI.

type FrameOutputData struct {
	Surfaces []Surface

	Copy    string // things we want to put into the clipboard
	Paste   bool   // to request a clipboard read!
	OpenURL string // open in system browser / scheme handler after the frame

	NextFrameRequested bool
	FrameHasChanges    bool

	// SurfacesHash is the content hash of this frame's surface list (what
	// FrameHasChanges is derived from). A backend can compare it against the hash
	// of the frame currently on screen to decide there is nothing to present —
	// robust to produce/present not being 1:1 (tear-defer, collapsed produces),
	// where FrameHasChanges (produced-vs-produced) would be misleading.
	SurfacesHash uint64

	// Glyph bitmap cache deltas for this frame (only populated when
	// ui.Host.GlyphCacheBudgetBytes > 0). The backend keeps a plain map of platform
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
	ui.frameInProgress = true
	ui.runFirstFrame = ui.FrameNumber + 1

	// Build the frame; if the build queried geometry that had no answer yet
	// (see geometryQueryMissed), or hit previous-frame sizes whose layout
	// target moved this pass (resize / reflow — see
	// commitLayoutSizesAndDetectStale), the layout is known-incomplete —
	// run one more pass so the backend never presents it. Each pass is a
	// complete frame (FrameNumber advances; input is consumed by the first
	// pass only), so the second pass reads the first's resolved geometry.
	// One extra pass settles the direct-dependency case; longer chains keep
	// converging across presented frames via FrameHasChanges below. Output
	// (surfaces, glyph deltas, hashes, clipboard) is harvested once, from
	// the final pass — glyph deltas or a copy request harvested from a
	// discarded pass would be lost to the backend.
	var anyRequested bool
	for pass := 0; ; pass++ {
		// ======== begin frame pass ========
		ui.FrameNumber++
		ui.stabilizeRequested = false
		flushStaleCommands()

		// reset frame variables
		ui.frameFocusTrap = nil
		ui.buildingFocusTrap = nil
		// Earn-its-keep: must be re-asserted by TextInput / app each pass.
		ui.Host.WantsKeyboard = false

		prevFrameStart := ui.frameStart
		ui.frameStart = time.Now()
		ui.timeDelta = float32(ui.frameStart.Sub(prevFrameStart).Milliseconds()) / 1e3

		// click-streak detection (double clicks and beyond): a click close in
		// time and space to the previous one continues the streak
		if ui.Host.FrameInput.Mouse == MouseClick {
			d := Vec2Sub(ui.Host.Input.MousePoint, ui.lastClickPoint)
			near := d[0]*d[0]+d[1]*d[1] <= DoubleClickSlop*DoubleClickSlop
			if near && ui.frameStart.Sub(ui.lastClickTime) <= DoubleClickInterval {
				ui.clickStreak++
			} else {
				ui.clickStreak = 1
			}
			ui.Host.FrameInput.ClickCount = ui.clickStreak
			ui.lastClickTime = ui.frameStart
			ui.lastClickPoint = ui.Host.Input.MousePoint
		}

		// focus cycling state
		ui.prevFocused = ui.focused
		ui.focused = ui.nextFocused
		_cycleFocusOnTab(nil) // this should work if nothing is focused!

		// detect hovers based on last frame artifacts
		ui.directHovered = nil
		g.ResetSlice(&ui.hoverList)
		for _, hoverable := range slices.Backward(ui.hoverables) {
			if RectContainsPoint(hoverable.Rect, ui.Host.Input.MousePoint) {
				c := hoverable.Container
				ui.directHovered = c.node
				for c != nil {
					if !c.ClickThrough {
						g.Append(&ui.hoverList, c.node)
					}
					c = c.parent
				}
				break
			}
		}

		// touch hit chains (same timing and geometry as hover, per contact)
		g.ResetSlice(&ui.touchingList)
		for i := range ui.Host.Input.Touches {
			t := &ui.Host.Input.Touches[i]
			if !t.Active {
				continue
			}
			for _, hoverable := range slices.Backward(ui.hoverables) {
				if !RectContainsPoint(hoverable.Rect, t.Pos) {
					continue
				}
				c := hoverable.Container
				direct := true
				for c != nil {
					if !c.ClickThrough {
						g.Append(&ui.touchingList, ContainerTouchInfo{
							TouchId: t.Id,
							Target:  c.node,
							Direct:  direct,
						})
						direct = false
					}
					c = c.parent
				}
				break
			}
		}

		ui.Host.NextFrame.Store(false)

		// root container
		root := new(_Container)
		ui.current = root
		root.node = ui.identRoot
		ui.currentIdent = ui.identRoot
		rootPrevRD, _ := ui.identRoot.prevRenderData()
		rootSize := ui.Host.WindowSize
		ui.current.resolvedSize = rootSize
		ui.current.MinSize = rootSize
		ui.current.MaxSize = rootSize
		ui.current.Clip = true
		ui.current.ScrollOffset = rootPrevRD.ScrollOffset
		// Root enables all animation channels so children inherit via &= cascade
		// (a zero root would zero every descendant).
		ui.current.Animations = AnimAll
		// Current text style environment: always defined from root downward.
		// Non-zero so children that leave TextStyle unset inherit via cascade.
		ui.current.TextStyle = DefaultTextStyle()

		// Backend chrome (Wayland/js CSD titlebar, Android's keyboard accessory
		// bar) is not core's concern: backends that inject it wrap frameFn
		// before Run and do their own WindowSize bookkeeping inside the
		// wrapper (see waylandbackend.wrapFrame / jsbackend.wrapFrame /
		// androidbackend.wrapFrame). CSD wrappers size the root from the full
		// surface, then leave Host.WindowSize as the content/client size.
		frameFn()
		PopupsHost()
		DebugPanel()

		resolveSizeFromInside(root)

		// ======== begin layout ========
		// note: "current" is the root container when we arrive here
		resolveSizesFromOutside(ui.current)
		// Record pre-animation layout targets and settle if a geometry query
		// hit sizes whose target moved this pass (before AnimSize eases rd).
		commitLayoutSizesAndDetectStale(ui.current)
		resolveOrigins(ui.current)
		// Clip to the root's layout size, not Host.WindowSize: with CSD those
		// differ (root = surface, WindowSize = content below the titlebar).
		applyClipping(ui.current, Rect{Size: ui.current.resolvedSize})

		// ======== begin rendering surfaces ========
		g.ResetSlice(&ui.surfaces)
		g.ResetSlice(&ui.hoverables)
		g.ResetSlice(&ui.focusables)

		_renderToSurfaces(ui.current)
		ui.SurfaceCount = len(ui.surfaces)

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

		generic.Reset(&ui.Host.FrameInput)
		// ======== end frame pass ========

		anyRequested = anyRequested || ui.Host.NextFrame.Load()
		if !ui.stabilizeRequested || pass >= 1 {
			break
		}
	}

	// Prune identity nodes whose key wasn't claimed for a few frames —
	// AFTER the final pass, so a forward-referenced key that was built
	// late in the frame is already stamped and never looks stale
	// (identity.go, retention sweep).
	maybeSweepIdentTree()

	// Reclaim image registry + IM file/dir caches not touched this window.
	// Same N: contentCachePruneAfterFrames. Placement matches identity sweep.
	maybeSweepImages()
	maybeSweepContentCaches()

	var output FrameOutputData

	output.Surfaces = ui.surfaces

	if ui.Host.GlyphCacheBudgetBytes > 0 {
		output.GlyphsAdded, output.GlyphsEvicted = updateGlyphCache(ui.surfaces)
	}

	// Shape + raster are done. File-backed NewFont heaps (color emoji, CJK)
	// are not needed to blit cached glyphs or to hit the shape cache.
	if unloadFileBackedParsedFonts() > 0 {
		runtime.GC()
	}

	var newSurfacesHash = computeSurfacesHash(ui.surfaces)
	output.SurfacesHash = newSurfacesHash
	if ui.surfaceHash != newSurfacesHash {
		output.FrameHasChanges = true
	}
	output.NextFrameRequested = anyRequested || output.FrameHasChanges || pendingCommandNeedsNextFrame()
	ui.surfaceHash = newSurfacesHash

	ui.frameInProgress = false

	output.Copy = ui.Host.Copy
	output.Paste = ui.Host.Paste
	output.OpenURL = ui.Host.OpenURL
	ui.Host.Copy = ""
	ui.Host.Paste = false
	ui.Host.OpenURL = ""

	ui.Host.LayoutTime = time.Since(runStart)

	lastFrameMu.Lock()
	lastFrameOutput = output
	lastFrameOutput.Surfaces = append([]Surface(nil), output.Surfaces...)
	lastFrameMu.Unlock()

	return output
}

// LastFrameOutput is the FrameOutputData from the most recently completed
// RunFrameFn. Surfaces are a copy. Read it on a later frame (or after
// RunFrameFn returns); the pass currently inside frameFn has not harvested yet.
func LastFrameOutput() FrameOutputData {
	lastFrameMu.Lock()
	defer lastFrameMu.Unlock()
	out := lastFrameOutput
	out.Surfaces = append([]Surface(nil), lastFrameOutput.Surfaces...)
	return out
}

var (
	lastFrameMu     sync.Mutex
	lastFrameOutput FrameOutputData
)

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

func pushSurface(s Surface) {
	g.Append(&ui.surfaces, s)
}

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
	// clickThroughSet: ClickThrough / NoClickThrough was applied. Open-time
	// cascade from a ClickThrough parent skips this container when set (same
	// role as animationsSet for YesAnimate), so a hit-testable card can sit
	// inside a ClickThrough overlay.
	clickThroughSet bool
	Focusable       bool // items that can receive focus via clicking or tab-cycling
	FocusTrap       bool // this container wants to be a focus trap (only for modals)

	// Clip constrains children (drawing and pointer events) to this container's
	// bounds. Attrs() defaults Clip to true; opt out with NoClip. Raw AttrSet{}
	// leaves Clip false. Does not cascade — each container chooses independently.
	// Ancestor clip still applies via applyClipping's inherited clip rect.
	Clip bool

	// Animations selects which channels ease toward new values between frames.
	// Enable bits (1 = animate that channel). Default from Attrs() is AnimAll
	// with animationsSet false (inherits parent via cascade). Explicit setters
	// (NoAnimate, YesAnimate, AnimateOnly, Viewport) set animationsSet so the
	// open-time cascade does not rewrite the mask — Attrs(YesAnimate) works
	// under Viewport without ModAttrs.
	Animations    AnimFlags
	animationsSet bool // unexported: true if Animations was set explicitly

	// maxCrossUnset: UnsetMaxCross was applied. Open-time cross-axis MaxSize
	// cascade skips this container (same role as animationsSet for YesAnimate).
	// Attrs(UnsetMaxCross) and ModAttrs(UnsetMaxCross) both stick.
	maxCrossUnset bool

	// Paragraph text style for this subtree (cascades to descendants).
	// Wholesale cascade only: zero value means unset — parent.TextStyle is
	// cloned at container open. Root is initialized to DefaultTextStyle() each
	// frame. Amend with AmendTextStyle; reset with SetTextStyle.
	TextStyle TextStyleAttrs
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
	parentIdent := ui.currentIdent
	node := parentIdent.claimChild(key, funcCodePtr(builder))

	// cascade some special attributes
	// caller can override this by using `ModAttrs` inside the builder function
	// note: current is still the parent at this point in the function
	// Animations: if the child left the mask unset, intersect with the parent
	// (Viewport/NoAnimate zero the unset subtree so scroll doesn't ease). An
	// explicit Attrs(YesAnimate) / AnimateOnly / NoAnimate sets animationsSet
	// and is left alone — no ModAttrs required to re-enable under Viewport.
	if !attrs.animationsSet {
		attrs.Animations &= ui.current.Animations
	}
	if ui.current.ClickThrough && !attrs.clickThroughSet {
		attrs.ClickThrough = true
	}
	// Cross-axis MaxSize cascade: a column's MaxWidth (or a row's MaxHeight)
	// becomes each child's max on that axis when the child left it unset.
	// Padding on the parent is peeled off so the child's budget is the
	// content-box offer. Main-axis max is not cascaded (a wrapping row's
	// MaxWidth must not cap every button). Opt out with UnsetMaxCross (in
	// Attrs or ModAttrs — maxCrossUnset blocks cascade like animationsSet).
	{
		_, crossAxis := MainCrossAxes(ui.current.Row)
		if !attrs.maxCrossUnset && ui.current.MaxSize[crossAxis] > 0 && attrs.MaxSize[crossAxis] == 0 {
			var pad = PadSize(ui.current.Padding)
			avail := ui.current.MaxSize[crossAxis] - pad[crossAxis]
			if avail < 0 {
				avail = 0
			}
			attrs.MaxSize[crossAxis] = avail
		}
	}
	// Text style cascade: wholesale inherit when the child left TextStyle zero.
	// Clone so Families is not shared with the parent. Parent always has a
	// non-zero style once root is initialized each frame.
	if generic.IsZeroBytes(attrs.TextStyle) {
		attrs.TextStyle = TextStyleClone(ui.current.TextStyle)
	}

	applyPopupZ(&attrs)

	var c = new(_Container)
	generic.Append(&ui.current.children, c)
	c.node = node
	c.AttrSet = attrs
	c.parent = ui.current
	ui.current = c
	ui.currentIdent = node
	prevRD, _ := node.prevRenderData()
	c.ScrollOffset = prevRD.ScrollOffset

	if attrs.FocusTrap {
		ui.buildingFocusTrap = c.node
		ui.frameFocusTrap = c.node
		defer func() {
			ui.buildingFocusTrap = nil
		}()

		// NOTE: timing sensitive: FirstRender assumes `current` is set properly
		// in this case, it is set, so this should work, but something to be
		// aware of
		stealFocusOnMount()
	}
	c.node.focusTrapOwner = ui.buildingFocusTrap // the focus trap is its own focus trap owner

	if builder != nil {
		builder()
	}

	resolveSizeFromInside(c)

	ui.current = c.parent
	ui.currentIdent = parentIdent
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
	if len(ui.current.children) > 0 {
		panic("ATTRS SHOULD BE CHANGED **BEFORE** ADD CHILD ELEMENTS!")
	}
	for _, fn := range fns {
		fn(&ui.current.AttrSet)
	}
	applyPopupZ(&ui.current.AttrSet)
}

// GetAttrs returns the current container's attribute set.
func GetAttrs() AttrSet {
	return ui.current.AttrSet
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
		desired := Vec2Add(ui.current.ScrollOffset, ui.Host.FrameInput.Scroll)

		var paddingSize Vec2
		paddingSize[0] = ui.current.Padding[PAD_LEFT] + ui.current.Padding[PAD_RIGHT]
		paddingSize[1] = ui.current.Padding[PAD_TOP] + ui.current.Padding[PAD_BOTTOM]

		prevRD, _ := ui.current.node.prevRenderData()
		scrollableSize := Vec2Sub(prevRD.ContentSize, Vec2Sub(prevRD.ResolvedSize, paddingSize))
		CapAbove(&scrollableSize[0], 0)
		CapAbove(&scrollableSize[1], 0)

		g.Clamp(0, &desired[0], scrollableSize[0])
		g.Clamp(0, &desired[1], scrollableSize[1])
		ui.current.ScrollOffset = desired
	}
}

// GetScrollOffset returns the current container's scroll offset.
func GetScrollOffset() Vec2 {
	return ui.current.ScrollOffset
}

// GetFrameNumber returns the current frame-pass counter (advances on every
// RunFrameFn pass, including settle). Useful for per-frame caches on hooks.
func GetFrameNumber() int64 {
	return ui.FrameNumber
}

// SetScrollOffset records the desired scroll offset as-is; layout clamps it
// against THIS frame's content and available size once both are known (see
// resolveOrigins), and the clamped value is what the frame renders with and
// commits. Clamping here would have to use previous-frame data — which
// silently wiped offsets restored onto containers whose previous frame had
// no content yet (a list rebuilt on tab switch).
func SetScrollOffset(offset Vec2) {
	ui.current.ScrollOffset = offset
}

// PressAction reports a completed click gesture on the current container: it
// becomes active on mouse-down while hovered and returns true when the button is
// released while still hovered — the standard button behavior.
func PressAction() bool {
	var action bool
	if IsHovered() {
		if ui.Host.FrameInput.Mouse == MouseClick {
			// action = true
			setActive()
		}
	}
	if IsActive() {
		if ui.Host.FrameInput.Mouse == MouseRelease {
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
	if ui.Host.FrameInput.Mouse == MouseClick {
		if ui.focused != ui.current.node && IsHovered() {
			focusImmediate()
		} else if ui.focused == ui.current.node && !IsHovered() {
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
	return ui.focused == ui.current.node && ui.prevFocused != ui.current.node
}

// IdReceivedFocusNow reports whether the container with the given handle gained
// focus on this frame.
func IdReceivedFocusNow(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && ui.focused == n && ui.prevFocused != n
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

// layoutSizeSettleEps: subpixel noise must not force a settle pass; half a
// logical pixel is enough for resize/reflow while ignoring float chatter.
const layoutSizeSettleEps float32 = 0.5

// commitLayoutSizesAndDetectStale records each node's pre-animation layout
// target and, when a public geometry query hit this pass, requests a settle
// if that target moved versus the previous pass. Comparing layout targets
// (not rd.ResolvedSize) keeps AnimSize easing from looking like instability:
// the ease changes presented size while the target stays put.
func commitLayoutSizesAndDetectStale(c *_Container) {
	n := c.node
	if n != nil {
		if n.geometryQueryFrame == ui.FrameNumber && n.layoutSizeFrame == ui.FrameNumber-1 {
			dz0 := Absf32(c.resolvedSize[0] - n.layoutSize[0])
			dz1 := Absf32(c.resolvedSize[1] - n.layoutSize[1])
			if dz0 > layoutSizeSettleEps || dz1 > layoutSizeSettleEps {
				ui.stabilizeRequested = true
			}
		}
		n.layoutSize = c.resolvedSize
		n.layoutSizeFrame = ui.FrameNumber
	}
	for _, child := range c.children {
		commitLayoutSizesAndDetectStale(child)
	}
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

		// Floating children do not participate in main-axis packing or gaps
		// (see resolveSizesFromInside: inFlowOnLine). Origins still walk the
		// full child index range so floats can sit between in-flow siblings.

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
			//
			// Channel selection: child.Animations enable bits (see AnimFlags).
			// resolvedOrigin is not animated — it is recomputed from the
			// parent's origin + relativeOrigin below.
			prev, ok := child.node.prevRenderData()
			if ok && child.Animations != 0 && child.node.bornFrame < ui.runFirstFrame {
				var rate = min(1, ui.timeDelta*20)
				var distCutoff float32 = 1
				var clrCutoff float32 = 0.01
				af := child.Animations
				if af&AnimSize != 0 {
					animateVec2From(&child.resolvedSize, prev.ResolvedSize, rate, distCutoff)
				}
				if af&AnimPos != 0 {
					animateVec2From(&child.relativeOrigin, prev.RelativeOrigin, rate, distCutoff)
				}
				if af&AnimPad != 0 {
					animateVec4From(&child.Padding, prev.Padding, rate, distCutoff)
				}
				if af&AnimCorners != 0 {
					animateVec4From(&child.Corners, prev.Corners, rate, distCutoff)
				}
				if af&AnimBorder != 0 {
					animateFrom(&child.BorderWidth, prev.BorderWidth, rate, distCutoff)
				}
				if af&AnimAlpha != 0 {
					animateFrom(&child.Transparency, prev.Transparency, rate, clrCutoff)
				}
				// Color channels (Background / Gradient / BorderColor) remain
				// off until someone needs them; add AnimColor then.
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
	if container.node.rdFrame != ui.FrameNumber-1 {
		container.node.bornFrame = ui.FrameNumber
	}
	container.node.rd = rd
	container.node.rdFrame = ui.FrameNumber
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

	// MaxSize on the main axis drives wrap packing here. Cross-axis MaxSize
	// is cascaded into children at open time (see ContainerWithKey); main-axis
	// max is intentionally not cascaded so a wrapping row's MaxWidth does not
	// cap every item on that row.
	maxMain := container.MaxSize[mainAxis]

	// apply wrapping if we have a max value for the main axis (e.g. max width for a vertical layout)
	{
		var lineStart int
		var lineSize Vec2
		// Count in-flow children on the current line — not raw child index.
		// A leading float (e.g. menu hover highlight) used to make the first
		// real child look "not first" (i != lineStart) and pick up an extra Gap.
		var inFlowOnLine int
		for i, child := range container.children {
			// skip floating items
			if child.Floats {
				continue
			}
			var gap = container.Gap
			if inFlowOnLine == 0 {
				gap = 0
			}
			if inFlowOnLine > 0 && maxMain > 0 && container.Wrap && padStart[mainAxis]+lineSize[mainAxis]+gap+child.resolvedSize[mainAxis] > maxMain {
				// apply wrapping!
				generic.Append(&container.wrapLines, _WrapLine{
					size:  lineSize,
					start: lineStart,
					end:   i,
				})
				lineStart = i
				lineSize = Vec2{}
				inFlowOnLine = 0
				gap = 0
			}

			lineSize[mainAxis] += gap + child.resolvedSize[mainAxis]
			lineSize[crossAxis] = max(child.resolvedSize[crossAxis], lineSize[crossAxis])
			inFlowOnLine++
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

// Interaction focus graph lives on *UI (ui.active, ui.focused, …).

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
		g.Append(&ui.hoverables, HoverableArtifacts{
			// Rect:      resolvedRect,
			Rect:      container.ScreenRect,
			Container: container,
		})
	}

	if container.Focusable {
		if ui.frameFocusTrap == nil || // enforce focus trapping
			container.node.focusTrapOwner == ui.frameFocusTrap {
			g.Append(&ui.focusables, container.node)
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
	ui.nextFocused = ui.current.node
}

func focusImmediate() {
	ui.focused = ui.current.node
	ui.nextFocused = ui.current.node
}

// FocusImmediateOn moves keyboard focus to the container with the given handle
// immediately (this frame), if the handle is valid.
func FocusImmediateOn(id ContainerId) {
	n := resolveIdent(id)
	if n == nil {
		return
	}
	ui.focused = n
	ui.nextFocused = n
}

// Blur gives up the current container's pending focus, unless another container
// has already requested focus this frame.
func Blur() {
	// do not blur if something else already requested focus!
	if ui.nextFocused == ui.current.node {
		ui.nextFocused = nil
	}
}

// ClearFocus drops keyboard focus immediately (this frame). Use when a parent
// wants to dismiss child focus (e.g. Escape blurring a field).
func ClearFocus() {
	ui.focused = nil
	ui.nextFocused = nil
}

// grab focus if this is our first render and nothing else is focused
func AutoFocus() {
	if FirstRender() && ui.nextFocused == nil {
		Focus()
	}
}

func stealFocusOnMount() {
	if FirstRender() {
		ui.focused = nil
		ui.nextFocused = nil
	}
}

// dir should be 1 or -1, but an arbitrary number should work too ..
func cycleFocus(dir int) {
	if len(ui.focusables) == 0 {
		return
	}

	idx := slices.Index(ui.focusables, ui.focused)
	if idx == -1 {
		// special case
		if dir < 0 {
			idx = len(ui.focusables)
		}
	}
	nextIdx := (idx + dir) % len(ui.focusables)
	if nextIdx < 0 {
		nextIdx += len(ui.focusables)
	}
	ui.nextFocused = ui.focusables[nextIdx]
}

// CycleFocusOnTab moves focus to the next focusable container (or the previous
// one, with Shift) when the current container has focus and Tab is pressed. Call
// it so you don't have to wire up tab navigation yourself.
func CycleFocusOnTab() {
	_cycleFocusOnTab(ui.current.node)
}

func _cycleFocusOnTab(currentNode *identNode) {
	// if has focus && tab key is pressed: cycle focus

	if ui.focused != currentNode {
		return
	}

	if ui.Host.FrameInput.Key == KeyTab {
		var dir = 1
		if ui.Host.Input.Modifiers&ModShift != 0 {
			dir = -1
		}
		cycleFocus(dir)
	}
}

// FirstRender reports whether the current container is being built for the first
// time — it has no previous-frame data yet, making this the place for one-time
// setup.
func FirstRender() bool {
	_, found := ui.current.node.prevRenderData()
	return !found
}

// HasFocus reports whether the current container holds keyboard focus.
func HasFocus() bool {
	return ui.focused == ui.current.node
}

// IdHasFocus reports whether the container with the given handle holds keyboard
// focus.
func IdHasFocus(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && ui.focused == n
}

// isChildNode reports whether target is current or a descendant of current,
// walking the identity tree's parent chain.
func isChildNode(target *identNode) bool {
	for n := target; n != nil; n = n.parent {
		if n == ui.current.node {
			return true
		}
	}
	return false
}

// HasFocusWithin reports whether the current container, or any of its
// descendants, holds keyboard focus.
func HasFocusWithin() bool {
	return isChildNode(ui.focused)
}

// IdIsHovered reports whether the pointer is over the container with the given
// handle (anywhere in its hover stack, not necessarily on top).
func IdIsHovered(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && slices.Contains(ui.hoverList, n)
}

// IsIdHoveredDirectly reports whether the container with the given handle is the
// topmost hovered container — nothing else is drawn over it at the pointer.
func IsIdHoveredDirectly(id ContainerId) bool {
	n := resolveIdent(id)
	return n != nil && len(ui.hoverList) > 0 && ui.hoverList[0] == n
}

// IsHovered reports whether the pointer is over the current container
// (including when it is only under a child). Prefer this over
// IsHoveredDirectly for ordinary hit-testing.
func IsHovered() bool {
	return slices.Contains(ui.hoverList, ui.current.node)
}

// IsHoveredDirectly reports whether the current container is the topmost
// hovered container — nothing is drawn over it at the pointer. Rare: use
// when you care about the "whitespace" of this box specifically (e.g. a
// modal backdrop), not the default for buttons/keys with child chrome.
func IsHoveredDirectly() bool {
	return len(ui.hoverList) > 0 && ui.hoverList[0] == ui.current.node
}

// IsTouched reports whether any active touch's hit chain includes the
// current container (direct hit or ancestor). Same idea as IsHovered:
// children (labels, chips) do not steal the touch from their parent.
// Prefer this for almost all multi-touch hit-testing.
func IsTouched() bool {
	n := ui.current.node
	for i := range ui.touchingList {
		if ui.touchingList[i].Target == n {
			return true
		}
	}
	return false
}

// IsTouchedDirectly reports whether the current container is the frontmost
// hit for at least one active touch (no child or sibling on top). Rare —
// same niche as IsHoveredDirectly (e.g. "did they touch the empty backdrop,
// not a control drawn on it?"). Default to IsTouched.
func IsTouchedDirectly() bool {
	n := ui.current.node
	for i := range ui.touchingList {
		if ui.touchingList[i].Target == n && ui.touchingList[i].Direct {
			return true
		}
	}
	return false
}

// TouchingIds appends to dst the ids of touches whose hit chain includes the
// current container (direct or ancestor). Pass dst[:0] to reuse a buffer.
func TouchingIds(dst []uint32) []uint32 {
	n := ui.current.node
	for i := range ui.touchingList {
		if ui.touchingList[i].Target == n {
			dst = append(dst, ui.touchingList[i].TouchId)
		}
	}
	return dst
}

// TouchingIdsDirect appends to dst the ids of touches for which the current
// container is the frontmost hit.
func TouchingIdsDirect(dst []uint32) []uint32 {
	n := ui.current.node
	for i := range ui.touchingList {
		if ui.touchingList[i].Target == n && ui.touchingList[i].Direct {
			dst = append(dst, ui.touchingList[i].TouchId)
		}
	}
	return dst
}

// TouchById returns the active contact with the given id, if any.
func TouchById(id uint32) (TouchInfo, bool) {
	for i := range ui.Host.Input.Touches {
		t := ui.Host.Input.Touches[i]
		if t.Active && t.Id == id {
			return t, true
		}
	}
	return TouchInfo{}, false
}

// IsClicked reports whether the current container was clicked this frame — it is
// hovered and the mouse went down.
func IsClicked() bool {
	return IsHovered() && ui.Host.FrameInput.Mouse == MouseClick
}

// IsDoubleClicked reports whether this frame's click is the second (or
// later) click of a streak on the current container. Note the first click
// of the pair fires IsClicked on its own frame — the standard select-then-
// escalate pattern (click selects, double-click acts) needs no special
// handling for that.
func IsDoubleClicked() bool {
	return IsHovered() && ui.Host.FrameInput.Mouse == MouseClick && ui.Host.FrameInput.ClickCount >= 2
}

// IdIsClicked reports whether the container with the given handle was clicked
// this frame.
func IdIsClicked(id ContainerId) bool {
	return IdIsHovered(id) && ui.Host.FrameInput.Mouse == MouseClick
}

func setActive() {
	ui.active = ui.current.node
}

func unsetActive() {
	ui.active = nil
}

// IsActive reports whether the current container is the active one — the target
// that captured the pointer on mouse-down and is holding it until release.
func IsActive() bool {
	return ui.active != nil && ui.active == ui.current.node
}

// CurrentId returns the current container's identity handle: an opaque, stable,
// comparable token accepted anywhere a ContainerId is (focus, hover, screen-rect
// queries, popup anchors).
func CurrentId() ContainerId {
	return ContainerId(ui.current.node)
}

// GetLastId returns the identity handle of the current container's last
// child (like CurrentId's, for the child just built).
func GetLastId() ContainerId {
	if len(ui.current.children) == 0 {
		return nil
	}
	return ContainerId(generic.Last(ui.current.children).node)
}

// should be considered a low level function
// it returns the resolved *intrinsic* size of the last child of the current container
func GetLastSize() Vec2 {
	if len(ui.current.children) == 0 {
		return Vec2{}
	}
	return generic.Last(ui.current.children).resolvedSize
}

// The current-container accessors read from the identity node; the ...Of
// variants take a ContainerId handle and read the same data for that container.
// An unknown or not-yet-built handle yields zero values.

func idRenderData(id ContainerId) RenderData {
	n := resolveIdent(id)
	if n == nil {
		// an id can be legitimately unregistered here: a forward reference
		// to a container built later this frame. The settle pass resolves it.
		if ui.frameInProgress {
			ui.stabilizeRequested = true
		}
		return RenderData{}
	}
	return queriedRenderData(n)
}

// GetRenderData returns the current container's render data — resolved geometry,
// padding, and scroll offset.
func GetRenderData() RenderData {
	return queriedRenderData(ui.current.node)
}

// GetRenderDataOf returns the render data of the container with the given handle.
func GetRenderDataOf(id ContainerId) RenderData {
	return idRenderData(id)
}

// Get the screen rect of the current element from the previous frame data
func GetScreenRect() Rect {
	return queriedRenderData(ui.current.node).screenRect
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
	return queriedRenderData(ui.current.node).ResolvedSize
}

// GetAvailableSize returns the size of the current container's content area —
// its resolved size minus padding.
func GetAvailableSize() Vec2 {
	return GetContentRect().Size
}

// GetContentRect returns the current container's content rectangle: its resolved
// rectangle inset by padding.
func GetContentRect() Rect {
	return contentRectOf(queriedRenderData(ui.current.node))
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
