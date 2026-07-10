//go:build linux

package waylandbackend

import (
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/widgets"
)

// Client-side decorations. Of shirei's backends only Wayland lacks a window
// manager that draws the titlebar/borders — GNOME/Mutter in particular requires
// the client to decorate itself. So the Wayland backend offers a Titlebar() the
// app places at the top of its frame (drag to move, a close button) plus
// pointer-level edge resizing. Move/resize are driven by xdg_toplevel, which
// starts an interactive compositor grab from the input serial.
//
// (Server-side decorations via the xdg-decoration protocol — real titlebars on
// KDE/sway — are a later add; they'd also let us skip CSD where granted.)

const titlebarHeight = 34

// csdEnabled gates the client-side titlebar/resize. Always on for now; once
// xdg-decoration negotiation lands this turns off when the compositor draws SSD.
var csdEnabled = true

// drawTitlebar builds the client-side title bar (window title + close button) and
// starts an interactive move when dragged. It's installed as shirei.DecorationFn,
// so the core draws it above the app's content transparently — the app does
// nothing. Runs only while CSD is active.
func drawTitlebar() {
	Container(Attrs(Row, Expand, FixHeight(titlebarHeight), Background(0, 0, 88, 1),
		Grad(0, 0, -5, 0), CrossAlign(AlignMiddle), Pad2(0, 8), Gap(8)), func() {
		startDrag := IsClicked() // mouse pressed somewhere on the bar this frame
		Label(winTitle, FontSize(14), TextColor(0, 0, 25, 1))
		widgets.Filler(1)
		if closeButton() {
			quit = true
		} else if startDrag {
			startMove()
		}
	})
}

// closeButton is a small flat "×" that highlights red on hover.
func closeButton() bool {
	clicked := false
	Container(Attrs(Row, Center, FixSize(26, 26), Corners(5)), func() {
		if IsHovered() {
			ModAttrs(Background(5, 70, 62, 1))
		}
		if IsClicked() {
			clicked = true
		}
		Label("×", FontSize(20), TextColor(0, 0, 25, 1))
	})
	return clicked
}

func startMove() {
	if xdgToplevel != nil && seat != nil {
		xdgToplevel.Move(seat, pointerSerial)
	}
}

// resizeBorder is the logical-pixel hit zone along the window edges that begins
// an interactive resize.
const resizeBorder = 6

// tryStartResize begins an interactive resize if the press lands in the edge
// border; reports whether it consumed the press. Called from the pointer handler.
func tryStartResize(serial uint32) bool {
	if !csdEnabled || xdgToplevel == nil || seat == nil {
		return false
	}
	p := InputState.MousePoint
	// Use the full window size: pointer coords are full-window, but WindowSize is
	// the content area (it excludes the titlebar).
	edge := resizeEdgeAt(p[0], p[1], float32(logicalW), float32(logicalH))
	if edge == zxdg.ToplevelResizeEdgeNone {
		return false
	}
	xdgToplevel.Resize(seat, serial, edge)
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
