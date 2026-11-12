//go:build windows && (amd64 || arm64)

package win32backend

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"go.hasen.dev/shirei"
	"golang.org/x/sys/windows"
)

// Called from the windowed provider test with the same published snapshot.
func checkMSAAProvider(t *testing.T, nodes []shirei.AccessNode) {
	t.Helper()
	if !msaaReady {
		t.Fatal("MSAA unavailable")
	}
	check := func(hr uintptr) {
		t.Helper()
		if int32(hr) < 0 {
			t.Fatalf("MSAA HRESULT %#x", uint32(hr))
		}
	}
	root := uintptr(unsafe.Pointer(&currentWindow.access.live[0].msaa))
	child := accessInt(1)
	var button uintptr
	check(msaaCall(root, 9, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&button))))
	defer accessRelease(button)
	var unknown uintptr
	check(msaaCall(button, 0, uintptr(unsafe.Pointer(&accessIIDs[0])), uintptr(unsafe.Pointer(&unknown))))
	if unknown != uintptr(unsafe.Pointer(&currentWindow.access.live[1].simple)) {
		t.Fatal("MSAA and UIA identity differ")
	}
	accessRelease(unknown)
	// A scripting client accesses accName through the standard type information.
	null := windows.GUID{}
	name, _ := syscall.UTF16PtrFromString("accName")
	var member int32
	check(msaaCall(button, 5, uintptr(unsafe.Pointer(&null)), uintptr(unsafe.Pointer(&name)), 1, 0, uintptr(unsafe.Pointer(&member))))
	self := accessInt(0)
	params := struct {
		Args              *accessVariant
		Named             *int32
		Count, NamedCount uint32
	}{Args: &self, Count: 1}
	var result accessVariant
	check(msaaCall(button, 6, uintptr(uint32(member)), uintptr(unsafe.Pointer(&null)), 0, 2, uintptr(unsafe.Pointer(&params)), uintptr(unsafe.Pointer(&result)), 0, 0))
	if result.VT != 8 {
		t.Fatalf("IDispatch accName VARIANT %+v", result)
	}
	length, _, _ := msaaStringLength.Call(uintptr(result.Data))
	if syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(uintptr(result.Data))), int(length))) != "Save preferences" {
		t.Fatal("IDispatch label mismatch")
	}
	accessClearVariant.Call(uintptr(unsafe.Pointer(&result)))
	empty := accessVariant{}
	var ignored uintptr
	if hr := msaaCall(root, 9, uintptr(unsafe.Pointer(&empty)), uintptr(unsafe.Pointer(&ignored))); uint32(hr) != accessInvalid {
		t.Fatal("accChild accepts VT_EMPTY")
	}
	check(msaaCall(button, 25, uintptr(unsafe.Pointer(&self))))
	flushAccessAction()
	if a := shirei.GetFrameInput().AccessAction; a.ID != 1 || a.Kind != shirei.AccessPress {
		t.Fatalf("MSAA action dispatch %+v", a)
	}
	changed := append([]shirei.AccessNode(nil), nodes...)
	changed[0].Disabled = true
	changed[0].Actions = 0
	changed[2].Protected = true
	changed[2].Value = "secret"
	updateAccess(changed, true)
	if hr := msaaCall(button, 25, uintptr(unsafe.Pointer(&self))); uint32(hr) != accessDisabled {
		t.Fatalf("MSAA disabled action %#x", hr)
	}
	slider := uintptr(unsafe.Pointer(&currentWindow.access.live[3].msaa))
	var value uintptr
	hr := msaaCall(slider, 11, uintptr(unsafe.Pointer(&self)), uintptr(unsafe.Pointer(&value)))
	if hr != 1 || value != 0 {
		t.Fatal("MSAA exposes protected value")
	}
	changed[0].Hidden = true
	updateAccess(changed, true)
	runtime.GC()
	if hr := msaaCall(button, 10, uintptr(unsafe.Pointer(&self)), uintptr(unsafe.Pointer(&value))); uint32(hr) != accessUnavailable {
		t.Fatalf("MSAA stale object %#x", hr)
	}
	updateAccess(nodes, true)
	shirei.GetFrameInput().AccessAction = shirei.AccessAction{}
	t.Log("MSAA COM/IDispatch, shared identity, actions, disabled/hidden/protected nodes")
}
