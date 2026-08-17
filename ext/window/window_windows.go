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

	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procSetWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	procDefSubclassProc     = comctl32.NewProc("DefSubclassProc")
	procRemoveWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")

	winMu      sync.Mutex
	subclassed bool
	targetMinW float32
	targetMinH float32
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

func minSizeSubclassProc(hWnd uintptr, uMsg uint32, wParam, lParam, uIdSubclass, dwRefData uintptr) uintptr {
	if uMsg == wmGetMinMaxInfo {
		winMu.Lock()
		w := targetMinW
		h := targetMinH
		winMu.Unlock()

		if w > 0 || h > 0 {
			scale := shirei.GetHost().WindowScale
			if scale <= 0 {
				scale = 1
			}
			mmi := (*minMaxInfo)(unsafe.Pointer(lParam))
			if w > 0 {
				mmi.ptMinTrackSize.x = int32(w*scale + 0.5)
			}
			if h > 0 {
				mmi.ptMinTrackSize.y = int32(h*scale + 0.5)
			}
			return 0
		}
	}
	r, _, _ := procDefSubclassProc.Call(hWnd, uintptr(uMsg), wParam, lParam)
	return r
}

func init() {
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
		targetMinW = minW
		targetMinH = minH
		needSubclass := !subclassed
		winMu.Unlock()

		if needSubclass {
			cb := syscall.NewCallback(minSizeSubclassProc)
			r, _, _ := procSetWindowSubclass.Call(hwnd, cb, subclassID, 0)
			if r != 0 {
				winMu.Lock()
				subclassed = true
				winMu.Unlock()
			}
		}

		// Ensure current window dimensions are at least minW x minH
		scale := shirei.GetHost().WindowScale
		if scale <= 0 {
			scale = 1
		}
		devMinW := int32(minW*scale + 0.5)
		devMinH := int32(minH*scale + 0.5)

		var rc winRect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		curW := rc.right - rc.left
		curH := rc.bottom - rc.top

		if curW < devMinW || curH < devMinH {
			newW := curW
			if newW < devMinW {
				newW = devMinW
			}
			newH := curH
			if newH < devMinH {
				newH = devMinH
			}
			procSetWindowPos.Call(hwnd, 0, uintptr(rc.left), uintptr(rc.top), uintptr(newW), uintptr(newH), swpNoZOrder|swpNoActivate)
		}
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

		scale := shirei.GetHost().WindowScale
		if scale <= 0 {
			scale = 1
		}
		devX := int32(float32(x)*scale + 0.5)
		devY := int32(float32(y)*scale + 0.5)

		procSetWindowPos.Call(hwnd, 0, uintptr(devX), uintptr(devY), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	}
}
