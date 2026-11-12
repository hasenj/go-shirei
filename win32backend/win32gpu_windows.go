package win32backend

import (
	"fmt"
	"os"
	"unsafe"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
)

var gpuOK bool

func tryInitGPU() {
	if g.EnvFalsy("SHIREI_GPU") {
		fmt.Fprintln(os.Stderr, "gpurender: off (SHIREI_GPU=0)")
		return
	}
	if remote, _, _ := procGetSystemMetrics.Call(smRemoteSession); remote != 0 {
		fmt.Fprintln(os.Stderr, "gpurender: RDP session, software")
		return
	}
	if err := gpurender.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: fallback to software: %v\n", err)
		return
	}
	gpuOK = true
	fmt.Fprintf(os.Stderr, "gpurender: D3D11 %s\n", gpurender.DeviceName())
}

func presentGPU(hdc uintptr, cw, ch int) bool {
	src, err := gpurender.PresentDC(unsafe.Pointer(currentWindow.hwnd))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: GetDC: %v; software from now\n", err)
		disableGPU()
		return false
	}
	defer gpurender.ReleasePresentDC(unsafe.Pointer(currentWindow.hwnd))
	if !presentDC(hdc, src, cw, ch) {
		fmt.Fprintln(os.Stderr, "gpurender: presentation failed; software from now")
		disableGPU()
		return false
	}
	return true
}

func renderGPU(hdc uintptr, cw, ch int, scale float32, out shirei.FrameOutputData) bool {
	err := gpurender.Render(unsafe.Pointer(currentWindow.hwnd), cw, ch, scale, out.Surfaces, out.GlyphRuns, out.GlyphsAdded, out.GlyphsEvicted, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpurender: %v; software from now\n", err)
		disableGPU()
		return false
	}
	return presentGPU(hdc, cw, ch)
}

func releaseGPU() {
	gpurender.Release()
	gpuOK = false
}

func disableGPU() {
	releaseGPU()
	for h, w := range windowsByHandle {
		w.haveFrame, w.havePresented = false, false
		w.dirty = true
		procInvalidateRect.Call(h, 0, 0)
	}
}

func blitLastGPU(hdc uintptr, cw, ch int) bool {
	if !gpuOK {
		return false
	}
	return presentGPU(hdc, cw, ch)
}

// WM_PRINTCLIENT copies the retained client image into a caller-owned DC.
func copyPresentedFrame(hdc uintptr) {
	if !currentWindow.havePresented {
		return
	}
	src := uintptr(currentWindow.memDC)
	if gpuOK {
		var err error
		src, err = gpurender.PresentDC(unsafe.Pointer(currentWindow.hwnd))
		if err != nil {
			return
		}
		defer gpurender.ReleasePresentDC(unsafe.Pointer(currentWindow.hwnd))
	}
	if src != 0 {
		procBitBlt.Call(hdc, 0, 0, uintptr(currentWindow.presentW), uintptr(currentWindow.presentH), src, 0, 0, srccopy)
	}
}
