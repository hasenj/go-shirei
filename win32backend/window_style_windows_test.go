package win32backend

import (
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
)

// Exercise real windows, both presentation paths, alpha composition, and target
// recreation. Run on Windows or Wine; no compositor or Win32 calls are mocked.
func TestWindowStyles(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	enableDPIAwareness()
	for _, useGPU := range []bool{false, true} {
		if useGPU {
			if err := gpurender.Init(); err != nil {
				t.Logf("D3D11 unavailable; software cases completed: %v", err)
				continue
			}
		}
		for _, transparent := range []bool{false, true} {
			for _, undecorated := range []bool{false, true} {
				name := fmt.Sprintf("GPU=%t/transparent=%t/undecorated=%t", useGPU, transparent, undecorated)
				t.Log(name)
				mainWindow = newWindowState(shirei.ActiveUI())
				currentWindow = mainWindow
				mainCloseRequested = false
				currentWindow.winTransparent, currentWindow.winUndecorated, winQuiet = transparent, undecorated, true
				shirei.GetHost().TransparentBackground = transparent
				currentWindow.frameFn = func() {}
				SetupWindow("Shirei window style check", 160, 120)
				currentWindow.hwnd = 0
				currentWindow.haveFrame, currentWindow.havePresented, currentWindow.timerOn = false, false, false
				if err := createWindow(); err != nil {
					t.Fatal(err)
				}
				showWindow()
				gpuOK = useGPU
				cw, ch := clientSize()
				if cw != int(160*dpiForCreation()/96) || ch != int(120*dpiForCreation()/96) {
					t.Fatalf("%s: initial client size %dx%d", name, cw, ch)
				}
				var bounds win32Rect
				procGetWindowRect.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&bounds)))
				if undecorated && (int(bounds.Right-bounds.Left) != cw || int(bounds.Bottom-bounds.Top) != ch) {
					t.Fatalf("%s: nonclient border remains: %+v, client %dx%d", name, bounds, cw, ch)
				}
				for frame, extra := range []int32{0, 80, 0, 0} {
					procSetWindowPos.Call(uintptr(currentWindow.hwnd), 0, 0, 0,
						uintptr(bounds.Right-bounds.Left+extra), uintptr(bounds.Bottom-bounds.Top+extra),
						swpNozorder|swpNoactivate|2) // SWP_NOMOVE
					cw, ch = clientSize()
					red, green := shirei.Vec4{0, 100, 50, .5}, shirei.Vec4{120, 100, 50, .5}
					surfaces := []shirei.Surface{
						{Rect: shirei.Rect{Origin: shirei.Vec2{16, 16}, Size: shirei.Vec2{48, 48}}, Color1: red, Color2: red, Corners: shirei.N4(8)},
						{Rect: shirei.Rect{Origin: shirei.Vec2{32, 16}, Size: shirei.Vec2{48, 48}}, Color1: green, Color2: green},
					}
					if frame == 3 {
						surfaces = nil
					}
					shirei.GetHost().WindowScale = 1
					currentWindow.havePresented = false
					renderAndPresent(0, cw, ch, shirei.FrameOutputData{Surfaces: surfaces})
					if !currentWindow.havePresented || useGPU && !gpuOK {
						t.Fatalf("%s frame %d: presentation failed", name, frame)
					}
					if useGPU {
						// Read the GDI-compatible target through the same DC used for
						// presentation, including its alpha channel.
						src, err := gpurender.PresentDC(unsafe.Pointer(currentWindow.hwnd))
						if err != nil || !ensureDIB(cw, ch) {
							t.Fatalf("readback setup: %v", err)
						}
						procBitBlt.Call(uintptr(currentWindow.memDC), 0, 0, uintptr(cw), uintptr(ch), src, 0, 0, srccopy)
						procGdiFlush.Call()
						gpurender.ReleasePresentDC(unsafe.Pointer(currentWindow.hwnd))
					}
					for _, sample := range []struct {
						x, y int
						bgra [4]byte
					}{
						{0, 0, [4]byte{}},
						{16, 16, [4]byte{}}, // rounded corner
						{24, 32, [4]byte{0, 0, 128, 128}},
						{40, 32, [4]byte{0, 128, 64, 192}},
					} {
						want := sample.bgra
						if frame == 3 {
							want = [4]byte{}
						}
						if !transparent {
							for c := range 3 {
								want[c] += 255 - want[3]
							}
							want[3] = 255
						}
						offset := (sample.y*cw + sample.x) * 4
						got := [4]byte(currentWindow.dibBuf[offset : offset+4])
						for c := range 4 {
							if delta := int(got[c]) - int(want[c]); delta < -2 || delta > 2 {
								t.Fatalf("%s frame %d at (%d,%d): %v want %v", name, frame, sample.x, sample.y, got, want)
							}
						}
						if transparent && !undecorated {
							procGdiFlush.Call()
							var box win32Rect
							var origin win32Point
							procGetWindowRect.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&box)))
							procClientToScreen.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&origin)))
							x, y := sample.x+int(origin.X-box.Left), sample.y+int(origin.Y-box.Top)
							i := (y*currentWindow.layeredW + x) * 4
							if actual := [4]byte(currentWindow.layeredPixels[i : i+4]); actual != got {
								t.Fatalf("%s: client alpha/placement changed in full-window image: %v want %v", name, actual, got)
							}
							if currentWindow.layeredPixels[3] != 255 {
								t.Fatal("native frame is not opaque")
							}
						}
					}
				}
				procDestroyWindow.Call(uintptr(currentWindow.hwnd))
				class, _ := syscall.UTF16PtrFromString("shireiWindowClass")
				user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(class)), uintptr(hinstance))
				currentWindow.hwnd = 0
			}
		}
	}
	currentWindow.winTransparent, currentWindow.winUndecorated, winQuiet = false, false, false
	shirei.GetHost().TransparentBackground = false
}
