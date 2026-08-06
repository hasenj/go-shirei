package widgets

import (
	"fmt"
	"image"
	"image/color"

	"go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
)

// Default outline accents for ImageWipe (git-style: green = left/new, red = right/old).
var (
	ImageWipeLeftAccent  = Vec4{130, 55, 42, 1} // green
	ImageWipeRightAccent = Vec4{8, 70, 48, 1}   // red
	// ImageWipeDiffHighlight is a suggested high-visibility magenta tint
	// (set alpha yourself; 0 means off when used as DiffHighlightColor).
	ImageWipeDiffHighlight = Vec4{350, 80, 52, 1}
)

const imageWipeDefaultOutline = 6

// ImageWipeAttrs configures ImageWipe.
//
// Layout of the two images:
//   - RightImage is drawn full-frame (the side revealed when the slider is low).
//   - LeftImage is clipped to the left of the split (grows as OutSlider → 1).
//
// At OutSlider 0 the view is all RightImage; at 1 it is all LeftImage.
// Empty LeftLabel / RightLabel omit the corner tags. Zero accent colors use
// ImageWipeLeftAccent / ImageWipeRightAccent. OutlineThickness 0 means the
// default (6); pass a negative value for no outline.
//
// Diff highlight (optional): a full-frame overlay of pixels that differ between
// LeftImage and RightImage, drawn above both images and below the wipe handle
// so it stays visible on both sides of the split. DiffHighlightColor is HSLA;
// its alpha is the overlay opacity (0 = off). Mask is precomputed when the
// source images or tint RGB change; only alpha is applied each frame via Trans.
type ImageWipeAttrs struct {
	LeftImage  ImageId
	RightImage ImageId
	OutSlider  *float32 // 0..1; written while the user drags

	LeftAccentColor  Vec4
	RightAccentColor Vec4
	OutlineThickness float32 // 0 → default 6; <0 → no outline

	LeftLabel  string
	RightLabel string

	// DiffHighlightColor tints differing pixels. Alpha is opacity (0 = no
	// highlight). Zero value means off (no default tint).
	DiffHighlightColor Vec4

	// MaxSize is an optional max box for the wipe plane (same rule as ImageView:
	// never enlarge). Displayed size is RestrictedSize of the image content
	// into this box. Zero width → GetAvailableSize().x (caller should pass a
	// real pane width from an Extrinsic parent). Zero height → no height cap.
	// If neither image has dimensions yet, a small placeholder is used.
	//
	// Callers that pre-bake bitmaps to MaxSize×WindowScale (filling the box)
	// get a 1:1 soft-render path when content size matches that bake.
	MaxSize Vec2
}

// ImageWipe draws a before/after wipe compare with a draggable vertical split.
// Drag anywhere on the image (or the center knob) to set *OutSlider.
func ImageWipe(attrs ImageWipeAttrs) {
	if attrs.OutSlider == nil {
		return
	}
	t := *attrs.OutSlider
	generic.Clamp(0, &t, 1)
	*attrs.OutSlider = t

	leftAccent := AccentOrFallback(attrs.LeftAccentColor, ImageWipeLeftAccent)
	rightAccent := AccentOrFallback(attrs.RightAccentColor, ImageWipeRightAccent)

	outline := attrs.OutlineThickness
	if outline == 0 {
		outline = imageWipeDefaultOutline
	}
	if outline < 0 {
		outline = 0
	}

	// Base = image pixels. Cap = MaxSize (width-only when MaxSize.y == 0).
	maxW, maxH := attrs.MaxSize[0], attrs.MaxSize[1]
	if maxW < 1 {
		if avail := GetAvailableSize(); avail[0] > 1 {
			maxW = avail[0]
		}
	}
	if maxW < 1 {
		maxW = 800
	}

	var viewW, viewH float32
	if content := imageWipeContentSize(attrs.LeftImage, attrs.RightImage); content[0] >= 1 && content[1] >= 1 {
		fit := RestrictedSize(content, Vec2{maxW, maxH})
		viewW, viewH = fit[0], fit[1]
	} else {
		viewW, viewH = 240, 160
	}

	Container(Attrs(
		FixSize(viewW, viewH), Clip, Focusable, NoAnimate,
		Background(0, 0, 88, 1),
	), func() {
		// Whole surface is the hit target (same idea as ProcessSlider).
		PressAction()
		if IsActive() && !GetInputState().MouseFromTouch {
			origin := GetScreenRect().Origin
			x := GetInputState().MousePoint[0] - origin[0]
			nt := x / viewW
			generic.Clamp(0, &nt, 1)
			*attrs.OutSlider = nt
			t = nt
		}
		if IsTouched() {
			ids := TouchingIds(nil)
			if len(ids) > 0 {
				if ti, ok := TouchById(ids[0]); ok {
					origin := GetScreenRect().Origin
					x := ti.Pos[0] - origin[0]
					nt := x / viewW
					generic.Clamp(0, &nt, 1)
					*attrs.OutSlider = nt
					t = nt
				}
			}
		}

		// Bottom: full right (old / base).
		if attrs.RightImage != 0 {
			Container(Attrs(Float(0, 0), FixSize(viewW, viewH), NoAnimate, ClickThrough), func() {
				ImageView(attrs.RightImage, Vec2{viewW, viewH})
			})
		}

		// Top: left image clipped to [0, splitX).
		splitX := viewW * t
		if attrs.LeftImage != 0 && splitX > 0.5 {
			Container(Attrs(Float(0, 0), FixSize(splitX, viewH), Clip, NoAnimate, ClickThrough), func() {
				ImageView(attrs.LeftImage, Vec2{viewW, viewH})
			})
		}

		// Diff highlight: full frame, above both images, below the wipe chrome.
		// Not clipped by the split — marks where left and right disagree.
		// Alpha of DiffHighlightColor is opacity; RGB is the tint.
		hl := attrs.DiffHighlightColor[3]
		generic.Clamp(0, &hl, 1)
		if hl > 0.001 {
			tint := attrs.DiffHighlightColor
			tint[3] = 1 // bake opaque pixels; fade with Trans
			if hid := imageWipeDiffHighlight(attrs.LeftImage, attrs.RightImage, tint); hid != 0 {
				// Trans: 0 opaque, 1 invisible. Opacity hl → Trans(1-hl).
				Container(Attrs(
					Float(0, 0), FixSize(viewW, viewH),
					Trans(1-hl), NoAnimate, ClickThrough,
				), func() {
					ImageView(hid, Vec2{viewW, viewH})
				})
			}
		}

		// Soft grip lane + bar + knob (visual only; drag is on the parent).
		const barW float32 = 3
		const gripW float32 = 28
		barX := splitX - barW/2
		if barX < 0 {
			barX = 0
		}
		if barX > viewW-barW {
			barX = viewW - barW
		}
		Element(Attrs(
			Float(splitX-gripW/2, 0), FixSize(gripW, viewH),
			ClickThrough, NoAnimate,
			Background(0, 0, 0, 0.06),
		))
		Element(Attrs(
			Float(barX, 0), FixSize(barW, viewH),
			ClickThrough, NoAnimate,
			Background(0, 0, 100, 1),
			BorderWidth(1), BorderColor(0, 0, 20, 0.55),
		))
		const knob float32 = 36
		knobY := (viewH - knob) / 2
		Container(Attrs(
			Float(splitX-knob/2, knobY), FixSize(knob, knob),
			Corners(knob/2), ClickThrough, NoAnimate, Center,
			Background(0, 0, 100, 1),
			BorderWidth(1), BorderColor(0, 0, 30, 0.5),
			BoxShadow(1),
		), func() {
			Element(Attrs(Float(knob*0.35, knob*0.28), FixSize(2, knob*0.44),
				Background(0, 0, 35, 1), ClickThrough, NoAnimate))
			Element(Attrs(Float(knob*0.58, knob*0.28), FixSize(2, knob*0.44),
				Background(0, 0, 35, 1), ClickThrough, NoAnimate))
		})

		// Split-colored outline: left of the wipe uses left accent, right uses right.
		if outline > 0 {
			imageWipeOutline(viewW, viewH, splitX, outline, leftAccent, rightAccent)
		}

		// Corner labels (optional).
		pad := outline + 6
		if pad < 8 {
			pad = 8
		}
		if attrs.LeftLabel != "" {
			imageWipeTag(attrs.LeftLabel, pad, pad, leftAccent)
		}
		if attrs.RightLabel != "" {
			// Approximate right-edge placement; tag sizes itself to text.
			imageWipeTagRight(attrs.RightLabel, viewW, pad, rightAccent)
		}
	})
}

// imageWipeContentSize returns the pixel bounding box that covers both sides
// (max width × max height). Zero ImageId or unloaded images contribute nothing.
func imageWipeContentSize(left, right ImageId) Vec2 {
	var w, h float32
	for _, id := range []ImageId{left, right} {
		if id == 0 {
			continue
		}
		img := LookupImage(id)
		if img == nil {
			continue
		}
		iw, ih := float32(img.Config.Width), float32(img.Config.Height)
		if iw > w {
			w = iw
		}
		if ih > h {
			h = ih
		}
	}
	return Vec2{w, h}
}

// imageWipeDiffHighlight builds (or reuses) an image with DiffHighlightColor on
// differing pixels and full transparency elsewhere. Cached by source ids +
// generations + tint so opacity can change every frame without recomputing.
func imageWipeDiffHighlight(left, right ImageId, tint Vec4) ImageId {
	if left == 0 || right == 0 {
		return 0
	}
	la := LookupImage(left)
	ra := LookupImage(right)
	if la == nil || ra == nil || len(la.Pix) == 0 || len(ra.Pix) == 0 {
		return 0
	}
	// Alpha is applied at paint time; cache key is tint RGB only.
	key := fmt.Sprintf("imagewipe-hl/%d@%d/%d@%d/%.0f-%.0f-%.0f",
		left, la.Generation, right, ra.Generation, tint[0], tint[1], tint[2])
	// Fast path: already registered under this key.
	if id := GetImageId(key); id != 0 {
		if LookupImage(id) != nil {
			return id
		}
	}
	mask := buildDiffHighlightRGBA(&la.RGBA, &ra.RGBA, tint)
	if mask == nil {
		return 0
	}
	return UseImage(key, mask)
}

func buildDiffHighlightRGBA(a, b *image.RGBA, tintHSLA Vec4) *image.RGBA {
	if a == nil || b == nil {
		return nil
	}
	w := a.Bounds().Dx()
	h := a.Bounds().Dy()
	if bw := b.Bounds().Dx(); bw > w {
		w = bw
	}
	if bh := b.Bounds().Dy(); bh > h {
		h = bh
	}
	if w < 1 || h < 1 {
		return nil
	}
	nrgba := HSLAColor(tintHSLA)
	tr, tg, tb, ta := nrgba.R, nrgba.G, nrgba.B, nrgba.A
	if ta == 0 {
		ta = 255
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	aw, ah := a.Bounds().Dx(), a.Bounds().Dy()
	bw, bh := b.Bounds().Dx(), b.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			inA := x < aw && y < ah
			inB := x < bw && y < bh
			var ca, cb color.RGBA
			if inA {
				i := a.PixOffset(x, y)
				ca = color.RGBA{a.Pix[i], a.Pix[i+1], a.Pix[i+2], a.Pix[i+3]}
			}
			if inB {
				i := b.PixOffset(x, y)
				cb = color.RGBA{b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3]}
			}
			if !inA || !inB || ca != cb {
				i := out.PixOffset(x, y)
				out.Pix[i+0] = tr
				out.Pix[i+1] = tg
				out.Pix[i+2] = tb
				out.Pix[i+3] = ta
			}
			// else leave transparent zero
		}
	}
	return out
}

func imageWipeOutline(w, h, splitX, th float32, left, right Vec4) {
	// Clamp split into the interior so both colors can show when thickness is large.
	if splitX < 0 {
		splitX = 0
	}
	if splitX > w {
		splitX = w
	}
	// Left edge
	Element(Attrs(Float(0, 0), FixSize(th, h), ClickThrough, NoAnimate, BackgroundVec(left)))
	// Right edge
	Element(Attrs(Float(w-th, 0), FixSize(th, h), ClickThrough, NoAnimate, BackgroundVec(right)))
	// Top: left portion + right portion
	if splitX > 0 {
		lw := splitX
		if lw > w {
			lw = w
		}
		Element(Attrs(Float(0, 0), FixSize(lw, th), ClickThrough, NoAnimate, BackgroundVec(left)))
	}
	if splitX < w {
		Element(Attrs(Float(splitX, 0), FixSize(w-splitX, th), ClickThrough, NoAnimate, BackgroundVec(right)))
	}
	// Bottom
	if splitX > 0 {
		lw := splitX
		if lw > w {
			lw = w
		}
		Element(Attrs(Float(0, h-th), FixSize(lw, th), ClickThrough, NoAnimate, BackgroundVec(left)))
	}
	if splitX < w {
		Element(Attrs(Float(splitX, h-th), FixSize(w-splitX, th), ClickThrough, NoAnimate, BackgroundVec(right)))
	}
}

func imageWipeTag(text string, x, y float32, accent Vec4) {
	bg := accent
	if bg[3] > 0.9 {
		bg[3] = 0.88
	}
	fg := ContrastingTextColor(accent)
	Container(Attrs(
		Float(x, y), Pad2(4, 6), Corners(4), ClickThrough, NoAnimate,
		BackgroundVec(bg),
	), func() {
		Label(text, FontSize(11), FontWeight(WeightBold), TextColorVec(fg))
	})
}

// imageWipeTagRight places a tag whose right edge sits near viewW - pad.
// Uses a float from the left with a temporary measure: we draw at an estimated
// x from the right by building the tag and accepting a slight overshoot when
// the label is very long (caller can shorten). For simplicity we float from
// the right via negative... Shirei Float is top-left based, so estimate width.
func imageWipeTagRight(text string, viewW, pad float32, accent Vec4) {
	// Rough width from rune count; good enough for short corner tags.
	est := float32(len(text))*7 + 16
	x := viewW - pad - est
	if x < pad {
		x = pad
	}
	imageWipeTag(text, x, pad, accent)
}
