package slay

import (
	"cmp"
	"hash/maphash"
	"math"
	"slices"
	"time"

	"go.hasen.dev/generic"
	g "go.hasen.dev/generic"
)

type FrameFn func()

// for the backend
var requested = false

func RequestNextFrame() {
	// FIXME race condition? need to make this concurrency safe?
	requested = true
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

	Composition string // text being input via IME
}

// transient (frame level) input state
var FrameInput struct {
	Mouse  MouseAction
	Motion Vec2 // mouse movement
	Scroll Vec2

	Key KeyCode

	Text string // text inputted this frame (could come from IME completion)
	// TODO: more robust keyboard input
}

// to be set by backend
var WindowSize Vec2

var hoverList []any

var frameStart time.Time = time.Now()
var timeDelta float32 // fraction of a second

// to be filled by the backend
var TotalFrameTime time.Duration

// to be filled here
var LayoutTime time.Duration

var copyRequested string
var pasteRequested bool

func RequestTextCopy(text string) {
	copyRequested = text
}

func RequestPaste() {
	pasteRequested = true
}

type FrameOutputData struct {
	Surfaces []Surface

	Copy  string // things we want to put into the clipboard
	Paste bool   // to request a clipboard read!

	NextFrameRequested bool
	FrameHasChanges    bool
}

// RunFrame is meant to be called by the app & rendering backend
func RunFrameFn(frameFn FrameFn) FrameOutputData {
	// profiler.GetProfiler().Start()
	// defer DoProfileOutput()

	prevFrameStart := frameStart
	frameStart = time.Now()
	timeDelta = float32(frameStart.Sub(prevFrameStart).Milliseconds()) / 1e3

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
			directHovered = c.Id
			for c != nil {
				if !c.ClickThrough {
					g.Append(&hoverList, c.Id)
				}
				c = c.parent
			}
			break
		}
	}

	// surfaces
	g.ResetSlice(&surfaces)
	requested = false

	type root_type int

	// root container
	root := new(Container)
	current = root
	current.Id = root_type(0)
	current.scope = scopeIdFrom(current.Id)
	current.resolvedSize = WindowSize
	current.MinSize = WindowSize
	current.MaxSize = WindowSize
	current.Clip = true
	current.scrollOffset = renderData[current.Id].scrollOffset

	frameFn()

	resolveSizeFromInside(root)

	// do the flex layout thing to the container tree!
	// note: "current" is the root container when we arrive here
	performLayout(current)

	generic.Reset(&FrameInput)

	var output FrameOutputData

	output.Surfaces = surfaces
	var newSurfacesHash = computeSurfacesHash(surfaces)
	// fmt.Println(surfaceHash, newSurfacesHash)
	if surfaceHash != newSurfacesHash {
		output.FrameHasChanges = true
	}
	output.NextFrameRequested = requested || output.FrameHasChanges
	// output.NextFrameRequested = requested
	surfaceHash = newSurfacesHash

	renderData = renderDataNext
	renderDataNext = nil
	generic.InitMap(&renderDataNext)

	// unused hooks are removed in the subsequent frame
	hooksMap = hooksMapNext
	hooksMapNext = nil
	generic.InitMap(&hooksMapNext)

	output.Copy = copyRequested
	output.Paste = pasteRequested
	copyRequested = ""
	pasteRequested = false

	LayoutTime = time.Since(frameStart)

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

func N4(v f32) Vec4 {
	return [4]f32{v, v, v, v}
}

type Rect struct {
	Origin Vec2
	Size   Vec2
}

func RectContainsPoint(r Rect, p Vec2) bool {
	tl := r.Origin                  // top left
	br := Vec2Add(r.Origin, r.Size) // bottom right
	// TODO: a version that can also do it for rounded corners! where corners are Vec4
	return p[0] >= tl[0] && p[0] < br[0] && p[1] >= tl[1] && p[1] < br[1]
}

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

	Transperancy    float32
	PopTransperancy bool

	// applies to both image and glyph
	// ContentScale float32

	// TODO: image, glyph, shape (vector)
}

func Vec2Add(v1 Vec2, v2 Vec2) Vec2 {
	return Vec2{
		v1[0] + v2[0],
		v1[1] + v2[1],
	}
}

func Vec2Sub(v1 Vec2, v2 Vec2) Vec2 {
	return Vec2{
		v1[0] - v2[0],
		v1[1] - v2[1],
	}
}

func Vec2Mul(v1 Vec2, f float32) Vec2 {
	return Vec2{
		v1[0] * f,
		v1[1] * f,
	}
}

func Vec4Add(v1 Vec4, v2 Vec4) Vec4 {
	return Vec4{
		v1[0] + v2[0],
		v1[1] + v2[1],
		v1[2] + v2[2],
		v1[3] + v2[3],
	}
}

func Vec4Sub(v1 Vec4, v2 Vec4) Vec4 {
	return Vec4{
		v1[0] - v2[0],
		v1[1] - v2[1],
		v1[2] - v2[2],
		v1[3] - v2[3],
	}
}

var surfaces = make([]Surface, 0, 1024*16)

func PushSurface(s Surface) {
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

type Attrs struct {

	// padding order is: top right bottom left
	Padding Vec4

	Gap float32

	// 0 means opaque, 1 means transperant (opacity = 1)
	// using this instead of opacity because the zero value is the good default
	Transperancy float32

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

	Corners Vec4

	// flags
	// Layout things ..
	Row          bool
	Wrap         bool
	ExpandAcross bool
	Floats       bool
	// size is not determined by content but by size constraints, flex growth, and cross axis expansion
	ExtrinsicSize bool

	// Event things
	ClickThrough bool
	Focusable    bool // items that can receive focus via clicking or tab-cycling

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

func PaddingVH(v float32, h float32) Vec4 {
	return Vec4{v, h, v, h}
}

func PadSize(padding Vec4) Vec2 {
	var size Vec2
	size[0] = padding[PAD_LEFT] + padding[PAD_RIGHT]
	size[1] = padding[PAD_TOP] + padding[PAD_BOTTOM]
	return size
}

type Handle int32

type Container struct {
	Id any
	Attrs

	scope scopeId

	// image!
	imageId ImageId

	// text!
	fontId      FontId
	glyphId     GlyphId
	glyphOffset Vec2

	resolvedSize   Vec2
	relativeOrigin Vec2
	resolvedOrigin Vec2

	screenRect Rect // resolved size / origin clipped by parent clipping region

	scrollOffset Vec2

	// wrapping info!
	wrapLines   []_WrapLine
	contentSize Vec2 // used for scrolling

	parent     *Container
	children   []Container
	nextAutoId int
}

type _WrapLine struct {
	size Vec2
	// slice into the parent container's children
	start, end int
}

type RenderData struct {
	Attrs
	parentId       any
	ResolvedSize   Vec2
	RelativeOrigin Vec2
	ResolvedOrigin Vec2
	contentSize    Vec2
	scrollOffset   Vec2
	screenRect     Rect
}

var renderData = make(map[any]RenderData)
var renderDataNext = make(map[any]RenderData)

// builder stuff
var current *Container

func Layout(attrs Attrs, builder func()) {
	LayoutId(nil, attrs, builder)
}

var doProfileNextFrame = false
var profileOutputFile string

func ProfileNextFrame(outputFilename string) {
	doProfileNextFrame = true
	profileOutputFile = outputFilename
}

/*
func DoProfileOutput() {
	if doProfileNextFrame {
		doProfileNextFrame = false
		f, _ := os.Create(profileOutputFile)
		profiler.GetProfiler().Report(f)
		f.Close()
	}
}
*/

// for when you don't want the id to be globally unique ..
// FIXME FIXME it does not seem like this thing actually works ..
func RelId(id any) scopeId {
	var s = addChildScope(current.scope, id)
	return s
}

// open/close a container
func LayoutId(id any, attrs Attrs, builder func()) {
	// defer profiler.Time("LayoutId")()

	// if no id is passed, create a synthetic id based on current scope (which itself could be synthetic!)
	var newScope scopeId
	if id == nil {
		// nextAutoId only increments when a child no explicit id is added.
		// this way, occasionally inserted elements with ids do not disrupt
		// the id generation process
		newScope = addChildScope(current.scope, current.nextAutoId)
		id = newScope
		current.nextAutoId++
	} else {
		// this is a kind of hack to make RelId work
		switch sid := id.(type) {
		case scopeId:
			newScope = sid
		default:
			newScope = scopeIdFrom(id)
		}
	}

	// cascade some special attributes
	// caller can override this by using `ModAttrs` inside the builder function
	// note: current is still the parent at this point in the function
	if current.NoAnimate {
		attrs.NoAnimate = current.NoAnimate
	}
	if current.ClickThrough {
		attrs.ClickThrough = current.ClickThrough
	}

	var c = generic.AllocAppend(&current.children)
	c.Id = id
	c.scope = newScope
	c.Attrs = attrs
	c.parent = current
	current = c
	c.scrollOffset = renderData[c.Id].scrollOffset

	if builder != nil {
		builder()
	}

	resolveSizeFromInside(c)

	current = c.parent
}

// small helper to make code look cleaner
func Element(attrs Attrs) {
	LayoutId(nil, attrs, nil)
}

func ElementId(id any, attrs Attrs) {
	LayoutId(id, attrs, nil)
}

func Nil() {
	LayoutId(nil, Attrs{}, nil)
}

func ModAttrs(fns ...func(*Attrs)) {
	if len(current.children) > 0 {
		panic("ATTRS SHOULD BE CHANGED **BEFORE** ADD CHILD ELEMENTS!")
	}
	for _, fn := range fns {
		fn(&current.Attrs)
	}
}

func CapBelow[T cmp.Ordered](v *T, c T) {
	*v = min(*v, c)
}

func CapAbove[T cmp.Ordered](v *T, f T) {
	*v = max(*v, f)
}

func ScrollOnInput() {
	if IsHovered() {
		current.scrollOffset = Vec2Add(current.scrollOffset, FrameInput.Scroll)

		var paddingSize Vec2
		paddingSize[0] = current.Padding[PAD_LEFT] + current.Padding[PAD_RIGHT]
		paddingSize[1] = current.Padding[PAD_TOP] + current.Padding[PAD_BOTTOM]

		// sizing hasn't been resolved yet, so we have to use data from previous frame!
		contentSize := renderData[current.Id].contentSize
		resolvedSize := renderData[current.Id].ResolvedSize

		availableSize := Vec2Sub(resolvedSize, paddingSize)

		// module by max available space
		scrollableSize := Vec2Sub(contentSize, availableSize)
		CapAbove(&scrollableSize[0], 0)
		CapAbove(&scrollableSize[1], 0)

		g.Clamp(0, &current.scrollOffset[0], scrollableSize[0])
		g.Clamp(0, &current.scrollOffset[1], scrollableSize[1])
	}
}

func PressAction() bool {
	var action bool
	if IsHovered() {
		if FrameInput.Mouse == MouseClick {
			// action = true
			SetActive()
		}
	}
	if IsActive() {
		if FrameInput.Mouse == MouseRelease {
			UnsetActive()
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
		if focused != current.Id && IsHovered() {
			FocusImmediate()
		} else if focused == current.Id && !IsHovered() {
			// blur.
			//
			// this should not conflict with any other element trying to grab
			// focus on input (e.g. by running this very function)
			Blur()
		}
	}
}

func ReceivedFocusNow() bool {
	return focused == current.Id && prevFocused != current.Id
}

func IdReceivedFocusNow(id any) bool {
	return focused == id && prevFocused != id
}

func MainCrossAxes(row bool) (int, int) {
	if row {
		return 0, 1
	} else {
		return 1, 0
	}
}

func performLayout(root *Container) {
	// defer profiler.Time("performLayout")()

	resolveSizesFromOutside(root)
	resolveOrigins(root)
	applyClipping(root, Rect{Size: WindowSize})

	beginRenderToSurfaces(root)
}

func Absf32(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) &^ (1 << 31))
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

func resolveOrigins(container *Container) {
	// defer profiler.Time("resolveOrigins")()

	// sizes are already resolved here!
	mainAxis, crossAxis := MainCrossAxes(container.Row)

	var paddingSize Vec2
	paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
	paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]

	availableSize := Vec2Sub(container.resolvedSize, paddingSize)

	var nextLineOrigin Vec2
	nextLineOrigin[0] += container.Padding[PAD_LEFT]
	nextLineOrigin[1] += container.Padding[PAD_TOP]

	nextLineOrigin = Vec2Sub(nextLineOrigin, container.scrollOffset)

	// cross alignment works on two levels: first we apply it to the wrap lines, then we apply it inside each wrap line!
	switch container.CrossAlign {
	case AlignMiddle:
		nextLineOrigin[crossAxis] += (availableSize[crossAxis] - container.contentSize[crossAxis]) / 2
	case AlignEnd:
		nextLineOrigin[crossAxis] += (availableSize[crossAxis] - container.contentSize[crossAxis])
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
			child := &container.children[j]
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
			prev, ok := renderData[child.Id]
			if ok && !child.NoAnimate {
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
				animateFrom(&child.Transperancy, prev.Transperancy, rate, clrCutoff)
			}

			// apply relative origins **after** animations!
			for i := range container.children {
				child := &container.children[i]
				child.resolvedOrigin = Vec2Add(container.resolvedOrigin, child.relativeOrigin)
			}

			// FIXME: does this thrash the cache? should we do it in a second for loop?
			resolveOrigins(child)
		}

		// wrap lines are traversed on the cross axis
		nextLineOrigin[crossAxis] += wrapLine.size[crossAxis] + container.Gap
	}

	var parentId any
	if container.parent != nil {
		parentId = container.parent.Id
	}
	renderDataNext[container.Id] = RenderData{
		parentId:       parentId,
		Attrs:          container.Attrs,
		ResolvedSize:   container.resolvedSize,
		RelativeOrigin: container.relativeOrigin,
		ResolvedOrigin: container.resolvedOrigin,
		contentSize:    container.contentSize,
		scrollOffset:   container.scrollOffset,
	}
}

// this is called after resolving origins for everything
// it doesn't actually "clip" the view; it determines what
// the screen rect is when clipping is taken into account
func applyClipping(container *Container, clipRect Rect) {
	resolvedRect := Rect{
		Origin: container.resolvedOrigin,
		Size:   container.resolvedSize,
	}
	container.screenRect = RectIntersect(clipRect, resolvedRect)
	// renderDataNext is already filled; don't override it; just set the screen rect
	render := renderDataNext[container.Id]
	render.screenRect = container.screenRect
	renderDataNext[container.Id] = render

	nextClipRect := container.screenRect
	if !container.Clip {
		nextClipRect = clipRect
	}
	for i := range container.children {
		child := &container.children[i]
		applyClipping(child, nextClipRect)
	}

}

// called during the build up of the layout
func resolveSizeFromInside(container *Container) {
	// defer profiler.Time("resolveSizeFromInside")()

	attrs := container.Attrs

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
	container.contentSize = contentSize

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
func resolveSizesFromOutside(container *Container) {
	// defer profiler.Time("resolveSizesFromOutside")()

	mainAxis, crossAxis := MainCrossAxes(container.Row)

	var paddingSize Vec2
	paddingSize[0] = container.Padding[PAD_LEFT] + container.Padding[PAD_RIGHT]
	paddingSize[1] = container.Padding[PAD_TOP] + container.Padding[PAD_BOTTOM]

	// sizing hasn't been resolved yet, so we have to use data from previous frame!
	resolvedSize := container.resolvedSize

	// FIXME FIXME expand across does not work when wrapping elements; specially in row layout!

	// FIXME: only apply expansion and growth if wrap is not enabled
	// FIXME: need to create some test case (similar to demo1) for quick visual confirmation
	availableSize := Vec2Sub(resolvedSize, paddingSize)
	acrossSize := availableSize[crossAxis]

	for i := range container.wrapLines {
		wrapLine := &container.wrapLines[i]
		var growthRequest float32
		// acrossSize := wrapLine.size[crossAxis]
		roomForGrowth := availableSize[mainAxis] - wrapLine.size[mainAxis]

		for j := wrapLine.start; j < wrapLine.end; j++ {
			child := &container.children[j]
			// skip floating items
			if child.Floats {
				continue
			}

			growthRequest += child.Grow
			if child.ExpandAcross {
				child.resolvedSize[crossAxis] = acrossSize
			}
		}

		// ues; flex growth is applied inside a wrapped line!!
		if roomForGrowth > 0 && growthRequest > 0 {
			growthFactor := roomForGrowth / growthRequest
			for j := wrapLine.start; j < wrapLine.end; j++ {
				child := &container.children[j]
				// skip floating items
				if child.Floats {
					continue
				}

				// works fine for the zero case too, so no need for an if
				growthAmount := child.Attrs.Grow * growthFactor
				child.resolvedSize[mainAxis] += growthAmount
				wrapLine.size[mainAxis] += growthAmount // don't forget to apply the growth to the wrap line! otherwise alignment computations will get out of sync!
			}
		}
	}

	// recurse!
	for i := range container.children {
		child := &container.children[i]
		resolveSizesFromOutside(child)
	}
}

type HoverableArtifacts struct {
	Rect      Rect
	Container *Container
}

var hoverables []HoverableArtifacts
var focusables []any

var active any  // active means it's being engaged with the mouse
var focused any // focused means it receives key events
var directHovered any
var hovered []any
var prevFocused any // to know when focus changes!
var nextFocused any // requested focus!

var SurfaceCount int

func beginRenderToSurfaces(root *Container) {
	// defer profiler.Time("beginRenderToSurfaces")()

	g.ResetSlice(&surfaces)
	g.ResetSlice(&hoverables)
	g.ResetSlice(&focusables)

	_renderToSurfaces(root)
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
}

// should only be called from beginRenderToSurfaces
func _renderToSurfaces(container *Container) {
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

		PushSurface(Surface{
			Rect:       shRect,
			ImageId:    _IMBlurShadow(shRect.Size, container.Corners, container.Shadow.Blur, container.Shadow.Alpha),
			ImageScale: false,
		})
	}

	PushSurface(Surface{
		Rect:    resolvedRect,
		Color1:  container.Background,
		Color2:  Vec4Add(container.Background, container.Gradient),
		Corners: container.Corners,

		ImageId:      container.imageId,
		ImageScale:   true,
		FontId:       container.fontId,
		GlyphId:      container.glyphId,
		GlyphOffset:  container.glyphOffset,
		Clip:         clip1,
		Transperancy: container.Transperancy,
	})

	if !container.ClickThrough {
		g.Append(&hoverables, HoverableArtifacts{
			// Rect:      resolvedRect,
			Rect:      container.screenRect,
			Container: container,
		})
	}
	if container.Focusable {
		g.Append(&focusables, container.Id)
	}

	for i := range container.children {
		_renderToSurfaces(&container.children[i])
	}

	// border and clipping
	if container.BorderWidth > 0 || shouldClip || container.Transperancy > 0 {
		PushSurface(Surface{
			Rect:    resolvedRect,
			Color1:  container.BorderColor,
			Color2:  container.BorderColor,
			Corners: container.Corners,
			Stroke:  container.BorderWidth,
			Clip:    clip2,

			PopTransperancy: container.Transperancy > 0,
		})
	}
}

func Focus() {
	nextFocused = current.Id
}

func FocusImmediate() {
	focused = current.Id
	nextFocused = current.Id
}

func FocusImmediateOn(id any) {
	focused = id
	nextFocused = id
}

func Blur() {
	// do not blur if something else already requested focus!
	if nextFocused == current.Id {
		nextFocused = nil
	}
}

// grab focus if this is our first render and nothing else is focused
func AutoFocus() {
	if FirstRender() && nextFocused == nil {
		Focus()
	}
}

// dir should be 1 or -1, but an arbitrary number should work too ..
func CycleFocus(dir int) {
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

func CycleFocusOnTab() {
	_cycleFocusOnTab(current.Id)
}

func _cycleFocusOnTab(currentId any) {
	// if has focus && tab key is pressed: cycle focus

	if focused != currentId {
		return
	}

	if FrameInput.Key == KeyTab {
		var dir = 1
		if InputState.Modifiers&ModShift != 0 {
			dir = -1
		}
		CycleFocus(dir)
	}
}

func FirstRender() bool {
	_, found := renderData[current.Id]
	return !found
}

func HasFocus() bool {
	return focused == current.Id
}

func IdHasFocus(id any) bool {
	return focused == id
}

func isChild(target any) bool {
	for target != nil {
		if target == current.Id {
			return true
		} else {
			target = renderData[target].parentId
		}
	}
	return false
}

func HasFocusWithin() bool {
	return isChild(focused)
}

func IdIsHovered(id any) bool {
	return slices.Contains(hoverList, id)
}

func IsIdHoveredDirectly(id any) bool {
	return len(hoverList) > 0 && hoverList[0] == id
}

func IsHovered() bool {
	return slices.Contains(hoverList, current.Id)
}

func IsHoveredDirectly() bool {
	return len(hoverList) > 0 && hoverList[0] == current.Id
}

func IsClicked() bool {
	return IsHovered() && FrameInput.Mouse == MouseClick
}

func IdIsClicked(id any) bool {
	return IdIsHovered(id) && FrameInput.Mouse == MouseClick
}

func SetActive() {
	active = current.Id
}

func UnsetActive() {
	active = nil
}

func IsActive() bool {
	return active != nil && active == current.Id
}

func CurrentId() any {
	return current.Id
}

func GetLastId() any {
	if len(current.children) == 0 {
		return nil
	}
	return generic.Last(current.children).Id
}

func GetRenderData() RenderData {
	return renderData[current.Id]
}

func GetRenderDataOf(id any) RenderData {
	return renderData[id]
}

// Get the screen rect of the current element from the previous frame data
func GetScreenRect() Rect {
	return renderData[current.Id].screenRect
}

func GetScreenRectOf(target any) Rect {
	return renderData[target].screenRect
}

func GetResolvedRectOf(target any) Rect {
	var rd = renderData[target]
	return Rect{
		Origin: rd.ResolvedOrigin,
		Size:   rd.ResolvedSize,
	}
}

func GetResolvedSize() Vec2 {
	return renderData[current.Id].ResolvedSize
}

func GetAvailableSize() Vec2 {
	return GetContentRectOf(current.Id).Size
}

func GetContentRect() Rect {
	return GetContentRectOf(current.Id)
}

func GetContentRectOf(id any) Rect {
	var rd = renderData[id]

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

// applications can set this to make the IME box appears in the right place
var CaretPos Vec2
