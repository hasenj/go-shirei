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
	traceRequestNextFrame()
	ui.Host.NextFrame.Store(true)
}

func frameHasTransientInput() bool {
	fi := ui.Host.FrameInput
	return fi.Mouse != 0 || fi.Key != 0 || fi.Text != "" ||
		fi.Scroll != (Vec2{}) || fi.Motion != (Vec2{}) ||
		fi.TouchesBeganCount > 0 || fi.TouchesEndedCount > 0
}

// SetBackendWake registers a function the input-command kernel calls after
// injecting a frame of input, so a quiet/non-key window still produces.
// The callback must be safe from a non-UI goroutine (typically bounce to
// the backend's UI thread).
var backendWake func()

func SetBackendWake(fn func()) { backendWake = fn }

func wakeBackend() {
	if backendWake != nil {
		backendWake()
	}
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

	// GlyphRuns is the stamp buffer GlyphRunFirst/Count index into. Backends
	// that present after the next produce must copy this with Surfaces.
	GlyphRuns []GlyphRun

	// ContainerCount is the live layout tree after the last pass (one node per
	// Container/Element). Glyph stamps are not containers. ContainerBuilt is
	// ContainerWithKey calls on this UI this pass, plus nested Measure trees.
	ContainerCount int
	ContainerBuilt int
}

// RunFrame is meant to be called by the app & rendering backend
func RunFrameFn(frameFn FrameFn) FrameOutputData {
	// absolutely necessary or this mutex would be useless!
	mutex.Lock()
	defer mutex.Unlock()

	ui.FrameTimings = FrameTimings{ProduceStart: time.Now()}
	ui.frameInProgress = true
	ui.runFirstFrame = ui.FrameNumber + 1

	// Build the frame; if the build queried geometry that had no answer yet
	// (see geometryQueryMissed), or hit previous-frame sizes whose layout
	// target moved this pass (resize / reflow — see
	// commitLayoutSizeAndDetectStale), the layout is known-incomplete —
	// run one more pass so the backend never presents it. Each pass is a
	// complete frame (FrameNumber advances; input is consumed by the first
	// pass only), so the second pass reads the first's resolved geometry.
	// One extra pass settles the direct-dependency case; longer chains keep
	// converging across presented frames via FrameHasChanges below. Output
	// (surfaces, glyph deltas, hashes, clipboard) is harvested once, from
	// the final pass — glyph deltas or a copy request harvested from a
	// discarded pass would be lost to the backend.
	var anyRequested bool
	var hadInput bool
	for pass := 0; ; pass++ {
		// ======== begin frame pass ========
		ui.FrameNumber++
		ui.stabilizeRequested = false
		flushStaleCommands()

		ui.containerBuilt = 0
		ui.treeCount = 0
		ui.anyFocusable = false
		ui.anyTabAfter = false
		ui.anyAccess = false
		if pass == 0 {
			hadInput = frameHasTransientInput()
		}

		// reset frame variables
		ui.frameFocusTrap = nil
		ui.buildingFocusTrap = nil
		ui.trapMountedThisFrame = false
		ui.nextAccess = AccessAttrs{}
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

		// Tab cycles the source-order focus ring for whatever is focused
		// (or the first/last stop if nothing is).
		ui.prevFocused = ui.focused
		ui.focused = ui.nextFocused
		_cycleFocusOnTab(ui.focused)

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

		// root container (pooled; the swap recycles containers from two
		// passes ago — this pass's hover detection above already consumed
		// the previous pass's hoverables)
		swapContainerSlab()
		root := newContainer()
		ui.current = root
		root.node = ui.identRoot
		ui.currentIdent = ui.identRoot
		rootSize := ui.Host.WindowSize
		ui.current.resolvedSize = rootSize
		ui.current.MinSize = rootSize
		ui.current.MaxSize = rootSize
		ui.current.Clip = true
		if ui.identRoot.rdFrame == ui.FrameNumber-1 {
			ui.current.ScrollOffset = ui.identRoot.scrollOffset
		}
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
		warnLeftoverAccess()

		resolveSizeFromInside(root)

		// ======== begin layout ========
		// note: "current" is the root container when we arrive here
		// Root preamble: resolveLayout does these per child from the parent's
		// loop; the root has no parent (and is never animated).
		commitLayoutSizeAndDetectStale(ui.current)
		resolveSizesFromOutside(ui.current)
		// Clip to the root's layout size, not Host.WindowSize: with CSD those
		// differ (root = surface, WindowSize = content below the titlebar).
		resolveLayout(ui.current, Rect{Size: ui.current.resolvedSize})
		collectAccessTree(ui.current)
		revealFocusedInScrollPorts()

		// ======== begin rendering surfaces ========
		g.ResetSlice(&ui.surfaces)
		g.ResetSlice(&ui.hoverables)
		g.ResetSlice(&ui.focusables)
		if ui.anyFocusable {
			collectFocusables(ui.current)
		}
		g.ResetSlice(&ui.tabAfterSpecs)
		if ui.anyTabAfter {
			gatherTabAfter(ui.current, &ui.tabAfterSpecs)
		}
		applyTabAfter(ui.tabAfterSpecs)
		focusTrapFirstStop()
		tabAfterFirstStop(ui.tabAfterSpecs)

		g.ResetSlice(&ui.glyphRuns)
		_renderToSurfaces(ui.current, Rect{Size: ui.current.resolvedSize})
		ui.SurfaceCount = len(ui.surfaces)
		ui.ContainerCount = ui.treeCount + 1
		ui.ContainerBuilt = ui.containerBuilt

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
	output.GlyphRuns = ui.glyphRuns
	output.ContainerCount = ui.ContainerCount
	output.ContainerBuilt = ui.ContainerBuilt

	if ui.Host.GlyphCacheBudgetBytes > 0 {
		output.GlyphsAdded, output.GlyphsEvicted = updateGlyphCache(ui.surfaces, ui.glyphRuns)
	}

	// Shape + raster are done. File-backed faces unused for
	// parsedFontIdleFrames can drop; shape and glyph caches still draw.
	if unloadFileBackedParsedFonts() > 0 {
		runtime.GC()
	}

	var newSurfacesHash = computeSurfacesHash(ui.surfaces, ui.glyphRuns)
	output.SurfacesHash = newSurfacesHash
	if ui.surfaceHash != newSurfacesHash {
		output.FrameHasChanges = true
	}
	pending := pendingCommandNeedsNextFrame()
	// A pass that saw pointer/key/text/wheel/touch requests exactly one
	// follow-up produce (empty FrameInput, DownKeys/pointer left as they
	// were). Then idle unless something else asked. Same for OS input and
	// drive injects.
	output.NextFrameRequested = anyRequested || output.FrameHasChanges || pending || hadInput
	traceFrameWake(output.FrameHasChanges, anyRequested, pending)
	ui.surfaceHash = newSurfacesHash

	ui.frameInProgress = false

	output.Copy = ui.Host.Copy
	output.Paste = ui.Host.Paste
	output.OpenURL = ui.Host.OpenURL
	ui.lastCopy = ui.Host.Copy
	ui.Host.Copy = ""
	ui.Host.Paste = false
	ui.Host.OpenURL = ""

	ui.FrameTimings.ProduceEnd = time.Now()
	ui.Host.LayoutTime = ui.FrameTimings.ProduceEnd.Sub(ui.FrameTimings.ProduceStart)

	return output
}

// LastFrameSurfaces returns a copy of the surface list from the most recently
// completed frame pass. Behavior-test drivers are the intended consumer.
//
// Call it from inside the frame (frameFn / a widget body): the render stage
// rebuilds ui.surfaces AFTER the app's build code runs, so during the build
// the list still holds the previous pass's output. From any other goroutine,
// serialize with WithFrameLock or the read races with the render stage.
func LastFrameSurfaces() []Surface {
	return append([]Surface(nil), ui.surfaces...)
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

	// GlyphRunFirst/Count index a span of FrameOutputData.GlyphRuns (or the
	// backend's stashed copy). A line of text is one surface plus N run items.
	// First is not content (hash zeros it).
	GlyphRunFirst int32
	GlyphRunCount int32

	// applies to both image and glyph
	// ContentScale float32

	// TODO: image, glyph, shape (vector)
}

// GlyphRun is one glyph in a GlyphRunCount span. Same fields a per-glyph
// Surface used to carry (rect, font, color, offset).
type GlyphRun struct {
	Rect        Rect
	Color       Vec4
	FontId      FontId
	GlyphId     GlyphId
	GlyphOffset Vec2
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

func computeSurfacesHash(ss []Surface, runs []GlyphRun) uint64 {
	var h maphash.Hash
	h.SetSeed(surfaceHashSeed)
	write := func(b []byte) { h.Write(b) }
	for i := range ss {
		writeSurfaceHash(write, &ss[i], runs)
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
	// TabAfter, when set, orders this container's focusable subtree immediately
	// after that id in the tab ring. Source-order collect still runs; a post-pass
	// splices the run. Unset (nil) leaves the subtree where it was collected.
	TabAfter ContainerId

	// Clip constrains children (drawing and pointer events) to this container's
	// bounds. Attrs() defaults Clip to true; opt out with NoClip. Raw AttrSet{}
	// leaves Clip false. Does not cascade — each container chooses independently.
	// Ancestor clip still applies via resolveLayout's inherited clip rect.
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

	// Optional paint lists for a text line: one layout box, many glyphs.
	// glyphRuns is the line's precomputed relative GlyphRun geometry —
	// shared straight from the shape cache, or a per-glyph color copy when
	// spans recolor. glyphRunColor tints every run when nonzero; zero means
	// the colors are baked into the runs.
	glyphRuns     []GlyphRun
	glyphRunColor Vec4
	paintRects    []paintRect
	textRunWidth  float32 // shaped line width; used with MainAlign to place runs
	textRunEm     float32 // max glyph em on the line; the run surface height

	resolvedSize   Vec2
	relativeOrigin Vec2
	resolvedOrigin Vec2

	ScreenRect Rect // resolved size / origin clipped by parent clipping region

	ScrollOffset Vec2
	// scrollOnInput: ScrollOnInput ran on this container this pass — it is a
	// scrollport. Focus reveal only pans these, not every Clip.
	scrollOnInput bool

	// wrapping info!
	wrapLines       []_WrapLine
	anyGrowOrExpand bool // in-flow child with Grow or ExpandAcross; from-outside no-ops otherwise
	ContentSize     Vec2 // used for scrolling

	access    AccessAttrs
	accessSet bool

	parent   *_Container
	children []*_Container

	// node is this container's stable cross-frame identity in the identity
	// tree (identity.go), holding its render data, hooks, and interaction
	// state.
	node *identNode
}

// glyphStamp is the per-glyph layout datum kept alongside a line's
// precomputed GlyphRuns: advance for decoration bands (selection, span
// background/underline/strike) and cluster for mapping glyphs back to rune
// ranges (span colors, selection). Paint geometry lives in the runs.
type glyphStamp struct {
	Advance float32
	Cluster int32
}

// paintRect is a decoration band (selection, span background, underline, strike)
// in line-container-local coordinates.
type paintRect struct {
	Origin Vec2
	Size   Vec2
	Color  Vec4
}

type _WrapLine struct {
	size Vec2
	// slice into the parent container's children
	start, end int
}

// RenderData is last-pass layout committed onto the identity node: resolved
// geometry and the channels AnimSize / AnimPos / AnimPad / AnimCorners /
// AnimBorder / AnimAlpha ease from. Scroll lives on the identity node
// (GetScrollOffset / GetScrollOffsetOf). Visual attributes live on AttrSet
// / GetAttrs for the container currently being built.
type RenderData struct {
	ResolvedSize   Vec2
	RelativeOrigin Vec2
	ResolvedOrigin Vec2
	ContentSize    Vec2
	screenRect     Rect
	Padding        Vec4
	Corners        Vec4
	BorderWidth    f32
	Transparency   float32
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
	// Text style cascade: wholesale inherit when the child left TextStyle
	// zero. A plain copy shares the parent's interned family list pointer.
	// Parent always has a non-zero style once root is initialized each frame.
	if generic.IsZeroBytes(&attrs.TextStyle) {
		attrs.TextStyle = ui.current.TextStyle
	}

	applyPopupZ(&attrs)

	ui.containerBuilt++
	ui.treeCount++
	var c = newContainer()
	generic.Append(&ui.current.children, c)
	c.node = node
	c.AttrSet = attrs
	c.parent = ui.current
	ui.current = c
	ui.currentIdent = node
	if node.rdFrame == ui.FrameNumber-1 {
		c.ScrollOffset = node.scrollOffset
	}

	if attrs.FocusTrap {
		prevBuilding := ui.buildingFocusTrap
		ui.buildingFocusTrap = c.node
		ui.frameFocusTrap = c.node
		defer func() {
			ui.buildingFocusTrap = prevBuilding
		}()
		// NOTE: timing sensitive: FirstRender assumes `current` is set properly
		// in this case, it is set, so this should work, but something to be
		// aware of
		stealFocusOnMount()
	} else if attrs.TabAfter != nil && ui.buildingFocusTrap == nil {
		// Queued popups are not layout children of a FocusTrap. Inherit the
		// TabAfter target's trap so the spliced run is still in the ring.
		if t := resolveIdent(attrs.TabAfter); t != nil && t.focusTrapOwner != nil {
			prevBuilding := ui.buildingFocusTrap
			ui.buildingFocusTrap = t.focusTrapOwner
			defer func() {
				ui.buildingFocusTrap = prevBuilding
			}()
		}
	}
	c.node.focusTrapOwner = ui.buildingFocusTrap // the focus trap is its own focus trap owner

	if builder != nil {
		builder()
	}

	if c.Focusable {
		ui.anyFocusable = true
	}
	if c.TabAfter != nil {
		ui.anyTabAfter = true
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
	if ui.current.Focusable {
		ui.anyFocusable = true
	}
	if ui.current.TabAfter != nil {
		ui.anyTabAfter = true
	}
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
	ui.current.scrollOnInput = true
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

// GetScrollOffset returns the current container's live scroll offset
// (this pass, including ScrollOnInput / SetScrollOffset so far).
func GetScrollOffset() Vec2 {
	return ui.current.ScrollOffset
}

// GetScrollOffsetOf returns the last presented scroll offset of the
// container with the given handle. Missing identity or a node not
// presented last pass yields zero (and requests settle while building).
func GetScrollOffsetOf(id ContainerId) Vec2 {
	n := resolveIdent(id)
	if n == nil {
		if ui.frameInProgress {
			ui.stabilizeRequested = true
		}
		return Vec2{}
	}
	want := ui.FrameNumber
	if ui.frameInProgress {
		want = ui.FrameNumber - 1
	}
	if n.rdFrame != want {
		if ui.frameInProgress && !n.detached {
			ui.stabilizeRequested = true
		}
		return Vec2{}
	}
	return n.scrollOffset
}

// GetFrameNumber returns the current frame-pass counter (advances on every
// RunFrameFn pass, including settle). Useful for per-frame caches on hooks.
func GetFrameNumber() int64 {
	return ui.FrameNumber
}

// InputEpoch is constant for every pass of one RunFrameFn (including settle)
// and changes at the next RunFrameFn. Keyboard press-release uses it so a
// settle pass does not look like a key-up.
func InputEpoch() int64 {
	return ui.runFirstFrame
}

// SetScrollOffset records the desired scroll offset as-is; layout clamps it
// against THIS frame's content and available size once both are known (see
// resolveLayout), and the clamped value is what the frame renders with and
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

// commitLayoutSizeAndDetectStale records the node's pre-animation layout
// target and, when a public geometry query hit this pass, requests a settle
// if that target moved versus the previous pass. Comparing layout targets
// (not rd.ResolvedSize) keeps AnimSize easing from looking like instability:
// the ease changes presented size while the target stays put. resolveLayout
// runs it for each child before that child's animate block; the root (never
// animated) gets it from the layout preamble at the call sites.
func commitLayoutSizeAndDetectStale(c *_Container) {
	n := c.node
	if n == nil {
		return
	}
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

// resolveLayout is the single geometry walk. For container — whose own
// origin, size, and animations are final on entry (its parent's loop below
// assigned and animated them; the root's are fixed by the caller) — it
// resolves the screen rect against the clip chain, clamps the scroll offset,
// positions the children, and commits render data. Per child, order is
// load-bearing:
//
//  1. commit the pre-animation layout target (settle detection must compare
//     animation-independent targets),
//  2. size the child's children from outside (growth budgets read the
//     child's pre-animation box),
//  3. packing math against pre-animation child sizes,
//  4. the animate block eases presented values,
//  5. resolvedOrigin from the post-animation relativeOrigin, then recurse:
//     the subtree lays out against the presented rect.
func resolveLayout(container *_Container, clipRect Rect) {
	// The screen rect is the resolved rect punched through the ancestor clip
	// chain — what the container actually shows through. Children clip to it
	// unless this container opts out.
	container.ScreenRect = RectIntersect(clipRect, Rect{
		Origin: container.resolvedOrigin,
		Size:   container.resolvedSize,
	})
	nextClipRect := container.ScreenRect
	if !container.Clip {
		nextClipRect = clipRect
	}

	// Zero offset is already in range (SetScrollOffset of a negative is
	// non-zero and still clamps). Skip the rest of packing when there are
	// no children: wrapLines is empty and there is nothing to place.
	if container.ScrollOffset != (Vec2{}) {
		var paddingSize Vec2
		paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
		paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]
		availableSize := Vec2Sub(container.resolvedSize, paddingSize)
		scrollableSize := Vec2Sub(container.ContentSize, availableSize)
		CapAbove(&scrollableSize[0], 0)
		CapAbove(&scrollableSize[1], 0)
		g.Clamp(0, &container.ScrollOffset[0], scrollableSize[0])
		g.Clamp(0, &container.ScrollOffset[1], scrollableSize[1])
	}

	if len(container.children) > 0 {
		mainAxis, crossAxis := MainCrossAxes(container.Row)

		var paddingSize Vec2
		paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
		paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]

		availableSize := Vec2Sub(container.resolvedSize, paddingSize)

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

				// Pre-animation bookkeeping (steps 1–2): target commit and the
				// child's own from-outside sizing, both before the animate block
				// eases child.resolvedSize below.
				commitLayoutSizeAndDetectStale(child)
				resolveSizesFromOutside(child)

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
				if child.Animations != 0 {
					prev, ok := child.node.prevRenderData()
					if ok && child.node.bornFrame < ui.runFirstFrame {
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

				resolveLayout(child, nextClipRect)
			}

			// wrap lines are traversed on the cross axis
			nextLineOrigin[crossAxis] += wrapLine.size[crossAxis] + container.Gap
		}
	}

	rd := RenderData{
		ResolvedSize:   container.resolvedSize,
		RelativeOrigin: container.relativeOrigin,
		ResolvedOrigin: container.resolvedOrigin,
		ContentSize:    container.ContentSize,
		screenRect:     container.ScreenRect,
		Padding:        container.Padding,
		Corners:        container.Corners,
		BorderWidth:    container.BorderWidth,
		Transparency:   container.Transparency,
	}
	if container.node.rdFrame != ui.FrameNumber-1 {
		container.node.bornFrame = ui.FrameNumber
	}
	container.node.rd = rd
	container.node.rdFrame = ui.FrameNumber
	container.node.scrollOffset = container.ScrollOffset
}

func findContainerByNode(c *_Container, n *identNode) *_Container {
	if c == nil || n == nil {
		return nil
	}
	if c.node == n {
		return c
	}
	for _, ch := range c.children {
		if found := findContainerByNode(ch, n); found != nil {
			return found
		}
	}
	return nil
}

func contentClipRect(a *_Container) Rect {
	r := a.ScreenRect
	pad := a.Padding
	r.Origin[0] += pad[PAD_LEFT]
	r.Origin[1] += pad[PAD_TOP]
	r.Size[0] -= pad[PAD_LEFT] + pad[PAD_RIGHT]
	r.Size[1] -= pad[PAD_TOP] + pad[PAD_BOTTOM]
	if r.Size[0] < 0 {
		r.Size[0] = 0
	}
	if r.Size[1] < 0 {
		r.Size[1] = 0
	}
	return r
}

// revealDelta is the amount to add to ScrollOffset so [f0,f1] sits in [v0,v1].
// Increasing ScrollOffset moves content up/left (resolved origin decreases).
func revealDelta(f0, f1, v0, v1 float32) float32 {
	vs := v1 - v0
	if vs <= 0 {
		return 0
	}
	fs := f1 - f0
	if fs >= vs {
		return f0 - v0
	}
	if f0 < v0 {
		return f0 - v0
	}
	if f1 > v1 {
		return f1 - v1
	}
	return 0
}

const revealScrollEps float32 = 0.5

// revealFocusedInScrollPorts pans ScrollOnInput ancestors so a newly focused
// node is inside each port's content clip. Offset is written onto this pass's
// rd so a settle pass restores it. No-op if focus did not change, the node
// was not laid out, or it is already visible.
func revealFocusedInScrollPorts() {
	if ui.focused == nil || ui.focused == ui.prevFocused {
		return
	}
	leaf := findContainerByNode(ui.current, ui.focused)
	if leaf == nil {
		return
	}
	F := Rect{Origin: leaf.resolvedOrigin, Size: leaf.resolvedSize}
	changed := false
	for a := leaf.parent; a != nil; a = a.parent {
		if !a.scrollOnInput || a.node == ui.focused {
			continue
		}
		V := contentClipRect(a)
		dx := revealDelta(F.Origin[0], F.Origin[0]+F.Size[0], V.Origin[0], V.Origin[0]+V.Size[0])
		dy := revealDelta(F.Origin[1], F.Origin[1]+F.Size[1], V.Origin[1], V.Origin[1]+V.Size[1])
		if Absf32(dx) < revealScrollEps {
			dx = 0
		}
		if Absf32(dy) < revealScrollEps {
			dy = 0
		}
		if dx == 0 && dy == 0 {
			continue
		}
		a.ScrollOffset[0] += dx
		a.ScrollOffset[1] += dy
		if a.node != nil {
			a.node.scrollOffset = a.ScrollOffset
		}
		F.Origin[0] -= dx
		F.Origin[1] -= dy
		changed = true
	}
	if changed {
		ui.stabilizeRequested = true
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

	var contentSize Vec2
	if len(container.children) > 0 {
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
			if child.Grow != 0 || child.ExpandAcross {
				container.anyGrowOrExpand = true
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

		// the wrap lines are sorted along the across dimension!! so build the content size by summing the cross axis (with gaps) and maxing the main axis
		for i, wrapLine := range container.wrapLines {
			var gap float32
			if i > 0 {
				gap = container.Gap
			}
			contentSize[mainAxis] = max(contentSize[mainAxis], wrapLine.size[mainAxis])
			contentSize[crossAxis] += gap + wrapLine.size[crossAxis]
		}
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
// resolveSizesFromOutside distributes container's resolved box to its
// children: cross-axis expansion and main-axis growth within each wrap line,
// then rebuilds ContentSize from the updated lines. One node, no recursion —
// resolveLayout runs it per child BEFORE that child's animate block, so the
// budget below reads the pre-animation box (growth targets and settle
// detection must not chase eased sizes); the root gets it from the layout
// preamble at the call sites.
func resolveSizesFromOutside(container *_Container) {
	if !container.anyGrowOrExpand {
		return
	}

	mainAxis, crossAxis := MainCrossAxes(container.Row)

	var paddingSize Vec2
	paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
	paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]

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
	// alignment computations in resolveLayout work with the post-expansion
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
}

type HoverableArtifacts struct {
	Rect      Rect
	Container *_Container
}

// Interaction focus graph lives on *UI (ui.active, ui.focused, …).

// _renderToSurfaces walks the resolved container tree into the frame's
// surfaces / hoverables lists (see "begin rendering surfaces" in RunFrameFn).
// Focusables are collected separately in source order (collectFocusables).
//
// clipRect is the ancestor clip (same chain as resolveLayout). A container
// whose ScreenRect is empty is fully outside that clip. If it also Clips,
// descendants cannot paint and the subtree is skipped. If it does not Clip,
// children can still sit in the visible range (overflow, floats) and are
// visited against the same ancestor clip.
func _renderToSurfaces(container *_Container, clipRect Rect) {
	screen := container.ScreenRect
	screenEmpty := screen.Size[0] <= 0 || screen.Size[1] <= 0

	nextClip := clipRect
	if container.Clip {
		nextClip = screen
	}
	skipChildren := len(container.children) == 0 || (container.Clip && screenEmpty)

	// Shadow is drawn before this container's own clip and can spill into
	// view from an otherwise empty ScreenRect. A transparency group must
	// still wrap visible descendants when this box itself is off-screen.
	skipOwn := screenEmpty && container.Shadow.Alpha == 0 &&
		(skipChildren || container.Transparency == 0)
	if skipOwn && skipChildren {
		return
	}

	// Clip constrains later surfaces (text, descendants), not this node's
	// own fill — ClipPush is applied after the fill is drawn. A childless
	// node with no text has nothing to clip. Fill is skipped when it would
	// not paint and is not needed as a clip/transparency opener.
	emitKids := !skipChildren && len(container.children) > 0
	hasText := len(container.glyphRuns) > 0 || len(container.paintRects) > 0
	needClip := container.Clip && (emitKids || hasText)
	fillVisual := container.Background[3] > 0 || container.Gradient != (Vec4{}) ||
		container.imageId != 0 || (container.fontId > 0 && container.glyphId > 0)
	needFill := fillVisual || needClip || (container.Transparency > 0 && (emitKids || hasText))
	openedTransparency := container.Transparency > 0 && needFill
	needPop := needClip || openedTransparency || container.BorderWidth > 0

	var clip1, clip2 ClipStackOp
	if needClip {
		clip1 = ClipPush
		clip2 = ClipPop
	}

	resolvedRect := Rect{
		Origin: container.resolvedOrigin,
		Size:   container.resolvedSize,
	}

	if !skipOwn {
		if container.Shadow.Alpha > 0 {
			blur := container.Shadow.Blur
			unpadded := resolvedRect.Size
			shRect := resolvedRect
			shRect.Origin = Vec2Add(shRect.Origin, container.Shadow.Offset)
			// Shadow image is the card plus blur*2 padding on each side.
			shRect.Origin = Vec2Add(shRect.Origin, Vec2{-blur * 2, -blur * 2})
			shRect.Size = Vec2{unpadded[0] + blur*4, unpadded[1] + blur*4}

			pushSurface(Surface{
				Rect:       shRect,
				ImageId:    _IMBlurShadow(unpadded, container.Corners, blur, container.Shadow.Alpha),
				ImageScale: false,
			})
		}

		if needFill {
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
		}

		if !container.ClickThrough {
			g.Append(&ui.hoverables, HoverableArtifacts{
				Rect:      container.ScreenRect,
				Container: container,
			})
		}

		emitTextRuns(container)
	}

	if !skipChildren {
		// Children are emitted in Z order. Z is rarely set, so almost every list
		// is already non-decreasing — walk it in place and only clone+sort when
		// an out-of-order pair shows up (a stable sort of an already-ordered
		// list is the identity, so skipping it is behavior-identical).
		children := container.children
		for i := 1; i < len(children); i++ {
			if children[i].Z < children[i-1].Z {
				children = slices.Clone(children)
				slices.SortStableFunc(children, func(a, b *_Container) int {
					if a.Z > b.Z {
						return 1
					} else if a.Z == b.Z {
						return 0
					} else {
						return -1
					}
				})
				break
			}
		}

		for _, child := range children {
			_renderToSurfaces(child, nextClip)
		}
	}

	if !skipOwn && needPop {
		pushSurface(Surface{
			Rect:    resolvedRect,
			Color1:  container.BorderColor,
			Color2:  container.BorderColor,
			Corners: container.Corners,
			Stroke:  container.BorderWidth,
			Clip:    clip2,

			PopTransparency: openedTransparency,
		})
	}
}

func emitTextRuns(c *_Container) {
	if len(c.paintRects) == 0 && len(c.glyphRuns) == 0 {
		return
	}
	origin := c.resolvedOrigin
	for i := range c.paintRects {
		r := &c.paintRects[i]
		pushSurface(Surface{
			Rect:   Rect{Origin: Vec2Add(origin, r.Origin), Size: r.Size},
			Color1: r.Color,
			Color2: r.Color,
		})
	}
	if len(c.glyphRuns) == 0 {
		return
	}
	padL := c.Padding[PAD_LEFT]
	padR := c.Padding[PAD_RIGHT]
	padT := c.Padding[PAD_TOP]
	avail := c.resolvedSize[0] - padL - padR
	x := padL - c.ScrollOffset[0]
	y := padT - c.ScrollOffset[1]
	runW := c.textRunWidth
	if runW <= 0 {
		for i := range c.glyphRuns {
			runW += c.glyphRuns[i].Rect.Size[0]
		}
	}
	switch c.MainAlign {
	case AlignMiddle:
		x += (avail - runW) / 2
	case AlignEnd:
		x += avail - runW
	}
	// The runs carry line-relative geometry precomputed at shape time; emit
	// is a bulk copy plus an origin shift (and the uniform tint, unless the
	// span path baked per-glyph colors — glyphRunColor zero).
	first := len(ui.glyphRuns)
	ui.glyphRuns = append(ui.glyphRuns, c.glyphRuns...)
	dst := ui.glyphRuns[first:]
	ox := origin[0] + x
	oy := origin[1] + y
	if c.glyphRunColor != (Vec4{}) {
		for i := range dst {
			dst[i].Rect.Origin[0] += ox
			dst[i].Rect.Origin[1] += oy
			dst[i].Color = c.glyphRunColor
		}
	} else {
		for i := range dst {
			dst[i].Rect.Origin[0] += ox
			dst[i].Rect.Origin[1] += oy
		}
	}
	pushSurface(Surface{
		Rect: Rect{
			Origin: Vec2{ox, oy},
			Size:   Vec2{runW, c.textRunEm},
		},
		GlyphRunFirst: int32(first),
		GlyphRunCount: int32(len(dst)),
	})
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
		ui.trapMountedThisFrame = true
	}
}

// collectFocusables walks the layout tree in source order (not Z-sorted
// paint order) and fills ui.focusables. InFront/Behind therefore do not
// scramble tab order relative to reading order.
func collectFocusables(container *_Container) {
	if container.Focusable {
		if ui.frameFocusTrap == nil ||
			container.node.focusTrapOwner == ui.frameFocusTrap {
			g.Append(&ui.focusables, container.node)
		}
	}
	for _, child := range container.children {
		collectFocusables(child)
	}
}

func nodeInside(n, root *identNode) bool {
	for x := n; x != nil; x = x.parent {
		if x == root {
			return true
		}
	}
	return false
}

func gatherTabAfter(c *_Container, out *[]*_Container) {
	for _, ch := range c.children {
		gatherTabAfter(ch, out)
	}
	if c.TabAfter != nil {
		*out = append(*out, c)
	}
}

// applyTabAfter reorders ui.focusables so each TabAfter subtree sits
// immediately after its target id. Inner specs run first.
func applyTabAfter(specs []*_Container) {
	for _, spec := range specs {
		after := resolveIdent(spec.TabAfter)
		if after == nil {
			continue
		}
		if slices.Index(ui.focusables, after) < 0 {
			continue
		}
		var run, rest []*identNode
		afterInRun := false
		for _, n := range ui.focusables {
			if nodeInside(n, spec.node) {
				if n == after {
					afterInRun = true
				}
				run = append(run, n)
			} else {
				rest = append(rest, n)
			}
		}
		if afterInRun || len(run) == 0 {
			continue
		}
		i := slices.Index(rest, after)
		if i < 0 {
			continue
		}
		out := make([]*identNode, 0, len(ui.focusables))
		out = append(out, rest[:i+1]...)
		out = append(out, run...)
		out = append(out, rest[i+1:]...)
		ui.focusables = out
	}
}

// tabAfterFirstStop focuses the first stop in a newly mounted TabAfter
// subtree unless something inside it already requested focus.
func tabAfterFirstStop(specs []*_Container) {
	for i := len(specs) - 1; i >= 0; i-- {
		spec := specs[i]
		if spec.node.bornFrame != ui.FrameNumber {
			continue
		}
		var first *identNode
		for _, n := range ui.focusables {
			if nodeInside(n, spec.node) {
				first = n
				break
			}
		}
		if first == nil {
			continue
		}
		if ui.nextFocused != nil && nodeInside(ui.nextFocused, spec.node) {
			continue
		}
		target := resolveIdent(spec.TabAfter)
		// Only steal when the trigger has (or is about to have) focus —
		// not when a persistent TabAfter subtree is born on an empty ring.
		if ui.nextFocused != target && ui.focused != target {
			continue
		}
		ui.nextFocused = first
	}
}

// focusTrapFirstStop puts keyboard focus on the first control inside a
// newly mounted FocusTrap when nothing inside the trap has requested it
// (AutoFocus on a field still wins). Takes effect next frame, like Focus().
func focusTrapFirstStop() {
	if !ui.trapMountedThisFrame || ui.frameFocusTrap == nil || len(ui.focusables) == 0 {
		return
	}
	if ui.nextFocused != nil && ui.nextFocused.focusTrapOwner == ui.frameFocusTrap {
		return
	}
	ui.nextFocused = ui.focusables[0]
}

// dir should be 1 or -1, but an arbitrary number should work too ..
func cycleFocus(dir int) {
	cycleFocusFrom(ui.focused, dir)
}

func cycleFocusFrom(from *identNode, dir int) {
	if len(ui.focusables) == 0 {
		return
	}

	idx := slices.Index(ui.focusables, from)
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

// Tab steps the tab ring from the current focus (or to the first/last
// focusable if nothing is focused). Same rule as the frame-loop Tab key.
// Uses last frame's source-order ring; the move takes effect next frame.
func Tab() {
	dir := 1
	if ui.Host.Input.Modifiers&ModShift != 0 {
		dir = -1
	}
	cycleFocus(dir)
}

// TabFrom steps the tab ring as if Tab (or Shift+Tab) was pressed while id
// held focus. Uses last frame's source-order ring. The move takes effect
// next frame, like Focus(). Menus use this so Tab can dismiss the popup and
// land on the next page control even when the filter field held focus.
func TabFrom(id ContainerId) {
	n := resolveIdent(id)
	if n == nil {
		return
	}
	dir := 1
	if ui.Host.Input.Modifiers&ModShift != 0 {
		dir = -1
	}
	cycleFocusFrom(n, dir)
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

// FirstRender is true when this node was not presented on the previous
// pass (rdFrame is not FrameNumber-1). One-time setup goes here. A settle
// pass sees the first pass's rd, so this is false then. A node that was
// unbuilt for a frame and comes back is FirstRender again.
func FirstRender() bool {
	return ui.current.node.rdFrame != ui.FrameNumber-1
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

// FocusedId is the handle of the container that holds keyboard focus, or nil.
func FocusedId() ContainerId {
	return ContainerId(ui.focused)
}

// IdHasFocusWithin reports whether the container with the given handle, or any
// of its descendants, holds keyboard focus.
func IdHasFocusWithin(id ContainerId) bool {
	n := resolveIdent(id)
	if n == nil {
		return false
	}
	for x := ui.focused; x != nil; x = x.parent {
		if x == n {
			return true
		}
	}
	return false
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

// AnyHovered reports whether the pointer is over any container but the root,
// so a host compositing Shirei over its own scene can route a click.
func AnyHovered() bool {
	return len(ui.hoverList) > 0 && ui.hoverList[0] != ui.identRoot
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

// GetRenderData returns the current container's last-pass layout: resolved
// geometry, padding, and animation-channel values. Scroll is
// GetScrollOffset (live) / GetScrollOffsetOf (last presented).
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
