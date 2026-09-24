package shirei

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	g "go.hasen.dev/generic"
)

// AccessAttrs is the pending / stamped semantic record for one access node.
// NextAccess* writes fields on an implicit current value; AssignAccess applies
// that value to the current container and clears it. Last write wins per field.
type AccessAttrs struct {
	Name                   string // query name (no spaces); not the spoken label, not identity
	Role                   string // button, checkbox, text, …
	Checked                bool   // snapshot this frame
	Value                  string // snapshot this frame (text field, slider, …)
	Label                  string // spoken name; independent of the drive query Name
	Description            string
	Disabled               bool
	Protected              bool // value is not exported; automatic child text is suppressed
	Editable               bool // distinguishes text inputs from static text
	Multiline              bool
	Hidden                 bool // excluded from platform accessibility, retained for drive queries
	Numeric                bool
	Number, Min, Max, Step float32
}

// AccessNode is one semantic container in this pass's access tree. Assigned
// controls and automatic text nodes are included; layout wrappers are omitted.
type AccessNode struct {
	AccessAttrs
	ID              uint64 // identity serial (#N in drive records)
	PaintOrder      int    // front-to-back hit testing uses descending paint order
	ParentID        uint64 // nearest access ancestor; zero means the host view
	Bounds          Rect   // unclipped rectangle in window logical points
	KeyboardFocused bool   // exact keyboard target, not its ancestors
	Actions         AccessActionKind
	Focusable       bool
	Path            string // access-parent chain, space-separated; empty Name is "-"
	Container       ContainerId
	Rect            Rect // on-screen (clipped) rectangle
	Focused         bool
	Hovered         bool
	Z               float32
	Scroll          Vec2
	ScrollMax       Vec2
	ScrollPort      bool

	parent int // index in ui.access; -1 if none
}

// NextAccessName sets the pending access name for the next AssignAccess.
// The name is a query handle for drive tests.
// It is not container identity — that is Container / ContainerWithKey.
// Names need not be unique, even among siblings: many rows can all be
// named "proc". Lowercase, no spaces (queries split on spaces). A name
// must not start with '#' (that prefix is an identity serial).
func NextAccessName(name string) {
	if strings.HasPrefix(name, "#") {
		fmt.Fprintf(os.Stderr, "shirei: access name %q must not start with #\n", name)
		return
	}
	ui.nextAccess.Name = name
}

func NextAccessRole(role string) {
	ui.nextAccess.Role = role
}

func NextAccessChecked(checked bool) {
	ui.nextAccess.Checked = checked
}

func NextAccessValue(value string) {
	ui.nextAccess.Value = value
}

// NextAccessLabel sets the spoken label independently of the query name.
func NextAccessLabel(label string) { ui.nextAccess.Label = label }

func NextAccessDescription(description string) { ui.nextAccess.Description = description }
func NextAccessDisabled(disabled bool)         { ui.nextAccess.Disabled = disabled }
func NextAccessProtected(protected bool)       { ui.nextAccess.Protected = protected }
func NextAccessEditable(editable, multiline bool) {
	ui.nextAccess.Editable, ui.nextAccess.Multiline = editable, multiline
}
func NextAccessHidden(hidden bool) { ui.nextAccess.Hidden = hidden }
func NextAccessRange(value, min, max, step float32) {
	ui.nextAccess.Numeric = true
	ui.nextAccess.Number, ui.nextAccess.Min, ui.nextAccess.Max, ui.nextAccess.Step = value, min, max, step
}

// AssignAccess stamps the pending AccessAttrs onto the current container and
// clears the pending value. Container does not auto-consume; the widget (or
// app chrome) chooses which node is the access target.
func AssignAccess() {
	if ui.current == nil || !ui.frameInProgress {
		return
	}
	ui.current.access = ui.nextAccess
	ui.current.accessSet = true
	ui.anyAccess = true
	ui.nextAccess = AccessAttrs{}
}

func warnLeftoverAccess() {
	a := ui.nextAccess
	if a == (AccessAttrs{}) {
		return
	}
	fmt.Fprintf(os.Stderr, "shirei: leftover NextAccess* (name=%q role=%q) without AssignAccess\n", a.Name, a.Role)
	ui.nextAccess = AccessAttrs{}
}

func collectAccessTree(root *_Container) {
	g.ResetSlice(&ui.access)
	if ui.accessByName == nil {
		ui.accessByName = make(map[string][]int)
	} else {
		clear(ui.accessByName)
	}
	if !ui.anyAccess {
		return
	}
	walkAccess(root, -1, -1, false)
}

// textOwner is -1 for standalone text, -2 for suppressed text, or the
// access index of a control whose label is assembled from its text children.
func walkAccess(c *_Container, parentIdx, textOwner int, hidden bool) {
	hidden = hidden || c.access.Hidden
	idx := parentIdx
	a := c.access
	if c.accessText != "" && !c.accessSet {
		if textOwner >= 0 && !hidden {
			label := &ui.access[textOwner].Label
			if *label != "" {
				*label += " "
			}
			*label += c.accessText
		} else if textOwner == -1 {
			a.Role, a.Label = "statictext", c.accessText
		}
	}
	if c.accessSet || a.Role == "statictext" {
		idx = len(ui.access)
		pad := PadSize(c.Padding)
		scrollMax := Vec2Sub(c.ContentSize, Vec2Sub(c.resolvedSize, pad))
		CapAbove(&scrollMax[0], 0)
		CapAbove(&scrollMax[1], 0)
		n := AccessNode{
			AccessAttrs:     a,
			Bounds:          Rect{Origin: c.resolvedOrigin, Size: c.resolvedSize},
			KeyboardFocused: c.node == ui.focused,
			Focusable:       c.Focusable,
			Actions:         c.accessActions,
			PaintOrder:      c.accessOrder,
			ID:              c.node.serial,
			Container:       ContainerId(c.node),
			Rect:            c.ScreenRect,
			Focused:         identOnFocusChain(c.node),
			Hovered:         slices.Contains(ui.hoverList, c.node),
			Z:               c.Z,
			Scroll:          c.ScrollOffset,
			ScrollMax:       scrollMax,
			ScrollPort:      c.scrollOnInput,
			parent:          parentIdx,
		}
		if parentIdx >= 0 {
			n.ParentID = ui.access[parentIdx].ID
		}
		n.Hidden = hidden
		if ui.frameFocusTrap != nil && c.node.focusTrapOwner != ui.frameFocusTrap {
			n.Hidden = true
		}
		if n.Protected {
			n.Value = ""
		}
		ui.access = append(ui.access, n)
		switch a.Role {
		case "button", "checkbox", "radio", "switch", "slider", "menuitem", "text", "statictext", "progressbar":
			textOwner = -2
			if a.Label == "" && !a.Protected && !a.Editable && a.Role != "slider" && a.Role != "progressbar" {
				textOwner = idx
			}
		}
		if a.Protected {
			textOwner = -2
		}
		if n.Name != "" {
			ui.accessByName[n.Name] = append(ui.accessByName[n.Name], idx)
		}
		ui.access[idx].Path = accessPath(idx)
	}
	for _, ch := range c.children {
		walkAccess(ch, idx, textOwner, hidden)
	}
}

func accessPath(idx int) string {
	var n int
	for i := idx; i >= 0; i = ui.access[i].parent {
		n++
	}
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := idx; i >= 0; i = ui.access[i].parent {
		n--
		name := ui.access[i].Name
		if name == "" {
			name = "-"
		}
		parts[n] = name
	}
	return strings.Join(parts, " ")
}

func accessMatches(idx int, tokens []string) bool {
	last := tokens[len(tokens)-1]
	if ui.access[idx].Name != last {
		return false
	}
	need := tokens[:len(tokens)-1]
	i := len(need) - 1
	for p := ui.access[idx].parent; p >= 0 && i >= 0; p = ui.access[p].parent {
		if ui.access[p].Name == need[i] {
			i--
		}
	}
	return i < 0
}

// QueryContainers returns every access node whose name path matches q, in tree
// order. q is space-separated names: the last token is the leaf, earlier
// tokens are access-ancestors (not necessarily immediate). Exact token match.
// A q of the form #N (no spaces) is an identity serial: at most one node.
func QueryContainers(q string) []AccessNode {
	q = strings.TrimSpace(q)
	if strings.HasPrefix(q, "#") && !strings.ContainsAny(q, " \t") {
		id, err := strconv.ParseUint(q[1:], 10, 64)
		if err != nil || id == 0 {
			return nil
		}
		if n, ok := accessNodeBySerial(id); ok {
			return []AccessNode{n}
		}
		return nil
	}
	tokens := strings.Fields(q)
	if len(tokens) == 0 {
		return nil
	}
	hits := ui.accessByName[tokens[len(tokens)-1]]
	var out []AccessNode
	for _, idx := range hits {
		if accessMatches(idx, tokens) {
			out = append(out, ui.access[idx])
		}
	}
	return out
}

// QueryContainer returns the first QueryContainers match.
func QueryContainer(q string) (AccessNode, bool) {
	all := QueryContainers(q)
	if len(all) == 0 {
		return AccessNode{}, false
	}
	return all[0], true
}

func identOnFocusChain(n *identNode) bool {
	for x := ui.focused; x != nil; x = x.parent {
		if x == n {
			return true
		}
	}
	return false
}

// FocusIDs is the focused identity and its ancestors, leaf first.
// Empty when nothing is focused.
func FocusIDs() []uint64 {
	var out []uint64
	for n := ui.focused; n != nil; n = n.parent {
		out = append(out, n.serial)
	}
	return out
}

// HoverIDs is the hover stack (direct hit first, then ancestors, skipping
// ClickThrough), matching ui.hoverList.
func HoverIDs() []uint64 {
	if len(ui.hoverList) == 0 {
		return nil
	}
	out := make([]uint64, len(ui.hoverList))
	for i, n := range ui.hoverList {
		out[i] = n.serial
	}
	return out
}

func accessNodeBySerial(id uint64) (AccessNode, bool) {
	if id == 0 {
		return AccessNode{}, false
	}
	for i := range ui.access {
		if ui.access[i].ID == id {
			return ui.access[i], true
		}
	}
	return AccessNode{}, false
}

// AccessActionKind is a bit set of supported accessibility operations. A
// request contains exactly one bit; nodes advertise all operations they handle.
type AccessActionKind uint32

const (
	AccessPress AccessActionKind = 1 << iota
	AccessFocus
	AccessIncrement
	AccessDecrement
	AccessSetValue
)

// AccessAction targets a stable ID from a completed Access snapshot. Backends
// queue requests and deliver one per frame through FrameInput.AccessAction.
type AccessAction struct {
	ID    uint64
	Kind  AccessActionKind
	Value float32
}

// ProcessAccessAction advertises the current control's supported actions and
// takes its pending request, if any. Call from the control's interaction helper
// before reading focus. Focus requests use the ordinary focus/reveal path.
// Other actions are returned for the helper to apply to its normal state.
func ProcessAccessAction(supported AccessActionKind, disabled bool) (AccessAction, bool) {
	c := ui.current
	if c == nil {
		return AccessAction{}, false
	}
	c.accessActions = supported
	if disabled {
		c.accessActions = 0
		return AccessAction{}, false
	}
	a := ui.Host.FrameInput.AccessAction
	if a.ID != c.node.serial || a.Kind == 0 || a.Kind&(a.Kind-1) != 0 || a.Kind&supported == 0 {
		return AccessAction{}, false
	}
	previous, ok := accessNodeBySerial(a.ID)
	if !ok || previous.Disabled || previous.Hidden {
		return AccessAction{}, false
	}
	ui.Host.FrameInput.AccessAction = AccessAction{}
	if a.Kind == AccessFocus {
		FocusImmediateOn(ContainerId(c.node))
		ShowFocusIndicator()
	}
	RequestNextFrame()
	return a, true
}
