package widgets

import (
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
)

// focused input state!
type TextInputState struct {
	start   time.Time
	cursor  int
	cursor2 int
}

var activeInput TextInputState

func (s *TextInputState) Range(runes []rune) (int, int) {
	var from = s.cursor2
	var to = s.cursor
	if to < from {
		to, from = from, to
	}
	from = max(0, from)
	to = min(to, len(runes))
	return from, to
}

// offset should be 0 or -1
func (s *TextInputState) delete(buf *string, offset int) {
	runes := []rune(*buf)
	var delFrom = s.cursor2
	var delTo = s.cursor
	if delTo < delFrom {
		delTo, delFrom = delFrom, delTo
	}
	if delFrom == delTo {
		delFrom += offset
		delTo = delFrom + 1
	}
	delFrom = max(0, delFrom)
	delTo = min(delTo, len(runes))
	count := delTo - delFrom
	if count > 0 {
		g.RemoveAt(&runes, delFrom, count)
		*buf = string(runes)
		s.cursor = max(0, min(delFrom, len(runes)))
		s.cursor2 = s.cursor
		s.start = time.Now()
	}
}

func (s *TextInputState) insert(buf *string, text string) {
	if s.cursor != s.cursor2 {
		s.delete(buf, 0)
	}
	runes := []rune(*buf)
	newRunes := []rune(text)
	if s.cursor > len(runes) {
		s.cursor = len(runes)
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
	g.InsertAt(&runes, s.cursor, newRunes...)
	*buf = string(runes)
	s.cursor += len(newRunes)
	s.cursor2 = s.cursor
	s.start = time.Now()
}

func computeCursorPos(cursor int, text ShapedText) Vec2 {
	// for now just a linear scan
	// should be fine for small text
	var pos Vec2
	for idx, line := range text.Lines {
		pos[0] = 0
		// look for glyph with cluster == curspr
		for _, segment := range line.Segments {
			for _, g := range segment.Glyphs {
				if g.Cluster == int32(cursor) {
					// if the segment direction is RTL, the cursor should be placed to the right of the character
					if segment.Dir == RTL {
						pos[0] += g.XAdvance
						return pos
					} else {
						return pos
					}
				}
				pos[0] += g.XAdvance
			}
		}
		if idx < len(text.Lines)-1 {
			pos[1] += line.Height
		}
	}
	return pos
}

func computeCursorIndex(contentRect Rect, pos Vec2, shaped ShapedText) int {
	if len(shaped.Runes) == 0 {
		return 0
	}
	// for now just a linear scan
	pos = Vec2Sub(pos, contentRect.Origin)

	// "clamp" position to the edges of the box if outside so we don't worry
	// about edge cases
	g.Clamp(0, &pos[0], contentRect.Size[0])
	g.Clamp(0, &pos[1], contentRect.Size[1])

	// pass 1: find the line worth searching
	// it must be the first line we fine whose bottom is below the mouse cursor
	// and if we don't find any, then it's the last line!
	var line *ShapedTextLine
	var y float32
	for i := range shaped.Lines {
		line = &shaped.Lines[i]
		if y+line.Height >= pos[1] {
			break
		}
		y += line.Height
	}

	// clamp to the line itself this time!
	// FIXME I think we also need to consider alignment?
	// if alignment setting pushes the line to the left side, we need to apply the offset to the cursor position to!
	g.Clamp(0, &pos[0], line.Width-0.1)

	// NOTE the rules here are still wip
	// pass 2: find the glyph
	// use the half point and switch on the segment direction
	//     LTR segment -> cursor in left half of box
	//     RTL segment -> cursor in right half of box (wip)
	var x float32
	var glyph *Glyph
	for segmentIndex := range line.Segments {
		segment := &line.Segments[segmentIndex]
		for glyphIndex := range segment.Glyphs {
			glyph = &segment.Glyphs[glyphIndex]
			if x+glyph.XAdvance >= pos[0] {
				// mouse pointer is inside this glyph; let's figure out which side it is
				leftSide := x+(glyph.XAdvance/2) > pos[0]
				switch segment.Dir {
				case LTR:
					if leftSide {
						return int(glyph.Cluster)
					} else {
						return int(glyph.Cluster) + 1
					}
				case RTL:
					if leftSide {
						return int(glyph.Cluster) + 1
					} else {
						return int(glyph.Cluster)
					}
				}
			}
			centerX := x + (glyph.XAdvance / 2)
			if centerX > pos[0] && y > pos[1] {
				break
			}
			x += glyph.XAdvance
		}
	}
	g.Assert(false, "glyph not resolved after loop")
	return 0
}

func EditorSetCursor(editorId any, cursor int) {
	if IdHasFocus(editorId) {
		activeInput.cursor = cursor
		activeInput.cursor2 = cursor
	}
}

func TextInput(buf *string) {
	TextInputExt(buf, DefaultTextInputAttrs())
}

func PasswordInput(buf *string) {
	attrs := DefaultTextInputAttrs()
	attrs.Masked = true
	TextInputExt(buf, attrs)
}

type TextInputAttrs struct {
	FontSize float32
	Padding  Vec4
	MinWidth float32
	MaxWidth float32

	Masked bool
}

func DefaultTextInputAttrs() (out TextInputAttrs) {
	out.FontSize = DefaultTextSize
	out.Padding = N4(out.FontSize / 2)
	return out
}

func TextInputExt(buf *string, attrs TextInputAttrs) {
	var padSize = PadSize(attrs.Padding)
	var inputContainerAttrs = Attrs{
		Focusable:  true,
		Corners:    N4(2),
		Background: Vec4{0, 0, 90, 1},
		Gradient:   Vec4{0, 0, 4, 0},
		Padding:    attrs.Padding,
		MinSize:    Vec2{padSize[0] + attrs.FontSize*10, attrs.FontSize + padSize[1]},
		Border: Border{
			BorderWidth: 1,
			BorderColor: Vec4{0, 0, 50, 1},
		},
	}
	var inputTextAttrs = DefaultTextAttrs()
	inputTextAttrs.Size = attrs.FontSize
	inputTextAttrs.Color = Vec4{0, 0, 0, 1}

	Layout(inputContainerAttrs, func() {
		var size = GetResolvedSize()
		if size == (Vec2{}) {
			size = inputContainerAttrs.MinSize
		}

		var shaped ShapedText
		if attrs.Masked {
			var masked = strings.Repeat("•", utf8.RuneCountInString(*buf))
			shaped = ShapeText(masked, inputTextAttrs)
		} else {
			shaped = ShapeText(*buf, inputTextAttrs)
		}

		var selectionFrom = 0
		var selectionTo = 0

		AutoFocus()
		FocusOnClick()
		CycleFocusOnTab()

		PressAction()

		if ReceivedFocusNow() {
			g.Reset(&activeInput)
			activeInput.start = time.Now()
		}

		var shift = slices.Contains(InputState.DownKeys, KeyShift)
		// DebugVar("mouse shift:", shift)

		contentRect := GetContentRect()

		// mouse selection
		// first clicked!
		if IsClicked() {
			activeInput.cursor = computeCursorIndex(contentRect, InputState.MousePoint, shaped)
			if !shift || ReceivedFocusNow() {
				activeInput.cursor2 = activeInput.cursor
			}
			activeInput.start = time.Now()
		} else if IsActive() {
			// mouse is moving!
			activeInput.cursor = computeCursorIndex(contentRect, InputState.MousePoint, shaped)
			activeInput.start = time.Now()
		}

		if HasFocus() {
			ModAttrs(BG(0, 0, 91, 1), Grad(0, 0, 4, 0), Bo(0, 0, 30, 1))

			// DebugVar("cursor1", activeInput.cursor)
			// DebugVar("cursor2", activeInput.cursor2)

			var ctrl = ModCtrl
			if runtime.GOOS == "darwin" {
				ctrl = ModCmd
			}

			var paste = Combo(KeyV, ctrl)
			var copy = Combo(KeyC, ctrl)
			var cut = Combo(KeyX, ctrl)
			var selAll = Combo(KeyA, ctrl)

			// Modifiers flag is not set unless another regular key is pressed, so we have to use this trick!
			// TODO: always use this and eschew modifier flags?
			var shift = InputState.Modifiers&ModShift != 0

			switch ActiveCombo() {
			case paste:
				shirei.RequestPaste()
			case copy:
				from, to := activeInput.Range(shaped.Runes)
				if from != to {
					shirei.RequestTextCopy(string(shaped.Runes[from:to]))
				}
			case cut:
				// FIXME unify cutting and deleting into the same funciton, with flags to control which ops are performed
				from, to := activeInput.Range(shaped.Runes)
				if from != to {
					shirei.RequestTextCopy(string(shaped.Runes[from:to]))
				}
				activeInput.delete(buf, 0)
			case selAll:
				activeInput.cursor2 = 0
				activeInput.cursor = len(shaped.Runes)
			}

			switch FrameInput.Key {
			case KeyLeft:
				activeInput.cursor = max(0, activeInput.cursor-1)
				if !shift {
					activeInput.cursor2 = activeInput.cursor
				}
				activeInput.start = time.Now()

			case KeyRight:
				activeInput.cursor = min(activeInput.cursor+1, utf8.RuneCountInString(*buf))
				if !shift {
					activeInput.cursor2 = activeInput.cursor
				}
				activeInput.start = time.Now()

			case KeyDeleteBackward:
				activeInput.delete(buf, -1)

			case KeyDeleteForward:
				activeInput.delete(buf, 0)
			}

			if FrameInput.Text != "" {
				activeInput.insert(buf, FrameInput.Text)
			}

			selectionFrom = activeInput.cursor2
			selectionTo = activeInput.cursor
			if selectionFrom > selectionTo {
				selectionFrom, selectionTo = selectionTo, selectionFrom
			}
		}

		// top shadow!
		Element(TW(NoAnimate, Float(0, 0), FixSize(size[0], 4), BG(0, 0, 20, 0.5), Grad(0, 0, 0, -0.5)))

		ShapedTextLayout(shaped, inputTextAttrs, selectionFrom, selectionTo)

		if HasFocus() {
			RequestNextFrame()

			var alpha float32 = 1
			var dur = time.Since(activeInput.start)
			var slot = int(dur / (time.Millisecond * 600))
			if slot%2 == 1 {
				alpha = 0
			}
			var rd = GetRenderData()
			var pos = computeCursorPos(activeInput.cursor, shaped)
			pos[0] += rd.Padding[PAD_LEFT]
			pos[1] += rd.Padding[PAD_RIGHT]
			Layout(TW(MinSize(1, inputTextAttrs.Size), BG(0, 0, 30, alpha), FloatV(pos)), func() {
				r := GetScreenRect()
				shirei.CaretPos = Vec2Add(r.Origin, Vec2{0, r.Size[1]})
			})
		}
	})
}
