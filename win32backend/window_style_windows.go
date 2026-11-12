package win32backend

import (
	"fmt"
	"os"
	"unsafe"
)

// presentDC accepts premultiplied BGRA from either renderer. Layered windows
// retain the supplied alpha; ordinary windows copy only the visible RGB.
func presentDC(hdc, src uintptr, cw, ch int) bool {
	if !currentWindow.winTransparent {
		if hdc == 0 {
			hdc, _, _ = procGetDC.Call(uintptr(currentWindow.hwnd))
			if hdc == 0 {
				return false
			}
			defer procReleaseDC.Call(uintptr(currentWindow.hwnd), hdc)
		}
		r, _, _ := procBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch), src, 0, 0, srccopy)
		return r != 0
	}

	currentWindow.presentingLayered = true
	defer func() { currentWindow.presentingLayered = false }()
	size := win32Point{int32(cw), int32(ch)} // SIZE has the same layout as POINT
	origin := win32Point{}
	if !currentWindow.winUndecorated {
		// UpdateLayeredWindow takes the whole window, including the native frame.
		// Ask Windows to draw that frame into a DIB, then insert our client pixels.
		var bounds win32Rect
		procGetWindowRect.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&bounds)))
		client := win32Point{}
		procClientToScreen.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&client)))
		x, y := int(client.X-bounds.Left), int(client.Y-bounds.Top)
		w, h := int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top)
		if x < 0 || y < 0 || x+cw > w || y+ch > h || !ensureLayeredDIB(w, h) {
			return false
		}
		clear(currentWindow.layeredPixels)
		procDefWindowProcW.Call(uintptr(currentWindow.hwnd), wmPrint, currentWindow.layeredDC, prfNonclient)
		procGdiFlush.Call()
		// GDI draws RGB without defining alpha. Keep the frame opaque, leaving
		// the client's alpha untouched when it is copied below.
		for py := 0; py < h; py++ {
			for px := 0; px < w; px++ {
				if py < y || py >= y+ch || px < x || px >= x+cw {
					currentWindow.layeredPixels[(py*w+px)*4+3] = 255
				}
			}
		}
		r, _, _ := procBitBlt.Call(currentWindow.layeredDC, uintptr(x), uintptr(y), uintptr(cw), uintptr(ch), src, 0, 0, srccopy)
		if r == 0 {
			return false
		}
		src = currentWindow.layeredDC
		size = win32Point{int32(w), int32(h)}
	}
	// BLENDFUNCTION: AC_SRC_OVER, no flags, full constant alpha, AC_SRC_ALPHA.
	blend := [4]byte{0, 0, 255, 1}
	r, _, err := procUpdateLayeredWindow.Call(uintptr(currentWindow.hwnd), 0, 0,
		uintptr(unsafe.Pointer(&size)), src, uintptr(unsafe.Pointer(&origin)),
		0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "win32backend: UpdateLayeredWindow: %v\n", err)
	}
	return r != 0
}

func ensureLayeredDIB(w, h int) bool {
	if currentWindow.layeredBM != 0 && currentWindow.layeredW == w && currentWindow.layeredH == h {
		return true
	}
	releaseLayeredDIB()
	if w <= 0 || h <= 0 {
		return false
	}
	currentWindow.layeredDC, _, _ = procCreateCompatibleDC.Call(0)
	if currentWindow.layeredDC == 0 {
		return false
	}
	bmi := bitmapInfo{Header: bitmapInfoHeader{
		Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32, Compression: biRGB,
	}}
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	var bits unsafe.Pointer
	currentWindow.layeredBM, _, _ = procCreateDIBSection.Call(currentWindow.layeredDC, uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if currentWindow.layeredBM == 0 || bits == nil {
		releaseLayeredDIB()
		return false
	}
	currentWindow.layeredOldBM, _, _ = procSelectObject.Call(currentWindow.layeredDC, currentWindow.layeredBM)
	currentWindow.layeredPixels = unsafe.Slice((*byte)(bits), w*h*4)
	currentWindow.layeredW, currentWindow.layeredH = w, h
	return true
}

func releaseLayeredDIB() {
	if currentWindow.layeredBM != 0 {
		procSelectObject.Call(currentWindow.layeredDC, currentWindow.layeredOldBM)
		procDeleteObject.Call(currentWindow.layeredBM)
	}
	if currentWindow.layeredDC != 0 {
		procDeleteDC.Call(currentWindow.layeredDC)
	}
	currentWindow.layeredDC, currentWindow.layeredBM, currentWindow.layeredOldBM = 0, 0, 0
	currentWindow.layeredPixels = nil
	currentWindow.layeredW, currentWindow.layeredH = 0, 0
}

func undecoratedHitTest(hWnd, lparam uintptr) uintptr {
	const (
		client      = 1
		left        = 10
		right       = 11
		top         = 12
		topLeft     = 13
		topRight    = 14
		bottom      = 15
		bottomLeft  = 16
		bottomRight = 17
	)
	if zoomed, _, _ := procIsZoomed.Call(hWnd); zoomed != 0 {
		return client
	}
	var bounds win32Rect
	procGetWindowRect.Call(hWnd, uintptr(unsafe.Pointer(&bounds)))
	x, y := int32(int16(lparam)), int32(int16(lparam>>16))
	border := int32(6 * dpiScale())
	l, r := x < bounds.Left+border, x >= bounds.Right-border
	t, b := y < bounds.Top+border, y >= bounds.Bottom-border
	switch {
	case t && l:
		return topLeft
	case t && r:
		return topRight
	case b && l:
		return bottomLeft
	case b && r:
		return bottomRight
	case l:
		return left
	case r:
		return right
	case t:
		return top
	case b:
		return bottom
	default:
		return client
	}
}
