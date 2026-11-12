//go:build darwin && !ios && !x11darwin

package main

import (
	"fmt"
	"structs"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/ext/window"
)

const platformSupported = true

func nsWindow(ctx BackendContext) objc.ID {
	return objc.ID(uintptr(ctx.(interface{ NSWindow() unsafe.Pointer }).NSWindow()))
}

func nativeClosed(ctx BackendContext) bool { return nsWindow(ctx) == 0 }

func closeNative(ctx BackendContext) {
	nsWindow(ctx).Send(objc.RegisterName("performClose:"), objc.ID(0))
}

// Register a process-local preference; the user's defaults remain untouched.
func preferTabs() {
	s := objc.RegisterName
	str := func(text string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(s("stringWithUTF8String:"), text)
	}
	dict := objc.ID(objc.GetClass("NSDictionary")).Send(s("dictionaryWithObject:forKey:"), str("always"), str("AppleWindowTabbingMode"))
	objc.ID(objc.GetClass("NSUserDefaults")).Send(s("standardUserDefaults")).Send(s("setVolatileDomain:forName:"), dict, str("NSArgumentDomain"))
	if objc.Send[uint](objc.ID(objc.GetClass("NSWindow")), s("userTabbingPreference")) != 1 {
		fail("tab preference is not Always in this process")
	}
}

var surfaceLock func(uintptr, uint32, *uint32) int32
var surfaceUnlock func(uintptr, uint32, *uint32) int32
var surfaceBase func(uintptr) uintptr
var surfaceStride func(uintptr) uintptr

func checkNative(p *panel) (bool, error) {
	if GetHost().WindowSize != (Vec2{float32(320 + p.tag%3*20), float32(240 + p.tag%2*10)}) {
		return false, fmt.Errorf("panel %d has another window's size", p.tag)
	}
	s := objc.RegisterName
	w := nsWindow(p.context)
	minimum := objc.Send[nativeSize](w, s("contentMinSize"))
	if minimum.Width != float64(280+p.tag%3*10) || minimum.Height != float64(180+p.tag%2*10) {
		return false, fmt.Errorf("panel %d minimum size belongs to another window", p.tag)
	}
	if !objc.Send[bool](w, s("isKindOfClass:"), objc.GetClass("NSPanel")) || objc.Send[bool](w, s("canBecomeMainWindow")) || !objc.Send[bool](w, s("canBecomeKeyWindow")) {
		return false, fmt.Errorf("panel %d has application-window semantics", p.tag)
	}
	if objc.Send[uint](w, s("tabbingMode")) != 2 || w.Send(s("tabbedWindows")).Send(s("count")) > 1 {
		return false, fmt.Errorf("panel %d allows tabbing", p.tag)
	}
	if p.transparent != GetHost().TransparentBackground || objc.Send[bool](w, s("isOpaque")) == p.transparent {
		return false, fmt.Errorf("panel %d has incorrect transparency", p.tag)
	}
	if (objc.Send[uint](w, s("styleMask"))&1 == 0) != p.undecorated {
		return false, fmt.Errorf("panel %d has incorrect decorations", p.tag)
	}
	view := w.Send(s("contentView"))
	children := view.Send(s("accessibilityChildren"))
	if children.Send(s("count")) == 0 {
		return false, fmt.Errorf("panel %d has no accessibility tree", p.tag)
	}
	for i := uint(0); i < objc.Send[uint](children, s("count")); i++ {
		element := children.Send(s("objectAtIndex:"), i)
		if element.Send(s("accessibilityWindow")) != w {
			return false, fmt.Errorf("panel %d accessibility belongs to another window", p.tag)
		}
	}
	layer := view.Send(s("layer")).Send(s("sublayers")).Send(s("lastObject"))
	if objc.Send[bool](layer, s("isOpaque")) == p.transparent {
		return false, fmt.Errorf("panel %d layer opacity is incorrect", p.tag)
	}
	surface := uintptr(layer.Send(s("contents")))
	if surface == 0 {
		return false, nil
	}
	if surfaceLock == nil {
		lib, err := purego.Dlopen("/System/Library/Frameworks/IOSurface.framework/IOSurface", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return false, err
		}
		purego.RegisterLibFunc(&surfaceLock, lib, "IOSurfaceLock")
		purego.RegisterLibFunc(&surfaceUnlock, lib, "IOSurfaceUnlock")
		purego.RegisterLibFunc(&surfaceBase, lib, "IOSurfaceGetBaseAddress")
		purego.RegisterLibFunc(&surfaceStride, lib, "IOSurfaceGetBytesPerRow")
	}
	if surfaceLock(surface, 1, nil) != 0 {
		return false, fmt.Errorf("cannot read presented surface")
	}
	defer surfaceUnlock(surface, 1, nil)
	base, stride := surfaceBase(surface), surfaceStride(surface)
	corner := *(*[4]byte)(unsafe.Pointer(base))
	wantAlpha := byte(255)
	if p.transparent {
		wantAlpha = 0
	}
	if corner[3] != wantAlpha {
		return false, fmt.Errorf("panel %d clear alpha=%d, want %d", p.tag, corner[3], wantAlpha)
	}
	scale := objc.Send[float64](w, s("backingScaleFactor"))
	pixel := *(*[4]byte)(unsafe.Pointer(base + uintptr(24*scale)*stride + uintptr(24*scale)*4))
	expected := [3][4]byte{{0, 0, 255, 255}, {0, 255, 0, 255}, {255, 0, 0, 255}}
	if pixel != expected[p.tag%3] {
		return false, fmt.Errorf("panel %d GPU marker=%v, want %v", p.tag, pixel, expected[p.tag%3])
	}
	return true, nil
}

type nativeRange struct {
	_                structs.HostLayout
	Location, Length uint
}

func markNative(ctx BackendContext, text string) {
	s := objc.RegisterName
	str := objc.ID(objc.GetClass("NSString")).Send(s("stringWithUTF8String:"), text)
	nsWindow(ctx).Send(s("contentView")).Send(s("setMarkedText:selectedRange:replacementRange:"), str, nativeRange{Location: 1}, nativeRange{Location: ^uint(0) >> 1})
}

func finishNativeComposition(ctx BackendContext) {
	nsWindow(ctx).Send(objc.RegisterName("contentView")).Send(objc.RegisterName("resignFirstResponder"))
}

type nativeSize struct {
	_             structs.HostLayout
	Width, Height float64
}

func resizeNative(ctx BackendContext, tag int) {
	window.SetMinSize(float32(280+tag%3*10), float32(180+tag%2*10))
	window.SetSize(float32(320+tag%3*20), float32(240+tag%2*10))
}

const nativeChecksDescription = "all four popup styles, independent input, composition, menus and toasts, pixels, resizing, native ownership"

type nativePoint struct {
	_    structs.HostLayout
	X, Y float64
}

type nativeRect struct {
	_      structs.HostLayout
	Origin nativePoint
	Size   nativeSize
}

func mainSizeOK() bool {
	s := objc.RegisterName
	w := nsWindow(mainContext)
	minimum := objc.Send[nativeSize](w, s("contentMinSize"))
	frame := objc.Send[nativeRect](w, s("frame"))
	primary := objc.ID(objc.GetClass("NSScreen")).Send(s("screens")).Send(s("firstObject"))
	display := objc.Send[nativeRect](primary, s("frame"))
	ok := GetHost().WindowSize == (Vec2{600, 350}) && minimum.Width == 560 && minimum.Height == 310 &&
		frame.Origin.X == 120 && display.Size.Height-frame.Origin.Y-frame.Size.Height == 160
	if !ok {
		fmt.Printf("main geometry: size=%v minimum=%v frame=%v\n", GetHost().WindowSize, minimum, frame)
	}
	return ok
}
