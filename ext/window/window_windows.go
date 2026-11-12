//go:build windows

package window

import (
	"sync"
	"syscall"
	"unsafe"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/win32backend"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	procGetClientRect        = user32.NewProc("GetClientRect")
	procGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procSetWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	procDefSubclassProc      = comctl32.NewProc("DefSubclassProc")
	procRemoveWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")

	winMu           sync.Mutex
	windowMinimums  = map[uintptr]shirei.Vec2{}
	minSizeCallback uintptr
)

const (
	wmGetMinMaxInfo = 0x0024
	subclassID      = 0x534D // "SM" Shirei MinSize
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpNoSize       = 0x0001
	smCxScreen      = 0
	smCyScreen      = 1
)

type point struct {
	x, y int32
}

type minMaxInfo struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

type winRect struct {
	left, top, right, bottom int32
}

func windowScale(hwnd uintptr) float32 {
	if procGetDpiForWindow.Find() == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
			return float32(dpi) / 96
		}
	}
	return 1
}

func windowInsets(hwnd uintptr) (int32, int32) {
	var frame, client winRect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&frame)))
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
	return frame.right - frame.left - (client.right - client.left), frame.bottom - frame.top - (client.bottom - client.top)
}

func minSizeSubclassProc(hWnd uintptr, uMsg uint32, wParam, lParam, uIdSubclass, dwRefData uintptr) uintptr {
	if uMsg == 0x82 { // WM_NCDESTROY
		winMu.Lock()
		delete(windowMinimums, hWnd)
		winMu.Unlock()
		procRemoveWindowSubclass.Call(hWnd, minSizeCallback, subclassID)
	}
	if uMsg == wmGetMinMaxInfo {
		// Let the native procedure fill its defaults before applying content limits.
		r, _, _ := procDefSubclassProc.Call(hWnd, uintptr(uMsg), wParam, lParam)
		winMu.Lock()
		minimum := windowMinimums[hWnd]
		winMu.Unlock()
		scale := windowScale(hWnd)
		dx, dy := windowInsets(hWnd)
		mmi := (*minMaxInfo)(unsafe.Pointer(lParam))
		if minimum[0] > 0 {
			mmi.ptMinTrackSize.x = max(mmi.ptMinTrackSize.x, int32(minimum[0]*scale+.5)+dx)
		}
		if minimum[1] > 0 {
			mmi.ptMinTrackSize.y = max(mmi.ptMinTrackSize.y, int32(minimum[1]*scale+.5)+dy)
		}
		return r
	}
	r, _, _ := procDefSubclassProc.Call(hWnd, uintptr(uMsg), wParam, lParam)
	return r
}

func init() {
	minSizeCallback = syscall.NewCallback(minSizeSubclassProc)
	setPlatformMinSize = func(ctx shirei.BackendContext, minW, minH float32) {
		c, ok := ctx.(win32backend.Context)
		if !ok {
			return
		}
		h := c.HWND()
		if h == nil {
			return
		}
		hwnd := uintptr(h)

		winMu.Lock()
		_, exists := windowMinimums[hwnd]
		windowMinimums[hwnd] = shirei.Vec2{minW, minH}
		winMu.Unlock()
		if !exists {
			if r, _, _ := procSetWindowSubclass.Call(hwnd, minSizeCallback, subclassID, 0); r == 0 {
				winMu.Lock()
				delete(windowMinimums, hwnd)
				winMu.Unlock()
				return
			}
		}
		var client winRect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		scale := windowScale(hwnd)
		width, height := float32(client.right-client.left)/scale, float32(client.bottom-client.top)/scale
		if width < minW || height < minH {
			setPlatformSize(ctx, max(width, minW), max(height, minH))
		}
	}

	setPlatformSize = func(ctx shirei.BackendContext, w, h float32) {
		c, ok := ctx.(win32backend.Context)
		if !ok {
			return
		}
		handle := c.HWND()
		if handle == nil {
			return
		}
		hwnd := uintptr(handle)

		winMu.Lock()
		minimum := windowMinimums[hwnd]
		winMu.Unlock()
		scale := windowScale(hwnd)
		dx, dy := windowInsets(hwnd)
		devW := int32(max(w, minimum[0])*scale+.5) + dx
		devH := int32(max(h, minimum[1])*scale+.5) + dy
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(devW), uintptr(devH), swpNoZOrder|swpNoActivate|2)
	}

	setPlatformCenter = func(ctx shirei.BackendContext) {
		c, ok := ctx.(win32backend.Context)
		if !ok {
			return
		}
		h := c.HWND()
		if h == nil {
			return
		}
		hwnd := uintptr(h)

		var rc winRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		winW := rc.right - rc.left
		winH := rc.bottom - rc.top

		sW, _, _ := procGetSystemMetrics.Call(uintptr(smCxScreen))
		sH, _, _ := procGetSystemMetrics.Call(uintptr(smCyScreen))

		posX := (int32(sW) - winW) / 2
		posY := (int32(sH) - winH) / 2
		if posX < 0 {
			posX = 0
		}
		if posY < 0 {
			posY = 0
		}

		procSetWindowPos.Call(hwnd, 0, uintptr(posX), uintptr(posY), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	}

	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {
		c, ok := ctx.(win32backend.Context)
		if !ok {
			return
		}
		h := c.HWND()
		if h == nil {
			return
		}
		hwnd := uintptr(h)

		scale := windowScale(hwnd)
		devX := int32(float32(x)*scale + 0.5)
		devY := int32(float32(y)*scale + 0.5)

		procSetWindowPos.Call(hwnd, 0, uintptr(devX), uintptr(devY), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	}
}

func activatePlatformApp() {
	win32backend.Activate()
	getPlatformPosition = func(ctx shirei.BackendContext) (shirei.Vec2, bool) {
		c, ok := ctx.(win32backend.Context)
		if !ok {
			return shirei.Vec2{}, false
		}
		h := c.HWND()
		if h == nil {
			return shirei.Vec2{}, false
		}
		hwnd := uintptr(h)
		var rc winRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		scale := windowScale(hwnd)
		if scale <= 0 {
			scale = 1
		}
		return shirei.Vec2{float32(rc.left) / scale, float32(rc.top) / scale}, true
	}

	getPlatformHovered = func(ctx shirei.BackendContext) bool {
		c, ok := ctx.(win32backend.Context)
		if !ok {
			return false
		}
		h := c.HWND()
		if h == nil {
			return false
		}
		hwnd := uintptr(h)
		var rc winRect
		var pt point
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
			return false
		}
		return pt.x >= rc.left && pt.x < rc.right && pt.y >= rc.top && pt.y < rc.bottom
	}

}
