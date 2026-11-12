//go:build windows && (amd64 || arm64)

package win32backend

import (
	"fmt"
	"math"
	"os"
	"slices"
	"sync"
	"syscall"
	"unsafe"

	"go.hasen.dev/shirei"
	"golang.org/x/sys/windows"
)

// COM callbacks read an owned snapshot, never Shirei's live frame state.
// Each live node owns one reference. Retired nodes stay anchored until their
// last COM reference is released; their methods return UIA_E_ELEMENTNOTAVAILABLE.
type accessInterface struct {
	vtable *uintptr
	owner  *accessProvider
}
type accessProvider struct {
	access                                        *windowAccess
	simple, fragment, root, invoke, toggle, value accessInterface
	msaa, oleWindow                               accessInterface
	msaaID                                        int32
	refs                                          uint32
	live                                          bool
	node                                          shirei.AccessNode
	parent                                        uint64
	children                                      []uint64
	bounds, clipped                               accessRect
}
type accessRect struct{ X, Y, W, H float64 }

// Windows VARIANT is 24 bytes on both 64-bit Windows architectures.
type accessVariant struct {
	VT          uint16
	reserved    [3]uint16
	Data, Extra uint64
}

const (
	accessWake         = 0x8000 + 73
	accessUnavailable  = 0x80040201
	accessDisabled     = 0x80040200
	accessInvalid      = 0x80070057
	accessNoInterface  = 0x80004002
	accessNotSupported = 0x80040204
	accessOutOfMemory  = 0x8007000e
)

type windowAccess struct {
	scale                       float64
	live                        map[uint64]*accessProvider
	pending                     []shirei.AccessAction
	order                       []uint64
	hwnd                        uintptr
	active, visible, ready, com bool
	focus                       uint64
	objects                     map[int32]*accessProvider
}

func newWindowAccess() *windowAccess {
	return &windowAccess{live: make(map[uint64]*accessProvider), objects: make(map[int32]*accessProvider)}
}

var (
	accessMu            sync.Mutex
	accessRetained      = map[*accessProvider]bool{}
	accessUIAReady      bool
	accessTables        [6][10]uintptr
	accessPointCallback = syscall.NewCallback(accessFromPoint)
	accessValueCallback = syscall.NewCallback(accessSetValue)

	accessUIA            = windows.NewLazySystemDLL("uiautomationcore.dll")
	accessOLE            = windows.NewLazySystemDLL("oleaut32.dll")
	accessCOMDLL         = windows.NewLazySystemDLL("ole32.dll")
	accessReturn         = accessUIA.NewProc("UiaReturnRawElementProvider")
	accessHost           = accessUIA.NewProc("UiaHostProviderFromHwnd")
	accessEvent          = accessUIA.NewProc("UiaRaiseAutomationEvent")
	accessPropertyEvent  = accessUIA.NewProc("UiaRaiseAutomationPropertyChangedEvent")
	accessStructureEvent = accessUIA.NewProc("UiaRaiseStructureChangedEvent")
	accessDisconnect     = accessUIA.NewProc("UiaDisconnectProvider")
	accessBSTR           = accessOLE.NewProc("SysAllocStringLen")
	accessClearVariant   = accessOLE.NewProc("VariantClear")
	accessArray          = accessOLE.NewProc("SafeArrayCreateVector")
	accessArrayPut       = accessOLE.NewProc("SafeArrayPutElement")
	accessArrayDestroy   = accessOLE.NewProc("SafeArrayDestroy")
	accessCoInitialize   = accessCOMDLL.NewProc("CoInitializeEx")
	accessCoUninitialize = accessCOMDLL.NewProc("CoUninitialize")
	accessClientToScreen = user32.NewProc("ClientToScreen")
	accessGetFocus       = user32.NewProc("GetFocus")
	accessIsIconic       = user32.NewProc("IsIconic")
	accessIsVisible      = user32.NewProc("IsWindowVisible")
	accessIIDs           = [...]windows.GUID{
		accessGUID("00000000-0000-0000-c000-000000000046"), // IUnknown
		accessGUID("d6dd68d1-86fd-4332-8666-9abedea2d24c"), // IRawElementProviderSimple
		accessGUID("f7063da8-8359-439c-9297-bbc5299a7d87"), // IRawElementProviderFragment
		accessGUID("620ce2a5-ab8f-40a9-86cb-de3c75599b58"), // IRawElementProviderFragmentRoot
		accessGUID("54fcb24b-e18e-47a2-b4d3-eccbe77599a2"), // IInvokeProvider
		accessGUID("56d00bd0-c4f4-433c-a836-1a52a57e0892"), // IToggleProvider
		accessGUID("36dc7aef-33e6-4691-afe1-2be7274b3d33"), // IRangeValueProvider
	}
)

func accessGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString("{" + s + "}")
	if err != nil {
		panic(err)
	}
	return g
}

// These entry points adapt floating-point Windows arguments to the integer
// arguments supported by syscall.NewCallback. The assembly tail-calls the
// runtime's callback trampoline; it does not call Go using the native ABI.
func accessPointThunkAddr() uintptr
func accessValueThunkAddr() uintptr

var accessTablesOnce sync.Once

func initAccessTables() {
	accessTablesOnce.Do(func() {
		accessUIAReady = accessReturn.Find() == nil && accessHost.Find() == nil
		initMSAA()
		q, a, r := syscall.NewCallback(accessQuery), syscall.NewCallback(accessAddRef), syscall.NewCallback(accessRelease)
		for i := range accessTables {
			accessTables[i][0], accessTables[i][1], accessTables[i][2] = q, a, r
		}
		copy(accessTables[0][3:], []uintptr{syscall.NewCallback(accessOptions), syscall.NewCallback(accessPattern), syscall.NewCallback(accessProperty), syscall.NewCallback(accessHostProvider)})
		copy(accessTables[1][3:], []uintptr{syscall.NewCallback(accessNavigate), syscall.NewCallback(accessRuntimeID), syscall.NewCallback(accessBounds), syscall.NewCallback(accessEmptyArray), syscall.NewCallback(accessSetFocus), syscall.NewCallback(accessFragmentRoot)})
		copy(accessTables[2][3:], []uintptr{accessPointThunkAddr(), syscall.NewCallback(accessGetFocused)})
		accessTables[3][3] = syscall.NewCallback(accessPress)
		copy(accessTables[4][3:], []uintptr{syscall.NewCallback(accessPress), syscall.NewCallback(accessToggleState)})
		copy(accessTables[5][3:], []uintptr{accessValueThunkAddr(), syscall.NewCallback(accessNumber), syscall.NewCallback(accessReadOnly), syscall.NewCallback(accessMaximum), syscall.NewCallback(accessMinimum), syscall.NewCallback(accessLargeChange), syscall.NewCallback(accessSmallChange)})

	})
}

func initAccess() {
	access := currentWindow.access
	if accessReturn.Find() != nil && msaaReturn.Find() != nil {
		return
	}
	hr, _, _ := accessCoInitialize.Call(0, 2) // COINIT_APARTMENTTHREADED, on the UI thread.
	if int32(hr) < 0 {
		return
	}
	access.com = true
	initAccessTables()
	accessMu.Lock()
	access.hwnd = uintptr(currentWindow.hwnd)
	newAccessProvider(access, shirei.AccessNode{})
	access.ready = true
	accessMu.Unlock()
	noteInput()
}

// Caller holds accessMu.
func newAccessProvider(access *windowAccess, n shirei.AccessNode) *accessProvider {
	p := &accessProvider{access: access, refs: 1, live: true, node: n}
	for i, face := range []*accessInterface{&p.simple, &p.fragment, &p.root, &p.invoke, &p.toggle, &p.value} {
		face.vtable = &accessTables[i][0]
		face.owner = p
	}
	p.msaa = accessInterface{vtable: &msaaTable[0], owner: p}
	p.oleWindow = accessInterface{vtable: &msaaWindowTable[0], owner: p}
	if n.ID == 0 {
		p.msaaID = -4
	} else if msaaNextID < math.MaxInt32 {
		msaaNextID++
		p.msaaID = msaaNextID
	}
	if p.msaaID != 0 {
		access.objects[p.msaaID] = p
	}
	access.live[n.ID] = p
	accessRetained[p] = true
	return p
}
func accessSelf(self uintptr) *accessProvider { return (*accessInterface)(unsafe.Pointer(self)).owner }
func accessPut(out uintptr, face *accessInterface) {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if face != nil {
		face.owner.refs++
		*(*uintptr)(unsafe.Pointer(out)) = uintptr(unsafe.Pointer(face))
	}
}
func accessDrop(p *accessProvider) uint32 {
	p.refs--
	if p.refs == 0 {
		delete(accessRetained, p)
	}
	return p.refs
}
func accessAddRef(self uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	p := accessSelf(self)
	p.refs++
	return uintptr(p.refs)
}
func accessRelease(self uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	return uintptr(accessDrop(accessSelf(self)))
}
func accessQuery(self, iid, out uintptr) uintptr {
	if out == 0 || iid == 0 {
		return accessInvalid
	}
	accessMu.Lock()
	defer accessMu.Unlock()
	p := accessSelf(self)
	var face *accessInterface
	switch *(*windows.GUID)(unsafe.Pointer(iid)) {
	case msaaIID, msaaDispatchIID:
		if msaaReady {
			face = &p.msaa
		}
	case msaaWindowIID:
		if msaaReady {
			face = &p.oleWindow
		}
	case accessIIDs[0], accessIIDs[1]:
		face = &p.simple
	case accessIIDs[2]:
		face = &p.fragment
	case accessIIDs[3]:
		if p.node.ID == 0 {
			face = &p.root
		}
	// Interface identity stays fixed for the lifetime of the object. Pattern
	// availability and actions are checked against its current snapshot.
	case accessIIDs[4]:
		face = &p.invoke
	case accessIIDs[5]:
		face = &p.toggle
	case accessIIDs[6]:
		face = &p.value
	}
	if !accessUIAReady && *(*windows.GUID)(unsafe.Pointer(iid)) != accessIIDs[0] && face != &p.msaa && face != &p.oleWindow {
		face = nil
	}
	accessPut(out, face)
	if face == nil {
		return accessNoInterface
	}
	return 0
}
func accessOptions(self, out uintptr) uintptr {
	options := int32(2)
	if msaaReady {
		options |= 0x80
	}
	*(*int32)(unsafe.Pointer(out)) = options
	return 0
}
func accessPatternFace(p *accessProvider, id int32) *accessInterface {
	n := p.node
	switch id {
	case 10000:
		if n.Role == "button" || n.Role == "menuitem" {
			return &p.invoke
		}
	case 10015:
		if n.Role == "checkbox" || n.Role == "switch" {
			return &p.toggle
		}
	case 10003:
		if n.Numeric && !n.Protected {
			return &p.value
		}
	}
	return nil
}
func accessPattern(self, id, out uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	accessPut(out, accessPatternFace(p, int32(id)))
	return 0
}
func accessHostProvider(self, out uintptr) uintptr {
	access := accessSelf(self).access
	*(*uintptr)(unsafe.Pointer(out)) = 0
	accessMu.Lock()
	p := accessSelf(self)
	live, root, h := p.live, p.node.ID == 0, access.hwnd
	accessMu.Unlock()
	if !live {
		return accessUnavailable
	}
	if !root {
		return 0
	}
	hr, _, _ := accessHost.Call(h, out)
	return hr
}
func accessControlType(n shirei.AccessNode) int32 {
	switch n.Role {
	case "button":
		return 50000
	case "checkbox", "switch":
		return 50002
	case "text":
		if n.Editable {
			return 50004
		}
		return 50020
	case "statictext":
		return 50020
	case "slider":
		return 50015
	case "progressbar":
		return 50012
	case "radio":
		return 50013
	case "menu":
		return 50009
	case "menuitem":
		return 50011
	}
	return 50026 // Group
}
func accessInt(v int32) accessVariant { return accessVariant{VT: 3, Data: uint64(uint32(v))} }
func accessBool(v bool) accessVariant {
	if v {
		return accessVariant{VT: 11, Data: 0xffff}
	}
	return accessVariant{VT: 11}
}
func accessDouble(v float64) accessVariant { return accessVariant{VT: 5, Data: math.Float64bits(v)} }
func accessString(s string) accessVariant {
	// UTF16FromString rejects embedded NULs. Replace them so a bad label cannot
	// panic or truncate unrelated accessibility data.
	u := []uint16{}
	for _, r := range s {
		if r == 0 {
			r = '\ufffd'
		}
		if r <= 0xffff {
			u = append(u, uint16(r))
		} else {
			r -= 0x10000
			u = append(u, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&1023)))
		}
	}
	u = append(u, 0)
	p, _, _ := accessBSTR.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
	return accessVariant{VT: 8, Data: uint64(p)}
}
func accessPropertyValue(p *accessProvider, id int32, active, visible bool) accessVariant {
	n := p.node
	switch id {
	case 30001:
		arr, _, _ := accessArray.Call(5, 0, 4)
		if arr == 0 {
			return accessVariant{}
		}
		for i, v := range []float64{p.bounds.X, p.bounds.Y, p.bounds.W, p.bounds.H} {
			index := int32(i)
			hr, _, _ := accessArrayPut.Call(arr, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&v)))
			if int32(hr) < 0 {
				accessArrayDestroy.Call(arr)
				return accessVariant{}
			}
		}
		return accessVariant{VT: 0x2005, Data: uint64(arr)}
	case 30003:
		if n.ID == 0 {
			return accessInt(50033)
		}
		return accessInt(accessControlType(n))
	case 30005:
		return accessString(n.Label)
	case 30008:
		return accessBool(active && n.KeyboardFocused)
	case 30009:
		return accessBool(n.Focusable && !n.Disabled)
	case 30010:
		return accessBool(!n.Disabled)
	case 30011:
		return accessString(fmt.Sprintf("%s#%d", n.Name, n.ID))
	case 30012:
		return accessString("shireiWindowClass")
	case 30013:
		return accessString(n.Description)
	case 30016, 30017:
		return accessBool(true)
	case 30019:
		return accessBool(n.Protected)
	case 30022:
		return accessBool(!visible || p.clipped.W <= 0 || p.clipped.H <= 0)
	case 30023:
		if n.Role == "slider" {
			return accessInt(1)
		}
		return accessInt(0)
	case 30024:
		return accessString("Shirei")
	case 30031:
		return accessBool(accessPatternFace(p, 10000) != nil)
	case 30033:
		return accessBool(accessPatternFace(p, 10003) != nil)
	case 30041:
		return accessBool(accessPatternFace(p, 10015) != nil)
	case 30047:
		if n.Numeric && !n.Protected {
			return accessDouble(float64(n.Number))
		}
	case 30048:
		if n.Numeric {
			return accessBool(n.Disabled || n.Actions&shirei.AccessSetValue == 0)
		}
	case 30049:
		if n.Numeric && !n.Protected {
			return accessDouble(float64(n.Min))
		}
	case 30050:
		if n.Numeric && !n.Protected {
			return accessDouble(float64(n.Max))
		}
	case 30051, 30052:
		if n.Numeric && !n.Protected {
			return accessDouble(float64(n.Step))
		}
	case 30086:
		if accessPatternFace(p, 10015) != nil {
			if n.Checked {
				return accessInt(1)
			}
			return accessInt(0)
		}
	}
	return accessVariant{} // VT_EMPTY lets the host supply unsupported properties.
}
func accessProperty(self, id, out uintptr) uintptr {
	access := accessSelf(self).access
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	accessMu.Lock()
	p := accessSelf(self)
	copy := *p
	active, visible := access.active, access.visible
	accessMu.Unlock()
	if !copy.live {
		return accessUnavailable
	}
	*(*accessVariant)(unsafe.Pointer(out)) = accessPropertyValue(&copy, int32(id), active, visible)
	return 0
}
func accessNavigate(self, direction, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	var target *accessProvider
	switch direction {
	case 0:
		if p.node.ID != 0 {
			target = access.live[p.parent]
		}
	case 1, 2:
		if p.node.ID != 0 {
			siblings := access.live[p.parent].children
			i := slices.Index(siblings, p.node.ID)
			if direction == 1 {
				i++
			} else {
				i--
			}
			if i >= 0 && i < len(siblings) {
				target = access.live[siblings[i]]
			}
		}
	case 3:
		if len(p.children) > 0 {
			target = access.live[p.children[0]]
		}
	case 4:
		if len(p.children) > 0 {
			target = access.live[p.children[len(p.children)-1]]
		}
	default:
		return accessInvalid
	}
	if target != nil {
		accessPut(out, &target.fragment)
	}
	return 0
}
func accessRuntimeID(self, out uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	accessMu.Lock()
	p := accessSelf(self)
	id, live := p.node.ID, p.live
	accessMu.Unlock()
	if !live {
		return accessUnavailable
	}
	if id == 0 {
		return 0
	}
	arr, _, _ := accessArray.Call(3, 0, 3) // VT_I4, UiaAppendRuntimeId + 64-bit serial.
	if arr == 0 {
		return accessOutOfMemory
	}
	for i, v := range []int32{3, int32(id), int32(id >> 32)} {
		index := int32(i)
		hr, _, _ := accessArrayPut.Call(arr, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&v)))
		if int32(hr) < 0 {
			accessArrayDestroy.Call(arr)
			return hr
		}
	}
	*(*uintptr)(unsafe.Pointer(out)) = arr
	return 0
}
func accessBounds(self, out uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*accessRect)(unsafe.Pointer(out)) = accessRect{}
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	*(*accessRect)(unsafe.Pointer(out)) = p.bounds
	return 0
}
func accessEmptyArray(self, out uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	accessMu.Lock()
	defer accessMu.Unlock()
	if !accessSelf(self).live {
		return accessUnavailable
	}
	return 0
}
func accessFragmentRoot(self, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if !accessSelf(self).live {
		return accessUnavailable
	}
	accessPut(out, &access.live[0].root)
	return 0
}
func accessFromPoint(self, xBits, yBits, out uintptr) uintptr {
	access := accessSelf(self).access
	x, y := math.Float64frombits(uint64(xBits)), math.Float64frombits(uint64(yBits))
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if !accessSelf(self).live {
		return accessUnavailable
	}
	var hit *accessProvider
	order := -1
	for _, id := range access.order {
		p := access.live[id]
		r := p.clipped
		if access.visible && x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H && p.node.PaintOrder >= order {
			hit = p
			order = p.node.PaintOrder
		}
	}
	if hit == nil {
		p := access.live[0]
		r := p.bounds
		if access.visible && x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H {
			hit = p
		}
	}
	if hit != nil {
		accessPut(out, &hit.fragment)
	}
	return 0
}
func accessGetFocused(self, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if !accessSelf(self).live {
		return accessUnavailable
	}
	if access.active {
		if p := access.live[access.focus]; p != nil {
			accessPut(out, &p.fragment)
		}
	}
	return 0
}
func accessQueue(self uintptr, kind shirei.AccessActionKind, value float64) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	p := accessSelf(self)
	n := p.node
	if !p.live {
		accessMu.Unlock()
		return accessUnavailable
	}
	if n.Disabled {
		accessMu.Unlock()
		return accessDisabled
	}
	if n.Actions&kind == 0 && !(n.ID == 0 && kind == shirei.AccessFocus) {
		accessMu.Unlock()
		return accessNotSupported
	}
	if kind == shirei.AccessSetValue && (n.Protected || math.IsNaN(value) || math.IsInf(value, 0) || value < float64(n.Min) || value > float64(n.Max)) {
		accessMu.Unlock()
		return accessInvalid
	}
	if len(access.pending) >= 256 {
		accessMu.Unlock()
		return 0x80004005
	}
	access.pending = append(access.pending, shirei.AccessAction{ID: n.ID, Kind: kind, Value: float32(value)})
	h := access.hwnd
	accessMu.Unlock()
	procPostMessageW.Call(h, accessWake, 0, 0)
	return 0
}
func accessPress(self uintptr) uintptr    { return accessQueue(self, shirei.AccessPress, 0) }
func accessSetFocus(self uintptr) uintptr { return accessQueue(self, shirei.AccessFocus, 0) }
func accessSetValue(self, bits uintptr) uintptr {
	return accessQueue(self, shirei.AccessSetValue, math.Float64frombits(uint64(bits)))
}
func accessToggleState(self, out uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*int32)(unsafe.Pointer(out)) = 0
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	if p.node.Checked {
		*(*int32)(unsafe.Pointer(out)) = 1
	}
	return 0
}
func accessRange(self, out uintptr, field int) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*float64)(unsafe.Pointer(out)) = 0
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	n := p.node
	if !n.Numeric || n.Protected {
		return accessNotSupported
	}
	v := n.Number
	switch field {
	case 1:
		v = n.Max
	case 2:
		v = n.Min
	case 3:
		v = n.Step
	}
	*(*float64)(unsafe.Pointer(out)) = float64(v)
	return 0
}
func accessNumber(s, o uintptr) uintptr      { return accessRange(s, o, 0) }
func accessMaximum(s, o uintptr) uintptr     { return accessRange(s, o, 1) }
func accessMinimum(s, o uintptr) uintptr     { return accessRange(s, o, 2) }
func accessLargeChange(s, o uintptr) uintptr { return accessRange(s, o, 3) }
func accessSmallChange(s, o uintptr) uintptr { return accessRange(s, o, 3) }
func accessReadOnly(self, out uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*int32)(unsafe.Pointer(out)) = 1
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	if !p.node.Disabled && p.node.Actions&shirei.AccessSetValue != 0 {
		*(*int32)(unsafe.Pointer(out)) = 0
	}
	return 0
}

func accessGetObject(h, w, l uintptr) (uintptr, bool) {
	access := currentWindow.access
	id := int32(l)
	accessMu.Lock()
	var p *accessProvider
	if access.ready {
		if id == -25 {
			if accessUIAReady {
				p = access.live[0]
			}
		} else if msaaReady {
			p = access.objects[id]
		}
	}
	if p != nil {
		p.refs++
	}
	accessMu.Unlock()
	if p == nil {
		return 0, false
	}
	defer accessRelease(uintptr(unsafe.Pointer(&p.simple)))
	var result uintptr
	if id == -25 {
		result, _, _ = accessReturn.Call(h, w, l, uintptr(unsafe.Pointer(&p.simple)))
	} else {
		result, _, _ = msaaReturn.Call(uintptr(unsafe.Pointer(&msaaIID)), w, uintptr(unsafe.Pointer(&p.msaa)))
	}
	if os.Getenv("SHIREI_ACCESS_DEBUG") != "" {
		kind := "MSAA"
		if id == -25 {
			kind = "UIA"
		}
		fmt.Fprintf(os.Stderr, "%s WM_GETOBJECT(%d) -> %#x\n", kind, id, result)
	}
	return result, true
}

func flushAccessAction() {
	access := currentWindow.access
	accessMu.Lock()
	var a shirei.AccessAction
	if len(access.pending) > 0 {
		a = access.pending[0]
		access.pending = slices.Delete(access.pending, 0, 1)
	}
	if a.Kind != 0 {
		p := access.live[a.ID]
		if p == nil || p.node.Disabled || (p.node.Actions&a.Kind == 0 && !(a.ID == 0 && a.Kind == shirei.AccessFocus)) {
			a = shirei.AccessAction{}
		}
	}
	more := len(access.pending) > 0
	accessMu.Unlock()
	if a.Kind == shirei.AccessFocus {
		procSetForegroundWindow.Call(uintptr(currentWindow.hwnd))
		procSetFocus.Call(uintptr(currentWindow.hwnd))
	}
	if a.ID != 0 {
		shirei.GetFrameInput().AccessAction = a
	}
	if more {
		shirei.RequestNextFrame()
	}
}

// Window state can change without a drawable frame (for example, minimizing).
func refreshAccess() {
	access := currentWindow.access
	if !access.ready {
		return
	}
	accessMu.Lock()
	nodes := make([]shirei.AccessNode, 0, len(access.order))
	for _, id := range access.order {
		nodes = append(nodes, access.live[id].node)
	}
	accessMu.Unlock()
	updateAccess(nodes, false)
}

func updateAccess(nodes []shirei.AccessNode, changed bool) {
	access := currentWindow.access
	if !access.ready {
		return
	}
	var origin win32Point
	accessClientToScreen.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&origin)))
	focus, _, _ := accessGetFocus.Call()
	iconic, _, _ := accessIsIconic.Call(uintptr(currentWindow.hwnd))
	visible, _, _ := accessIsVisible.Call(uintptr(currentWindow.hwnd))
	active, shown := focus == uintptr(currentWindow.hwnd), iconic == 0 && visible != 0
	scale := float64(dpiScale())
	cw, ch := clientSize()
	rect := func(r shirei.Rect) accessRect {
		return accessRect{float64(origin.X) + float64(r.Origin[0])*scale, float64(origin.Y) + float64(r.Origin[1])*scale, float64(r.Size[0]) * scale, float64(r.Size[1]) * scale}
	}
	accessMu.Lock()
	rootBounds := accessRect{float64(origin.X), float64(origin.Y), float64(cw), float64(ch)}
	unchanged := !changed && access.live[0].bounds == rootBounds && access.active == active && access.visible == shown && access.scale == scale && access.live[0].node.Label == currentWindow.winTitle
	accessMu.Unlock()
	if unchanged {
		return
	}
	type change struct {
		p        *accessProvider
		old, now accessProvider
	}
	var changes []change
	var removed []*accessProvider
	accessMu.Lock()
	oldFocus, oldActive, oldVisible := access.focus, access.active, access.visible
	access.focus = 0
	access.active = active
	access.visible = shown
	access.scale = scale
	oldOrder := slices.Clone(access.order)
	access.order = access.order[:0]
	root := access.live[0]
	oldRoot := *root
	root.node = shirei.AccessNode{AccessAttrs: shirei.AccessAttrs{Label: currentWindow.winTitle}, Focusable: true}
	root.bounds = accessRect{float64(origin.X), float64(origin.Y), float64(cw), float64(ch)}
	root.clipped = root.bounds
	root.children = nil
	live := map[uint64]bool{0: true}
	structure := false
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		live[n.ID] = true
		p := access.live[n.ID]
		fresh := p == nil
		if fresh {
			p = newAccessProvider(access, n)
			structure = true
		}
		old := *p
		p.node = n
		p.bounds = rect(n.Bounds)
		p.clipped = rect(n.Rect)
		p.children = nil
		p.parent = n.ParentID
		if old.node.ParentID != n.ParentID {
			structure = true
		}
		access.order = append(access.order, n.ID)
		if n.KeyboardFocused {
			access.focus = n.ID
		}
		if !fresh && (old.node.AccessAttrs != n.AccessAttrs || old.node.KeyboardFocused != n.KeyboardFocused || old.node.Focusable != n.Focusable || old.node.Actions != n.Actions || old.bounds != p.bounds || old.clipped != p.clipped || oldActive != active || oldVisible != shown) {
			p.refs++
			changes = append(changes, change{p, old, *p})
		}
	}
	root.node.KeyboardFocused = access.focus == 0
	root.refs++
	changes = append(changes, change{root, oldRoot, *root})
	for _, id := range access.order {
		p := access.live[id]
		if !live[p.parent] {
			p.parent = 0
		}
		parent := access.live[p.parent]
		parent.children = append(parent.children, id)
	}
	for id, p := range access.live {
		if !live[id] {
			p.live = false
			delete(access.live, id)
			delete(access.objects, p.msaaID)
			removed = append(removed, p)
			structure = true
		}
	}
	structure = structure || !slices.Equal(oldOrder, access.order)
	focusTarget := access.live[access.focus]
	if focusTarget != nil {
		focusTarget.refs++
	}
	accessMu.Unlock()
	// External calls may synchronously call our providers. Never hold accessMu.
	for _, p := range removed {
		msaaEmit(0x8001, p.msaaID)
		if accessDisconnect.Find() == nil {
			accessDisconnect.Call(uintptr(unsafe.Pointer(&p.simple)))
		}
		accessRelease(uintptr(unsafe.Pointer(&p.simple)))
	}
	if structure {
		msaaEmit(0x8004, root.msaaID)
	}
	if structure && accessStructureEvent.Find() == nil {
		accessStructureEvent.Call(uintptr(unsafe.Pointer(&root.simple)), 2, 0, 0)
	}
	properties := []int32{30001, 30003, 30005, 30008, 30009, 30010, 30011, 30013, 30019, 30022, 30031, 30033, 30041, 30047, 30048, 30049, 30050, 30051, 30052, 30086}
	for _, c := range changes {
		msaaChanged(c.p, &c.old, &c.now, oldActive, oldVisible, active, shown)
		if accessPropertyEvent.Find() == nil {
			for _, id := range properties {
				old := accessPropertyValue(&c.old, id, oldActive, oldVisible)
				now := accessPropertyValue(&c.now, id, active, shown)
				equal := old == now
				if id == 30001 {
					equal = c.old.bounds == c.now.bounds
				}
				// BSTR addresses differ even for equal strings.
				if old.VT == 8 && now.VT == 8 {
					switch id {
					case 30005:
						equal = c.old.node.Label == c.now.node.Label
					case 30011:
						equal = c.old.node.Name == c.now.node.Name
					case 30013:
						equal = c.old.node.Description == c.now.node.Description
					}
				}
				if !equal {
					accessPropertyEvent.Call(uintptr(unsafe.Pointer(&c.p.simple)), uintptr(id), uintptr(unsafe.Pointer(&old)), uintptr(unsafe.Pointer(&now)))
				}
				accessClearVariant.Call(uintptr(unsafe.Pointer(&old)))
				accessClearVariant.Call(uintptr(unsafe.Pointer(&now)))
			}
		}
		accessRelease(uintptr(unsafe.Pointer(&c.p.simple)))
	}
	if focusTarget != nil {
		if active && (oldFocus != access.focus || !oldActive) {
			msaaEmit(0x8005, focusTarget.msaaID)
		}
		if active && (oldFocus != access.focus || !oldActive) && accessEvent.Find() == nil {
			accessEvent.Call(uintptr(unsafe.Pointer(&focusTarget.simple)), 20005)
		}
		accessRelease(uintptr(unsafe.Pointer(&focusTarget.simple)))
	}
}
func closeAccess() {
	access := currentWindow.access
	if !access.ready {
		return
	}
	accessMu.Lock()
	access.ready = false
	access.hwnd = 0
	access.pending = nil
	var retired []*accessProvider
	for _, p := range access.live {
		p.live = false
		retired = append(retired, p)
	}
	clear(access.live)
	clear(access.objects)
	accessMu.Unlock()
	for _, p := range retired {
		if accessDisconnect.Find() == nil {
			accessDisconnect.Call(uintptr(unsafe.Pointer(&p.simple)))
		}
		accessRelease(uintptr(unsafe.Pointer(&p.simple)))
	}
	if accessReturn.Find() == nil {
		accessReturn.Call(uintptr(currentWindow.hwnd), 0, 0, 0)
	}
	if access.com {
		accessCoUninitialize.Call()
		access.com = false
	}
}
