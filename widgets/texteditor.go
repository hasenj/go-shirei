package widgets

import (
	"fmt"
	"math"
	"time"

	. "go.hasen.dev/shirei"
)

// TextEditorAttrs configures a document viewport. TextStyle's zero value
// inherits the current style with monospace fonts. Lines scroll horizontally;
// each logical line occupies one row. File I/O and toolbars belong to the app.
type TextEditorAttrs struct {
	TextStyle   TextStyleAttrs
	LineNumbers bool
	// ReadOnly blocks user edits, including undo and IME commits, while keeping
	// selection, copy and navigation. It does not restrict document methods.
	ReadOnly    bool
	NoAutoFocus bool
	// GutterView builds a fixed-width slot before the line numbers for each
	// visible line. Its ordinary UI input is independent of text selection.
	GutterWidth float32
	GutterView  func(line int)
	// LineBackground paints the visible row behind its gutter and text.
	LineBackground func(line int) Vec4
	// LineSpans supplies paint-only spans in document rune coordinates. Only
	// color, background, underline and strike apply; font metrics stay fixed.
	LineSpans func(line int, base TextStyleAttrs) []StyleSpan
	// OnPointer runs over the text with a document position, including hover.
	// Returning true consumes that frame's mouse selection input. The callback
	// must not mutate the document or build UI.
	OnPointer func(pos LinePos) bool
}

type textEditorScroll struct {
	line int
	frac float32
}

// TextEditorScrollToLine reveals a zero-based line without changing selection
// or focus. viewportFrac places it within the viewport (0 = top, .5 = middle);
// a negative value scrolls only enough to make the line visible. Post before
// rendering the editor. The last request wins, keyed by the document pointer.
func TextEditorScrollToLine(document *TextDocument, line int, viewportFrac float32) {
	if document != nil {
		PostCommand("widgets.TextEditor", document, "scroll-to-line", textEditorScroll{line, viewportFrac})
	}
}

// TextEditorState describes this frame's view without exporting the document.
// FirstVisible and LastVisible are inclusive logical line indexes.
type TextEditorState struct {
	HasFocus                  bool
	Changed                   bool
	Scroll                    Vec2
	FirstVisible, LastVisible int
	Caret                     Rect // screen coordinates; may be outside the clipped viewport
}

type textEditorView struct {
	generation, selectionVersion uint64
	started                      bool
	goalX                        float32
	hasGoal                      bool
	maxWidth                     float32
	fontSize                     float32
	blink                        time.Time
	clickCount                   int
	composition                  string
	compositionSel               [2]int
	revealPending                bool
	scrollRequest                *textEditorScroll
}

// TextEditor edits a persistent document in the available space, with line
// numbers and monospace text. Place it under a parent with a height budget.
func TextEditor(document *TextDocument, style ...TextStyleFn) TextEditorState {
	return TextEditorExt(document, TextEditorAttrs{
		TextStyle: TextStyleWith(TextStyle(Fonts(Monospace...)), style...), LineNumbers: true,
	})
}

// TextEditorExt renders a virtualized document surface. It shares keyboard
// commands, cluster-aware geometry, selection paint, and clipboard operations
// with TextInput; only visible lines and navigation targets are shaped.
// Tab inserts a tab character, displayed at four-space stops; Shift+Tab moves
// focus out of the editor.
func TextEditorExt(d *TextDocument, attrs TextEditorAttrs) (out TextEditorState) {
	return textEditor(d, attrs, CurrentColorScheme.TextInput, CurrentColorScheme.ScrollBar, ScrollBars)
}

// TextEditorStyled supplies literal document, selection, caret, and scrollbar
// colors. TextStyle controls fonts and size; style.Text supplies the text ink.
func TextEditorStyled(d *TextDocument, attrs TextEditorAttrs, style TextInputStyle, scrollStyle ScrollBarStyle) TextEditorState {
	if attrs.TextStyle.FontSize == 0 {
		attrs.TextStyle = TextStyle(Fonts(Monospace...))
	}
	attrs.TextStyle.TextColor = style.Text
	return textEditor(d, attrs, style, scrollStyle, scrollBarWithStyle(scrollStyle))
}

func textEditor(d *TextDocument, attrs TextEditorAttrs, colors TextInputStyle, scrollStyle ScrollBarStyle, scrollBar ScrollBarFn) (out TextEditorState) {
	if d == nil {
		return
	}
	style := attrs.TextStyle
	if style.FontSize == 0 {
		style = TextStyle(Fonts(Monospace...))
	}
	style.FontSize *= ComfortScale()
	lineHeight := float32(math.Ceil(float64(max(style.FontSize*1.6, CaretHeightForStyle(style)+4))))
	pad := float32(8)
	customGutter := max(0, attrs.GutterWidth) * ComfortScale()
	gutter := customGutter
	if attrs.LineNumbers {
		gutter += float32(len(fmt.Sprint(d.LineCount()))+2)*style.FontSize*.65 + pad
	}
	ContainerWithKey(d, Attrs(Viewport, Focusable, NoAnimate, BackgroundVec(colors.Background)), func() {
		if !attrs.ReadOnly {
			ModAttrs(AcceptTab)
		}
		NextAccessRole("text")
		NextAccessEditable(!attrs.ReadOnly, true)
		AssignAccess()
		st := Use[textEditorView]("text-editor")
		reveal := st.revealPending || !st.started || st.generation != d.generation || st.selectionVersion != d.selectionVersion
		if !st.started || st.generation != d.generation || st.fontSize != style.FontSize {
			*st = textEditorView{started: true, generation: d.generation, fontSize: style.FontSize, blink: time.Now()}
			SetScrollOffset(Vec2{})
			reveal = true
		}
		if req, ok := TakeCommand[textEditorScroll]("widgets.TextEditor", d, "scroll-to-line"); ok {
			st.scrollRequest = &req
		}
		if !attrs.NoAutoFocus {
			AutoFocus()
		}
		FocusOnClick()
		out.HasFocus = HasFocus()
		if ReceivedFocusNow() {
			st.blink = time.Now()
			d.lastOp = _EditNone
		}
		ScrollOnInput()
		scrollBar()
		ScrollBarStyled(ScrollBarAttrs{Horizontal: true}, scrollStyle)
		scroll := GetScrollOffset()
		size := GetResolvedSize()
		origin := GetScreenRect().Origin
		avail := Vec2{max(1, size[0]-gutter-2*pad-12), max(1, size[1]-2*pad-12)}
		mouse := GetInputState().MousePoint
		inside := mouse[0] >= origin[0]+customGutter && mouse[0] < origin[0]+size[0]-12 && mouse[1] < origin[1]+size[1]-12
		composition := ""
		var compositionSel [2]int
		if out.HasFocus && !attrs.ReadOnly {
			composition = GetInputState().Composition
			compositionSel = GetInputState().CompositionSel
		}
		if st.composition != composition || st.compositionSel != compositionSel {
			reveal = true
			st.blink = time.Now()
		}
		st.composition, st.compositionSel = composition, compositionSel
		shape := func(line int) textLayout {
			caret := 0
			comp := ""
			if line == d.cursor.Line {
				caret = d.cursor.Rune
				comp = composition
			}
			return makeTextLayout(d.Line(line), caret, comp, compositionSel, style, false, 0)
		}
		pointerHandled := false
		if attrs.OnPointer != nil && IsHovered() && inside && mouse[0] >= origin[0]+gutter+pad {
			line := min(max(0, int(math.Floor(float64((mouse[1]-origin[1]-pad+scroll[1])/lineHeight)))), d.LineCount()-1)
			tl := shape(line)
			x := mouse[0] - origin[0] - gutter - pad + scroll[0]
			pointerHandled = attrs.OnPointer(LinePos{line, tl.DisplayToDoc(computeCursorIndexInText(Vec2{x, style.FontSize / 2}, tl.displayShaped))})
		}
		if !pointerHandled && (inside || IsActive()) {
			PressAction()
		}
		revision := d.revision
		// Hit testing uses document coordinates, even outside the viewport while
		// dragging. The same line layout supplies click and keyboard geometry.
		if !pointerHandled && ((IsClicked() && inside) || (IsActive() && st.clickCount <= 1)) {
			dragging := !IsClicked()
			if dragging {
				for axis := range 2 {
					low := origin[axis] + pad
					high := origin[axis] + size[axis] - pad - 12
					if mouse[axis] < low {
						scroll[axis] -= min(24, low-mouse[axis])
					}
					if mouse[axis] > high {
						scroll[axis] += min(24, mouse[axis]-high)
					}
					scroll[axis] = max(0, scroll[axis])
				}
				RequestNextFrame()
			}
			line := min(max(0, int(math.Floor(float64((mouse[1]-origin[1]-pad+scroll[1])/lineHeight)))), d.LineCount()-1)
			tl := shape(line)
			x := mouse[0] - origin[0] - gutter - pad + scroll[0]
			pos := LinePos{line, tl.DisplayToDoc(computeCursorIndexInText(Vec2{x, style.FontSize / 2}, tl.displayShaped))}
			if IsClicked() {
				st.clickCount = GetFrameInput().ClickCount
			}
			switch {
			case !dragging && st.clickCount >= 3:
				end := LinePos{line, len(tl.shaped.Runes)}
				if line+1 < d.LineCount() {
					end = LinePos{line + 1, 0}
				}
				d.SetSelection(LinePos{line, 0}, end)
			case !dragging && st.clickCount == 2:
				d.SetSelection(pos, pos)
				d.apply(_EditCommand{Op: _EditSelectWord, Pos: pos.Rune}, tl.bounds)
			default:
				anchor := pos
				if dragging || GetInputState().Modifiers&ModShift != 0 {
					anchor = d.anchor
				}
				d.SetSelection(anchor, pos)
			}
			st.hasGoal = false
			st.blink = time.Now()
		}
		if out.HasFocus {
			if !attrs.ReadOnly {
				WantKeyboard()
			}
			input := GetFrameInput()
			cmds := decodeEditKeys(input.Key, GetInputState().Modifiers, input.Text, editPrimaryMod(), editKeyOpts{VerticalMotion: true, Newlines: true})
			if input.Key == KeyTab && GetInputState().Modifiers == 0 && !attrs.ReadOnly {
				cmds = append(cmds, _EditCommand{Op: _EditInsert, Text: "\t"})
			}
			if (input.Key == KeyPageUp || input.Key == KeyPageDown) && GetInputState().Modifiers&^ModShift == 0 {
				op := _EditMoveUp
				if input.Key == KeyPageDown {
					op = _EditMoveDown
				}
				for range max(1, int(avail[1]/lineHeight)-1) {
					cmds = append(cmds, _EditCommand{Op: op, Extend: GetInputState().Modifiers&ModShift != 0})
				}
			}
			for _, cmd := range cmds {
				if attrs.ReadOnly {
					switch cmd.Op {
					case _EditInsert, _EditDeleteBackward, _EditDeleteForward, _EditDeleteWordBackward, _EditDeleteWordForward, _EditDeleteToLineStart, _EditDeleteSelection, _EditCut, _EditPaste, _EditUndo, _EditRedo:
						continue
					}
				}
				tl := shape(d.cursor.Line)
				switch cmd.Op {
				case _EditMoveUp, _EditMoveDown:
					if !st.hasGoal {
						st.goalX = tl.DocCaretPos(d.cursor.Rune, caretAffinityDefault)[0]
						st.hasGoal = true
					}
					line := d.cursor.Line - 1
					if cmd.Op == _EditMoveDown {
						line = d.cursor.Line + 1
					}
					line = min(max(0, line), d.LineCount()-1)
					target := shape(line)
					pos := LinePos{line, computeCursorIndexInText(Vec2{st.goalX, style.FontSize / 2}, target.shaped)}
					anchor := pos
					if cmd.Extend {
						anchor = d.anchor
					}
					d.SetSelection(anchor, pos)
				case _EditMoveTo:
					// Decoder MoveTo commands address the document edges.
					pos := LinePos{}
					if cmd.Pos > 0 {
						line := d.LineCount() - 1
						pos = LinePos{line, len([]rune(d.Line(line)))}
					}
					anchor := pos
					if cmd.Extend {
						anchor = d.anchor
					}
					d.SetSelection(anchor, pos)
					st.hasGoal = false
				default:
					r := d.apply(cmd, tl.bounds)
					if r.Copy != "" {
						RequestTextCopy(r.Copy)
					}
					if r.Paste {
						RequestPaste()
					}
					st.hasGoal = false
				}
				if cmd.Op != _EditCopy && cmd.Op != _EditPaste {
					reveal = true
					st.blink = time.Now()
				}
			}
		}
		out.Changed = d.revision != revision
		if out.Changed {
			st.maxWidth = 0
		}
		caretLayout := shape(d.cursor.Line)
		caret := caretLayout.CaretPos(caretLayout.displayCursor, caretAffinityDefault)
		caret[1] = float32(d.cursor.Line) * lineHeight
		st.maxWidth = max(st.maxWidth, caretLayout.ContentSize(style.FontSize)[0]+pad)
		if reveal && size[0] > 0 && size[1] > 0 {
			if caret[0] < scroll[0] {
				scroll[0] = caret[0]
			}
			if caret[0]+2 > scroll[0]+avail[0] {
				scroll[0] = caret[0] + 2 - avail[0]
			}
			if caret[1] < scroll[1] {
				scroll[1] = caret[1]
			}
			if caret[1]+lineHeight > scroll[1]+avail[1] {
				scroll[1] = caret[1] + lineHeight - avail[1]
			}
		}
		if req := st.scrollRequest; req != nil && size[1] > 0 {
			y := float32(min(max(0, req.line), d.LineCount()-1)) * lineHeight
			if req.frac >= 0 {
				scroll[1] = y - min(1, req.frac)*avail[1]
			} else if y < scroll[1] {
				scroll[1] = y
			} else if y+lineHeight > scroll[1]+avail[1] {
				scroll[1] = y + lineHeight - avail[1]
			}
			st.scrollRequest = nil
		}
		scroll[1] = min(max(0, scroll[1]), max(0, float32(d.LineCount())*lineHeight-avail[1]))
		scroll[0] = min(max(0, scroll[0]), max(0, st.maxWidth-avail[0]))
		first := min(d.LineCount()-1, max(0, int(scroll[1]/lineHeight)))
		last := min(d.LineCount()-1, first+int(math.Ceil(float64(size[1]/lineHeight))))
		selection := LineSelection{Anchor: d.anchor, Head: d.cursor}
		from, to := selection.Ordered()
		for line := first; line <= last; line++ {
			tl := shape(line)
			width := tl.ContentSize(style.FontSize)[0]
			st.maxWidth = max(st.maxWidth, width+pad)
			y := pad + float32(line)*lineHeight - scroll[1]
			if attrs.LineBackground != nil {
				Element(Attrs(Float(0, y), FixSize(max(0, size[0]-12), lineHeight), BackgroundVec(attrs.LineBackground(line)), ClickThrough, NoAnimate))
			}
			if attrs.GutterView != nil && customGutter > 0 {
				ContainerWithKey(line, Attrs(Float(0, y), FixSize(customGutter, lineHeight), NoAnimate, Clip), func() {
					attrs.GutterView(line)
				})
			}
			if attrs.LineNumbers {
				muted := style.TextColor
				muted[3] *= .55
				Container(Attrs(Float(customGutter+pad, y), FixSize(gutter-customGutter-pad, lineHeight), Row, MainAlign(AlignEnd), CrossMid, ClickThrough, NoAnimate), func() {
					Label(fmt.Sprint(line+1), FontSize(style.FontSize), Fonts(Monospace...), TextColorVec(muted))
				})
			}
			// A fixed gutter clips horizontally scrolled text independently.
			Container(Attrs(Float(gutter+pad, y), FixSize(max(0, size[0]-gutter-pad-12), lineHeight), Clip, ClickThrough, NoAnimate), func() {
				lo, hi := selection.LineRange(line, len(tl.shaped.Runes))
				if line >= from.Line && line < to.Line && from != to {
					Element(Attrs(Float(width-scroll[0], 0), FixSize(style.FontSize*.6, lineHeight), BackgroundVec(colors.Selection), ClickThrough, NoAnimate))
				}
				Container(Attrs(Float(-scroll[0], 0), UnsetMaxCross, NoAnimate, ClickThrough), func() {
					if tl.composing {
						lo, hi = tl.DocToDisplay(lo), tl.DocToDisplay(hi)
					}
					var spans []StyleSpan
					if attrs.LineSpans != nil {
						for _, sp := range attrs.LineSpans(line, style) {
							paint := style
							paint.TextColor, paint.Background = sp.Style.TextColor, sp.Style.Background
							paint.Underline, paint.Strike = sp.Style.Underline, sp.Style.Strike
							spans = append(spans, StyleSpan{From: tl.DocToDisplay(sp.From), To: tl.DocToDisplay(sp.To), Style: paint})
						}
					}
					ShapedTextLayoutExt(tl.displayShaped, style, ShapedTextLayoutAttrs{
						SelectionFrom: lo, SelectionTo: hi, SelectionColor: colors.Selection, RowHeight: lineHeight,
					}, spans...)
					if tl.composing {
						offset := Vec2{0, -(lineHeight - style.FontSize) / 2}
						drawTextInputUnderline(tl.displayShaped, style.FontSize, offset, tl.compositionFrom, tl.compositionTo, 1, colors.Caret)
						drawTextInputUnderline(tl.displayShaped, style.FontSize, offset, tl.compositionSelFrom, tl.compositionSelTo, 2, colors.Caret)
					}
				})
			})
		}
		SetScrollOffset(scroll)
		Element(Attrs(FixSize(max(size[0], st.maxWidth+gutter+pad*2+12), float32(d.LineCount())*lineHeight+pad*2+12), ClickThrough, NoAnimate))
		pos := Vec2{pad + gutter + caret[0] - scroll[0], pad + caret[1] - scroll[1] + (lineHeight-style.FontSize)/2}
		caretH := CaretHeightForStyle(style)
		out.Caret = Rect{Origin: Vec2Add(origin, pos), Size: Vec2{1, max(style.FontSize, caretH)}}
		if out.HasFocus {
			GetHost().CaretPos = Vec2Add(out.Caret.Origin, Vec2{0, out.Caret.Size[1]})
			GetHost().CaretHeight = out.Caret.Size[1]
			GetHost().CompositionPos = GetHost().CaretPos
			if composition != "" {
				anchor := caretLayout.CaretPos(caretLayout.compositionFrom, caretAffinityDefault)
				GetHost().CompositionPos[0] = origin[0] + pad + gutter + anchor[0] - scroll[0]
			}
			alpha := float32(1)
			if !GetHost().HeadlessRender && time.Since(st.blink) < 5*time.Second {
				RequestNextFrame()
				if int(time.Since(st.blink)/(600*time.Millisecond))%2 == 1 {
					alpha = 0
				}
			}
			if GetHost().WindowFocused && pos[0] >= gutter+pad {
				color := colors.Caret
				color[3] *= alpha
				Element(Attrs(FloatVec(pos), FixSizeVec(out.Caret.Size), BackgroundVec(color), ClickThrough, NoAnimate))
			}
		}
		st.selectionVersion = d.selectionVersion
		st.revealPending = reveal && (size[0] <= 0 || size[1] <= 0)
		out.Scroll = scroll
		out.FirstVisible = first
		out.LastVisible = last
	})
	return
}
