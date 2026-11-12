//go:build darwin && !ios

package cocoabackend

import (
	"fmt"
	"os"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

// AppKit / IOSurface via purego.objc — no cgo. Class IMPs (including
// NSTextInputClient methods that take/return NSRect/NSRange) need
// ebitengine/purego v0.11.0+.

const (
	nsApplicationActivationPolicyRegular = 0
	nsWindowStyleMaskTitled              = 1 << 0
	nsWindowStyleMaskClosable            = 1 << 1
	nsWindowStyleMaskMiniaturizable      = 1 << 2
	nsWindowStyleMaskResizable           = 1 << 3
	nsWindowStyleMaskNonactivatingPanel  = 1 << 7
	nsBackingStoreBuffered               = 2
	nsTerminateNow                       = 1

	nsTrackingMouseMoved         = 0x02
	nsTrackingMouseEnteredExited = 0x01
	nsTrackingActiveInKeyWindow  = 0x20
	nsTrackingActiveAlways       = 0x80
	nsTrackingInVisibleRect      = 0x200

	nsEventModifierFlagControl                    = 1 << 18
	nsEventModifierFlagCommand                    = 1 << 20
	nsEventModifierFlagDeviceIndependentFlagsMask = 0xffff0000
	nsBitmapFormatAlphaNonpremultiplied           = 1 << 1
	nsLayerWidthSizable                           = 1 << 1
	nsLayerHeightSizable                          = 1 << 4
	nsNotFound                                    = uint(^uint(0) >> 1)

	pixelFormatBGRA = 0x42475241 // 'BGRA'
)

type nsPoint struct {
	_    structs.HostLayout
	X, Y float64
}

type nsSize struct {
	_             structs.HostLayout
	Width, Height float64
}

type nsRect struct {
	_      structs.HostLayout
	Origin nsPoint
	Size   nsSize
}

type nsRange struct {
	_                structs.HostLayout
	Location, Length uint
}

func nsMakeRect(x, y, w, h float64) nsRect {
	return nsRect{Origin: nsPoint{X: x, Y: y}, Size: nsSize{Width: w, Height: h}}
}

var (
	appkitOnce sync.Once
	appkitErr  error
	sels       sync.Map

	kIOSurfaceWidth           objc.ID
	kIOSurfaceHeight          objc.ID
	kIOSurfaceBytesPerElement objc.ID
	kIOSurfaceBytesPerRow     objc.ID
	kIOSurfacePixelFormat     objc.ID
	kCAGravityResize          objc.ID
	nsPasteboardTypeString    objc.ID
	nsRunLoopCommonModes      objc.ID
	nsCalibratedRGBColorSpace objc.ID

	ioSurfaceCreate         func(props uintptr) uintptr
	ioSurfaceLock           func(s uintptr, opts uint32, seed *uint32) int32
	ioSurfaceUnlock         func(s uintptr, opts uint32, seed *uint32) int32
	ioSurfaceGetBytesPerRow func(s uintptr) uintptr
	ioSurfaceGetBaseAddress func(s uintptr) uintptr
	ioSurfaceIsInUse        func(s uintptr) uint8
	cfRelease               func(s uintptr)

	viewClass        objc.Class
	windowClass      objc.Class
	panelClass       objc.Class
	appDelegateClass objc.Class

	gDelegate objc.ID
)

func sel(name string) objc.SEL {
	if v, ok := sels.Load(name); ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	actual, _ := sels.LoadOrStore(name, s)
	return actual.(objc.SEL)
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), s)
}

func nsPlainString(obj objc.ID) string {
	if obj == 0 {
		return ""
	}
	if obj.Send(sel("isKindOfClass:"), objc.GetClass("NSAttributedString")) != 0 {
		obj = obj.Send(sel("string"))
	}
	if obj == 0 {
		return ""
	}
	if obj.Send(sel("isKindOfClass:"), objc.GetClass("NSString")) != 0 {
		return cstrings.NSStringToString(obj)
	}
	return cstrings.NSStringToString(obj.Send(sel("description")))
}

func nsArray(objs ...objc.ID) objc.ID {
	a := objc.ID(objc.GetClass("NSMutableArray")).Send(sel("array"))
	for _, o := range objs {
		a.Send(sel("addObject:"), o)
	}
	return a
}

func nsNumber(n int) objc.ID {
	return objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithInt:"), n)
}

func nsNumberU32(n uint32) objc.ID {
	return objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithUnsignedInt:"), n)
}

func ptr[T any](p uintptr) *T {
	return *(**T)(unsafe.Pointer(&p))
}

func loadConst(lib uintptr, name string) (objc.ID, error) {
	addr, err := purego.Dlsym(lib, name)
	if err != nil || addr == 0 {
		return 0, fmt.Errorf("missing %s", name)
	}
	return *ptr[objc.ID](addr), nil
}

func dlopen(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
}

func ensureAppkit() error {
	appkitOnce.Do(func() {
		for _, path := range []string{
			"/System/Library/Frameworks/Cocoa.framework/Cocoa",
			"/System/Library/Frameworks/QuartzCore.framework/QuartzCore",
			"/System/Library/Frameworks/IOSurface.framework/IOSurface",
		} {
			if _, err := dlopen(path); err != nil {
				appkitErr = err
				return
			}
		}
		ioLib, err := dlopen("/System/Library/Frameworks/IOSurface.framework/IOSurface")
		if err != nil {
			appkitErr = err
			return
		}
		qc, err := dlopen("/System/Library/Frameworks/QuartzCore.framework/QuartzCore")
		if err != nil {
			appkitErr = err
			return
		}
		appkit, err := dlopen("/System/Library/Frameworks/AppKit.framework/AppKit")
		if err != nil {
			appkitErr = err
			return
		}
		purego.RegisterLibFunc(&ioSurfaceCreate, ioLib, "IOSurfaceCreate")
		purego.RegisterLibFunc(&ioSurfaceLock, ioLib, "IOSurfaceLock")
		purego.RegisterLibFunc(&ioSurfaceUnlock, ioLib, "IOSurfaceUnlock")
		purego.RegisterLibFunc(&ioSurfaceGetBytesPerRow, ioLib, "IOSurfaceGetBytesPerRow")
		purego.RegisterLibFunc(&ioSurfaceGetBaseAddress, ioLib, "IOSurfaceGetBaseAddress")
		purego.RegisterLibFunc(&ioSurfaceIsInUse, ioLib, "IOSurfaceIsInUse")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")

		must := func(id objc.ID, e error) objc.ID {
			if e != nil {
				appkitErr = e
			}
			return id
		}
		kIOSurfaceWidth = must(loadConst(ioLib, "kIOSurfaceWidth"))
		kIOSurfaceHeight = must(loadConst(ioLib, "kIOSurfaceHeight"))
		kIOSurfaceBytesPerElement = must(loadConst(ioLib, "kIOSurfaceBytesPerElement"))
		kIOSurfaceBytesPerRow = must(loadConst(ioLib, "kIOSurfaceBytesPerRow"))
		kIOSurfacePixelFormat = must(loadConst(ioLib, "kIOSurfacePixelFormat"))
		kCAGravityResize = must(loadConst(qc, "kCAGravityResize"))
		nsPasteboardTypeString = must(loadConst(appkit, "NSPasteboardTypeString"))
		nsRunLoopCommonModes = must(loadConst(appkit, "NSRunLoopCommonModes"))
		nsCalibratedRGBColorSpace = must(loadConst(appkit, "NSCalibratedRGBColorSpace"))
		if appkitErr != nil {
			return
		}
		appkitErr = registerAccessClass()
		if appkitErr == nil {
			appkitErr = registerClasses()
		}
	})
	return appkitErr
}

func registerClasses() error {
	if c := objc.GetClass("ShireiView"); c != 0 {
		viewClass = c
		windowClass = objc.GetClass("ShireiWindow")
		panelClass = objc.GetClass("ShireiPanel")
		appDelegateClass = objc.GetClass("ShireiAppDelegate")
		return nil
	}
	// NSApplicationDelegate is lazily registered in AppKit and objc_getProtocol
	// returns nil without a static @protocol() reference (which needs cgo).
	// AppKit still calls the methods via respondsToSelector. NSTextInputClient
	// and NSWindowDelegate are available after dlopen.
	textClient := objc.GetProtocol("NSTextInputClient")
	winDel := objc.GetProtocol("NSWindowDelegate")
	var viewProtos []*objc.Protocol
	if textClient != nil {
		viewProtos = append(viewProtos, textClient)
	}
	var appProtos []*objc.Protocol
	if winDel != nil {
		appProtos = append(appProtos, winDel)
	}

	var err error
	windowClass, err = objc.RegisterClass("ShireiWindow", objc.GetClass("NSWindow"), nil, nil,
		[]objc.MethodDef{
			{Cmd: sel("canBecomeKeyWindow"), Fn: windowCanBecomeKey},
			{Cmd: sel("canBecomeMainWindow"), Fn: windowCanBecomeMain},
		})
	if err != nil {
		return err
	}
	panelClass, err = objc.RegisterClass("ShireiPanel", objc.GetClass("NSPanel"), nil, nil,
		[]objc.MethodDef{
			{Cmd: sel("canBecomeKeyWindow"), Fn: windowCanBecomeKey},
			{Cmd: sel("canBecomeMainWindow"), Fn: panelCanBecomeMain},
		})
	if err != nil {
		return err
	}
	viewClass, err = objc.RegisterClass("ShireiView", objc.GetClass("NSView"),
		viewProtos,
		nil,
		[]objc.MethodDef{
			{Cmd: sel("isFlipped"), Fn: viewIsFlipped},
			{Cmd: sel("isAccessibilityElement"), Fn: viewAccessElement},
			{Cmd: sel("accessibilityRole"), Fn: viewAccessRole},
			{Cmd: sel("accessibilityChildren"), Fn: viewAccessChildren},
			{Cmd: sel("accessibilityHitTest:"), Fn: accessHitTest},
			{Cmd: sel("accessibilityFocusedUIElement"), Fn: viewAccessFocused},
			{Cmd: sel("acceptsFirstResponder"), Fn: viewAcceptsFirstResponder},
			{Cmd: sel("acceptsFirstMouse:"), Fn: viewAcceptsFirstMouse},
			{Cmd: sel("drawRect:"), Fn: viewDrawRect},
			{Cmd: sel("renderFrame"), Fn: viewRenderFrameIMP},
			{Cmd: sel("tick:"), Fn: viewTick},
			{Cmd: sel("shireiWakeAndRender"), Fn: viewWakeAndRender},
			{Cmd: sel("updateTrackingAreas"), Fn: viewUpdateTrackingAreas},
			{Cmd: sel("mouseMoved:"), Fn: viewMouseMoved},
			{Cmd: sel("mouseEntered:"), Fn: viewMouseEntered},
			{Cmd: sel("mouseExited:"), Fn: viewMouseExited},
			{Cmd: sel("mouseDragged:"), Fn: viewMouseDragged},
			{Cmd: sel("mouseDown:"), Fn: viewMouseDown},
			{Cmd: sel("mouseUp:"), Fn: viewMouseUp},
			{Cmd: sel("rightMouseDown:"), Fn: viewRightMouseDown},
			{Cmd: sel("rightMouseUp:"), Fn: viewRightMouseUp},
			{Cmd: sel("scrollWheel:"), Fn: viewScrollWheel},
			{Cmd: sel("flagsChanged:"), Fn: viewFlagsChanged},
			{Cmd: sel("keyDown:"), Fn: viewKeyDown},
			{Cmd: sel("keyUp:"), Fn: viewKeyUp},
			{Cmd: sel("insertText:"), Fn: viewInsertText},
			{Cmd: sel("insertText:replacementRange:"), Fn: viewInsertTextReplacement},
			{Cmd: sel("setMarkedText:selectedRange:replacementRange:"), Fn: viewSetMarkedText},
			{Cmd: sel("unmarkText"), Fn: viewUnmarkText},
			{Cmd: sel("hasMarkedText"), Fn: viewHasMarkedText},
			{Cmd: sel("markedRange"), Fn: viewMarkedRange},
			{Cmd: sel("selectedRange"), Fn: viewSelectedRange},
			{Cmd: sel("attributedSubstringForProposedRange:actualRange:"), Fn: viewAttributedSubstring},
			{Cmd: sel("characterIndexForPoint:"), Fn: viewCharacterIndexForPoint},
			{Cmd: sel("firstRectForCharacterRange:actualRange:"), Fn: viewFirstRectForCharacterRange},
			{Cmd: sel("validAttributesForMarkedText"), Fn: viewValidAttributesForMarkedText},
			{Cmd: sel("doCommandBySelector:"), Fn: viewDoCommandBySelector},
			{Cmd: sel("resignFirstResponder"), Fn: viewResignFirstResponder},
		})
	if err != nil {
		return err
	}

	appDelegateClass, err = objc.RegisterClass("ShireiAppDelegate", objc.GetClass("NSObject"),
		appProtos,
		nil,
		[]objc.MethodDef{
			{Cmd: sel("wakeWindows"), Fn: appWakeWindows},
			{Cmd: sel("updatePopupWindows"), Fn: appUpdatePopupWindows},
			{Cmd: sel("windowShouldClose:"), Fn: appWindowShouldClose},
			{Cmd: sel("applicationShouldTerminateAfterLastWindowClosed:"), Fn: appShouldTerminateAfterLastWindowClosed},
			{Cmd: sel("applicationShouldTerminate:"), Fn: appShouldTerminate},
			{Cmd: sel("applicationDidFinishLaunching:"), Fn: appDidFinishLaunching},
			{Cmd: sel("windowDidResignKey:"), Fn: appWindowDidResignKey},
			{Cmd: sel("windowDidBecomeKey:"), Fn: appWindowDidBecomeKey},
			{Cmd: sel("windowDidChangeBackingProperties:"), Fn: appWindowNeedsFrame},
			{Cmd: sel("windowDidChangeScreen:"), Fn: appWindowNeedsFrame},
		})
	return err
}

func nsApp() objc.ID {
	return objc.ID(objc.GetClass("NSApplication")).Send(sel("sharedApplication"))
}

func viewPoint(self, event objc.ID) nsPoint {
	loc := objc.Send[nsPoint](event, sel("locationInWindow"))
	return objc.Send[nsPoint](self, sel("convertPoint:fromView:"), loc, objc.ID(0))
}

func viewSize(self objc.ID) (w, h float64) {
	r := objc.Send[nsRect](self, sel("bounds"))
	return r.Size.Width, r.Size.Height
}

func eventBare(event objc.ID) string {
	s := event.Send(sel("charactersIgnoringModifiers"))
	if s == 0 || objc.Send[uint](s, sel("length")) == 0 {
		return ""
	}
	return nsPlainString(s)
}

func noteInput() {
	currentWindow.gWantsFrame = true
}

func viewIsFlipped(objc.ID, objc.SEL) bool { return true }

// Borderless app windows participate in keyboard focus and the main-window menu.
func windowCanBecomeKey(objc.ID, objc.SEL) bool { return true }

func windowCanBecomeMain(objc.ID, objc.SEL) bool { return true }

func panelCanBecomeMain(objc.ID, objc.SEL) bool { return false }

func viewAcceptsFirstResponder(objc.ID, objc.SEL) bool { return true }

func viewAcceptsFirstMouse(objc.ID, objc.SEL, objc.ID) bool { return true }

func viewDrawRect(self objc.ID, _ objc.SEL, _ nsRect) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()
	if !currentWindow.ready || currentWindow.closing || currentWindow.rendering || framesInProgress > 0 {
		return
	}
	currentWindow.rendering = true
	defer func() { currentWindow.rendering = false }()

	w, h := viewSize(self)
	if needsProduce(w, h) {
		produceFrame(w, h)
	}
	renderAndPresent()
}

func viewRenderFrameIMP(self objc.ID, _ objc.SEL) { viewRenderFrame(self) }

func viewRenderFrame(self objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()
	if !currentWindow.ready || currentWindow.closing || currentWindow.rendering || framesInProgress > 0 {
		return
	}
	currentWindow.rendering = true
	defer func() { currentWindow.rendering = false }()

	w, h := viewSize(self)
	produceFrame(w, h)
	renderAndPresent()
}

func viewTick(self objc.ID, _ objc.SEL, _ objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	if currentWindow.gWantsFrame || frameRequested() {
		viewRenderFrame(self)
	}
}

func viewWakeAndRender(self objc.ID, _ objc.SEL) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	currentWindow.gWantsFrame = true
	viewRenderFrame(self)
}

func viewUpdateTrackingAreas(self objc.ID, cmd objc.SEL) {
	areas := self.Send(sel("trackingAreas")).Send(sel("copy"))
	n := objc.Send[uint](areas, sel("count"))
	for i := uint(0); i < n; i++ {
		self.Send(sel("removeTrackingArea:"), areas.Send(sel("objectAtIndex:"), i))
	}
	areas.Send(sel("release"))
	bounds := objc.Send[nsRect](self, sel("bounds"))
	opts := uint(nsTrackingMouseMoved | nsTrackingActiveInKeyWindow | nsTrackingInVisibleRect)
	if w := windowsByObject[self]; w != nil && w.popupNonactivating {
		// Nonactivating popups are never key; active-always tracking keeps
		// hover state available without taking focus.
		opts = uint(nsTrackingMouseMoved | nsTrackingMouseEnteredExited | nsTrackingActiveAlways | nsTrackingInVisibleRect)
	}
	ta := objc.ID(objc.GetClass("NSTrackingArea")).Send(sel("alloc"))
	ta = ta.Send(sel("initWithRect:options:owner:userInfo:"), bounds, opts, self, objc.ID(0))
	self.Send(sel("addTrackingArea:"), ta)
	ta.Send(sel("release"))
	self.SendSuper(cmd)
}

func mouseFromEvent(self, event objc.ID, action, button int) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	p := viewPoint(self, event)
	onMouse(p.X, p.Y, action, button)
	noteInput()
}

func viewMouseMoved(self objc.ID, _ objc.SEL, e objc.ID) { mouseFromEvent(self, e, mouseMove, 0) }
func viewMouseEntered(self objc.ID, _ objc.SEL, e objc.ID) {
	mouseFromEvent(self, e, mouseMove, 0)
}
func viewMouseExited(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()
	onMouse(-100000, -100000, mouseMove, 0)
	noteInput()
}
func viewMouseDragged(self objc.ID, _ objc.SEL, e objc.ID) { mouseFromEvent(self, e, mouseDrag, 0) }
func viewMouseUp(self objc.ID, _ objc.SEL, e objc.ID)      { mouseFromEvent(self, e, mouseUp, 0) }
func viewRightMouseUp(self objc.ID, _ objc.SEL, e objc.ID) { mouseFromEvent(self, e, mouseUp, 1) }

func ensureKeyWindow(self objc.ID) {
	if w := windowsByObject[self]; w != nil && w.popupNonactivating {
		// A nonactivating panel receives the click without taking the app
		// forward. Activating here would defeat the option.
		return
	}
	win := self.Send(sel("window"))
	if win != 0 && !objc.Send[bool](win, sel("isKeyWindow")) {
		nsApp().Send(sel("activateIgnoringOtherApps:"), true)
		win.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	}
}

func viewMouseDown(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	ensureKeyWindow(self)
	commitMarkedForInterruption(self)
	mouseFromEvent(self, e, mouseDown, 0)
}

func viewRightMouseDown(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	commitMarkedForInterruption(self)
	mouseFromEvent(self, e, mouseDown, 1)
}

func viewScrollWheel(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	onModifiers(objc.Send[uint](e, sel("modifierFlags")))
	dx := objc.Send[float64](e, sel("scrollingDeltaX"))
	dy := objc.Send[float64](e, sel("scrollingDeltaY"))
	if !objc.Send[bool](e, sel("hasPreciseScrollingDeltas")) {
		dx *= 10
		dy *= 10
	}
	onScroll(-dx, -dy)
	noteInput()
}

func viewFlagsChanged(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	onModifiers(objc.Send[uint](e, sel("modifierFlags")))
	noteInput()
}

func viewKeyDown(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	onModifiers(objc.Send[uint](e, sel("modifierFlags")))
	bare := eventBare(e)
	vkey := int(objc.Send[uint16](e, sel("keyCode")))
	mods := objc.Send[uint](e, sel("modifierFlags")) & nsEventModifierFlagDeviceIndependentFlagsMask
	if mods&nsEventModifierFlagCommand != 0 || mods&nsEventModifierFlagControl != 0 {
		onKeyDown(vkey, bare)
		noteInput()
		return
	}
	if currentWindow.markedStr == "" {
		onKeyDown(vkey, bare)
	}
	self.Send(sel("interpretKeyEvents:"), nsArray(e))
	noteInput()
}

func viewKeyUp(self objc.ID, _ objc.SEL, e objc.ID) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	onKeyUp(int(objc.Send[uint16](e, sel("keyCode"))), eventBare(e))
	noteInput()
}

func clampMarkedSel(selr nsRange, s string) nsRange {
	n := objc.Send[uint](nsString(s), sel("length"))
	if selr.Location == nsNotFound {
		return nsRange{Location: n, Length: 0}
	}
	start := selr.Location
	if start > n {
		start = n
	}
	end := start + selr.Length
	if end > n {
		end = n
	}
	return nsRange{Location: start, Length: end - start}
}

func notifyComposition(s string, selr nsRange) {
	clamped := clampMarkedSel(selr, s)
	onSetComposition(s, int(clamped.Location), int(clamped.Location+clamped.Length))
	noteInput()
}

func discardMarked() {
	if currentWindow.markedStr == "" {
		return
	}
	currentWindow.markedStr = ""
	currentWindow.markedSel = nsRange{Location: nsNotFound}
	onSetComposition("", 0, 0)
	noteInput()
}

func commitMarked() {
	if currentWindow.markedStr == "" {
		return
	}
	s := currentWindow.markedStr
	discardMarked()
	if s != "" {
		onCommitText(s)
		noteInput()
	}
}

func commitMarkedForInterruption(self objc.ID) {
	if currentWindow.markedStr == "" {
		return
	}
	commitMarked()
	if ctx := self.Send(sel("inputContext")); ctx != 0 {
		ctx.Send(sel("discardMarkedText"))
	}
	w, h := viewSize(self)
	produceFrame(w, h)
}

func viewInsertText(self objc.ID, _ objc.SEL, str objc.ID) {
	viewInsertTextReplacement(self, 0, str, nsRange{Location: nsNotFound})
}

func viewInsertTextReplacement(self objc.ID, _ objc.SEL, str objc.ID, _ nsRange) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	discardMarked()
	if s := nsPlainString(str); s != "" {
		onCommitText(s)
		noteInput()
	}
}

func viewSetMarkedText(self objc.ID, _ objc.SEL, str objc.ID, selected, replacement nsRange) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()

	if replacement.Location != nsNotFound && !currentWindow.loggedReplacementRange {
		fmt.Fprintf(os.Stderr, "shirei: NSTextInputClient replacementRange unsupported in v1: location=%d length=%d\n",
			replacement.Location, replacement.Length)
		currentWindow.loggedReplacementRange = true
	}
	s := nsPlainString(str)
	if s == "" {
		discardMarked()
		return
	}
	currentWindow.markedStr = s
	currentWindow.markedSel = clampMarkedSel(selected, s)
	notifyComposition(s, currentWindow.markedSel)
}

func viewUnmarkText(self objc.ID, _ objc.SEL) {
	leave := enterView(self)
	if leave == nil {
		return
	}
	defer leave()
	commitMarked()
}

func viewHasMarkedText(self objc.ID, _ objc.SEL) bool {
	leave := enterView(self)
	if leave == nil {
		return false
	}
	defer leave()
	return currentWindow.markedStr != ""
}

func viewMarkedRange(self objc.ID, _ objc.SEL) nsRange {
	leave := enterView(self)
	if leave == nil {
		return nsRange{Location: nsNotFound}
	}
	defer leave()

	if currentWindow.markedStr == "" {
		return nsRange{Location: nsNotFound}
	}
	return nsRange{Location: 0, Length: objc.Send[uint](nsString(currentWindow.markedStr), sel("length"))}
}

func viewSelectedRange(self objc.ID, _ objc.SEL) nsRange {
	leave := enterView(self)
	if leave == nil {
		return nsRange{}
	}
	defer leave()

	if currentWindow.markedStr == "" {
		return nsRange{}
	}
	return currentWindow.markedSel
}

func viewAttributedSubstring(objc.ID, objc.SEL, nsRange, *nsRange) objc.ID { return 0 }

func viewCharacterIndexForPoint(objc.ID, objc.SEL, nsPoint) uint { return nsNotFound }

func viewFirstRectForCharacterRange(self objc.ID, _ objc.SEL, rng nsRange, actual *nsRange) nsRect {
	leave := enterView(self)
	if leave == nil {
		return nsRect{}
	}
	defer leave()

	if actual != nil {
		*actual = rng
	}
	x, y, h := caretX(), caretY(), caretHeight()
	if h <= 0 {
		h = 16
	}
	viewRect := nsMakeRect(x, y-h, 1, h)
	windowRect := objc.Send[nsRect](self, sel("convertRect:toView:"), viewRect, objc.ID(0))
	if win := self.Send(sel("window")); win != 0 {
		return objc.Send[nsRect](win, sel("convertRectToScreen:"), windowRect)
	}
	return windowRect
}

func viewValidAttributesForMarkedText(objc.ID, objc.SEL) objc.ID {
	return objc.ID(objc.GetClass("NSArray")).Send(sel("array"))
}

func viewDoCommandBySelector(objc.ID, objc.SEL, objc.SEL) {}

func viewResignFirstResponder(self objc.ID, cmd objc.SEL) bool {
	leave := enterView(self)
	if leave == nil {
		return true
	}
	defer leave()

	commitMarkedForInterruption(self)
	return objc.SendSuper[bool](self, cmd)
}

func appShouldTerminateAfterLastWindowClosed(objc.ID, objc.SEL, objc.ID) bool { return !winKeepAlive }

func appShouldTerminate(objc.ID, objc.SEL, objc.ID) uint {
	exitWithCleanup()
	return nsTerminateNow
}

func appDidFinishLaunching(objc.ID, objc.SEL, objc.ID) {
	if !winQuiet {
		nsApp().Send(sel("activateIgnoringOtherApps:"), true)
		mainWindow.gWindow.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	}
}

func appWindowDidResignKey(_ objc.ID, _ objc.SEL, notification objc.ID) {
	leave := enterView(notification.Send(sel("object")))
	if leave == nil {
		return
	}
	defer leave()
	if currentWindow.gView != 0 {
		commitMarkedForInterruption(currentWindow.gView)
	}
	onWindowFocus(false)
}

func appWindowDidBecomeKey(_ objc.ID, _ objc.SEL, notification objc.ID) {
	leave := enterView(notification.Send(sel("object")))
	if leave == nil {
		return
	}
	defer leave()
	onWindowFocus(true)
}

func appWindowNeedsFrame(_ objc.ID, _ objc.SEL, notification objc.ID) {
	if w := windowsByObject[notification.Send(sel("object"))]; w != nil && !w.closed {
		w.gWantsFrame = true
		w.ui.RequestFrame()
	}
}

func setupWindow(title string, width, height int) {
	if err := ensureAppkit(); err != nil {
		panic(err)
	}
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("new"))
	defer pool.Send(sel("drain"))

	app := nsApp()
	app.Send(sel("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
	gDelegate = objc.ID(appDelegateClass).Send(sel("alloc")).Send(sel("init"))
	app.Send(sel("setDelegate:"), gDelegate)

	popupWakeTarget.Store(uintptr(gDelegate))
	if !createNativeWindow(title, width, height, winTransparent, winUndecorated, false) {
		panic("could not create the main window")
	}
	if winOverlay {
		setupOverlayWindow(currentWindow.gWindow, winW, winH)
	} else {
		currentWindow.gWindow.Send(sel("center"))
	}
	if winQuiet {
		currentWindow.gWindow.Send(sel("orderFront:"), objc.ID(0))
	} else {
		currentWindow.gWindow.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	}
}

func setupOverlayWindow(w objc.ID, width, height int) {
	if w == 0 {
		return
	}
	if screen := objc.ID(objc.GetClass("NSScreen")).Send(sel("mainScreen")); screen != 0 {
		frame := objc.Send[nsRect](screen, sel("frame"))
		topLeft := nsMakeRect(frame.Origin.X, frame.Origin.Y+frame.Size.Height-float64(height), float64(width), float64(height))
		w.Send(sel("setFrame:display:"), topLeft, true)
	}
	w.Send(sel("setLevel:"), 3)
	w.Send(sel("setCollectionBehavior:"), uint(1|16|256)) // all spaces, stationary, full-screen auxiliary
	w.Send(sel("setMovable:"), false)
	w.Send(sel("setMovableByWindowBackground:"), false)
	w.Send(sel("setHidesOnDeactivate:"), false)
	w.Send(sel("setCanHide:"), false)
}

// createNativeWindow registers a hidden window before installing its content.
func createNativeWindow(title string, width, height int, transparent, undecorated, panel bool) bool {
	frame := nsMakeRect(0, 0, float64(width), float64(height))
	style := uint(nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskResizable)
	cls := windowClass
	if panel {
		cls = panelClass
	} else {
		style |= nsWindowStyleMaskMiniaturizable
	}
	if undecorated {
		style = nsWindowStyleMaskResizable
	}
	if panel && currentWindow.popupNonactivating {
		style |= nsWindowStyleMaskNonactivatingPanel
	}
	w := currentWindow
	w.gWindow = objc.ID(cls).Send(sel("alloc")).Send(sel("initWithContentRect:styleMask:backing:defer:"),
		frame, style, uint(nsBackingStoreBuffered), false)
	if w.gWindow == 0 {
		return false
	}
	windowsByObject[w.gWindow] = w
	w.gWindow.Send(sel("setReleasedWhenClosed:"), false)
	w.gWindow.Send(sel("setTitle:"), nsString(title))
	if panel {
		w.gWindow.Send(sel("setTabbingMode:"), uint(2)) // NSWindowTabbingModeDisallowed
		w.gWindow.Send(sel("setFloatingPanel:"), true)
		w.gWindow.Send(sel("setHidesOnDeactivate:"), !w.popupFloating)
		w.gWindow.Send(sel("setBecomesKeyOnlyIfNeeded:"), false)
		if w.popupFloating {
			w.gWindow.Send(sel("setLevel:"), 3)
		}
	}
	if transparent {
		w.gWindow.Send(sel("setOpaque:"), false)
		w.gWindow.Send(sel("setBackgroundColor:"), objc.ID(objc.GetClass("NSColor")).Send(sel("clearColor")))
		w.gWindow.Send(sel("setHasShadow:"), false)
	}
	w.gView = objc.ID(viewClass).Send(sel("alloc")).Send(sel("initWithFrame:"), frame)
	if w.gView == 0 {
		delete(windowsByObject, w.gWindow)
		w.gWindow.Send(sel("release"))
		w.gWindow = 0
		return false
	}
	windowsByObject[w.gView] = w
	w.gWindow.Send(sel("setContentView:"), w.gView)
	w.gWindow.Send(sel("setDelegate:"), gDelegate)
	w.gWindow.Send(sel("setAcceptsMouseMovedEvents:"), true)
	w.gWindow.Send(sel("makeFirstResponder:"), w.gView)
	return true
}

func setAppIcon(path string) {
	if err := ensureAppkit(); err != nil || path == "" {
		return
	}
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("alloc"))
	img = img.Send(sel("initWithContentsOfFile:"), nsString(path))
	if img != 0 {
		nsApp().Send(sel("setApplicationIconImage:"), img)
	}
}

func setAppIconRGBA(pix []byte, w, h int) {
	if err := ensureAppkit(); err != nil || len(pix) == 0 || w <= 0 || h <= 0 {
		return
	}
	rep := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(sel("alloc"))
	rep = rep.Send(sel("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bitmapFormat:bytesPerRow:bitsPerPixel:"),
		uintptr(0), w, h, 8, 4, true, false,
		nsCalibratedRGBColorSpace, uint(nsBitmapFormatAlphaNonpremultiplied),
		w*4, 32)
	if rep == 0 {
		return
	}
	dst := uintptr(rep.Send(sel("bitmapData")))
	if dst == 0 {
		return
	}
	n := w * h * 4
	if n > len(pix) {
		n = len(pix)
	}
	copy(unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&dst))), n), pix[:n])
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("alloc"))
	img = img.Send(sel("initWithSize:"), nsSize{Width: float64(w), Height: float64(h)})
	img.Send(sel("addRepresentation:"), rep)
	nsApp().Send(sel("setApplicationIconImage:"), img)
}

func startWindowClock() {
	runLoop := objc.ID(objc.GetClass("NSRunLoop")).Send(sel("currentRunLoop"))
	if currentWindow.gView.Send(sel("respondsToSelector:"), sel("displayLinkWithTarget:selector:")) != 0 {
		dl := currentWindow.gView.Send(sel("displayLinkWithTarget:selector:"), currentWindow.gView, sel("tick:"))
		currentWindow.clock = dl.Send(sel("retain"))
		dl.Send(sel("addToRunLoop:forMode:"), runLoop, nsRunLoopCommonModes)
	} else {
		t := objc.ID(objc.GetClass("NSTimer")).Send(sel("timerWithTimeInterval:target:selector:userInfo:repeats:"),
			1.0/60.0, currentWindow.gView, sel("tick:"), objc.ID(0), true)
		currentWindow.clock = t.Send(sel("retain"))
		runLoop.Send(sel("addTimer:forMode:"), t, nsRunLoopCommonModes)
	}
}

func runApp() {
	startWindowClock()
	nsApp().Send(sel("run"))
}

func requestRedraw() {
	if gDelegate != 0 {
		gDelegate.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("wakeWindows"), objc.ID(0), false)
	}
}

func appWakeWindows(objc.ID, objc.SEL) {
	for object, w := range windowsByObject {
		if object == w.gView && !w.closed {
			w.gWantsFrame = true
		}
	}
}

func setWantsFrame(v bool) { currentWindow.gWantsFrame = v }

func backingScale() float64 {
	if currentWindow.gWindow == 0 {
		return 1
	}
	s := objc.Send[float64](currentWindow.gWindow, sel("backingScaleFactor"))
	if s <= 0 {
		return 1
	}
	return s
}

func nsWindowPtr() unsafe.Pointer {
	if currentWindow.gWindow == 0 {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&currentWindow.gWindow))
}

func enableZerocopy() {
	if currentWindow.gView == 0 {
		return
	}
	currentWindow.gView.Send(sel("setWantsLayer:"), true)
	currentWindow.gContentLayer = objc.ID(objc.GetClass("CALayer")).Send(sel("layer"))
	currentWindow.gContentLayer.Send(sel("setContentsGravity:"), kCAGravityResize)
	scale := backingScale()
	currentWindow.gContentLayer.Send(sel("setContentsScale:"), scale)
	currentWindow.gContentLayer.Send(sel("setFrame:"), objc.Send[nsRect](currentWindow.gView, sel("bounds")))
	currentWindow.gContentLayer.Send(sel("setAutoresizingMask:"), uint(nsLayerWidthSizable|nsLayerHeightSizable))
	currentWindow.gContentLayer.Send(sel("setOpaque:"), !currentWindow.ui.Host.TransparentBackground)
	currentWindow.gView.Send(sel("layer")).Send(sel("addSublayer:"), currentWindow.gContentLayer)
}

func setLayerContents(surface unsafe.Pointer) {
	if currentWindow.gContentLayer == 0 || surface == nil {
		return
	}
	cls := objc.ID(objc.GetClass("CATransaction"))
	cls.Send(sel("begin"))
	cls.Send(sel("setDisableActions:"), true)
	currentWindow.gContentLayer.Send(sel("setFrame:"), objc.Send[nsRect](currentWindow.gView, sel("bounds")))
	currentWindow.gContentLayer.Send(sel("setContentsScale:"), backingScale())
	currentWindow.gContentLayer.Send(sel("setContents:"), *(*objc.ID)(unsafe.Pointer(&surface)))
	cls.Send(sel("commit"))
}

func ioSurfaceCreateBuf(w, h int) unsafe.Pointer {
	if err := ensureAppkit(); err != nil {
		return nil
	}
	bpr := (w*4 + 255) &^ 255
	keys := nsArray(kIOSurfaceWidth, kIOSurfaceHeight, kIOSurfaceBytesPerElement,
		kIOSurfaceBytesPerRow, kIOSurfacePixelFormat)
	vals := nsArray(nsNumber(w), nsNumber(h), nsNumber(4), nsNumber(bpr), nsNumberU32(pixelFormatBGRA))
	dict := objc.ID(objc.GetClass("NSDictionary")).Send(sel("dictionaryWithObjects:forKeys:"), vals, keys)
	s := ioSurfaceCreate(uintptr(dict))
	if s == 0 {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&s))
}

func ioSurfaceRelease(s unsafe.Pointer) {
	if s == nil {
		return
	}
	cfRelease(uintptr(s))
}

func ioSurfaceLockBuf(s unsafe.Pointer) (base unsafe.Pointer, stride int) {
	if s == nil {
		return nil, 0
	}
	p := uintptr(s)
	ioSurfaceLock(p, 0, nil)
	stride = int(ioSurfaceGetBytesPerRow(p))
	addr := ioSurfaceGetBaseAddress(p)
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr)), stride
}

func ioSurfaceUnlockBuf(s unsafe.Pointer) {
	if s != nil {
		ioSurfaceUnlock(uintptr(s), 0, nil)
	}
}

func ioSurfaceInUseBuf(s unsafe.Pointer) bool {
	if s == nil {
		return false
	}
	return ioSurfaceIsInUse(uintptr(s)) != 0
}

func setClipboard(s string) {
	if err := ensureAppkit(); err != nil {
		return
	}
	pb := objc.ID(objc.GetClass("NSPasteboard")).Send(sel("generalPasteboard"))
	pb.Send(sel("clearContents"))
	pb.Send(sel("setString:forType:"), nsString(s), nsPasteboardTypeString)
}

func getClipboard() string {
	if err := ensureAppkit(); err != nil {
		return ""
	}
	pb := objc.ID(objc.GetClass("NSPasteboard")).Send(sel("generalPasteboard"))
	return nsPlainString(pb.Send(sel("stringForType:"), nsPasteboardTypeString))
}
