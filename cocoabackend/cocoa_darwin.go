//go:build darwin

// Package cocoabackend is a direct-macOS (AppKit) backend for shirei. AppKit
// provides the window, run loop, and input; all rasterization is done by shirei's
// core software renderer, which rasterizes each frame straight into an IOSurface
// that is set as a CALayer's contents — the window server composites it on the GPU
// with no per-frame CPU copy. It is an alternative to giobackend; see PLAN.md for
// the roadmap and ../notes/opus-ime-assessment.txt for the motivation (IME + CPU).
package cocoabackend

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework IOSurface
#include <stdlib.h>
#include "cocoa.h"
*/
import "C"

import (
	"image"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/iconimg"
	"go.hasen.dev/shirei/internal/qwerty"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache; 0 would disable it). 16 MB holds many thousands of glyph masks.
const glyphCacheBudget = 16 << 20

var (
	winTitle    string
	winIconPath string
	winIconImg  *image.NRGBA
	winW        int
	winH        int
	frameFn     shirei.FrameFn
)

// SetupWindow records the window parameters. The window is created in Run, on
// the main thread.
func SetupWindow(title string, width int, height int) {
	winTitle = title
	winW = width
	winH = height
}

// SetupIcon records the path of the image (any NSImage-readable format, e.g.
// PNG, including .icns) used as the app's Dock icon — macOS has no title-bar
// icons. Call it before Run; empty means the default icon.
func SetupIcon(imagePath string) {
	winIconPath = imagePath
}

// SetupIconImage is SetupIcon from an in-memory image (e.g. decoded from
// go:embed-ed bytes) instead of a file. It takes precedence over SetupIcon.
func SetupIconImage(img image.Image) {
	winIconImg = iconimg.FromImage(img)
}

// init locks the main goroutine to the main OS thread (thread 0). AppKit requires
// NSApplication/NSWindow to live on thread 0, and init runs there — before main()
// and before any goroutines exist — so main() and therefore Run stay on thread 0
// even after the app spawns background goroutines first. Locking only inside Run
// is too late: by then the scheduler may already have migrated the main goroutine
// off thread 0 (this crashed apps like vbeam/local_gui, which start goroutines
// before Run with "NSWindow should only be instantiated on the main thread").
func init() {
	runtime.LockOSThread()
}

// Run opens the window and runs the AppKit event loop. It must be called
// from the program's main goroutine (AppKit requires the main thread) and
// does not return until the app exits. System fonts are initialized by
// shirei on the first frame (RunFrameFn), not here.
func Run(fn shirei.FrameFn) {
	// Redundant with the init() lock above (which is the actual guarantee), but
	// harmless and documents intent.
	runtime.LockOSThread()

	frameFn = fn

	shirei.GlyphCacheBudgetBytes = glyphCacheBudget

	ctitle := C.CString(winTitle)
	defer C.free(unsafe.Pointer(ctitle))
	C.cocoa_setupWindow(ctitle, C.int(winW), C.int(winH))
	if winIconImg != nil {
		b := winIconImg.Bounds()
		C.cocoa_setAppIconRGBA((*C.uchar)(unsafe.Pointer(&winIconImg.Pix[0])),
			C.int(b.Dx()), C.int(b.Dy()))
	} else if winIconPath != "" {
		cicon := C.CString(winIconPath)
		C.cocoa_setAppIcon(cicon)
		C.free(unsafe.Pointer(cicon))
	}
	C.cocoa_enable_zerocopy()
	C.cocoa_run()
}

// IOSurface present pool. A few surfaces are rotated so we never render into the
// one the compositor is currently reading (IOSurfaceIsInUse). Static frames present
// nothing — the layer keeps showing the last surface.
type ioSurface struct {
	ref  unsafe.Pointer
	w, h int
}

var (
	surfacePool   []ioSurface
	lastPresented unsafe.Pointer
	presentW      int
	presentH      int
	havePresented bool

	// lastPresentedHash is the content hash of the frame currently on screen. The
	// present skips when the new frame's hash equals it — the renderer's proof that
	// nothing needs to be drawn. Tracked against the PRESENTED frame (not the last
	// produced one) so it stays correct when produce and present are not 1:1: a
	// tear-defer or two produces collapsing into one present would make a
	// produced-vs-produced comparison skip a frame that never reached the screen.
	lastPresentedHash uint64

	// presentDeferred: the last present found no free surface and postponed
	// itself to avoid tearing. Until it succeeds, the pending frame's content is
	// not yet on screen, so the unchanged-frame skip must not short-circuit.
	presentDeferred bool
)

// shireiRenderAndPresent renders the stashed frame's surfaces into an
// IOSurface (the actual rasterization work — this is where a profile's
// render time lands) and hands it to the window server. Only that final
// handoff is "zero copy": the IOSurface becomes the CALayer's contents
// directly, so the compositor reads the same memory we rendered into —
// no per-frame buffer copy. (The function was historically named
// shireiPresentZeroCopy after that strategy, which read as if the whole
// function copied nothing.)
//
//export shireiRenderAndPresent
func shireiRenderAndPresent(source C.int) {
	t0 := time.Now()
	defer func() { perfRecordPaint(time.Since(t0)) }()
	perfRecordPresentSource(int(source))

	scale := shirei.WindowScale
	if scale <= 0 {
		scale = 1
	}
	dw := int(shirei.WindowSize[0]*scale + 0.5)
	dh := int(shirei.WindowSize[1]*scale + 0.5)
	if dw <= 0 || dh <= 0 {
		return
	}

	// The frame on screen already shows this exact content: leave the layer as is.
	// The compositor keeps displaying it — idle is free, regardless of whether the
	// loop was kept awake (NextFrameRequested). (Unless a present is still deferred:
	// then this content isn't on screen yet and we must go through.)
	if havePresented && frameHash == lastPresentedHash && dw == presentW && dh == presentH && !presentDeferred {
		perfRecordPresentSkip()
		return
	}

	ensureSurfacePool(dw, dh)
	s := pickFreeSurface()
	if s == nil {
		// Every off-screen surface is still held by the compositor. Writing one
		// now would race its scan-out and tear the frame, so defer to the next
		// tick (the layer keeps showing the last frame meanwhile) and keep the
		// render loop awake so we actually retry.
		presentDeferred = true
		C.cocoa_setWantsFrame(1)
		return
	}
	presentDeferred = false

	perfRecordSurfaces(frameSurfaces) // classify before timing render, so its overhead is excluded

	var cstride C.int
	tLock := time.Now()
	base := C.iosurface_lock(s, &cstride)
	perfRecordLock(time.Since(tLock))
	if base == nil {
		return
	}
	stride := int(cstride)
	buf := unsafe.Slice((*byte)(base), stride*dh)
	tRender := time.Now()
	softRenderer.RenderInto(buf, stride, dw, dh, scale, frameSurfaces)
	perfRecordRender(time.Since(tRender))
	tUnlock := time.Now()
	C.iosurface_unlock(s)
	perfRecordUnlock(time.Since(tUnlock))

	tSet := time.Now()
	C.cocoa_set_layer_contents(s)
	perfRecordSetLayer(time.Since(tSet))
	lastPresented = s
	lastPresentedHash = frameHash
	presentW, presentH, havePresented = dw, dh, true
}

// ensureSurfacePool (re)creates the surfaces when the device size changes.
func ensureSurfacePool(w, h int) {
	if len(surfacePool) > 0 && surfacePool[0].w == w && surfacePool[0].h == h {
		return
	}
	for _, s := range surfacePool {
		C.iosurface_release(s.ref)
	}
	surfacePool = surfacePool[:0]
	lastPresented = nil
	havePresented = false
	for i := 0; i < 3; i++ {
		ref := unsafe.Pointer(C.iosurface_create(C.int(w), C.int(h)))
		if ref == nil {
			continue
		}
		surfacePool = append(surfacePool, ioSurface{ref: ref, w: w, h: h})
	}
}

// pickFreeSurface returns a surface the compositor is not reading and that is not
// the one currently on screen, so writing it cannot tear the displayed frame. It
// returns nil when every off-screen surface is still in use; the caller then
// defers the present rather than writing a surface mid scan-out.
func pickFreeSurface() unsafe.Pointer {
	for _, s := range surfacePool {
		if s.ref != lastPresented && C.iosurface_in_use(s.ref) == 0 {
			perfRecordPick(pickClean)
			return s.ref
		}
	}
	perfRecordPick(pickDefer)
	return nil
}

// Frame production is decoupled from presenting. Input events and the animation
// tick call shireiProduceFrame (synchronously, in Go) to run one shirei frame and
// stash its surfaces; shireiRenderAndPresent then renders them into an IOSurface and
// sets it as the layer's contents. This way each input event is consumed by its own
// frame regardless of when AppKit repaints.
var (
	frameSurfaces   []shirei.Surface // copy of the last produced frame's surfaces
	lastProducedW   float32
	lastProducedH   float32
	haveFrame       bool
	pendingText     string // committed text from NSTextInputClient, flushed next frame
	pendingPaste    string // clipboard text read after a paste request
	hasPendingPaste bool

	// frameHash is the content hash (FrameOutputData.SurfacesHash) of the last
	// produced frame. The present compares it against the hash of the frame already
	// on screen to skip identical frames (see shireiRenderAndPresent).
	frameHash uint64

	softRenderer shirei.SoftRenderer // rasterizes into the IOSurface (RenderInto)
)

//export shireiProduceFrame
func shireiProduceFrame(w C.double, h C.double) {
	shirei.WindowSize = shirei.Vec2{float32(w), float32(h)}
	shirei.WindowScale = float32(C.cocoa_backingScaleFactor())
	lastProducedW, lastProducedH = float32(w), float32(h)

	flushPendingFrameText()

	t0 := time.Now()
	out := shirei.RunFrameFn(frameFn)
	perfRecordProduce(time.Since(t0))

	// Surface is a flat value type, so a copy stays valid after the next frame
	// reuses shirei's internal surface buffer.
	frameSurfaces = append(frameSurfaces[:0], out.Surfaces...)
	haveFrame = true

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		pendingPaste = getClipboard()
		hasPendingPaste = true
		C.cocoa_requestRedraw()
	}

	var wf C.int
	if out.NextFrameRequested {
		wf = 1
	}
	C.cocoa_setWantsFrame(wf)

	// Record this frame's content hash; the present skips when it matches what is
	// already on screen. Deliberately NOT gated on NextFrameRequested: an animation,
	// blink timer, or pending async decode keeps the loop awake, but if the surfaces
	// it produces are identical there is still nothing to present, so the renderer
	// stays idle. The hash covers image generations (images.go), so an in-place
	// decode is never mistaken for "unchanged".
	frameHash = out.SurfacesHash
}

func flushPendingFrameText() {
	if hasPendingPaste {
		pendingText += pendingPaste
		pendingPaste = ""
		hasPendingPaste = false
	}
	if pendingText != "" {
		shirei.FrameInput.Text += pendingText
		pendingText = ""
	}
}

// shireiNeedsProduce reports whether drawRect: must produce a frame itself
// (no frame yet, or the view was resized) rather than just repaint the stash.
//
//export shireiNeedsProduce
func shireiNeedsProduce(w C.double, h C.double) C.int {
	if !haveFrame || float32(w) != lastProducedW || float32(h) != lastProducedH {
		return 1
	}
	return 0
}

//export shireiFrameRequested
func shireiFrameRequested() C.int {
	if shirei.FrameRequested() {
		return 1
	}
	return 0
}

//export shireiCaretX
func shireiCaretX() C.double {
	return C.double(shirei.CaretPos[0])
}

//export shireiCaretY
func shireiCaretY() C.double {
	return C.double(shirei.CaretPos[1])
}

//export shireiCaretHeight
func shireiCaretHeight() C.double {
	return C.double(shirei.CaretHeight)
}

func setClipboard(s string) {
	cs := C.CString(s)
	C.cocoa_setClipboard(cs)
	C.free(unsafe.Pointer(cs))
}

func getClipboard() string {
	cs := C.cocoa_getClipboard()
	if cs == nil {
		return ""
	}
	s := C.GoString(cs)
	C.free(unsafe.Pointer(cs))
	return s
}

// -----------------------------------------------------------------------------
//  Input (called from the NSView's event overrides)
// -----------------------------------------------------------------------------

// mouse actions, matching the ObjC side
const (
	mouseMove = 0
	mouseDown = 1
	mouseUp   = 2
	mouseDrag = 3
)

//export shireiMouse
func shireiMouse(x, y C.double, action, button C.int) {
	np := shirei.Vec2{float32(x), float32(y)}
	prev := shirei.InputState.MousePoint
	shirei.FrameInput.Motion = shirei.Vec2Add(shirei.FrameInput.Motion, shirei.Vec2Sub(np, prev))
	shirei.InputState.MousePoint = np
	shirei.InputState.MouseButton = shirei.MouseButton(button)

	switch action {
	case mouseDown:
		shirei.FrameInput.Mouse = shirei.MouseClick
	case mouseUp:
		shirei.FrameInput.Mouse = shirei.MouseRelease
	}
}

//export shireiScroll
func shireiScroll(dx, dy C.double) {
	shirei.FrameInput.Scroll = shirei.Vec2Add(shirei.FrameInput.Scroll,
		shirei.Vec2{float32(dx), float32(dy)})
}

//export shireiWindowFocus
func shireiWindowFocus(focused C.int) {
	shirei.WindowFocused = focused != 0
	// re-render once so focus-only affordances (the text caret) show/hide; the loop
	// then sleeps again if nothing else is animating.
	shirei.RequestNextFrame()
}

// NSEvent.modifierFlags bits (stable AppKit values).
const (
	nsShift   = 1 << 17
	nsControl = 1 << 18
	nsOption  = 1 << 19
	nsCommand = 1 << 20
)

//export shireiSetModifiers
func shireiSetModifiers(flags C.uint) {
	f := uint(flags)
	var m shirei.Modifiers
	if f&nsShift != 0 {
		m |= shirei.ModShift
	}
	if f&nsControl != 0 {
		m |= shirei.ModCtrl
	}
	if f&nsOption != 0 {
		m |= shirei.ModAlt
	}
	if f&nsCommand != 0 {
		m |= shirei.ModCmd
	}
	shirei.InputState.Modifiers = m

	// modifier keys arrive via flagsChanged, not keyDown, so mirror them into
	// DownKeys (shirei widgets check e.g. DownKeys contains KeyShift).
	syncModKey(m, shirei.ModShift, shirei.KeyShift)
	syncModKey(m, shirei.ModCtrl, shirei.KeyCtrl)
	syncModKey(m, shirei.ModAlt, shirei.KeyAlt)
	syncModKey(m, shirei.ModCmd, shirei.KeyCommand)
}

func syncModKey(m, bit shirei.Modifiers, k shirei.KeyCode) {
	if m&bit != 0 {
		g.SliceAddUniq(&shirei.InputState.DownKeys, k)
	} else {
		g.SliceRemove(&shirei.InputState.DownKeys, k)
	}
}

func keyDown(vkey int, bare string) {
	if code := mapVKey(uint16(vkey), bare); code != shirei.KeyCodeNone {
		shirei.FrameInput.Key = code
		g.SliceAddUniq(&shirei.InputState.DownKeys, code)
	}
}

//export shireiKeyDown
func shireiKeyDown(vkey C.int, cbare *C.char) {
	keyDown(int(vkey), C.GoString(cbare))
}

func queueCommittedText(text string) {
	if isPrintable(text) {
		pendingText += text
	}
}

//export shireiCommitText
func shireiCommitText(cchars *C.char) {
	queueCommittedText(C.GoString(cchars))
}

func utf16OffsetToRuneOffset(s string, units int) int {
	if units <= 0 {
		return 0
	}
	u16 := utf16.Encode([]rune(s))
	if units > len(u16) {
		units = len(u16)
	}
	return len(utf16.Decode(u16[:units]))
}

func setCompositionFromUTF16Offsets(text string, startUTF16 int, endUTF16 int) {
	start := utf16OffsetToRuneOffset(text, startUTF16)
	end := utf16OffsetToRuneOffset(text, endUTF16)
	if start > end {
		start, end = end, start
	}
	shirei.InputState.Composition = text
	shirei.InputState.CompositionSel = [2]int{start, end}
	shirei.RequestNextFrame()
}

//export shireiSetComposition
func shireiSetComposition(cchars *C.char, startUTF16 C.int, endUTF16 C.int) {
	setCompositionFromUTF16Offsets(C.GoString(cchars), int(startUTF16), int(endUTF16))
}

//export shireiKeyUp
func shireiKeyUp(vkey C.int, cbare *C.char) {
	if code := mapVKey(uint16(vkey), C.GoString(cbare)); code != shirei.KeyCodeNone {
		g.SliceRemove(&shirei.InputState.DownKeys, code)
	}
}

func isPrintable(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)[0]
	// NSEvent delivers function keys (arrows, Home/End, page keys, F1-F12,
	// forward delete) as private-use code points in the reserved
	// 0xF700-0xF8FF range: key identity, not typed text. Relaying them as
	// Text inserts invisible glyphless runes into text inputs (and the
	// caret then renders at end of line).
	if r >= 0xF700 && r <= 0xF8FF {
		return false
	}
	return r >= 0x20 && r != 0x7f
}

// Cocoa virtual key codes (hardware, layout-independent) for non-text keys.
const (
	vkReturn        = 0x24
	vkTab           = 0x30
	vkSpace         = 0x31
	vkDelete        = 0x33 // backspace
	vkEscape        = 0x35
	vkKeypadEnter   = 0x4C
	vkForwardDelete = 0x75
	vkHome          = 0x73
	vkEnd           = 0x77
	vkPageUp        = 0x74
	vkPageDown      = 0x79
	vkLeft          = 0x7B
	vkRight         = 0x7C
	vkDown          = 0x7D
	vkUp            = 0x7E

	// function keys (HIToolbox kVK_F* values)
	vkF1  = 0x7A
	vkF2  = 0x78
	vkF3  = 0x63
	vkF4  = 0x76
	vkF5  = 0x60
	vkF6  = 0x61
	vkF7  = 0x62
	vkF8  = 0x64
	vkF9  = 0x65
	vkF10 = 0x6D
	vkF11 = 0x67
	vkF12 = 0x6F
)

// mapVKey maps a Cocoa virtual key code to a shirei KeyCode. Special keys
// and the whole writing block are matched by their (layout-independent)
// virtual code — KeyW is the physical key at the US-QWERTY W position no
// matter the active layout; typed text still honors the layout via the
// separate Text path. Keys outside those tables fall back to
// charactersIgnoringModifiers, uppercased for letters.
func mapVKey(vk uint16, bare string) shirei.KeyCode {
	switch vk {
	case vkLeft:
		return shirei.KeyLeft
	case vkRight:
		return shirei.KeyRight
	case vkUp:
		return shirei.KeyUp
	case vkDown:
		return shirei.KeyDown
	case vkReturn, vkKeypadEnter:
		return shirei.KeyEnter
	case vkEscape:
		return shirei.KeyEscape
	case vkDelete:
		return shirei.KeyDeleteBackward
	case vkForwardDelete:
		return shirei.KeyDeleteForward
	case vkHome:
		return shirei.KeyHome
	case vkEnd:
		return shirei.KeyEnd
	case vkPageUp:
		return shirei.KeyPageUp
	case vkPageDown:
		return shirei.KeyPageDown
	case vkTab:
		return shirei.KeyTab
	case vkSpace:
		return shirei.KeySpace
	case vkF1:
		return shirei.KeyF1
	case vkF2:
		return shirei.KeyF2
	case vkF3:
		return shirei.KeyF3
	case vkF4:
		return shirei.KeyF4
	case vkF5:
		return shirei.KeyF5
	case vkF6:
		return shirei.KeyF6
	case vkF7:
		return shirei.KeyF7
	case vkF8:
		return shirei.KeyF8
	case vkF9:
		return shirei.KeyF9
	case vkF10:
		return shirei.KeyF10
	case vkF11:
		return shirei.KeyF11
	case vkF12:
		return shirei.KeyF12
	}
	if code := qwerty.FromMacVK(vk); code != shirei.KeyCodeNone {
		return code
	}
	if bare != "" {
		r := []rune(bare)[0]
		if r >= 'a' && r <= 'z' {
			return shirei.KeyCode(r - 'a' + 'A')
		}
		if r < 128 {
			return shirei.KeyCode(r)
		}
	}
	return shirei.KeyCodeNone
}
