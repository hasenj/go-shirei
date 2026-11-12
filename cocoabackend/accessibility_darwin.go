//go:build darwin && !ios

package cocoabackend

import (
	"fmt"
	"math"
	"slices"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"go.hasen.dev/shirei"
)

// Native objects and snapshots belong to the AppKit thread. Getters never
// enter Shirei's frame lock; setters queue input for a subsequent frame.
type accessElement struct {
	window   *windowState
	object   objc.ID
	node     shirei.AccessNode
	parent   objc.ID
	children []objc.ID
}

var (
	accessClass                                                                  objc.Class
	accessByObject                                                               = map[objc.ID]*accessElement{}
	accessPostNotification                                                       func(objc.ID, objc.ID)
	accessRoleDescription                                                        func(objc.ID, objc.ID) objc.ID
	accessLayoutChanged, accessValueChanged, accessFocusChanged, accessDestroyed objc.ID
)

func registerAccessClass() error {
	purego.RegisterLibFunc(&accessPostNotification, purego.RTLD_DEFAULT, "NSAccessibilityPostNotification")
	purego.RegisterLibFunc(&accessRoleDescription, purego.RTLD_DEFAULT, "NSAccessibilityRoleDescription")
	for _, c := range []struct {
		name   string
		target *objc.ID
	}{
		{"NSAccessibilityLayoutChangedNotification", &accessLayoutChanged},
		{"NSAccessibilityValueChangedNotification", &accessValueChanged},
		{"NSAccessibilityFocusedUIElementChangedNotification", &accessFocusChanged},
		{"NSAccessibilityUIElementDestroyedNotification", &accessDestroyed},
	} {
		value, err := loadConst(purego.RTLD_DEFAULT, c.name)
		if err != nil {
			return err
		}
		*c.target = value
	}
	var err error
	accessClass, err = objc.RegisterClass("ShireiAccessElement", objc.GetClass("NSAccessibilityElement"), nil, nil, []objc.MethodDef{
		{Cmd: sel("isAccessibilityElement"), Fn: accessIsElement},
		{Cmd: sel("accessibilityRole"), Fn: accessRole},
		{Cmd: sel("accessibilitySubrole"), Fn: accessSubrole},
		{Cmd: sel("accessibilityOrientation"), Fn: accessOrientation},
		{Cmd: sel("accessibilityIsAttributeSettable:"), Fn: accessAttributeSettable},
		{Cmd: sel("accessibilityRoleDescription"), Fn: accessRoleText},
		{Cmd: sel("accessibilityLabel"), Fn: accessLabel},
		{Cmd: sel("accessibilityHelp"), Fn: accessHelp},
		{Cmd: sel("accessibilityIdentifier"), Fn: accessIdentifier},
		{Cmd: sel("accessibilityParent"), Fn: accessParent},
		{Cmd: sel("accessibilityChildren"), Fn: accessChildren},
		{Cmd: sel("accessibilityWindow"), Fn: accessWindow},
		{Cmd: sel("accessibilityTopLevelUIElement"), Fn: accessWindow},
		{Cmd: sel("accessibilityFrame"), Fn: accessFrame},
		{Cmd: sel("accessibilityHitTest:"), Fn: accessHitTest},
		{Cmd: sel("accessibilityFocusedUIElement"), Fn: viewAccessFocused},
		{Cmd: sel("isAccessibilityEnabled"), Fn: accessEnabled},
		{Cmd: sel("isAccessibilityFocused"), Fn: accessIsFocused},
		{Cmd: sel("setAccessibilityFocused:"), Fn: accessSetFocused},
		{Cmd: sel("accessibilityValue"), Fn: accessValue},
		{Cmd: sel("accessibilityMinValue"), Fn: accessMin},
		{Cmd: sel("accessibilityMaxValue"), Fn: accessMax},
		{Cmd: sel("setAccessibilityValue:"), Fn: accessSetValue},
		{Cmd: sel("accessibilityPerformPress"), Fn: accessPress},
		{Cmd: sel("accessibilityPerformIncrement"), Fn: accessIncrement},
		{Cmd: sel("accessibilityPerformDecrement"), Fn: accessDecrement},
		{Cmd: sel("isAccessibilitySelectorAllowed:"), Fn: accessSelectorAllowed},
	})
	return err
}

func updateAccess(nodes []shirei.AccessNode) {
	previousFocus := currentWindow.accessFocused
	previousOrder := slices.Clone(currentWindow.accessOrder)
	currentWindow.accessFocused = 0
	currentWindow.accessRoots = currentWindow.accessRoots[:0]
	currentWindow.accessOrder = currentWindow.accessOrder[:0]
	live := make(map[uint64]bool, len(nodes))
	var values []objc.ID
	layoutChanged := false
	// Create/update every object before publishing links or notifying clients.
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		live[n.ID] = true
		e := currentWindow.accessByID[n.ID]
		if e == nil {
			object := objc.ID(accessClass).Send(sel("alloc")).Send(sel("init"))
			e = &accessElement{object: object, window: currentWindow}
			currentWindow.accessByID[n.ID], accessByObject[object] = e, e
			layoutChanged = true
		} else {
			old := e.node
			if old.Value != n.Value || old.Checked != n.Checked || old.Number != n.Number || old.Min != n.Min || old.Max != n.Max {
				values = append(values, e.object)
			}
			if old.ParentID != n.ParentID || old.Role != n.Role || old.Label != n.Label || old.Bounds != n.Bounds || old.Disabled != n.Disabled || old.Description != n.Description || old.Protected != n.Protected || old.Actions != n.Actions {
				layoutChanged = true
			}
		}
		e.node = n
		e.children = e.children[:0]
		e.parent = currentWindow.gView
		if n.KeyboardFocused {
			currentWindow.accessFocused = e.object
		}
		currentWindow.accessOrder = append(currentWindow.accessOrder, e.object)
	}
	for _, object := range currentWindow.accessOrder {
		e := accessByObject[object]
		if p := currentWindow.accessByID[e.node.ParentID]; p != nil && live[p.node.ID] {
			e.parent = p.object
			p.children = append(p.children, object)
		} else {
			currentWindow.accessRoots = append(currentWindow.accessRoots, object)
		}
	}
	layoutChanged = layoutChanged || !slices.Equal(previousOrder, currentWindow.accessOrder)
	var removed []*accessElement
	for id, e := range currentWindow.accessByID {
		if !live[id] {
			removed = append(removed, e)
			delete(currentWindow.accessByID, id)
			delete(accessByObject, e.object)
			layoutChanged = true
		}
	}
	// Native clients may retain removed objects. Their getters now return empty
	// values and their actions fail; our owning reference can be released.
	for _, e := range removed {
		accessPostNotification(e.object, accessDestroyed)
		e.object.Send(sel("release"))
	}
	for _, object := range values {
		accessPostNotification(object, accessValueChanged)
	}
	if layoutChanged {
		accessPostNotification(currentWindow.gView, accessLayoutChanged)
	}
	if previousFocus != currentWindow.accessFocused {
		target := currentWindow.accessFocused
		if target == 0 {
			target = currentWindow.gView
		}
		accessPostNotification(target, accessFocusChanged)
	}
}

func viewAccessElement(objc.ID, objc.SEL) bool { return true }
func viewAccessRole(objc.ID, objc.SEL) objc.ID { return nsString("AXGroup") }
func viewAccessChildren(self objc.ID, _ objc.SEL) objc.ID {
	w := windowsByObject[self]
	if w == nil {
		return nsArray()
	}
	return nsArray(w.accessRoots...)
}
func viewAccessFocused(self objc.ID, _ objc.SEL) objc.ID {
	w := accessWindowState(self)
	if w == nil {
		return 0
	}
	if w.accessFocused != 0 {
		return w.accessFocused
	}
	return w.gView
}
func accessIsElement(self objc.ID, _ objc.SEL) bool { return accessByObject[self] != nil }
func accessRole(self objc.ID, _ objc.SEL) objc.ID {
	e := accessByObject[self]
	if e == nil {
		return nsString("AXUnknown")
	}
	role := "AXGroup"
	switch e.node.Role {
	case "button":
		role = "AXButton"
	case "checkbox", "switch":
		role = "AXCheckBox"
	case "radio":
		role = "AXRadioButton"
	case "radiogroup":
		role = "AXRadioGroup"
	case "slider":
		role = "AXSlider"
	case "statictext":
		role = "AXStaticText"
	case "text":
		role = "AXStaticText"
		if e.node.Editable {
			role = "AXTextField"
			if e.node.Multiline {
				role = "AXTextArea"
			}
		}
	case "progressbar":
		role = "AXProgressIndicator"
	case "menu":
		role = "AXMenu"
	case "menuitem":
		role = "AXMenuItem"
	}
	return nsString(role)
}
func accessRoleText(self objc.ID, _ objc.SEL) objc.ID {
	return accessRoleDescription(accessRole(self, 0), 0)
}
func accessLabel(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil {
		if e.node.Role == "statictext" {
			return 0
		}
		return nsString(e.node.Label)
	}
	return 0
}
func accessHelp(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil {
		return nsString(e.node.Description)
	}
	return 0
}
func accessIdentifier(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil {
		return nsString(fmt.Sprintf("%s#%d", e.node.Name, e.node.ID))
	}
	return 0
}
func accessParent(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil {
		return e.parent
	}
	return 0
}
func accessChildren(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil {
		return nsArray(e.children...)
	}
	return nsArray()
}
func accessWindow(self objc.ID, _ objc.SEL) objc.ID {
	w := accessWindowState(self)
	if w == nil {
		return 0
	}
	return w.gWindow
}
func accessFrame(self objc.ID, _ objc.SEL) nsRect {
	w := accessWindowState(self)
	if w == nil {
		return nsRect{}
	}

	if e := accessByObject[self]; e != nil && w.gView != 0 {
		r := e.node.Bounds
		local := nsMakeRect(float64(r.Origin[0]), float64(r.Origin[1]), float64(r.Size[0]), float64(r.Size[1]))
		window := objc.Send[nsRect](w.gView, sel("convertRect:toView:"), local, objc.ID(0))
		return objc.Send[nsRect](w.gWindow, sel("convertRectToScreen:"), window)
	}
	return nsRect{}
}
func accessHitTest(self objc.ID, _ objc.SEL, point nsPoint) objc.ID {
	w := accessWindowState(self)
	if w == nil {
		return 0
	}

	screen := nsRect{Origin: point}
	window := objc.Send[nsRect](w.gWindow, sel("convertRectFromScreen:"), screen)
	local := objc.Send[nsRect](w.gView, sel("convertRect:fromView:"), window, objc.ID(0))
	p := shirei.Vec2{float32(local.Origin.X), float32(local.Origin.Y)}
	var hit objc.ID
	order := -1
	for _, object := range w.accessOrder {
		if self != w.gView {
			ancestor := object
			for ancestor != 0 && ancestor != self && ancestor != w.gView {
				e := accessByObject[ancestor]
				if e == nil {
					break
				}
				ancestor = e.parent
			}
			if ancestor != self {
				continue
			}
		}
		n := accessByObject[object].node
		if n.PaintOrder >= order && shirei.RectContainsPoint(n.Rect, p) {
			hit, order = object, n.PaintOrder
		}
	}
	if hit != 0 {
		return hit
	}
	return self
}
func accessEnabled(self objc.ID, _ objc.SEL) bool {
	e := accessByObject[self]
	return e != nil && !e.node.Disabled
}
func accessIsFocused(self objc.ID, _ objc.SEL) bool {
	w := accessWindowState(self)
	return w != nil && self == w.accessFocused
}
func accessSetFocused(self objc.ID, _ objc.SEL, value bool) {
	if value {
		queueAccess(self, shirei.AccessFocus, 0)
	}
}
func accessValue(self objc.ID, _ objc.SEL) objc.ID {
	e := accessByObject[self]
	if e == nil || e.node.Protected {
		return 0
	}
	n := e.node
	if n.Numeric {
		return objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithDouble:"), float64(n.Number))
	}
	switch n.Role {
	case "checkbox", "switch", "radio":
		if n.Checked {
			return nsNumber(1)
		}
		return nsNumber(0)
	case "statictext":
		return nsString(n.Label)
	}
	if n.Value != "" || n.Editable {
		return nsString(n.Value)
	}
	return 0
}
func accessMin(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil && e.node.Numeric {
		return objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithDouble:"), float64(e.node.Min))
	}
	return 0
}
func accessMax(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil && e.node.Numeric {
		return objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithDouble:"), float64(e.node.Max))
	}
	return 0
}
func accessSetValue(self objc.ID, _ objc.SEL, value objc.ID) {
	if value != 0 && value.Send(sel("isKindOfClass:"), objc.GetClass("NSNumber")) != 0 {
		number := objc.Send[float64](value, sel("doubleValue"))
		if !math.IsNaN(number) && !math.IsInf(number, 0) {
			queueAccess(self, shirei.AccessSetValue, float32(number))
		}
	}
}
func accessPress(self objc.ID, _ objc.SEL) bool { return queueAccess(self, shirei.AccessPress, 0) }
func accessIncrement(self objc.ID, _ objc.SEL) bool {
	return queueAccess(self, shirei.AccessIncrement, 0)
}
func accessDecrement(self objc.ID, _ objc.SEL) bool {
	return queueAccess(self, shirei.AccessDecrement, 0)
}
func accessSelectorAllowed(self objc.ID, cmd objc.SEL, selector objc.SEL) bool {
	e := accessByObject[self]
	if e == nil {
		return false
	}
	var kind shirei.AccessActionKind
	switch selector {
	case sel("accessibilityPerformPress"):
		kind = shirei.AccessPress
	case sel("accessibilityPerformIncrement"):
		kind = shirei.AccessIncrement
	case sel("accessibilityPerformDecrement"):
		kind = shirei.AccessDecrement
	case sel("setAccessibilityValue:"):
		kind = shirei.AccessSetValue
	case sel("setAccessibilityFocused:"):
		kind = shirei.AccessFocus
	default:
		return objc.SendSuper[bool](self, cmd, selector)
	}
	return !e.node.Disabled && e.node.Actions&kind != 0
}
func queueAccess(object objc.ID, kind shirei.AccessActionKind, value float32) bool {
	e := accessByObject[object]
	if e == nil || e.node.Disabled || e.node.Actions&kind == 0 {
		return false
	}
	e.window.accessPending = append(e.window.accessPending, shirei.AccessAction{ID: e.node.ID, Kind: kind, Value: value})
	// Schedule asynchronously even on the main thread: notification callbacks
	// can arrive while the current native snapshot is being published.
	e.window.gView.Send(sel("performSelector:withObject:afterDelay:"), sel("shireiWakeAndRender"), objc.ID(0), float64(0))
	return true
}
func flushAccessAction() {
	if len(currentWindow.accessPending) == 0 {
		return
	}
	shirei.GetFrameInput().AccessAction = currentWindow.accessPending[0]
	currentWindow.accessPending = slices.Delete(currentWindow.accessPending, 0, 1)
}

func accessSubrole(self objc.ID, _ objc.SEL) objc.ID {
	if e := accessByObject[self]; e != nil && e.node.Protected {
		return nsString("AXSecureTextField")
	}
	return 0
}
func accessOrientation(self objc.ID, _ objc.SEL) int {
	if e := accessByObject[self]; e != nil && e.node.Role == "slider" {
		return 2
	}
	return 0
}

// Attribute-based clients also query writability through the NSObject API.
func accessAttributeSettable(self objc.ID, _ objc.SEL, attribute objc.ID) bool {
	e := accessByObject[self]
	if e == nil || e.node.Disabled {
		return false
	}
	switch nsPlainString(attribute) {
	case "AXValue":
		return e.node.Actions&shirei.AccessSetValue != 0
	case "AXFocused":
		return e.node.Actions&shirei.AccessFocus != 0
	default:
		return false
	}
}

func accessWindowState(object objc.ID) *windowState {
	if w := windowsByObject[object]; w != nil && !w.closed {
		return w
	}
	if e := accessByObject[object]; e != nil && !e.window.closed {
		return e.window
	}
	return nil
}
