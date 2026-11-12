//go:build linux

package waylandbackend

import (
	"fmt"
	"go.hasen.dev/shirei/internal/wayland/wl"
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/widgets"
)

// Client-side decorations provide a titlebar and edge resizing when the
// compositor requires the client to decorate itself. xdg-decoration negotiates
// responsibility where supported; without it the client decorates itself.
// Move/resize use xdg_toplevel interactive grabs from the input serial.
//
// SetupWindow sizes are content/client (parity with macOS/Win32). createWindow
// grows the initial surface by titlebarHeight; wrapFrame then narrows
// Host.WindowSize so the app body matches the requested size. Compositor
// configures and interactive resizes already speak in full-surface size.
const titlebarHeight = 34

// SetupUndecorated omits the title bar and standard window buttons.
// Call before Run. Layer-shell v4 supplies a centered top-layer window with
// on-demand keyboard input; other compositors use an undecorated toplevel.
func SetupUndecorated() {
	currentWindow.undecorated = true
	currentWindow.csdEnabled = false
}

const decorationServerSide uint32 = 2

var decorationMgr *decorationManager

// These proxies implement version 1 of xdg-decoration-unstable-v1.
// Manager requests: destroy(0), get_toplevel_decoration(1).
// Decoration requests: destroy(0), set_mode(1), unset_mode(2).
// Decoration events: configure(0).
type decorationManager struct{ wl.BaseProxy }

func (*decorationManager) Dispatch(*wl.Event) {}

type toplevelDecoration struct {
	wl.BaseProxy
	window *windowState
}

func (d *toplevelDecoration) Dispatch(ev *wl.Event) {
	if d.window.closed {
		return
	}
	leave := enterWindow(d.window)
	defer leave()
	if ev.Opcode == 0 {
		currentWindow.pendingDecorationMode = ev.Uint32()
	}
}

func bindDecorationManager(name uint32) {
	ctx := registry.Context()
	mgr := &decorationManager{}
	ctx.Register(mgr)
	if err := registry.Bind(name, "zxdg_decoration_manager_v1", 1, mgr); err != nil {
		panic("waylandbackend: bind decoration manager: " + err.Error())
	}
	decorationMgr = mgr
}

func setupDecoration() error {
	if currentWindow.undecorated || decorationMgr == nil {
		return nil
	}
	ctx := decorationMgr.Context()
	decor := &toplevelDecoration{window: currentWindow}
	currentWindow.decoration = decor
	ctx.Register(decor)
	if err := ctx.SendRequest(decorationMgr, 1, decor, currentWindow.xdgToplevel); err != nil {
		return fmt.Errorf("waylandbackend: get toplevel decoration: %w", err)
	}
	if err := ctx.SendRequest(decor, 1, decorationServerSide); err != nil {
		return fmt.Errorf("waylandbackend: set decoration mode: %w", err)
	}
	return nil
}

// wrapFrame is the backend's own chrome wrapper (core knows nothing of
// decorations): the root spans the full surface (WindowSize as set per
// frame before this runs), the titlebar flows first, and the app builds
// inside an always-present content container — always-present because focus
// lives on identity nodes, and conditionally reparenting the app (e.g. on a
// future CSD/fullscreen toggle) would drop it. Host.WindowSize is narrowed
// to the content area during the app build, then restored to the surface so
// a settle pass still sizes the root correctly (see jsbackend.wrapFrame).
// drawFrame sets Host.WindowSize back to content after RunFrameFn.
//
// Popups drain at the root (core PopupsHost after this returns), not inside
// the content viewport. Menu Float coords are root-absolute
// (GetResolvedRectOf); parenting them under content, which is already
// origin-y = titlebarHeight, dropped every panel by that height.
func wrapFrame(appFn FrameFn) FrameFn {
	return func() {
		full := GetHost().WindowSize
		if currentWindow.csdEnabled {
			GetHost().WindowSize[1] = full[1] - titlebarHeight
			drawTitlebar()
		}
		ContainerWithKey("app-content", Attrs(Viewport), func() {
			appFn()
		})
		GetHost().WindowSize = full
	}
}

// drawTitlebar builds the client-side title bar (window title + close button) and
// starts an interactive move when dragged. wrapFrame places it above the
// app's content transparently — the app does nothing. Runs only while CSD is
// active.
func drawTitlebar() {
	Container(Attrs(Row, Expand, FixHeight(titlebarHeight), Background(0, 0, 88, 1),
		Grad(0, 0, -5, 0), CrossAlign(AlignMiddle), Pad2(0, 8), Gap(8)), func() {
		startDrag := IsClicked() // mouse pressed somewhere on the bar this frame
		Label(currentWindow.winTitle, FontSize(14), TextColor(0, 0, 25, 1))
		widgets.Filler(1)
		if closeButton() {
			requestWindowClose(currentWindow)
		} else if startDrag {
			startMove()
		}
	})
}

// closeButton is a small flat "×" that highlights red on hover.
func closeButton() bool {
	clicked := false
	Container(Attrs(Row, Center, FixSize(26, 26), Corners(5)), func() {
		NextAccessRole("button")
		NextAccessLabel("Close window")
		AssignAccess()
		action, requested := ProcessAccessAction(AccessPress, false)
		if IsHovered() {
			ModAttrs(Background(5, 70, 62, 1))
		}
		if IsClicked() || (requested && action.Kind == AccessPress) {
			clicked = true
		}
		Label("×", FontSize(20), TextColor(0, 0, 25, 1))
	})
	return clicked
}

func startMove() {
	if currentWindow.xdgToplevel != nil && seat != nil {
		currentWindow.xdgToplevel.Move(seat, pointerSerial)
	}
}

// resizeBorder is the logical-pixel hit zone along the window edges that begins
// an interactive resize.
const resizeBorder = 6

// tryStartResize begins an interactive resize if the press lands in the edge
// border; reports whether it consumed the press. Called from the pointer handler.
func tryStartResize(serial uint32) bool {
	if (!currentWindow.csdEnabled && !currentWindow.undecorated) || currentWindow.xdgToplevel == nil || seat == nil {
		return false
	}
	p := GetInputState().MousePoint
	// Use the full window size: pointer coords are full-window, but WindowSize is
	// the content area (it excludes the titlebar).
	edge := resizeEdgeAt(p[0], p[1], float32(currentWindow.logicalW), float32(currentWindow.logicalH))
	if edge == zxdg.ToplevelResizeEdgeNone {
		return false
	}
	currentWindow.xdgToplevel.Resize(seat, serial, edge)
	return true
}

func resizeEdgeAt(x, y, w, h float32) uint32 {
	left := x < resizeBorder
	right := x > w-resizeBorder
	top := y < resizeBorder
	bottom := y > h-resizeBorder
	switch {
	case top && left:
		return zxdg.ToplevelResizeEdgeTopLeft
	case top && right:
		return zxdg.ToplevelResizeEdgeTopRight
	case bottom && left:
		return zxdg.ToplevelResizeEdgeBottomLeft
	case bottom && right:
		return zxdg.ToplevelResizeEdgeBottomRight
	case left:
		return zxdg.ToplevelResizeEdgeLeft
	case right:
		return zxdg.ToplevelResizeEdgeRight
	case top:
		return zxdg.ToplevelResizeEdgeTop
	case bottom:
		return zxdg.ToplevelResizeEdgeBottom
	}
	return zxdg.ToplevelResizeEdgeNone
}
