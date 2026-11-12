//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"github.com/jezek/xgb/render"
	"github.com/jezek/xgb/xproto"
)

var argbVisual xproto.Visualid

// detectARGBVisual matches the renderer's premultiplied BGRA bytes to a native
// visual whose Render picture format explicitly carries an eight-bit alpha.
func detectARGBVisual() xproto.Visualid {
	if render.Init(X) != nil {
		return 0
	}
	formats, err := render.QueryPictFormats(X).Reply()
	if err != nil {
		return 0
	}
	for _, depth := range screen.AllowedDepths {
		if depth.Depth != 32 {
			continue
		}
		for _, visual := range depth.Visuals {
			if visual.Class != xproto.VisualClassTrueColor || visual.RedMask != 0xff0000 || visual.GreenMask != 0xff00 || visual.BlueMask != 0xff {
				continue
			}
			for _, scr := range formats.Screens {
				for _, d := range scr.Depths {
					for _, v := range d.Visuals {
						if v.Visual != visual.VisualId {
							continue
						}
						for _, f := range formats.Formats {
							if f.Id == v.Format && f.Type == render.PictTypeDirect && f.Depth == 32 && f.Direct.AlphaShift == 24 && f.Direct.AlphaMask == 255 {
								return visual.VisualId
							}
						}
					}
				}
			}
		}
	}
	return 0
}
