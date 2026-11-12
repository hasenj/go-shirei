//go:build windows && (amd64 || arm64)

package win32backend

import (
	"math"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"go.hasen.dev/shirei"
	"golang.org/x/sys/windows"
)

// IAccessible shares the provider's identity, snapshot and action queue with
// UIA. Each node is a full accessible object. Child arguments are one-based
// sibling indices; notifications use stable positive object IDs, not indices.
// VARIANT arguments are passed indirectly on both supported Windows ABIs.
var (
	msaaIID          = accessGUID("618736e0-3c3d-11cf-810c-00aa00389b71")
	msaaDispatchIID  = accessGUID("00020400-0000-0000-c000-000000000046")
	msaaWindowIID    = accessGUID("00000114-0000-0000-c000-000000000046")
	msaaLibraryIID   = accessGUID("1ea4dbf0-3c3b-11cf-810c-00aa00389b71")
	msaaDLL          = windows.NewLazySystemDLL("oleacc.dll")
	msaaReturn       = msaaDLL.NewProc("LresultFromObject")
	msaaStdObject    = msaaDLL.NewProc("CreateStdAccessibleObject")
	msaaNotify       = user32.NewProc("NotifyWinEvent")
	msaaLoadLibrary  = accessOLE.NewProc("LoadRegTypeLib")
	msaaLoadFile     = accessOLE.NewProc("LoadTypeLibEx")
	msaaNames        = accessOLE.NewProc("DispGetIDsOfNames")
	msaaInvoke       = accessOLE.NewProc("DispInvoke")
	msaaStringLength = accessOLE.NewProc("SysStringLen")
	msaaTable        [28]uintptr
	msaaWindowTable  [5]uintptr
	msaaTypeInfo     uintptr
	msaaReady        bool
	msaaNextID       int32
)

func msaaCall(object uintptr, slot int, args ...uintptr) uintptr {
	table := *(*uintptr)(unsafe.Pointer(object))
	method := *(*uintptr)(unsafe.Pointer(table + uintptr(slot)*unsafe.Sizeof(object)))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{object}, args...)...)
	return r
}
func initMSAA() {
	if msaaReturn.Find() != nil {
		return
	}
	// System type information also supplies IDispatch for automation clients.
	// Keep its owning reference for the process lifetime, including stale objects.
	if msaaTypeInfo == 0 {
		var lib uintptr
		hr, _, _ := msaaLoadLibrary.Call(uintptr(unsafe.Pointer(&msaaLibraryIID)), 1, 1, 0, uintptr(unsafe.Pointer(&lib)))
		if int32(hr) < 0 {
			if dir, err := windows.GetSystemDirectory(); err == nil {
				path, _ := syscall.UTF16PtrFromString(dir + `\oleacc.dll`)
				hr, _, _ = msaaLoadFile.Call(uintptr(unsafe.Pointer(path)), 2, uintptr(unsafe.Pointer(&lib))) // REGKIND_NONE
			}
		}
		if int32(hr) >= 0 && lib != 0 {
			msaaCall(lib, 6, uintptr(unsafe.Pointer(&msaaIID)), uintptr(unsafe.Pointer(&msaaTypeInfo)))
			msaaCall(lib, 2)
		}
	}
	methods := []any{accessQuery, accessAddRef, accessRelease, msaaTypeCount, msaaGetTypeInfo, msaaGetNames, msaaDispatch,
		msaaParent, msaaChildCount, msaaChild, msaaName, msaaValue, msaaDescription, msaaRole, msaaState, msaaHelp, msaaHelpTopic, msaaShortcut,
		msaaFocus, msaaSelection, msaaDefaultAction, msaaSelect, msaaLocation, msaaNavigate, msaaHitTest, msaaDoAction, msaaPutName, msaaPutValue}
	for i, method := range methods {
		msaaTable[i] = syscall.NewCallback(method)
	}
	msaaWindowTable = [5]uintptr{msaaTable[0], msaaTable[1], msaaTable[2], syscall.NewCallback(msaaGetWindow), syscall.NewCallback(msaaContextHelp)}
	msaaReady = true
}
func msaaTypeCount(self, out uintptr) uintptr {
	*(*uint32)(unsafe.Pointer(out)) = 0
	if msaaTypeInfo != 0 {
		*(*uint32)(unsafe.Pointer(out)) = 1
	}
	return 0
}
func msaaGetTypeInfo(self, index, locale, out uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if index != 0 {
		return 0x8002000b
	} // DISP_E_BADINDEX
	if msaaTypeInfo == 0 {
		return 0x80004001
	}
	msaaCall(msaaTypeInfo, 1)
	*(*uintptr)(unsafe.Pointer(out)) = msaaTypeInfo
	return 0
}
func msaaGetNames(self, iid, names, count, locale, out uintptr) uintptr {
	if *(*windows.GUID)(unsafe.Pointer(iid)) != (windows.GUID{}) {
		return 0x80020001
	}
	if msaaTypeInfo == 0 {
		return 0x80004001
	}
	hr, _, _ := msaaNames.Call(msaaTypeInfo, names, count, out)
	return hr
}
func msaaDispatch(self, member, iid, locale, flags, params, result, exception, argerr uintptr) uintptr {
	if *(*windows.GUID)(unsafe.Pointer(iid)) != (windows.GUID{}) {
		return 0x80020001
	}
	if msaaTypeInfo == 0 {
		return 0x80004001
	}
	hr, _, _ := msaaInvoke.Call(self, msaaTypeInfo, member, flags, params, result, exception, argerr)
	return hr
}
func msaaGetWindow(self, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if !accessSelf(self).live {
		return accessUnavailable
	}
	*(*uintptr)(unsafe.Pointer(out)) = access.hwnd
	return 0
}
func msaaContextHelp(self, enter uintptr) uintptr { return 0x80004001 }

// Caller holds accessMu; CHILDID_SELF is zero. Optional omitted arguments from
// IDispatch are equivalent to SELF. A child index never escapes as identity.
func msaaTarget(self, child uintptr) (*accessProvider, uintptr) {
	access := accessSelf(self).access
	p := accessSelf(self)
	if !p.live {
		return nil, accessUnavailable
	}
	if child == 0 {
		return nil, accessInvalid
	}
	v := *(*accessVariant)(unsafe.Pointer(child))
	if v.VT == 0 || (v.VT == 10 && uint32(v.Data) == 0x80020004) {
		return p, 0
	}
	if v.VT != 3 {
		return nil, accessInvalid
	}
	index := int32(v.Data)
	if index == 0 {
		return p, 0
	}
	if index < 1 || int(index) > len(p.children) {
		return nil, accessInvalid
	}
	return access.live[p.children[index-1]], 0
}
func msaaParent(self, out uintptr) uintptr {
	access := accessSelf(self).access
	*(*uintptr)(unsafe.Pointer(out)) = 0
	accessMu.Lock()
	p := accessSelf(self)
	if !p.live {
		accessMu.Unlock()
		return accessUnavailable
	}
	if p.node.ID != 0 {
		accessPut(out, &access.live[p.parent].msaa)
		accessMu.Unlock()
		return 0
	}
	h := access.hwnd
	accessMu.Unlock()
	// The system window object supplies the parent of our client-area root.
	hr, _, _ := msaaStdObject.Call(h, 0, uintptr(unsafe.Pointer(&msaaIID)), out)
	return hr
}
func msaaChildCount(self, out uintptr) uintptr {
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*int32)(unsafe.Pointer(out)) = 0
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	*(*int32)(unsafe.Pointer(out)) = int32(len(p.children))
	return 0
}
func msaaChild(self, child, out uintptr) uintptr {
	if child == 0 || (*accessVariant)(unsafe.Pointer(child)).VT != 3 {
		*(*uintptr)(unsafe.Pointer(out)) = 0
		return accessInvalid
	}
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*uintptr)(unsafe.Pointer(out)) = 0
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	accessPut(out, &p.msaa)
	return 0
}
func msaaValueString(n shirei.AccessNode) string {
	if n.Protected {
		return ""
	}
	if n.Numeric {
		return strconv.FormatFloat(float64(n.Number), 'f', -1, 32)
	}
	return n.Value
}
func msaaString(self, child, out uintptr, field int) uintptr {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	accessMu.Lock()
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		accessMu.Unlock()
		return hr
	}
	n := p.node
	accessMu.Unlock()
	var s string
	switch field {
	case 0:
		s = n.Label
	case 1:
		s = msaaValueString(n)
	case 2:
		s = n.Description
	case 3:
		if n.Actions&shirei.AccessPress != 0 && !n.Disabled {
			s = "Press"
			if n.Role == "checkbox" || n.Role == "switch" {
				s = "Check"
				if n.Checked {
					s = "Uncheck"
				}
			}
		}
	}
	if s == "" {
		return 1
	} // S_FALSE, no value for this property.
	v := accessString(s)
	if v.Data == 0 {
		return accessOutOfMemory
	}
	*(*uintptr)(unsafe.Pointer(out)) = uintptr(v.Data)
	return 0
}
func msaaName(s, c, o uintptr) uintptr          { return msaaString(s, c, o, 0) }
func msaaValue(s, c, o uintptr) uintptr         { return msaaString(s, c, o, 1) }
func msaaDescription(s, c, o uintptr) uintptr   { return msaaString(s, c, o, 2) }
func msaaHelp(s, c, o uintptr) uintptr          { return msaaString(s, c, o, 2) }
func msaaDefaultAction(s, c, o uintptr) uintptr { return msaaString(s, c, o, 3) }
func msaaShortcut(self, child, out uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	accessMu.Lock()
	defer accessMu.Unlock()
	_, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	return 1
}
func msaaHelpTopic(self, file, child, topic uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(file)) = 0
	*(*int32)(unsafe.Pointer(topic)) = -1
	accessMu.Lock()
	defer accessMu.Unlock()
	_, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	return 1
}
func msaaRole(self, child, out uintptr) uintptr {
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	accessMu.Lock()
	defer accessMu.Unlock()
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	role := int32(20) // ROLE_SYSTEM_GROUPING
	if p.node.ID == 0 {
		role = 10
	} else {
		switch p.node.Role {
		case "button":
			role = 43
		case "checkbox", "switch":
			role = 44
		case "radio":
			role = 45
		case "slider":
			role = 51
		case "progressbar":
			role = 48
		case "statictext":
			role = 41
		case "text":
			role = 41
			if p.node.Editable {
				role = 42
			}
		case "menu":
			role = 11
		case "menuitem":
			role = 12
		}
	}
	*(*accessVariant)(unsafe.Pointer(out)) = accessInt(role)
	return 0
}
func msaaStateBits(p *accessProvider, active, visible bool) int32 {
	n := p.node
	var state int32
	if n.Disabled {
		state |= 1
	}
	if active && n.KeyboardFocused {
		state |= 4
	}
	if n.Checked {
		state |= 16
	}
	if n.Focusable {
		state |= 0x100000
	}
	if n.Protected {
		state |= 0x20000000
	}
	if !visible {
		state |= 0x8000
	}
	if !visible || p.clipped.W <= 0 || p.clipped.H <= 0 {
		state |= 0x10000
	}
	if n.Numeric && n.Actions&shirei.AccessSetValue == 0 {
		state |= 0x40
	}
	return state
}
func msaaState(self, child, out uintptr) uintptr {
	access := accessSelf(self).access
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	accessMu.Lock()
	defer accessMu.Unlock()
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	*(*accessVariant)(unsafe.Pointer(out)) = accessInt(msaaStateBits(p, access.active, access.visible))
	return 0
}
func msaaDescendant(p, ancestor *accessProvider) bool {
	access := ancestor.access
	for p != nil {
		if p == ancestor {
			return true
		}
		if p.node.ID == 0 {
			break
		}
		p = access.live[p.parent]
	}
	return false
}

// Caller holds accessMu. Returned VT_DISPATCH transfers a reference to COM.
func msaaResult(out uintptr, p, self *accessProvider) uintptr {
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	if p == nil {
		return 1
	}
	if p == self {
		*(*accessVariant)(unsafe.Pointer(out)) = accessInt(0)
	} else {
		p.refs++
		*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{VT: 9, Data: uint64(uintptr(unsafe.Pointer(&p.msaa)))}
	}
	return 0
}
func msaaFocus(self, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	p := accessSelf(self)
	if !p.live {
		return accessUnavailable
	}
	focus := access.live[access.focus]
	if !access.active || !msaaDescendant(focus, p) {
		return 1
	}
	return msaaResult(out, focus, p)
}
func msaaSelection(self, out uintptr) uintptr {
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	accessMu.Lock()
	defer accessMu.Unlock()
	if !accessSelf(self).live {
		return accessUnavailable
	}
	return 1
}
func msaaAction(self, child uintptr, kind shirei.AccessActionKind, value float64) uintptr {
	accessMu.Lock()
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		accessMu.Unlock()
		return hr
	}
	p.refs++
	face := uintptr(unsafe.Pointer(&p.msaa))
	accessMu.Unlock()
	defer accessRelease(face)
	return accessQueue(face, kind, value)
}
func msaaSelect(self, flags, child uintptr) uintptr {
	if flags != 1 {
		return accessInvalid
	} // SELFLAG_TAKEFOCUS; selection is separate.
	return msaaAction(self, child, shirei.AccessFocus, 0)
}
func msaaDoAction(self, child uintptr) uintptr      { return msaaAction(self, child, shirei.AccessPress, 0) }
func msaaPutName(self, child, name uintptr) uintptr { return 0x80004001 }
func msaaPutValue(self, child, value uintptr) uintptr {
	if value == 0 {
		return accessInvalid
	}
	count, _, _ := msaaStringLength.Call(value)
	if count > 128 {
		return accessInvalid
	}
	s := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(value)), int(count)))
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return accessInvalid
	}
	return msaaAction(self, child, shirei.AccessSetValue, n)
}
func msaaLocation(self, left, top, width, height, child uintptr) uintptr {
	for _, out := range []uintptr{left, top, width, height} {
		*(*int32)(unsafe.Pointer(out)) = 0
	}
	accessMu.Lock()
	defer accessMu.Unlock()
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	r := p.bounds
	x, y := int32(math.Floor(r.X)), int32(math.Floor(r.Y))
	*(*int32)(unsafe.Pointer(left)) = x
	*(*int32)(unsafe.Pointer(top)) = y
	*(*int32)(unsafe.Pointer(width)) = int32(math.Ceil(r.X+r.W)) - x
	*(*int32)(unsafe.Pointer(height)) = int32(math.Ceil(r.Y+r.H)) - y
	return 0
}
func msaaNavigate(self, direction, child, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	p, hr := msaaTarget(self, child)
	if hr != 0 {
		return hr
	}
	var target *accessProvider
	switch direction {
	case 5, 6: // NAVDIR_NEXT / PREVIOUS
		if p.node.ID != 0 {
			siblings := access.live[p.parent].children
			for i, id := range siblings {
				if id == p.node.ID {
					if direction == 5 {
						i++
					} else {
						i--
					}
					if i >= 0 && i < len(siblings) {
						target = access.live[siblings[i]]
					}
					break
				}
			}
		}
	case 7:
		if len(p.children) > 0 {
			target = access.live[p.children[0]]
		}
	case 8:
		if len(p.children) > 0 {
			target = access.live[p.children[len(p.children)-1]]
		}
	default:
		return 0x80004001 // Spatial navigation is not implemented.
	}
	return msaaResult(out, target, accessSelf(self))
}
func msaaHitTest(self, x, y, out uintptr) uintptr {
	access := accessSelf(self).access
	accessMu.Lock()
	defer accessMu.Unlock()
	*(*accessVariant)(unsafe.Pointer(out)) = accessVariant{}
	parent := accessSelf(self)
	if !parent.live {
		return accessUnavailable
	}
	px, py := float64(int32(x)), float64(int32(y))
	order := -1
	var hit *accessProvider
	if access.visible {
		for _, id := range access.order {
			p := access.live[id]
			r := p.clipped
			if px >= r.X && py >= r.Y && px < r.X+r.W && py < r.Y+r.H && p.node.PaintOrder >= order && msaaDescendant(p, parent) {
				hit = p
				order = p.node.PaintOrder
			}
		}
		if hit == nil {
			r := parent.clipped
			if px >= r.X && py >= r.Y && px < r.X+r.W && py < r.Y+r.H {
				hit = parent
			}
		}
	}
	return msaaResult(out, hit, parent)
}
func msaaEmit(event uint32, object int32) {
	if msaaReady && object != 0 {
		msaaNotify.Call(uintptr(event), uintptr(currentWindow.hwnd), uintptr(uint32(object)), 0)
	}
}
func msaaChanged(p *accessProvider, old, now *accessProvider, oldActive, oldVisible, active, visible bool) {
	if old.node.Label != now.node.Label {
		msaaEmit(0x800c, p.msaaID)
	} // NAMECHANGE
	if msaaValueString(old.node) != msaaValueString(now.node) {
		msaaEmit(0x800e, p.msaaID)
	}
	if msaaStateBits(old, oldActive, oldVisible) != msaaStateBits(now, active, visible) {
		msaaEmit(0x800a, p.msaaID)
	}
	if old.bounds != now.bounds {
		msaaEmit(0x800b, p.msaaID)
	}
	if old.node.Description != now.node.Description {
		msaaEmit(0x800d, p.msaaID)
	}
}
