//go:build windows && (amd64 || arm64)

package win32backend

import (
	"math"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"go.hasen.dev/shirei"
)

// Exercise the native COM ABI, ownership and snapshot/action boundary. The
// separate check-uia client tests OS discovery and cross-process dispatch.
func TestAccessibilityProvider(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("Shirei provider ABI check")
	h, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow|wsVisible, 20, 20, 400, 400, 0, 0, 0, 0)
	if h == 0 {
		t.Fatal(err)
	}
	currentWindow.hwnd = syscall.Handle(h)
	currentWindow.winTitle = "Shirei provider ABI check"
	defer procDestroyWindow.Call(h)
	initAccess()
	if !currentWindow.access.ready {
		t.Fatal("UIA provider unavailable")
	}
	defer closeAccess()
	nodes := []shirei.AccessNode{
		{ID: 1, AccessAttrs: shirei.AccessAttrs{Role: "button", Name: "save", Label: "Save preferences"}, Actions: shirei.AccessPress | shirei.AccessFocus, Focusable: true, Bounds: shirei.Rect{Origin: shirei.Vec2{10, 10}, Size: shirei.Vec2{100, 30}}, Rect: shirei.Rect{Origin: shirei.Vec2{10, 10}, Size: shirei.Vec2{100, 30}}},
		{ID: 2, AccessAttrs: shirei.AccessAttrs{Role: "checkbox", Label: "Enable sound"}, Actions: shirei.AccessPress},
		{ID: 3, AccessAttrs: shirei.AccessAttrs{Role: "slider", Label: "Volume", Numeric: true, Number: 40, Min: 0, Max: 100, Step: 10}, Actions: shirei.AccessSetValue},
	}
	updateAccess(nodes, true)
	checkMSAAProvider(t, nodes)
	call := func(face uintptr, slot int, args ...uintptr) uintptr {
		t.Helper()
		table := *(**[10]uintptr)(unsafe.Pointer(face))
		all := append([]uintptr{face}, args...)
		hr, _, _ := syscall.SyscallN(table[slot], all...)
		return uintptr(uint32(hr))
	}
	check := func(hr uintptr) {
		t.Helper()
		if int32(hr) < 0 {
			t.Fatalf("COM HRESULT %#x", hr)
		}
	}
	root := uintptr(unsafe.Pointer(&currentWindow.access.live[0].fragment))
	var button uintptr
	check(call(root, 3, 3, uintptr(unsafe.Pointer(&button))))
	if button == 0 {
		t.Fatal("missing first child")
	}
	defer call(button, 2)
	var simple uintptr
	check(call(button, 0, uintptr(unsafe.Pointer(&accessIIDs[1])), uintptr(unsafe.Pointer(&simple))))
	defer call(simple, 2)
	var variant accessVariant
	check(call(simple, 5, 30005, uintptr(unsafe.Pointer(&variant))))
	if variant.VT != 8 || syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(uintptr(variant.Data))), 16)) != "Save preferences" {
		t.Fatalf("label VARIANT: %+v", variant)
	}
	accessClearVariant.Call(uintptr(unsafe.Pointer(&variant)))
	var identity uintptr
	check(call(button, 0, uintptr(unsafe.Pointer(&accessIIDs[0])), uintptr(unsafe.Pointer(&identity))))
	if identity != simple {
		t.Fatal("IUnknown identity differs across interfaces")
	}
	call(identity, 2)
	var array uintptr
	check(call(button, 4, uintptr(unsafe.Pointer(&array))))
	if array == 0 {
		t.Fatal("missing runtime ID")
	}
	accessArrayDestroy.Call(array)
	var box accessRect
	check(call(button, 5, uintptr(unsafe.Pointer(&box))))
	if box.W <= 0 || box.H <= 0 {
		t.Fatalf("bounds %+v", box)
	}
	var hit uintptr
	var fromPoint func(uintptr, float64, float64, *uintptr) uintptr
	purego.RegisterFunc(&fromPoint, accessTables[2][3])
	check(fromPoint(uintptr(unsafe.Pointer(&currentWindow.access.live[0].root)), box.X+1, box.Y+1, &hit))
	if hit != button {
		t.Fatalf("point callback ABI: got %#x want %#x", hit, button)
	}
	call(hit, 2)
	var pattern uintptr
	check(call(simple, 4, 10000, uintptr(unsafe.Pointer(&pattern))))
	if pattern == 0 {
		t.Fatal("missing Invoke")
	}
	check(call(pattern, 3))
	call(pattern, 2)
	flushAccessAction()
	if a := shirei.GetFrameInput().AccessAction; a.ID != 1 || a.Kind != shirei.AccessPress {
		t.Fatalf("invoke dispatch %+v", a)
	}
	slider := uintptr(unsafe.Pointer(&currentWindow.access.live[3].simple))
	check(call(slider, 4, 10003, uintptr(unsafe.Pointer(&pattern))))
	defer call(pattern, 2)
	table := *(**[10]uintptr)(unsafe.Pointer(pattern))
	var setValue func(uintptr, float64) uintptr
	purego.RegisterFunc(&setValue, table[3])
	check(setValue(pattern, 67.5))
	flushAccessAction()
	if a := shirei.GetFrameInput().AccessAction; a.ID != 3 || a.Value != 67.5 {
		t.Fatalf("double callback ABI: %+v", a)
	}
	if hr := uint32(setValue(pattern, math.NaN())); hr != accessInvalid {
		t.Fatalf("NaN accepted: %#x", hr)
	}
	if hr := uint32(setValue(pattern, 101)); hr != accessInvalid {
		t.Fatalf("out-of-range accepted: %#x", hr)
	}
	// Window movement updates physical coordinates without a new UI snapshot.
	procSetWindowPos.Call(h, 0, 70, 70, 0, 0, swpNozorder|swpNoactivate|1) // SWP_NOSIZE
	refreshAccess()
	var moved accessRect
	check(call(button, 5, uintptr(unsafe.Pointer(&moved))))
	if moved.X == box.X && moved.Y == box.Y {
		t.Fatal("window movement leaves stale bounds")
	}
	procShowWindow.Call(h, 0) // SW_HIDE
	refreshAccess()
	check(call(simple, 5, 30022, uintptr(unsafe.Pointer(&variant))))
	if variant.VT != 11 || variant.Data == 0 {
		t.Fatal("hidden window stays onscreen")
	}
	accessClearVariant.Call(uintptr(unsafe.Pointer(&variant)))
	nodes[0].Disabled = true
	nodes[0].Actions = 0
	nodes[0].Label = "Store preferences"
	updateAccess(nodes, true)
	if hr := call(button, 7); hr != accessDisabled {
		t.Fatalf("disabled focus accepted: %#x", hr)
	}
	// The provider identity survives metadata changes, and retained removed
	// objects remain callable even after the Go collector runs.
	var same uintptr
	check(call(root, 3, 3, uintptr(unsafe.Pointer(&same))))
	if same != button {
		t.Fatal("identity changes across frames")
	}
	call(same, 2)
	updateAccess(nodes[1:], true)
	runtime.GC()
	if hr := call(simple, 5, 30005, uintptr(unsafe.Pointer(&variant))); hr != accessUnavailable {
		t.Fatalf("stale node returns %#x", hr)
	}
	nodes[2].Protected = true
	updateAccess(nodes[1:], true)
	check(call(slider, 4, 10003, uintptr(unsafe.Pointer(&same))))
	if same != 0 {
		t.Fatal("protected range exposes pattern")
	}
	var value float64
	if hr := call(pattern, 4, uintptr(unsafe.Pointer(&value))); hr != accessNotSupported {
		t.Fatalf("retained pattern exposes protected value: %#x", hr)
	}
	t.Log("COM navigation, names, identity, bounds, floating-point hit/action ABI, queued input, disabled/removed/protected nodes")
}
