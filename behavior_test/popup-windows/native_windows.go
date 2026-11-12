package main

import (
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/ext/window"
	"golang.org/x/sys/windows"
)

const platformSupported = true

var user32 = syscall.NewLazyDLL("user32.dll")
var gdi32 = syscall.NewLazyDLL("gdi32.dll")
var imm32 = syscall.NewLazyDLL("imm32.dll")
var send = user32.NewProc("SendMessageW")
var getWindowRect = user32.NewProc("GetWindowRect")
var getClientRect = user32.NewProc("GetClientRect")
var getWindowLong = user32.NewProc("GetWindowLongPtrW")
var getWindow = user32.NewProc("GetWindow")
var immGetContext = imm32.NewProc("ImmGetContext")
var immReleaseContext = imm32.NewProc("ImmReleaseContext")
var immSetComposition = imm32.NewProc("ImmSetCompositionStringW")
var nativeComposition = map[uintptr]string{}

type nativeRect struct{ Left, Top, Right, Bottom int32 }

func hwnd(ctx BackendContext) uintptr {
	return uintptr(ctx.(interface{ HWND() unsafe.Pointer }).HWND())
}
func nativeClosed(ctx BackendContext) bool { return hwnd(ctx) == 0 }
func closeNative(ctx BackendContext)       { send.Call(hwnd(ctx), 0x10, 0, 0) }
func preferTabs()                          {}

func checkNative(p *panel) (bool, error) {
	h := hwnd(p.context)
	if GetHost().WindowSize != (Vec2{float32(320 + p.tag%3*20), float32(240 + p.tag%2*10)}) {
		return false, fmt.Errorf("panel %d has another window's size: %v", p.tag, GetHost().WindowSize)
	}
	style, _, _ := getWindowLong.Call(h, ^uintptr(15))   // GWL_STYLE
	exStyle, _, _ := getWindowLong.Call(h, ^uintptr(19)) // GWL_EXSTYLE
	owner, _, _ := getWindow.Call(h, 4)                  // GW_OWNER
	if style&0x80000000 == 0 || exStyle&0x80 == 0 || exStyle&0x40000 != 0 || owner != hwnd(p.ownerContext) {
		return false, fmt.Errorf("panel %d is not an owned tool window", p.tag)
	}
	if (style&0x00c00000 == 0) != p.undecorated || (exStyle&0x80000 != 0) != p.transparent || GetHost().TransparentBackground != p.transparent {
		return false, fmt.Errorf("panel %d has incorrect styles", p.tag)
	}
	var limits [5][2]int32
	send.Call(h, 0x24, 0, uintptr(unsafe.Pointer(&limits))) // WM_GETMINMAXINFO
	var frame, client nativeRect
	getWindowRect.Call(h, uintptr(unsafe.Pointer(&frame)))
	getClientRect.Call(h, uintptr(unsafe.Pointer(&client)))
	scale := GetHost().WindowScale
	expectedMin := [2]int32{
		int32(float32(280+p.tag%3*10)*scale) + frame.Right - frame.Left - client.Right,
		int32(float32(180+p.tag%2*10)*scale) + frame.Bottom - frame.Top - client.Bottom,
	}
	if limits[3] != expectedMin {
		return false, fmt.Errorf("panel %d minimum size belongs to another window: %v, want %v", p.tag, limits[3], expectedMin)
	}
	// Resolve the published accessibility root through the real WM_GETOBJECT path.
	iid, _ := windows.GUIDFromString("{618736e0-3c3d-11cf-810c-00aa00389b71}")
	var accessible uintptr
	hr, _, _ := syscall.NewLazyDLL("oleacc.dll").NewProc("AccessibleObjectFromWindow").Call(h, uintptr(uint32(0xfffffffc)), uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&accessible)))
	if int32(hr) < 0 || accessible == 0 {
		return false, fmt.Errorf("panel %d has no accessibility root: %#x", p.tag, hr)
	}
	defer nativeCOM(accessible, 2)
	var count int32
	nativeCOM(accessible, 8, uintptr(unsafe.Pointer(&count)))
	if count == 0 {
		return false, fmt.Errorf("panel %d accessibility tree is empty", p.tag)
	}
	windowIID, _ := windows.GUIDFromString("{00000114-0000-0000-c000-000000000046}")
	var window, nativeHandle uintptr
	nativeCOM(accessible, 0, uintptr(unsafe.Pointer(&windowIID)), uintptr(unsafe.Pointer(&window)))
	if window == 0 {
		return false, fmt.Errorf("panel %d accessibility window missing", p.tag)
	}
	nativeCOM(window, 3, uintptr(unsafe.Pointer(&nativeHandle)))
	nativeCOM(window, 2)
	if nativeHandle != h {
		return false, fmt.Errorf("panel %d accessibility belongs to another window", p.tag)
	}

	var r nativeRect
	getClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	width, height := int(r.Right), int(r.Bottom)
	// Read the retained client image through Windows' print-client message.
	dc, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		return false, fmt.Errorf("cannot allocate readback DC")
	}
	defer gdi32.NewProc("DeleteDC").Call(dc)
	info := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
		Colors                 [1]uint32
	}{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bitmap, _, _ := gdi32.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return false, fmt.Errorf("cannot allocate readback bitmap")
	}
	defer gdi32.NewProc("DeleteObject").Call(bitmap)
	old, _, _ := gdi32.NewProc("SelectObject").Call(dc, bitmap)
	defer gdi32.NewProc("SelectObject").Call(dc, old)
	send.Call(h, 0x318, dc, 4) // WM_PRINTCLIENT, PRF_CLIENT
	gdi32.NewProc("GdiFlush").Call()
	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	alpha := byte(255)
	if p.transparent {
		alpha = 0
	}
	if pixels[3] != alpha {
		return false, fmt.Errorf("panel %d clear alpha=%d, want %d", p.tag, pixels[3], alpha)
	}
	offset := (int(24*scale)*width + int(24*scale)) * 4
	pixel := [4]byte(pixels[offset : offset+4])
	expected := [3][4]byte{{0, 0, 255, 255}, {0, 255, 0, 255}, {255, 0, 0, 255}}
	if pixel != expected[p.tag%3] {
		return false, fmt.Errorf("panel %d marker=%v, want %v", p.tag, pixel, expected[p.tag%3])
	}
	return true, nil
}

func nativeCOM(object uintptr, slot int, args ...uintptr) uintptr {
	table := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(table + uintptr(slot)*unsafe.Sizeof(object)))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{object}, args...)...)
	return r
}

func markNative(ctx BackendContext, text string) {
	h := hwnd(ctx)
	nativeComposition[h] = text
	input, _, _ := immGetContext.Call(h)
	u := utf16.Encode([]rune(text))
	ok, _, _ := immSetComposition.Call(input, 9, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)*2), 0, 0) // SCS_SETSTR
	immReleaseContext.Call(h, input)
	if ok == 0 {
		fail("IMM32 cannot set the composition string")
		return
	}
	send.Call(h, 0x10f, 0, 8) // WM_IME_COMPOSITION, GCS_COMPSTR
}

func finishNativeComposition(ctx BackendContext) {
	h := hwnd(ctx)
	send.Call(h, 0x10e, 0, 0) // WM_IME_ENDCOMPOSITION
	// Commit via the native UTF-16 message path, independently for each HWND.
	for _, unit := range utf16.Encode([]rune(nativeComposition[h])) {
		send.Call(h, 0x102, uintptr(unit), 0)
	}
	delete(nativeComposition, h)
}

func resizeNative(ctx BackendContext, tag int) {
	window.SetMinSize(float32(280+tag%3*10), float32(180+tag%2*10))
	window.SetSize(float32(320+tag%3*20), float32(240+tag%2*10))
}

const nativeChecksDescription = "all four popup styles, independent input, composition, menus and toasts, pixels, resizing, native ownership"

func mainSizeOK() bool {
	h := hwnd(mainContext)
	var limits [5][2]int32
	send.Call(h, 0x24, 0, uintptr(unsafe.Pointer(&limits)))
	var frame, client nativeRect
	getWindowRect.Call(h, uintptr(unsafe.Pointer(&frame)))
	getClientRect.Call(h, uintptr(unsafe.Pointer(&client)))
	scale := GetHost().WindowScale
	minimum := [2]int32{int32(560*scale) + frame.Right - frame.Left - client.Right,
		int32(310*scale) + frame.Bottom - frame.Top - client.Bottom}
	return GetHost().WindowSize == (Vec2{600, 350}) && limits[3] == minimum &&
		frame.Left == int32(120*scale) && frame.Top == int32(160*scale)
}
